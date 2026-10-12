package qoder

import (
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	cases := []string{
		"hello world",
		"test",
		"hello world!!!",
	}

	for _, tc := range cases {
		encoded := Encode([]byte(tc))
		if encoded == tc {
			t.Errorf("encode(%q) = %q, expected different", tc, encoded)
		}
		decoded, err := DecodeString(encoded)
		if err != nil {
			t.Errorf("decode(encode(%q)) error: %v", tc, err)
		}
		if decoded != tc {
			t.Errorf("decode(encode(%q)) = %q, want %q", tc, decoded, tc)
		}
	}
}

func TestEncodeNotStandardBase64(t *testing.T) {
	encoded := Encode([]byte("test"))
	if encoded == "dGVzdA==" {
		t.Error("encoded result should not be standard base64")
	}
}
