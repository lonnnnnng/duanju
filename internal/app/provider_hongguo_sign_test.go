package app

import (
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestHongguoModelGorgonVector(t *testing.T) {
	const query = "aid=8662&app_name=novelread&device_id=1234567890123456789&iid=1234567890123456789&_rticket=1770000000000"
	body := []byte(`{"mixed_video_id_map":{"1004":["7693086454326496281"]}}`)
	const expected = "840492104001b1a0c3cca554d53d9df21e4fd403bce7ded12dc8"
	if actual := hongguoModelGorgon(query, body, 1770000000, 4242); actual != expected {
		t.Fatalf("model gorgon mismatch: %s", actual)
	}
}

func TestHongguoModelRequestSetsSignatureHeaders(t *testing.T) {
	body := []byte(`{"series_id":"1"}`)
	request, _ := http.NewRequest(http.MethodPost, hongguoAppBaseURL+"/novel/player/multi_video_model/v1/?aid=8662", nil)
	request.URL.RawQuery = "aid=8662&_rticket=1770000000000"
	signHongguoModelRequest(request, body, time.Unix(1770000000, 0))
	for _, name := range []string{"X-Gorgon", "X-Khronos", "X-SS-Req-Ticket", "X-SS-STUB", "X-Argus", "X-Ladon"} {
		if request.Header.Get(name) == "" {
			t.Fatalf("missing header %s", name)
		}
	}
	sum := md5.Sum(body)
	if request.Header.Get("X-SS-STUB") != strings.ToUpper(hex.EncodeToString(sum[:])) {
		t.Fatal("unexpected body stub")
	}
}
