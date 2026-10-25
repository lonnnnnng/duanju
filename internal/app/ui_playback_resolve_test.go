package app

import (
	"encoding/hex"
	"net/http"
	"testing"
	"time"
)

func TestBuildPlaybackNativeMedia(t *testing.T) {
	key := make([]byte, 16)
	for index := range key {
		key[index] = byte(index + 1)
	}
	media := providerMedia{
		URL: "https://cdn.example.test/1080.mp4", CENCKey: key, Quality: 1080,
		Duration: 90 * time.Second, Referer: "https://novel.snssdk.com/",
		Variants: []providerMedia{
			{URL: "https://cdn.example.test/720.mp4", CENCKey: key, Quality: 720},
			{URL: "https://cdn.example.test/1080.mp4", CENCKey: key, Quality: 1080},
			{URL: "https://cdn.example.test/nokey.mp4", Quality: 360},
		},
	}
	result, err := buildPlaybackNativeMedia(media, "700001", 3, "标题")
	if err != nil {
		t.Fatal(err)
	}
	if result.SeriesID != "700001" || result.Episode != 3 || result.Title != "标题" {
		t.Fatal("metadata lost")
	}
	if result.Key != hex.EncodeToString(key) || result.URL != media.URL || result.Quality != 1080 || result.Duration != 90 || result.Referer != "https://novel.snssdk.com/" {
		t.Fatalf("unexpected primary media: %+v", result)
	}
	if len(result.Variants) != 2 {
		t.Fatalf("expected deduplicated variants with keys, got %d", len(result.Variants))
	}
	for _, variant := range result.Variants {
		if variant.URL == "" || variant.Key == "" {
			t.Fatal("variant missing url or key")
		}
	}
	if _, err := buildPlaybackNativeMedia(providerMedia{URL: "https://cdn.example.test/nokey.mp4", Quality: 1080}, "700001", 1, ""); err == nil {
		t.Fatal("accepted media without a content key")
	}
}

func TestPlaybackResolveValidation(t *testing.T) {
	app := &UIApp{cfg: Config{dataDir: t.TempDir()}, playbacks: map[string]*playbackSession{}}
	result := historyJSONRequest(t, app, app.handlePlaybackResolve, "/api/ui/playback/native", map[string]any{"dramaId": "hongguo:700001", "episode": 0})
	if result.Code != http.StatusBadRequest {
		t.Fatalf("episode 0 not rejected: %d %s", result.Code, result.Body.String())
	}
	result = historyJSONRequest(t, app, app.handlePlaybackResolve, "/api/ui/playback/native", map[string]any{"dramaId": "other:700001", "episode": 1})
	if result.Code != http.StatusBadRequest {
		t.Fatalf("non-hongguo id not rejected: %d %s", result.Code, result.Body.String())
	}
}
