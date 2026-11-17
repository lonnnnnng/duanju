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

func TestHongguoV5SignMatchesUpstreamDeterministic(t *testing.T) {
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
	values := urlValues(map[string]string{
		"aid": "8662", "device_id": "123", "iid": "456", "cdid": "synthetic",
		"version_name": "7.4.3.32", "channel": "vivo_8662_64", "os_version": "12", "device_type": "PGT-AN10",
	})
	signedURL, headers, err := hongguoV5Sign("https://example.invalid/model", values, []byte(`{"fixture":true}`), time.Unix(1800000000, 0))
	if err != nil {
		t.Fatal(err)
	}
	wantURL := "https://example.invalid/model?_rticket=1800000000011&aid=8662&cdid=synthetic&channel=vivo_8662_64&device_id=123&device_type=PGT-AN10&iid=456&os_version=12&version_name=7.4.3.32"
	if signedURL != wantURL {
		t.Fatalf("url=%s", signedURL)
	}
	if headers["X-Khronos"] != "1800000000" {
		t.Fatalf("khronos=%s", headers["X-Khronos"])
	}
	if headers["X-SS-STUB"] != "82E9FB28C6B069812973D69832368531" {
		t.Fatalf("stub=%s", headers["X-SS-STUB"])
	}
	if headers["X-Helios"] != "AAECAxjnWC5DAsxQ2/mnLKzNFrul71BYsp3J36XftUuWs5HI" {
		t.Fatalf("helios=%s", headers["X-Helios"])
	}
	if headers["X-Medusa"] != "BdJJay2ZBqFJp0QoP2fnRyK/hT0aGwABlMo+FyAlAXkBGAAn+Vb/jHu8Ny6MD7ujH6v/yrvMv4L3jUi2I35RrElnT1kRjreYGZVaVKfG+vZQl4DIFKaSX/K5QbU/S2QgIDMNhHHNKMCYxnx0S9Pa3ffTGFl/7GBn2+53QwzBVhPnBHOg59WGVZP2+QjUxSU/ntURHRLE/EcMzYgCHzQCnzQSnxjNo3U7iX1l1nszM8BaJGyQ2uXXI+y67JJZN7AMKWgXtII/08i+LCv07p06m2eR/BfnkNqjK1fxuXzuYAp9nnCmrdjOzap2zp4v7nJXpc7cgzD1CiDD0ZKwOuSdQKPaDDBfw2okXYqtKjX/n89zIFvDA0yvIze5XDjjJLCMHYg9DP2IfL1hfSAohyGOpTR6fMODAMTjK8tTHI8y3O5kYxGTFglK36ls+rqrfarp/Rj3CT1J4tPsoFnojaCKl0bN78t/jgTVy6bh4zPfYafTWmb8boVzvLCepR694pA55Nk36QBUZBLzR2aJMAtcxr/+2cV3e6SPViZy357YAFppLoUEe8aIuP54NxEHGg1GteUBAtn1U5rgmpLIZtZYOOFRQ3pMXBtwH/7//x/+//8cHQ==" {
		t.Fatalf("medusa=%s", headers["X-Medusa"])
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
