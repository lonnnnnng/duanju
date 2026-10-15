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

func TestHongguoPreloadRequestsExactEpisode(t *testing.T) {
	const episode = "7695350271685168152"
	model := map[string]any{"video_duration": 40.868, "video_list": []any{map[string]any{
		"main_url": "https://media.example.test/requested.mp4", "video_meta": map[string]any{"codec_type": "h264", "definition": "1080p"},
	}}}
	encoded, _ := json.Marshal(model)
	for _, format := range []string{"object", "string"} {
		t.Run(format, func(t *testing.T) {
			d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
				var payload struct {
					IDs   map[string][]string `json:"mixed_video_id_map"`
					Scene string              `json:"dr_scene"`
					Biz   struct {
						Platform int  `json:"video_platform"`
						All      bool `json:"need_all_video_definition"`
					} `json:"biz_param"`
				}
				if request.Method != http.MethodPost || request.URL.Path != "/novel/player/multi_video_model/preload/v1/" || json.NewDecoder(request.Body).Decode(&payload) != nil {
					t.Fatal("incorrect preload request")
				}
				ids := payload.IDs["1004"]
				if len(payload.IDs) != 1 || len(ids) != 1 || ids[0] != episode || payload.Scene != "preload" || payload.Biz.Platform != 1024 || !payload.Biz.All {
					t.Fatal("preload request lost episode or required parameters")
				}
				if request.Header.Get("X-Gorgon") == "" || request.Header.Get("X-SS-STUB") == "" {
					t.Fatal("preload request was not signed")
				}
				var value any = model
				if format == "string" {
					value = string(encoded)
				}
				body, _ := json.Marshal(map[string]any{"code": 0, "data": map[string]any{
					"800002": map[string]any{"video_model": map[string]any{}}, episode: map[string]any{"video_model": value},
				}})
				return rankingHTTPResponse(request, http.StatusOK, string(body)), nil
			})
			media, err := d.resolveHongguoAppMedia(context.Background(), episode)
			if err != nil || media.URL != "https://media.example.test/requested.mp4" || media.Quality != 1080 || media.Duration != 40868*time.Millisecond {
				t.Fatalf("preload did not select requested media: %v", err)
			}
		})
	}
	for _, data := range []map[string]any{
		{"800002": map[string]any{"video_model": model}},
		{"video_model": model},
		{episode: map[string]any{"video_model": string(encoded) + " {}"}},
		{episode: map[string]any{"video_model": "null"}},
	} {
		if _, err := parseHongguoAppPreloadMedia(map[string]any{"data": data}, episode); err == nil {
			t.Fatal("accepted mismatched episode or malformed model")
		}
	}
}

func TestLiveHongguoPreloadPlayback(t *testing.T) {
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
