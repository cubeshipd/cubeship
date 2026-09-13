package discovery

import (
	"bytes"
	"fmt"
	"image/png"
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
