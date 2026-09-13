package catalog

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"slices"
	"testing"

	"cubeship/template"
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

// An icon drawn as a rounded tile, or with a transparent margin, is warned
// about and still accepted.
func TestIconAdvice(t *testing.T) {
	encode := func(img image.Image) []byte {
		var buf bytes.Buffer
		png.Encode(&buf, img)
		return buf.Bytes()
	}
	opaque := color.RGBA{255, 255, 255, 255}

	full := image.NewRGBA(image.Rect(0, 0, 256, 256))
	draw.Draw(full, full.Bounds(), image.NewUniform(opaque), image.Point{}, draw.Src)

	rounded := image.NewRGBA(image.Rect(0, 0, 256, 256))
	draw.Draw(rounded, rounded.Bounds(), image.NewUniform(opaque), image.Point{}, draw.Src)
	for _, c := range []image.Rectangle{
		image.Rect(0, 0, 20, 20), image.Rect(236, 0, 256, 20),
		image.Rect(0, 236, 20, 256), image.Rect(236, 236, 256, 256),
	} {
		draw.Draw(rounded, c, image.Transparent, image.Point{}, draw.Src)
	}

	padded := image.NewRGBA(image.Rect(0, 0, 256, 256))
	draw.Draw(padded, image.Rect(40, 40, 216, 216), image.NewUniform(opaque), image.Point{}, draw.Src)

	for _, tc := range []struct {
		name string
		img  image.Image
		want []string
	}{
		{"edge to edge", full, nil},
		{"a rounded tile", rounded, []string{"icon.rounded"}},
		{"a transparent margin", padded, []string{"icon.padding"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, d := range iconAdvice(encode(tc.img)) {
				if d.Severity != template.Warning {
					t.Errorf("%s is %s, want a warning", d.Code, d.Severity)
				}
				got = append(got, d.Code)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("codes = %v, want %v", got, tc.want)
			}
		})
	}
}
