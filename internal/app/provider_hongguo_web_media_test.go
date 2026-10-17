package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math/bits"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func hongguoTestContentKey(key []byte, tag string) string {
	plain := []byte("0" + hex.EncodeToString(key))
	raw := make([]byte, len(plain)+len(tag)+1)
	even, odd := byte(250), byte(85)
	for index, current := range plain {
		delta := 21 + bits.OnesCount(uint(index))
		if tag == "22" || tag == "webdevice" {
			delta = -delta
		}
		previous := even
		if index%2 != 0 {
			previous = odd
		}
		raw[index+1] = byte(int(current)+delta) ^ previous
		if index%2 == 0 {
			even = raw[index+1]
		} else {
			odd = raw[index+1]
		}
	}
	raw[0] = byte(len(tag)+48) ^ raw[1] ^ raw[2]
	seed := raw[len(plain)-1] ^ raw[len(plain)]
	for index := range tag {
		raw[len(plain)+1+index] = tag[index] ^ seed
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func TestHongguoAppAndWebContentKeys(t *testing.T) {
	key, _ := hex.DecodeString("00112233445566778899aabbccddeeff")
	for _, tag := range []string{"11", "22", "appdevice", "webdevice"} {
		decoded, err := hongguoContentKey(hongguoTestContentKey(key, tag))
		if err != nil || !bytes.Equal(key, decoded) {
			t.Fatalf("%s: key did not round trip: %v", tag, err)
		}
	}
	if _, err := hongguoContentKey(hongguoTestContentKey(key, "unknown")); err == nil {
		t.Fatal("accepted unknown key format")
	}
}

func protectedPlaybackFixture(now time.Time) map[string]any {
	key := hongguoTestContentKey(bytes.Repeat([]byte{0x37}, 16), "22")
	track := func(address string) map[string]any {
		return map[string]any{"main_url": address, "format": "dash", "kid": strings.Repeat("a", 32),
			"player_access_key": key, "expires_at": strconv.FormatInt(now.Add(time.Minute).Unix(), 10), "duration": 113.0}
	}
	video := track("https://video.example.test/video.mp4")
	video["audio"], video["vid"], video["codec_type"], video["definition"] = track("https://video.example.test/audio.mp4"), "media-id", "h264", "720p"
	return map[string]any{"base_resp": map[string]any{"StatusCode": 0}, "play_info": video}
}

func TestHongguoProtectedPlaybackValidation(t *testing.T) {
	now := time.Now()
	media, err := parseHongguoProtectedMedia(protectedPlaybackFixture(now), "https://hongguoduanju.com/", now)
	if err != nil || media.AudioURL == "" || len(media.CENCKey) != 16 || media.Quality != 720 || media.Duration != 113*time.Second {
		t.Fatalf("valid split media rejected: %v", err)
	}
	for _, test := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"unauthorized", func(v map[string]any) { v["base_resp"] = map[string]any{"StatusCode": 403} }},
		{"missing audio", func(v map[string]any) { delete(nestedMap(v, "play_info"), "audio") }},
		{"different audio key", func(v map[string]any) { nestedMap(v, "play_info", "audio")["player_access_key"] = "different" }},
		{"expired audio", func(v map[string]any) { nestedMap(v, "play_info", "audio")["expires_at"] = now.Unix() }},
		{"bad audio URL", func(v map[string]any) { nestedMap(v, "play_info", "audio")["main_url"] = "file:///tmp/test" }},
		{"bad kid", func(v map[string]any) { nestedMap(v, "play_info")["kid"] = "invalid" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := protectedPlaybackFixture(now)
			test.edit(fixture)
			if _, err := parseHongguoProtectedMedia(fixture, "https://hongguoduanju.com/", now); err == nil {
				t.Fatal("accepted invalid protected media")
			}
		})
	}
}

func TestHongguoProtectedPlaybackKeepsEpisodeIdentity(t *testing.T) {
	posts := 0
	wrongEpisode := true
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPost {
			posts++
			var payload map[string]string
			json.NewDecoder(request.Body).Decode(&payload)
			if request.URL.Path != "/api/video/protected/play-info" || payload["series_id"] != "700001" || payload["item_id"] != "800001" || payload["definition"] != "720p" {
				t.Error("wrong playback request")
			}
			body, _ := json.Marshal(protectedPlaybackFixture(time.Now()))
			return rankingHTTPResponse(request, 200, string(body)), nil
		}
		video := "800001"
		if wrongEpisode {
			video = "800002"
		}
		body, _ := json.Marshal(map[string]any{"loaderData": map[string]any{"player_page": map[string]any{"series_id": "700001", "vid": video, "protectedPlayback": true}}})
		return rankingHTTPResponse(request, 200, "<script>window._ROUTER_DATA="+string(body)+"</script>"), nil
	})
	if _, err := d.resolveHongguoWebMedia(context.Background(), "700001", "800001"); err == nil || posts != 0 {
		t.Fatal("requested media for a mismatched page")
	}
	wrongEpisode = false
	if _, err := d.resolveHongguoWebMedia(context.Background(), "700001", "800001"); err != nil || posts != 1 {
		t.Fatalf("matching episode failed: %v", err)
	}
}

func TestLiveHongguoProtectedPlayback(t *testing.T) {
	if os.Getenv("DUANJU_LIVE_PLAYBACK") != "1" {
		t.Skip("仅在手动验收站源播放时启用")
	}
	cfg := defaultConfig()
	cfg.dataDir, cfg.OutputDir, cfg.Retries = t.TempDir(), t.TempDir(), 1
	d := NewDownloader(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	media, err := d.resolveHongguoWebMedia(ctx, "7695332165143252030", "7695350266568117272")
	if err != nil {
		t.Fatal(publicError(err))
	}
	proxy, err := d.newHLSProxy(ctx, media, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	ffmpeg, err := d.ensureFFmpeg(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, offset := range []float64{0, 62} {
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
		t.Logf("decoded from %.0fs with audio and video", offset)
	}
	t.Logf("decoded real episode: quality=%dp duration=%.3fs separateAudio=%t", media.Quality, media.Duration.Seconds(), media.AudioURL != "")
}
