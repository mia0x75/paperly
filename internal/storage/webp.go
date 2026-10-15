// Package storage provides filesystem storage utilities: hashing, type
// sniffing, persistence and cleanup. Zero business logic; no dependency on
// domain / repository.
package storage

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"runtime"

	"github.com/KarpelesLab/gowebp"
	"github.com/disintegration/imaging"
)

// WebPMaxEdge is the max (in pixels) for the long edge after scaling uploaded
// images.
const WebPMaxEdge = 5120

// WebPCompressPreset is the WebP compression level; 1~5 maps 1:1 to the system
// settings UI.
type WebPCompressPreset int

const (
	// WebPLosslessMax 1. lossless, max file size
	WebPLosslessMax WebPCompressPreset = 1
	// WebPLossyHighQuality 2. lossy high quality, sharp text, larger size
	WebPLossyHighQuality WebPCompressPreset = 2
	// WebPLossyBalanced 3. lossy balanced, recommended
	WebPLossyBalanced WebPCompressPreset = 3
	// WebPLossyCompact 4. lossy compact, may lose fine text
	WebPLossyCompact WebPCompressPreset = 4
	// WebPLossyExtreme 5. lossy extreme compression, not recommended, may affect
	// text legibility
	WebPLossyExtreme WebPCompressPreset = 5
)

// webPPresetOptions returns the gowebp encoding options:
//
//	1 lossless          Lossy=false, Quality=100, Method=6
//	2 lossy high quality Lossy=true,  Quality=90,  Method=5
//	3 lossy balanced     Lossy=true,  Quality=75,  Method=5
//	4 lossy compact      Lossy=true,  Quality=50,  Method=5
//	5 lossy extreme      Lossy=true,  Quality=30,  Method=5
func webPPresetOptions(p WebPCompressPreset) *gowebp.Options {
	switch p {
	case WebPLosslessMax:
		return &gowebp.Options{Lossy: false, Quality: 100, Method: 6}
	case WebPLossyHighQuality:
		return &gowebp.Options{Lossy: true, Quality: 90, Method: 5}
	case WebPLossyCompact:
		return &gowebp.Options{Lossy: true, Quality: 50, Method: 5}
	case WebPLossyExtreme:
		return &gowebp.Options{Lossy: true, Quality: 30, Method: 5}
	case WebPLossyBalanced:
		fallthrough
	default:
		return &gowebp.Options{Lossy: true, Quality: 75, Method: 5}
	}
}

// decodeAndNormalize reads the image, applies EXIF orientation correction, then
// scales it down to the long-edge limit.
//
// Orientation is corrected before scaling. A portrait photo stores its pixels
// landscape with EXIF marking the rotation; if the scale ratio were computed
// from the long edge first, an actually-portrait image would be treated as
// landscape, making the ratio too large or forcing an unnecessary re-encode.
// imaging's AutoOrientation() corrects based on EXIF and returns the corrected
// image.Image.
//
// Scaling runs only when the long edge exceeds maxEdge, using Lanczos to
// balance sharpness and speed; small images are left untouched to avoid
// pointless re-encoding.
func decodeAndNormalize(r io.Reader, maxEdge int) (image.Image, error) {
	img, err := imaging.Decode(r, imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	if maxEdge <= 0 {
		return img, nil
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	longEdge := w
	if h > longEdge {
		longEdge = h
	}
	if longEdge <= maxEdge {
		return img, nil
	}
	scale := float64(maxEdge) / float64(longEdge)
	newW := int(float64(w)*scale + 0.5)
	newH := int(float64(h)*scale + 0.5)
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}
	return imaging.Resize(img, newW, newH, imaging.Lanczos), nil
}

func EncodeWebP(img image.Image, preset WebPCompressPreset) ([]byte, error) {
	var buf bytes.Buffer
	if err := gowebp.Encode(&buf, img, webPPresetOptions(preset)); err != nil {
		return nil, fmt.Errorf("encode webp: %w", err)
	}
	return buf.Bytes(), nil
}

// ImageToWebP reads the original image (JPEG/PNG/GIF), applies EXIF correction,
// caps the long edge to maxEdge, then encodes to a WebP byte stream by preset.
func ImageToWebP(r io.Reader, maxEdge int, preset WebPCompressPreset) ([]byte, error) {
	img, err := decodeAndNormalize(r, maxEdge)
	if err != nil {
		return nil, err
	}
	out, err := EncodeWebP(img, preset)
	// The decoded image.Image can be hundreds of MB for a large photo; force a
	// GC after each conversion so transient decode memory is reclaimed promptly
	// on small hosts (upload concurrency is also gated by memory tier).
	runtime.GC()
	return out, err
}
