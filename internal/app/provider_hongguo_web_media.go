package app

import (
	"bytes"
	"context"
	"crypto/aes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func (d *Downloader) resolveHongguoProtectedMedia(ctx context.Context, seriesID, videoID string) (providerMedia, error) {
	quality, _ := ctx.Value(playbackQualityKey{}).(int)
	quality = preferredDownloadQuality(quality, []int{360, 480, 720})
	payload, _ := json.Marshal(map[string]string{"series_id": seriesID, "item_id": videoID, "definition": strconv.Itoa(quality) + "p"})
	// long: 使用已成功返回该分集页面的域名，避免自定义站点或域名迁移时把页面与播放请求发往不同服务。
	address := d.providerURLCandidates(hongguoBaseURL + "/api/video/protected/play-info")[0]
	origin, err := url.Parse(address)
	if err != nil {
		return providerMedia{}, errors.New("网页播放接口地址无效")
	}
	referer := origin.Scheme + "://" + origin.Host + "/player/" + seriesID + "/" + videoID
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(payload))
	if err != nil {
		return providerMedia{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Referer", referer)
	request.Header.Set("Origin", origin.Scheme+"://"+origin.Host)
	request.Header.Set("X-Xs-From-Web", "1")
	response, err := d.doCatalogRequestWithTimeout(request, 20*time.Second)
	if err != nil {
		return providerMedia{}, err
	}
	defer response.Body.Close()
	const maxResponse = 256 * 1024
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	if err != nil {
		return providerMedia{}, errors.New("读取网页播放信息失败")
	}
	if response.StatusCode != http.StatusOK {
		return providerMedia{}, fmt.Errorf("网页播放接口 HTTP %d", response.StatusCode)
	}
	if len(body) == 0 || len(body) > maxResponse {
		return providerMedia{}, errors.New("网页播放接口返回空数据或超出大小限制")
	}
	var result map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil || decoder.Decode(new(any)) != io.EOF {
		return providerMedia{}, errors.New("网页播放信息格式异常")
	}
	media, err := parseHongguoProtectedMedia(result, referer, time.Now())
	level, message := "info", "网页播放信息已解析，音视频分离="+strconv.FormatBool(media.AudioURL != "")
	if err != nil {
		level, message = "warn", err.Error()
	}
	// long: 只记录请求结果和媒体结构，不把临时播放地址、密钥或响应正文写进诊断日志。
	d.recordDiagnostic(diagnosticEvent{Level: level, Event: "provider.hongguo.web_playback", Source: sourceHongguo,
		DramaID: providerDramaID(sourceHongguo, seriesID), HTTPStatus: response.StatusCode, Host: origin.Hostname(),
		Quality: media.Quality, Message: message})
	return media, err
}

func parseHongguoProtectedMedia(result map[string]any, referer string, now time.Time) (providerMedia, error) {
	code := mapString(nestedMap(result, "base_resp"), "StatusCode")
	if code != "0" {
		return providerMedia{}, errors.New("网页未授权播放该集，请在站点确认是否仍可访问")
	}
	info := nestedMap(result, "play_info")
	legacy := nestedMap(result, "legacy_play_info")
	if len(legacy) > 0 && len(info) > 0 {
		return providerMedia{}, errors.New("网页播放信息包含冲突的媒体格式")
	}
	if len(legacy) > 0 {
		info = legacy
	}
	duration, err := strconv.ParseFloat(mapString(info, "duration"), 64)
	if err != nil || math.IsNaN(duration) || math.IsInf(duration, 0) || duration <= 0 || duration > 24*60*60 || !hongguoWebMediaURL(mapString(info, "main_url")) {
		return providerMedia{}, errors.New("网页未返回有效媒体地址或时长")
	}
	media := providerMedia{URL: mapString(info, "main_url"), Referer: referer, Duration: time.Duration(duration * float64(time.Second))}
	if len(legacy) > 0 {
		return media, nil
	}
	definition := mapString(info, "definition")
	if definition != "360p" && definition != "480p" && definition != "720p" {
		return providerMedia{}, errors.New("网页返回了不支持的清晰度")
	}
	media.Quality, _ = strconv.Atoi(strings.TrimSuffix(definition, "p"))
	audio := nestedMap(info, "audio")
	// long: play_info.vid 是媒体 ID，不能与页面的分集 item_id 比较；分集身份由匹配后的页面和本次 POST 参数共同确定。
	if mapString(info, "vid") == "" || mapString(info, "codec_type") != "h264" || mapString(info, "format") != "dash" || mapString(audio, "format") != "dash" || !hongguoWebMediaURL(mapString(audio, "main_url")) {
		return providerMedia{}, errors.New("网页未返回完整的 DASH 音视频")
	}
	for _, track := range []map[string]any{info, audio} {
		expires, err := strconv.ParseInt(mapString(track, "expires_at"), 10, 64)
		if err != nil || expires <= now.Unix()+5 {
			return providerMedia{}, errors.New("网页播放地址已过期，请重试播放")
		}
		kid, err := hex.DecodeString(mapString(track, "kid"))
		if err != nil || len(kid) != aes.BlockSize {
			return providerMedia{}, errors.New("网页媒体密钥标识无效")
		}
	}
	key := mapString(info, "player_access_key")
	if key == "" || key != mapString(audio, "player_access_key") {
		return providerMedia{}, errors.New("网页音视频密钥不一致")
	}
	media.CENCKey, err = hongguoContentKey(key)
	if err != nil {
		return providerMedia{}, fmt.Errorf("网页媒体密钥不可用：%w", err)
	}
	media.AudioURL = mapString(audio, "main_url")
	media.Variants = []providerMedia{media}
	return media, nil
}

func hongguoWebMediaURL(raw string) bool {
	if len(raw) > 8192 {
		return false
	}
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Hostname() != "" && parsed.Port() == "" && parsed.User == nil
}
