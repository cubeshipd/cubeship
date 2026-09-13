package catalog

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

func TestIcons(t *testing.T) {
	wide := image.NewRGBA(image.Rect(0, 0, 300, 200))
	var buf bytes.Buffer
	png.Encode(&buf, wide)

	cases := []struct {
		name string
		body []byte
		code string
	}{
		{"a square PNG", squarePNG(512), ""},
		{"too small", squarePNG(64), "icon.size"},
		{"too large", squarePNG(2048), "icon.size"},
		{"not square", buf.Bytes(), "icon.size"},
		{"not a PNG", []byte("<svg/>"), "icon.not-png"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clean, code, _ := checkIcon(tc.body)
			if code != tc.code {
				t.Fatalf("code = %q, want %q", code, tc.code)
			}
			if code == "" {
				if _, err := png.Decode(bytes.NewReader(clean)); err != nil {
					t.Errorf("re-encoded icon does not decode: %v", err)
				}
			}
		})
	}
}
