package storage

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/stretchr/testify/require"
)

// makeImage generates a w×h solid-color PNG (input for the preset and scaling
// tests).
func makeImage(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 0x80, A: 0xFF})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

// TestImageToWebPProducesRIFF verifies the output is indeed a WebP container
// (RIFF....WEBP).
func TestImageToWebPProducesRIFF(t *testing.T) {
	out, err := ImageToWebP(bytes.NewReader(makeImage(t, 20, 10)), 0, WebPLossyBalanced)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(out), 12)
	require.Equal(t, "RIFF", string(out[0:4]))
	require.Equal(t, "WEBP", string(out[8:12]))
}

// TestImageToWebPRejectsGarbage verifies non-image input is rejected (must not
// silently produce an empty WebP).
func TestImageToWebPRejectsGarbage(t *testing.T) {
	_, err := ImageToWebP(bytes.NewReader([]byte("not an image")), 0, WebPLossyBalanced)
	require.Error(t, err)
}

// TestImageToWebPScalesLongEdge verifies that when the long edge exceeds
// maxEdge it scales proportionally down to maxEdge.
func TestImageToWebPScalesLongEdge(t *testing.T) {
	// 400×200 with a 100 long-edge cap -> should be 100×50.
	out, err := ImageToWebP(bytes.NewReader(makeImage(t, 400, 200)), 100, WebPLossyBalanced)
	require.NoError(t, err)
	cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
	require.NoError(t, err)
	require.Equal(t, 100, cfg.Width)
	require.Equal(t, 50, cfg.Height)
}

// TestImageToWebPNoUpscale verifies small images are not upscaled (original
// size kept when the long edge is not exceeded).
func TestImageToWebPNoUpscale(t *testing.T) {
	out, err := ImageToWebP(bytes.NewReader(makeImage(t, 30, 20)), WebPMaxEdge, WebPLossyBalanced)
	require.NoError(t, err)
	cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
	require.NoError(t, err)
	require.Equal(t, 30, cfg.Width)
	require.Equal(t, 20, cfg.Height)
}

// TestPresetsAreMonotonic verifies that higher levels produce smaller sizes
// (lossy levels, same image, same dimensions).
//
// The lossless level (1) is excluded: lossless is usually larger than low-quality
// lossy, so including it in a monotonic assertion would fail spuriously.
func TestPresetsAreMonotonic(t *testing.T) {
	src := makeImage(t, 200, 200)
	var prev int
	for _, p := range []WebPCompressPreset{WebPLossyHighQuality, WebPLossyBalanced, WebPLossyCompact, WebPLossyExtreme} {
		out, err := ImageToWebP(bytes.NewReader(src), 0, p)
		require.NoError(t, err)
		if prev != 0 {
			require.Less(t, len(out), prev, "档位 %d 应小于上一档体积", p)
		}
		prev = len(out)
	}
}

// TestImageToWebPAcceptsJPEG verifies JPEG input can be converted to WebP.
func TestImageToWebPAcceptsJPEG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	var jb bytes.Buffer
	require.NoError(t, jpeg.Encode(&jb, img, nil))

	out, err := ImageToWebP(bytes.NewReader(jb.Bytes()), 0, WebPLossyBalanced)
	require.NoError(t, err)
	require.Equal(t, "WEBP", string(out[8:12]))
}
