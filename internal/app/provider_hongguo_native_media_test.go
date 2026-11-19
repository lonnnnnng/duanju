package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHongguoModelRequestsExactEpisode(t *testing.T) {
	const episode = "7695350271685168152"
	model := map[string]any{"video_duration": 40.868, "video_list": []any{map[string]any{
		"main_url": "https://media.example.test/requested.mp4", "video_meta": map[string]any{"codec_type": "h264", "definition": "1080p"},
	}}}
	encoded, _ := json.Marshal(model)
	for _, format := range []string{"object", "string"} {
		t.Run(format, func(t *testing.T) {
			d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
				var payload struct {
					Biz   map[string]any `json:"biz_param"`
					Map   map[string]any `json:"mixed_video_id_map"`
					Scene string         `json:"dr_scene"`
				}
				if request.Method != http.MethodPost || request.URL.Path != "/novel/player/multi_video_model/player/v1/" || json.NewDecoder(request.Body).Decode(&payload) != nil {
					t.Fatal("incorrect model request")
				}
				ids, _ := payload.Map["1004"].([]any)
				if len(ids) != 1 || ids[0] != episode || payload.Scene != "player" || payload.Biz["video_platform"] != float64(1024) || payload.Biz["device_level"] != float64(1) {
					t.Fatal("model request lost episode or required parameters")
				}
				if request.URL.Query().Get("device_id") != hongguoPlaybackOfficialDeviceID || request.URL.Query().Get("iid") != hongguoPlaybackOfficialInstallID {
					t.Fatal("first model request must retain the verified official device identity")
				}
				if request.Header.Get("X-Helios") == "" || request.Header.Get("X-Medusa") == "" || request.Header.Get("X-SS-STUB") == "" {
					t.Fatal("model request was not V5 signed")
				}
				var value any = model
				if format == "string" {
					value = string(encoded)
				}
				body, _ := json.Marshal(map[string]any{"code": 0, "data": map[string]any{"1004": map[string]any{
					episode: map[string]any{"video_model": value}}}})
				return rankingHTTPResponse(request, http.StatusOK, string(body)), nil
			})
			media, err := d.resolveHongguoAppMedia(context.Background(), episode)
			if err != nil || media.URL != "https://media.example.test/requested.mp4" || media.Quality != 1080 || media.Duration != 40868*time.Millisecond {
				t.Fatalf("model did not select requested media: %v", err)
			}
		})
	}
	for _, data := range []map[string]any{
		{"800002": map[string]any{"video_model": model}},
		{episode: map[string]any{"video_model": string(encoded) + " {}"}},
		{episode: map[string]any{"video_model": "null"}},
		{"video_model": "not-json"},
	} {
		if _, err := parseHongguoAppPreloadMediaOrdered(map[string]any{"data": data}, episode, nil); err == nil {
			t.Fatal("accepted mismatched episode or malformed model")
		}
	}
}

func TestHongguoModelRetriesIncompatibleMediaWithAuxiliaryDevices(t *testing.T) {
	const episode = "7704572665582849049"
	const firstID, firstInstall = "8000000000000000001", "8000000000000000011"
	const secondID, secondInstall = "8000000000000000002", "8000000000000012"
	var used []string
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		deviceID := request.URL.Query().Get("device_id")
		used = append(used, deviceID)
		codec := "bytevc2"
		if deviceID == secondID {
			codec = "h264"
		}
		model := map[string]any{"video_duration": 40.0, "video_list": []any{map[string]any{
			"main_url": "https://media.example.test/retry.mp4", "video_meta": map[string]any{"codec_type": codec, "definition": "1080p"},
		}}}
		body, _ := json.Marshal(map[string]any{"code": 0, "data": map[string]any{"1004": map[string]any{
			episode: map[string]any{"video_model": model}}}})
		return rankingHTTPResponse(request, http.StatusOK, string(body)), nil
	})
	client := d.hongguoClient()
	client.mu.Lock()
	client.state.DeviceID, client.state.InstallID, client.state.Registered = firstID, firstInstall, true
	client.state.Devices = []hongguoDevice{{DeviceID: firstID, InstallID: firstInstall}, {DeviceID: secondID, InstallID: secondInstall}}
	client.mu.Unlock()

	media, err := d.resolveHongguoAppMedia(context.Background(), episode)
	if err != nil || media.URL != "https://media.example.test/retry.mp4" || media.Quality != 1080 {
		t.Fatalf("auxiliary device should provide compatible media: media=%+v error=%v", media, err)
	}
	want := []string{hongguoPlaybackOfficialDeviceID, firstID, secondID}
	if len(used) != len(want) {
		t.Fatalf("unexpected device retry count: %v", used)
	}
	for i := range want {
		if used[i] != want[i] {
			t.Fatalf("device retry order = %v, want %v", used, want)
		}
	}
	if deviceID, _, _ := d.hongguoDevice(); deviceID != secondID {
		t.Fatal("successful auxiliary device should remain active in the device pool")
	}
}

func TestHongguoModelDoesNotRetryMalformedMedia(t *testing.T) {
	const episode = "7704572665582849049"
	var requests int
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		requests++
		body := fmt.Sprintf(`{"code":0,"data":{"1004":{"%s":{"video_model":"not-json"}}}}`, episode)
		return rankingHTTPResponse(request, http.StatusOK, body), nil
	})
	client := d.hongguoClient()
	client.mu.Lock()
	client.state.DeviceID, client.state.InstallID, client.state.Registered = "8000000000000000001", "8000000000000000011", true
	client.state.Devices = []hongguoDevice{{DeviceID: client.state.DeviceID, InstallID: client.state.InstallID}}
	client.mu.Unlock()

	if _, err := d.resolveHongguoAppMedia(context.Background(), episode); err == nil {
		t.Fatal("malformed media should fail")
	}
	if requests != 1 {
		t.Fatalf("malformed media must not trigger an auxiliary retry, got %d requests", requests)
	}
}

func TestHongguoModelRetriesEmptyBodyWithAuxiliaryDevice(t *testing.T) {
	const episode = "7704572437853113406"
	const deviceID, installID = "8000000000000000001", "8000000000000011"
	var used []string
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		current := request.URL.Query().Get("device_id")
		used = append(used, current)
		if current == hongguoPlaybackOfficialDeviceID {
			return rankingHTTPResponse(request, http.StatusOK, ""), nil
		}
		model := map[string]any{"video_duration": 40.0, "video_list": []any{map[string]any{
			"main_url": "https://media.example.test/empty-body-retry.mp4", "video_meta": map[string]any{"codec_type": "h264", "definition": "1080p"},
		}}}
		body, _ := json.Marshal(map[string]any{"code": 0, "data": map[string]any{"1004": map[string]any{
			episode: map[string]any{"video_model": model}}}})
		return rankingHTTPResponse(request, http.StatusOK, string(body)), nil
	})
	client := d.hongguoClient()
	client.mu.Lock()
	client.state.DeviceID, client.state.InstallID, client.state.Registered = deviceID, installID, true
	client.state.Devices = []hongguoDevice{{DeviceID: deviceID, InstallID: installID}}
	client.mu.Unlock()

	media, err := d.resolveHongguoAppMedia(context.Background(), episode)
	if err != nil || media.URL != "https://media.example.test/empty-body-retry.mp4" {
		t.Fatalf("empty primary response should fall back to auxiliary media: media=%+v error=%v", media, err)
	}
	want := []string{hongguoPlaybackOfficialDeviceID, deviceID}
	if len(used) != len(want) || used[0] != want[0] || used[1] != want[1] {
		t.Fatalf("empty body did not rotate to the registered auxiliary device: %v", used)
	}
}

func TestLiveHongguoModelPlayback(t *testing.T) {
	if os.Getenv("DUANJU_LIVE_PLAYBACK") != "1" {
		t.Skip("仅在手动验收站源播放时启用")
	}
	cfg := defaultConfig()
	cfg.dataDir, cfg.OutputDir, cfg.Retries = t.TempDir(), t.TempDir(), 1
	d := NewDownloader(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	const series = "7695332165143252030"
	_, chapters, err := d.fetchHongguoChapters(ctx, series)
	if err != nil || len(chapters) != 78 {
		t.Fatalf("episode list: count=%d error=%v", len(chapters), publicError(err))
	}
	ffmpeg, err := d.ensureFFmpeg(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// long: 抽查前段、原失败分集、中间与结尾，避免仅恢复网页试看集却误报整条取流链路可用。
	for _, episode := range []int{1, 4, 40, 78} {
		t.Run(fmt.Sprintf("episode%d", episode), func(t *testing.T) {
			task := Task{DramaID: "hongguo:" + series, Index: episode, Chapter: chapters[episode-1], OutPath: filepath.Join(t.TempDir(), "episode.mp4")}
			media, err := d.resolveHongguoAppMedia(ctx, strings.TrimPrefix(task.Chapter.VideoURL, "hongguo-cenc://"))
			if err != nil {
				t.Fatal(publicError(err))
			}
			proxy, err := d.newHLSProxy(ctx, media, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer proxy.Close()
			for _, offset := range []float64{0, media.Duration.Seconds() / 2} {
				args := append([]string{"-xerror"}, playbackInputArgs(proxy.inputMedia(media), proxy.root, offset)...)
				args = append(args, "-t", "3", "-f", "null", "-")
				command := ffmpegMediaCommand(ctx, ffmpeg, args...)
				log := &playbackLog{text: cappedStringWriter{limit: 64 * 1024}, ready: make(chan struct{})}
				command.Stderr = log
				if err := command.Run(); err != nil {
					t.Fatal(playbackStreamError(ctx, proxy, log, media, err, nil))
				}
				_, detail := log.snapshot()
				if !strings.Contains(detail, "Video:") || !strings.Contains(detail, "Audio:") {
					t.Fatal("missing decoded audio or video")
				}
			}
			if episode == 4 {
				if err := d.downloadMediaWithProgress(ctx, task, nil); err != nil {
					t.Fatal(publicError(err))
				}
				info, err := probeMergeMedia(ctx, ffmpeg, task.OutPath)
				if err != nil || info.audioCodec == "" {
					t.Fatalf("download validation: %v", err)
				}
				t.Log("complete episode download contains audio and video")
			}
			t.Logf("decoded episode=%d quality=%dp duration=%.3fs at start and midpoint", episode, media.Quality, media.Duration.Seconds())
		})
	}
}

func TestLiveHongguoAuxiliaryDeviceDownload(t *testing.T) {
	if os.Getenv("DUANJU_LIVE_AUXILIARY_DOWNLOAD") != "1" {
		t.Skip("仅在手动验收辅助设备重试及原失败分集时启用")
	}
	const seriesID = "7704572437853113406"
	cfg := defaultConfig()
	cfg.dataDir, cfg.OutputDir, cfg.Retries = t.TempDir(), t.TempDir(), 1
	d := NewDownloader(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	_, chapters, err := d.fetchHongguoChapters(ctx, seriesID)
	if err != nil || len(chapters) < 491 {
		t.Fatalf("原失败分集列表不可用：chapters=%d error=%v", len(chapters), publicError(err))
	}
	t.Logf("第491集媒体 ID：%s", strings.TrimPrefix(chapters[490].VideoURL, "hongguo-cenc://"))
	task := Task{
		DramaID: "hongguo:" + seriesID, Index: 491, Total: len(chapters),
		Chapter: chapters[490],
		OutPath: filepath.Join(t.TempDir(), "episode-491.mp4"),
	}
	if err := d.DownloadEpisode(ctx, task); err != nil {
		t.Fatal(publicError(err))
	}
	if _, _, registered := d.hongguoDevice(); !registered {
		t.Fatal("原失败分集应通过注册的辅助设备取得兼容媒体")
	}
	ffmpeg, err := d.ensureFFmpeg(ctx)
	if err != nil {
		t.Fatal(err)
	}
	info, err := probeMergeMedia(ctx, ffmpeg, task.OutPath)
	if err != nil || info.videoCodec == "" || info.audioCodec == "" {
		t.Fatalf("下载文件音视频校验失败: %v", err)
	}
}
