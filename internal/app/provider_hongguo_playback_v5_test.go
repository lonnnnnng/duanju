package app

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"testing"
	"time"
)

func TestHongguoV5HashVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/v5/signing-v5-f13.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		TS     uint32 `json:"ts"`
		Query  string `json:"query"`
		Body   string `json:"body"`
		Digest string `json:"digest"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 32 {
		t.Fatalf("unexpected vector count %d", len(rows))
	}
	for _, row := range rows {
		query, _ := hex.DecodeString(row.Query)
		body, _ := hex.DecodeString(row.Body)
		out, err := hongguoV5Hash(query, body, row.TS)
		if err != nil {
			t.Fatalf("ts=%d: %v", row.TS, err)
		}
		if got := hex.EncodeToString(out[:16]); got != row.Digest {
			t.Fatalf("ts=%d digest=%s want=%s", row.TS, got, row.Digest)
		}
		if out[16]&0x24 != 0x24 {
			t.Fatalf("ts=%d check byte %02x", row.TS, out[16])
		}
	}
}

func TestHongguoV5CarrierVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/v5/signing-v5-carrier.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		TS        uint32 `json:"ts"`
		Key       string `json:"key"`
		IV        string `json:"iv"`
		Source    string `json:"source"`
		Encrypted string `json:"encrypted"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("unexpected vector count %d", len(rows))
	}
	for _, row := range rows {
		key, _ := hex.DecodeString(row.Key)
		iv, _ := hex.DecodeString(row.IV)
		source, _ := hex.DecodeString(row.Source)
		out, err := hongguoV5Carrier(key, iv, source, row.TS)
		if err != nil {
			t.Fatalf("ts=%d: %v", row.TS, err)
		}
		if got := hex.EncodeToString(out); got != row.Encrypted {
			t.Fatalf("ts=%d carrier mismatch", row.TS)
		}
	}
}

// long: 期望值由上游发布包内 JS 独立生成，覆盖首次 POST 和复用同一进程标识的无 body GET。
func TestHongguoV5SignMatchesUpstreamDeterministic(t *testing.T) {
	raw, err := os.ReadFile("testdata/v5/signing-v5-upstream.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			NowMs   int64             `json:"nowMs"`
			Base    string            `json:"base"`
			Values  map[string]string `json:"values"`
			Body    *string           `json:"body"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 2 {
		t.Fatal("expected POST and GET vectors")
	}
	previous := hongguoV5Rand
	seq := 0
	hongguoV5Rand = func(out []byte) {
		for i := range out {
			out[i] = byte(seq)
			seq++
		}
	}
	defer func() { hongguoV5Rand = previous; resetHongguoV5ProcessForTest() }()
	resetHongguoV5ProcessForTest()
	for i, row := range fixture.Cases {
		var body []byte
		if row.Body != nil {
			body = []byte(*row.Body)
		}
		address, headers, err := hongguoV5Sign(row.Base, urlValues(row.Values), body, time.UnixMilli(row.NowMs))
		if err != nil {
			t.Fatal(err)
		}
		if address != row.URL {
			t.Fatalf("case %d signed URL mismatch", i)
		}
		if len(headers) != len(row.Headers) {
			t.Fatalf("case %d header count mismatch", i)
		}
		for name, want := range row.Headers {
			if headers[name] != want {
				t.Fatalf("case %d %s differs from upstream", i, name)
			}
		}
	}
}

func urlValues(values map[string]string) url.Values {
	out := url.Values{}
	for key, value := range values {
		out.Set(key, value)
	}
	return out
}

func TestLiveHongguoV5Media(t *testing.T) {
	if os.Getenv("DUANJU_LIVE_V5") != "1" {
		t.Skip("仅在手动验收 V5 播放链路时启用")
	}
	cfg := defaultConfig()
	cfg.dataDir, cfg.OutputDir, cfg.Retries = t.TempDir(), t.TempDir(), 1
	d := NewDownloader(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	media, err := d.resolveHongguoAppMedia(ctx, "7704532501426867224")
	if err != nil {
		t.Fatal(publicError(err))
	}
	if media.URL == "" {
		t.Fatal("empty media url")
	}
	t.Logf("media url=%s quality=%d duration=%s", media.URL, media.Quality, media.Duration)
}
