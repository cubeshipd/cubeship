package catalog

import (
	"bytes"
	"fmt"
	"image/png"

	"cubeship/template"
)

const (
	iconMin = 128
	iconMax = 1024
)

// checkIcon returns the icon re-encoded, or the code and message of why
// it is refused. Re-encoded because a file taken verbatim is whatever
// its author put in it: metadata, trailing bytes, a second format
// pretending to be the first.
func checkIcon(b []byte) ([]byte, string, string) {
	// The header first, so a small file claiming to be enormous is
	// refused before anything is decompressed.
	cfg, err := png.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return nil, "icon.not-png", "icon.png is not a PNG image"
	}
	if cfg.Width != cfg.Height || cfg.Width < iconMin || cfg.Width > iconMax {
		return nil, "icon.size", fmt.Sprintf("icon.png is %d×%d: it is square, between %d and %d pixels a side",
			cfg.Width, cfg.Height, iconMin, iconMax)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, "icon.not-png", "icon.png is not a PNG image"
	}
	var out bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&out, img); err != nil {
		return nil, "icon.not-png", "icon.png is not a PNG image"
	}
	return out.Bytes(), "", ""
}

// iconAdvice warns about how an accepted icon will look framed square
// beside the others. It never refuses: an icon that looks odd is still the
// author's to ship.
func iconAdvice(b []byte) []template.Diagnostic {
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil
	}
	r := img.Bounds()
	alpha := func(x, y int) uint32 {
		_, _, _, a := img.At(x, y).RGBA()
		return a
	}
	var out []template.Diagnostic
	warn := func(code, message, hint string) {
		out = append(out, template.Diagnostic{
			Severity: template.Warning, Code: code, Message: message, Path: []any{}, Hint: hint,
		})
	}

	// A rounded tile: every corner see-through, the middle of the edges not.
	const half = 0x8000
	corners, edges := 0, 0
	for _, p := range [][2]int{{r.Min.X, r.Min.Y}, {r.Max.X - 1, r.Min.Y}, {r.Min.X, r.Max.Y - 1}, {r.Max.X - 1, r.Max.Y - 1}} {
		if alpha(p[0], p[1]) < half {
			corners++
		}
	}
	midX, midY := (r.Min.X+r.Max.X)/2, (r.Min.Y+r.Max.Y)/2
	for _, p := range [][2]int{{midX, r.Min.Y}, {midX, r.Max.Y - 1}, {r.Min.X, midY}, {r.Max.X - 1, midY}} {
		if alpha(p[0], p[1]) >= half {
			edges++
		}
	}
	if corners == 4 && edges >= 3 {
		warn("icon.rounded", "icon.png has transparent corners, so it shows as a rounded tile",
			"icons are framed square: fill the corners and draw it edge to edge")
	}

	// A margin: the artwork, whatever is not nearly see-through, well inside
	// the image.
	minX, minY, maxX, maxY := r.Max.X, r.Max.Y, r.Min.X-1, r.Min.Y-1
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if alpha(x, y) > 10*257 {
				minX, minY = min(minX, x), min(minY, y)
				maxX, maxY = max(maxX, x), max(maxY, y)
			}
		}
	}
	if maxX >= minX {
		w, h := maxX-minX+1, maxY-minY+1
		if w*10 < r.Dx()*9 || h*10 < r.Dy()*9 {
			warn("icon.padding", fmt.Sprintf("icon.png's artwork fills %d×%d of its %d×%d pixels, so it shows smaller than other icons",
				w, h, r.Dx(), r.Dy()), "crop the transparent margin and scale the artwork to the edges")
		}
	}
	return out
}
