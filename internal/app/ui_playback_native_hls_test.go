package app

import "testing"

func TestIsNativeHLSFileName(t *testing.T) {
	for _, name := range []string{"index.m3u8", "init.mp4", "000000.m4s", "999999.m4s"} {
		if !isNativeHLSFileName(name) {
			t.Fatalf("rejected valid native HLS file: %s", name)
		}
	}
	for _, name := range []string{"../index.m3u8", "index.m3u8/x", "00000.m4s", "0000000.m4s", "00a000.m4s", "index.m3u84", "other.mp4"} {
		if isNativeHLSFileName(name) {
			t.Fatalf("accepted invalid native HLS file: %s", name)
		}
	}
}
