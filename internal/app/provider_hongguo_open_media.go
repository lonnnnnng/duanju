package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 红果开放接口取流（与安卓端同一条链路）。
//
// 2026-11-06 起 `/novel/player/multi_video_model/v1/` 的签名 POST 恒返回 200 + 0 字节，
// 而 `/reading/bookapi/video_tab/multi_video_model/v/` 携带旧版全套签名
// （X-Gorgon/X-Argus/X-Ladon）时可以正常返回，并且只有 version_code=74232 会给 HEVC。
const hongguoOpenMediaBaseURL = "https://api5-normal-sinfonlinec.fqnovel.com"

func (downloader *Downloader) resolveHongguoOpenMedia(ctx context.Context, videoID string) (providerMedia, error) {
	client := downloader.hongguoClient()
	query := url.Values{
		"iid": {client.state.InstallID}, "device_id": {client.state.DeviceID},
		"ac": {"wifi"}, "channel": {"hongguo_baidu_pz_kaiping0105_android"},
		"aid": {"8662"}, "app_name": {"novelread"},
		"version_code": {"74232"}, "version_name": {"7.4.2.32"},
		"manifest_version_code": {"74232"}, "update_version_code": {"74232"},
		"device_platform": {"android"}, "os": {"android"}, "ssmix": {"a"},
		"device_type": {"Redmi Note 8 Pro"}, "device_brand": {"Redmi"},
		"language": {"zh"}, "os_api": {"36"}, "os_version": {"16"},
		"resolution": {"1080*2198"}, "dpi": {"440"}, "gender": {"2"},
		"need_personal_recommend": {"1"}, "player_so_load": {"1"}, "is_android_pad_screen": {"0"},
		"host_abi": {"arm64-v8a"}, "dragon_device_type": {"phone"},
		"rom_version": {"BP4A.251205.006 release-keys"}, "compliance_status": {"0"},
		"cdid":     {"1f1d14b3-8c25-4733-9f6b-2fdc01640081"},
		"video_id": {videoID}, "video_platform": {"1024"}, "need_all_video_definition": {"1"},
	}
	address := hongguoOpenMediaBaseURL + "/reading/bookapi/video_tab/multi_video_model/v/?" + query.Encode()

	// 该接口会间歇返回 200 + 0 字节（上游限流），必须重试；空正文不算致命错误。
	attempts := downloader.cfg.Retries
	if attempts < 3 {
		attempts = 3
	}
	if attempts > 4 {
		attempts = 4
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(time.Duration(attempt) * time.Second):
			case <-ctx.Done():
				return providerMedia{}, ctx.Err()
			}
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			return providerMedia{}, err
		}
		request.Header.Set("User-Agent", hongguoAppUserAgent)
		request.Header.Set("Accept", "application/json")
		nonce, err := newHongguoCommentNonce()
		if err != nil {
			return providerMedia{}, errors.New("无法准备请求签名")
		}
		started := time.Now()
		response, err := downloader.doPreparedCatalogRequest(request, 20*time.Second, func(prepared *http.Request) {
			signHongguoCommentRequest(prepared, nonce, time.Now())
		})
		if err != nil {
			downloader.hostHealth.report(address, false, 0)
			lastErr = err
			if ctx.Err() != nil {
				return providerMedia{}, err
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
			downloader.hostHealth.report(address, false, 0)
			lastErr = fmt.Errorf("开放取流接口 HTTP %d", response.StatusCode)
			if response.StatusCode >= 400 && response.StatusCode < 500 {
				return providerMedia{}, lastErr
			}
			continue
		}
		if len(content) == 0 {
			downloader.hostHealth.report(address, false, 0)
			lastErr = errors.New("开放取流接口返回空正文（HTTP 200）")
			continue
		}
		if len(content) > providerMaxBodyBytes {
			return providerMedia{}, errors.New("开放取流接口响应超出大小限制")
		}
		downloader.hostHealth.report(address, true, time.Since(started))
		var result map[string]any
		decoder := json.NewDecoder(strings.NewReader(string(content)))
		decoder.UseNumber()
		if err := decoder.Decode(&result); err != nil || result == nil {
			lastErr = errors.New("开放取流接口返回格式异常")
			continue
		}
		if code := mapString(result, "code"); code != "" && code != "0" {
			lastErr = fmt.Errorf("开放取流接口暂不可用（%s）", truncate(code, 20))
			continue
		}
		return parseHongguoOpenMedia(result, videoID)
	}
	if lastErr == nil {
		lastErr = errors.New("开放取流接口未返回数据")
	}
	return providerMedia{}, lastErr
}

func parseHongguoOpenMedia(result map[string]any, videoID string) (providerMedia, error) {
	entry := nestedMap(result, "data", "video_model_data", videoID)
	if len(entry) == 0 {
		return providerMedia{}, errors.New("开放取流接口未返回所请求分集")
	}
	model, _ := entry["video_model"].(map[string]any)
	if encoded, ok := entry["video_model"].(string); ok {
		decoder := json.NewDecoder(strings.NewReader(encoded))
		decoder.UseNumber()
		if err := decoder.Decode(&model); err != nil {
			return providerMedia{}, errors.New("开放取流接口播放信息格式异常")
		}
	}
	if len(model) == 0 {
		return providerMedia{}, errors.New("开放取流接口缺少播放信息")
	}
	return selectHongguoAppMedia(model)
}
