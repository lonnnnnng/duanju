package app

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func hongguoPlaybackResponseFixture(episode, codec string) string {
	return fmt.Sprintf(`{"code":0,"data":{"1004":{"%s":{"video_model":{"video_list":[
		{"main_url":"https://media.example.test/1080.mp4","video_meta":{"codec_type":%q,"definition":"1080p"}},
		{"main_url":"https://media.example.test/720.mp4","video_meta":{"codec_type":%q,"definition":"720p"}}
	]}}}}}`, episode, codec, codec)
}

func TestHongguoPlaybackFallbackPreservesEpisodeAndQuality(t *testing.T) {
	const episode = "7708355462780947518"
	var paths []string
	var bodies [][]byte
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		paths = append(paths, request.URL.Path)
		if request.URL.Path == hongguoVideoTabPath {
			query := request.URL.Query()
			if request.Method != http.MethodGet || request.URL.Host != "api5-normal-sinfonlineb.fqnovel.com" ||
				query.Get("video_id") != episode || query.Get("video_id_type") != "0" || query.Get("device_level") != "1" ||
				query.Get("version_code") != "73532" || query.Get("manifest_version_code") != "73532" || query.Get("channel") != "update_64" {
				t.Fatal("video_tab did not use the upstream host, query, or app version")
			}
			if request.Header.Get("X-Medusa") == "" || request.Header.Get("X-Helios") == "" || request.Header.Get("X-SS-STUB") != "" {
				t.Fatal("video_tab V5 GET signature does not match the empty-body request")
			}
			return rankingHTTPResponse(request, 200, ""), nil
		}
		if request.URL.Query().Get("device_id") != hongguoPlaybackOfficialDeviceID {
			t.Fatal("fallback should try the same device before rotating")
		}
		if request.Header.Get("X-Medusa") == "" || request.Header.Get("X-SS-STUB") == "" {
			t.Fatal("fallback lost V5 signature or body digest")
		}
		query := request.URL.Query()
		if query.Get("version_code") != "74332" || query.Get("version_name") != "7.4.3.32" ||
			query.Get("manifest_version_code") != query.Get("version_code") || query.Get("update_version_code") != query.Get("version_code") {
			t.Fatal("playback version fields are incomplete or inconsistent")
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, body)
		digest := md5.Sum(body)
		if request.Header.Get("X-SS-STUB") != hexUpper(digest[:]) {
			t.Fatal("signature does not cover the exact request body")
		}
		var payload struct {
			IDs map[string][]string `json:"mixed_video_id_map"`
			Biz map[string]any      `json:"biz_param"`
		}
		if json.Unmarshal(body, &payload) != nil || !slices.Equal(payload.IDs["1004"], []string{episode}) || payload.Biz["need_all_video_definition"] != true {
			t.Fatal("fallback changed the episode or discarded full quality variants")
		}
		if request.URL.Path == hongguoPlaybackPath {
			if request.Header.Get("X-Helios") == "" {
				t.Fatal("legacy player signature must retain Helios")
			}
			return rankingHTTPResponse(request, 200, ""), nil
		}
		if request.URL.Path != hongguoPlaybackFallbackPath {
			t.Fatal("unexpected fallback endpoint", request.URL.Path)
		}
		if request.Header.Get("X-Helios") != "" {
			// long: 模拟当前服务端对批量入口旧签名头组合返回空正文，修复前无法取得媒体。
			return rankingHTTPResponse(request, 200, ""), nil
		}
		return rankingHTTPResponse(request, 200, hongguoPlaybackResponseFixture(episode, "hevc")), nil
	})
	d.cfg.Retries = 3
	media, err := d.resolveHongguoAppMedia(context.Background(), episode)
	if err != nil || media.Quality != 1080 || len(media.Variants) != 2 {
		t.Fatalf("fallback lost full quality media: quality=%d variants=%d error=%v", media.Quality, len(media.Variants), err)
	}
	if !slices.Equal(paths, []string{hongguoVideoTabPath, hongguoPlaybackPath, hongguoPlaybackFallbackPath}) {
		t.Fatal("empty response should switch endpoints immediately", paths)
	}
	if len(bodies) != 2 || !bytes.Equal(bodies[0], bodies[1]) {
		t.Fatal("fallback changed the signed request body")
	}
}

func TestHongguoVideoTabParsesRequestedEpisodeBeforePlayerFallback(t *testing.T) {
	const episode = "7708355462780947518"
	model := map[string]any{"video_duration": 40.0, "video_list": []any{map[string]any{
		"main_url": "https://media.example.test/video-tab.mp4", "video_meta": map[string]any{"codec_type": "h264", "definition": "1080p"},
	}}}
	body, _ := json.Marshal(map[string]any{"code": 0, "data": map[string]any{
		"video_model_data": map[string]any{episode: map[string]any{"video_model": model}},
	}})
	calls := 0
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		calls++
		if request.URL.Path != hongguoVideoTabPath {
			t.Fatalf("video_tab success must not call legacy player endpoint: %s", request.URL.Path)
		}
		return rankingHTTPResponse(request, 200, string(body)), nil
	})
	media, err := d.resolveHongguoAppMedia(context.Background(), episode)
	if err != nil || media.URL != "https://media.example.test/video-tab.mp4" || media.Quality != 1080 || calls != 1 {
		t.Fatalf("video_tab did not resolve exact episode: media=%+v calls=%d error=%v", media, calls, err)
	}
}

func TestHongguoPlaybackFallbackDoesNotMaskOtherFailures(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
	}{
		{"forbidden", "", 403},
		{"rate limited", "", 429},
		{"business error", `{"code":110001,"data":{"video_model":{}}}`, 200},
		{"null", `null`, 200},
		{"malformed", `{"code":0} {}`, 200},
		{"oversized", strings.Repeat(" ", providerMaxBodyBytes+1), 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
				calls++
				if request.URL.Path == hongguoVideoTabPath {
					return rankingHTTPResponse(request, 200, ""), nil
				}
				return rankingHTTPResponse(request, test.status, test.body), nil
			})
			_, err := d.resolveHongguoAppMedia(context.Background(), "7708355462780947518")
			if err == nil || errors.Is(err, errHongguoEmptyPlaybackBody) || calls != 2 {
				t.Fatalf("failure was retried as an empty response: calls=%d error=%v", calls, err)
			}
		})
	}
}

func TestHongguoPlaybackDoesNotRevisitFailedDevices(t *testing.T) {
	const episode = "7708355462780947518"
	for _, poolSize := range []int{2, 3} {
		t.Run(fmt.Sprint(poolSize), func(t *testing.T) {
			var used []string
			registrations := 0
			d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
				if request.URL.Path == hongguoVideoTabPath {
					return rankingHTTPResponse(request, 200, ""), nil
				}
				if request.URL.Path == "/service/2/device_register/" {
					registrations++
					if !slices.Equal(used, []string{hongguoPlaybackOfficialDeviceID, "8001", "8002"}) {
						t.Fatal("registered before exhausting existing devices", used)
					}
					return rankingHTTPResponse(request, 200, `{"device_id_str":"8003","install_id_str":"9003"}`), nil
				}
				id := request.URL.Query().Get("device_id")
				used = append(used, id)
				codec := "bytevc2"
				if id == "8003" {
					codec = "hevc"
				}
				return rankingHTTPResponse(request, 200, hongguoPlaybackResponseFixture(episode, codec)), nil
			})
			client := d.hongguoClient()
			client.state.DeviceID, client.state.InstallID, client.state.Registered = "8001", "9001", true
			client.state.Devices = nil
			for i := 1; i <= poolSize; i++ {
				client.state.Devices = append(client.state.Devices, hongguoDevice{DeviceID: fmt.Sprint(8000 + i), InstallID: fmt.Sprint(9000 + i)})
			}
			media, err := d.resolveHongguoAppMedia(context.Background(), episode)
			if err != nil || media.Quality != 1080 || registrations != 3-poolSize || !slices.Equal(used, []string{hongguoPlaybackOfficialDeviceID, "8001", "8002", "8003"}) {
				t.Fatalf("device retry failed: used=%v registrations=%d error=%v", used, registrations, err)
			}
		})
	}
}

func TestHongguoPlaybackAllEmptyAttemptsAreBounded(t *testing.T) {
	var used []string
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == hongguoVideoTabPath {
			return rankingHTTPResponse(request, 200, ""), nil
		}
		if request.URL.Path != hongguoPlaybackPath && request.URL.Path != hongguoPlaybackFallbackPath {
			t.Fatal("attempted registration after exhausting the rotation budget")
		}
		used = append(used, request.URL.Query().Get("device_id"))
		return rankingHTTPResponse(request, 200, ""), nil
	})
	client := d.hongguoClient()
	client.state.DeviceID, client.state.InstallID = "8001", "9001"
	client.state.Devices = []hongguoDevice{{"8001", "9001"}, {"8002", "9002"}, {"8003", "9003"}}
	d.cfg.Retries = 3
	_, err := d.resolveHongguoAppMedia(context.Background(), "7708355462780947518")
	want := []string{hongguoPlaybackOfficialDeviceID, hongguoPlaybackOfficialDeviceID, "8001", "8001", "8002", "8002", "8003", "8003"}
	if !errors.Is(err, errHongguoEmptyPlaybackBody) || !slices.Equal(used, want) {
		t.Fatalf("unexpected attempts=%v error=%v", used, err)
	}
}

func TestHongguoPlaybackSignsAfterQueueWait(t *testing.T) {
	var sentAt time.Time
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		sentAt = time.Now()
		ticket, _ := strconv.ParseInt(request.URL.Query().Get("_rticket"), 10, 64)
		if sentAt.UnixMilli()-ticket > 100 {
			t.Error("signature aged while waiting in the request queue")
		}
		return rankingHTTPResponse(request, 200, `{"code":0}`), nil
	})
	d.limiter = newRequestLimiter(1, time.Nanosecond)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	blocking, _ := http.NewRequestWithContext(ctx, http.MethodGet, hongguoPlaybackBaseURL, nil)
	release, err := d.limiter.acquire(ctx, blocking)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := d.hongguoPlaybackV5Request(ctx, []byte(`{}`), "8001", "9001")
		done <- err
	}()
	// ticket 分支最多向未来偏移 1023ms，等待超过此范围以识别排队前签名。
	time.Sleep(1200 * time.Millisecond)
	release()
	if err := <-done; err != nil || sentAt.IsZero() {
		t.Fatal("queued request failed", err)
	}
}

func TestHongguoPlaybackCancellationStopsFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		calls++
		cancel()
		return rankingHTTPResponse(request, 200, ""), nil
	})
	if _, err := d.resolveHongguoAppMedia(ctx, "7708355462780947518"); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal("cancelled playback attempted more requests", calls, err)
	}
}
