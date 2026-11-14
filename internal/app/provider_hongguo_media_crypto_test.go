package app

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"math/bits"
	"testing"
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
