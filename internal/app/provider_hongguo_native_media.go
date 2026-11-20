package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// long: 播放接口只接受被服务端信任的设备身份；这里设备级复用同机官方红果 App 的身份。
const (
	hongguoPlaybackOfficialDeviceID  = "3471634475833546"
	hongguoPlaybackOfficialInstallID = "1342981674659067"
)

var (
	errHongguoNoCompatibleMedia = errors.New(" App 未返回兼容的媒体，已跳过不支持的编码")
	errHongguoEmptyPlaybackBody = errors.New(" App 接口返回空正文（HTTP 200）")
)

func (downloader *Downloader) resolveHongguoAppMedia(ctx context.Context, videoID string) (providerMedia, error) {
	if !hongguoNumericID.MatchString(videoID) {
		return providerMedia{}, errors.New("视频 ID 无效")
	}
	payload := map[string]any{
		"biz_param": map[string]any{
			"detail_page_version": 0, "device_level": 1, "disable_digg_stat": false,
			"disable_video_relate_book": false, "need_all_video_definition": true,
			"need_mp4_align": false, "use_os_player": false, "use_server_dns": false,
			"video_platform": 1024,
		},
		"mixed_video_id_map": map[string]any{"1004": []string{videoID}},
		"dr_scene":           "player",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return providerMedia{}, err
	}
	deviceID, installID := hongguoPlaybackOfficialDeviceID, hongguoPlaybackOfficialInstallID
	rejected := make(map[string]bool)
	for rotations := 0; ; {
		result, err := downloader.hongguoPlaybackV5Request(ctx, body, deviceID, installID)
		if ctx.Err() != nil {
			return providerMedia{}, ctx.Err()
		}
		if err != nil && !errors.Is(err, errHongguoEmptyPlaybackBody) {
			return providerMedia{}, err
		}
		mediaErr := err
		var media providerMedia
		if err == nil {
			data := nestedMap(result, "data")
			if data == nil {
				return providerMedia{}, errors.New(" App 未返回所请求分集的播放信息")
			}
			bucket := data
			if nested := nestedMap(data, "1004"); len(nested) > 0 {
				bucket = nested
			}
			media, mediaErr = parseHongguoAppPreloadMediaOrdered(map[string]any{"data": bucket}, videoID, downloader.hostHealth)
		}
		if !errors.Is(mediaErr, errHongguoNoCompatibleMedia) && !errors.Is(mediaErr, errHongguoEmptyPlaybackBody) {
			return media, mediaErr
		}
		if rotations >= hongguoDeviceRotations {
			return providerMedia{}, mediaErr
		}
		rejected[deviceID] = true
		device, switchErr := downloader.ensureHongguoDevice(ctx, rejected)
		if switchErr != nil {
			return providerMedia{}, fmt.Errorf("%w（辅助设备切换失败：%v）", mediaErr, switchErr)
		}
		deviceID, installID = device.DeviceID, device.InstallID
		rotations++
	}
}

func (downloader *Downloader) hongguoPlaybackV5Request(ctx context.Context, body []byte, deviceID, installID string) (map[string]any, error) {
	result, err := downloader.hongguoPlaybackV5RequestPath(ctx, body, deviceID, installID, hongguoPlaybackPath)
	if !errors.Is(err, errHongguoEmptyPlaybackBody) || ctx.Err() != nil {
		return result, err
	}
	// 同一 App 的批量入口仍接受 V5 签名，返回完整清晰度与加密信息。
	// 主入口空响应时先换入口，再换设备；不回退到只有低清晰度的网页接口。
	downloader.recordDiagnostic(diagnosticEvent{Level: "warn", Event: "provider.hongguo.playback_fallback", Source: sourceHongguo,
		Message: "播放主入口返回空正文，尝试 App 批量取流入口"})
	return downloader.hongguoPlaybackV5RequestPath(ctx, body, deviceID, installID, hongguoPlaybackFallbackPath)
}

func (downloader *Downloader) hongguoPlaybackV5RequestPath(ctx context.Context, body []byte, deviceID, installID, path string) (map[string]any, error) {
	address := hongguoPlaybackBaseURL + path
	attempts := downloader.cfg.Retries
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 3 {
		attempts = 3
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(time.Duration(attempt) * time.Second):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		query := hongguoPlaybackQuery(deviceID, installID)
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		request.Header.Set("User-Agent", hongguoPlaybackUserAgent)
		request.Header.Set("Accept", "application/json")
		response, err := downloader.doPreparedCatalogRequest(request, 20*time.Second, func(prepared *http.Request) error {
			// 限流队列可能等待较久，必须在真正发送前生成时间戳和签名。
			signedURL, headers, err := hongguoV5Sign(address, query, body, time.Now())
			if err != nil {
				return err
			}
			prepared.URL, err = url.Parse(signedURL)
			if err != nil {
				return err
			}
			for name, value := range headers {
				prepared.Header.Set(name, value)
			}
			return nil
		})
		if err != nil {
			lastErr = err
			var backoff *requestBackoff
			if errors.As(err, &backoff) || ctx.Err() != nil {
				return nil, err
			}
			continue
		}
		content, readErr := io.ReadAll(io.LimitReader(response.Body, providerMaxBodyBytes+1))
		response.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if response.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf(" App 接口 HTTP %d", response.StatusCode)
			if response.StatusCode >= 400 && response.StatusCode < 500 {
				return nil, lastErr
			}
			continue
		}
		if len(content) == 0 {
			downloader.recordDiagnostic(diagnosticEvent{Level: "warn", Event: "provider.hongguo.playback_response", Source: sourceHongguo,
				Host: request.URL.Hostname(), HTTPStatus: response.StatusCode, Attempt: attempt + 1, Message: path + ":" + errHongguoEmptyPlaybackBody.Error()})
			return nil, errHongguoEmptyPlaybackBody
		}
		if len(content) > providerMaxBodyBytes {
			return nil, errors.New(" App 接口响应超出大小限制")
		}
		decoder := json.NewDecoder(bytes.NewReader(content))
		decoder.UseNumber()
		var result map[string]any
		if err := decoder.Decode(&result); err != nil || result == nil || decoder.Decode(new(any)) != io.EOF {
			lastErr = errors.New(" App 接口返回格式异常")
			continue
		}
		code := firstNonEmpty(mapString(result, "code", "Code", "status_code"), mapString(nestedMap(result, "BaseResp"), "StatusCode"))
		if code != "" && code != "0" {
			return nil, fmt.Errorf(" App 接口暂不可用（%s）", truncate(code, 20))
		}
		return result, nil
	}
	if lastErr == nil {
		lastErr = errors.New(" App 接口未返回有效数据")
	}
	return nil, lastErr
}

func parseHongguoAppPreloadMediaOrdered(result map[string]any, videoID string, health *providerHostHealth) (providerMedia, error) {
	if nestedMap(result, "data") == nil {
		return providerMedia{}, errors.New(" App 未返回所请求分集的播放信息")
	}
	data := nestedMap(result, "data")
	if entry := nestedMap(data, videoID); len(entry) > 0 {
		data = entry
	}
	model, _ := data["video_model"].(map[string]any)
	if encoded, ok := data["video_model"].(string); ok && encoded != "" {
		if decoded, err := decodeHongguoBase64(encoded); err == nil && len(decoded) > 0 {
			encoded = string(decoded)
		}
		decoder := json.NewDecoder(strings.NewReader(encoded))
		decoder.UseNumber()
		if err := decoder.Decode(&model); err != nil || decoder.Decode(new(any)) != io.EOF {
			return providerMedia{}, errors.New(" App 播放信息格式异常")
		}
	}
	if len(model) == 0 {
		if rows := anyList(data["video_list"]); len(rows) > 0 {
			model = map[string]any{
				"video_list":     data["video_list"],
				"video_duration": data["video_duration"],
				"duration":       data["duration"],
			}
		}
	}
	return selectHongguoAppMediaOrdered(model, health)
}

func selectHongguoAppMediaOrdered(model map[string]any, health *providerHostHealth) (providerMedia, error) {
	variants := anyList(model["video_list"])
	if rows, ok := model["video_list"].(map[string]any); ok && len(variants) == 0 {
		keys := make([]string, 0, len(rows))
		for key := range rows {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			variants = append(variants, rows[key])
		}
	}
	duration, _ := strconv.ParseFloat(mapString(model, "video_duration", "duration"), 64)
	var selected providerMedia
	bestQuality := -1
	var keyErr error
	choices := make(map[int]providerMedia)
	scores := make(map[int]int)
	for _, row := range variants {
		variant, _ := row.(map[string]any)
		meta := nestedMap(variant, "video_meta")
		codec := strings.ToLower(mapString(meta, "codec_type"))
		if codec == "bytevc2" || strings.Contains(strings.ToLower(mapString(variant, "gear_des_key")), "bytevc2") {
			continue
		}
		// long: 同一清晰度可能有多个备用 CDN 地址，按历史健康度排序，失败域名靠后。
		addresses := hongguoMediaAddresses(variant)
		if health != nil {
			addresses = health.orderMediaAddresses(addresses)
		}
		if len(addresses) == 0 {
			continue
		}
		media := providerMedia{URL: addresses[0], Referer: "https://novel.snssdk.com/", Duration: time.Duration(duration * float64(time.Second))}
		encryption := nestedMap(variant, "encrypt_info")
		spade := mapString(encryption, "spade_a")
		if spade != "" || encryption["encrypt"] == true || mapString(encryption, "encryption_method") == "cenc-aes-ctr" {
			var err error
			media.CENCKey, err = hongguoContentKey(spade)
			if err != nil {
				keyErr = err
				continue
			}
		}
		height, _ := strconv.Atoi(mapString(meta, "vheight"))
		if definition, err := strconv.Atoi(hongguoQualityNumber.FindString(mapString(meta, "definition"))); err == nil && definition > 0 {
			height = definition
		} else if width, _ := strconv.Atoi(mapString(meta, "vwidth")); width > 0 && (height == 0 || width < height) {
			height = width
		}
		media.Quality = height
		quality := height * 10
		if codec == "h264" || codec == "avc1" {
			quality++
		}
		if previous, exists := scores[height]; !exists || quality > previous {
			choices[height], scores[height] = media, quality
		}
		if selected.URL == "" || quality > bestQuality {
			selected, bestQuality = media, quality
		}
	}
	if selected.URL != "" {
		for _, media := range choices {
			selected.Variants = append(selected.Variants, media)
		}
		sort.Slice(selected.Variants, func(i, j int) bool { return selected.Variants[i].Quality > selected.Variants[j].Quality })
		return selected, nil
	}
	if keyErr != nil {
		return providerMedia{}, fmt.Errorf(" App 媒体密钥不可用: %w", keyErr)
	}
	return providerMedia{}, errHongguoNoCompatibleMedia
}

// hongguoMediaAddresses 汇总一个清晰度档位的所有可用地址；部分响应会把地址做 base64 编码。
func hongguoMediaAddresses(info map[string]any) []string {
	var addresses []string
	seen := map[string]bool{}
	var add func(any)
	add = func(value any) {
		switch value := value.(type) {
		case string:
			address := strings.TrimSpace(value)
			if len(address) > 8192 {
				return
			}
			if !isProviderHTTPMediaURL(address) {
				decoded, err := decodeHongguoBase64(address)
				if err != nil {
					return
				}
				address = strings.TrimSpace(string(decoded))
			}
			if isProviderHTTPMediaURL(address) && !seen[address] {
				seen[address] = true
				addresses = append(addresses, address)
			}
		case []any:
			for _, item := range value {
				add(item)
			}
		}
	}
	for _, key := range []string{"main_url", "backup_url", "backup_url_1", "backup_url_2", "backup_urls", "url_list"} {
		add(info[key])
	}
	return addresses
}
