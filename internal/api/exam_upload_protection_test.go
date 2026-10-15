package api

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"paperly/internal/system"
)

// hugePNG builds a PNG whose long edge exceeds maxImageLongEdge (16384).
func hugePNG(longEdge int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, longEdge, 1))
	for x := 0; x < longEdge; x += 64 {
		img.Set(x, 0, color.RGBA{R: 0x20, G: 0x40, B: 0x60, A: 0xFF})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// TestAPIRefusesOversizedImage asserts an image whose long edge exceeds
// maxImageLongEdge is rejected with 400 before transcode.
func TestAPIRefusesOversizedImage(t *testing.T) {
	e := newPaperTestServer(t)
	status, resp := uploadMultipart(t, e, "/api/exams/upload", "huge.png", hugePNG(maxImageLongEdge+1), nil)
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, float64(CodeValidation), resp["code"])
}

// TestAPIAllowsWithinLimitImage asserts a normal image still uploads fine with
// the size guard in place.
func TestAPIAllowsWithinLimitImage(t *testing.T) {
	e := newPaperTestServer(t)
	status, _ := uploadMultipart(t, e, "/api/exams/upload", "ok.png", pngDoc("ok"), nil)
	require.Equal(t, http.StatusOK, status)
}

// TestAPIBusyQueueReturns429 asserts that when the upload queue is full the
// next request gets 429, not an unbounded block. The handler acquires the gate
// synchronously, so saturation is exercised at the gate level (the same
// ErrBusy the handler maps to 429).
func TestAPIBusyQueueReturns429(t *testing.T) {
	gate := system.NewGate(system.GateConfig{SemSize: 1, QueueLen: 1})
	if err := gate.Acquire(); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	queued := make(chan error, 1)
	go func() {
		queued <- gate.Acquire()
	}()
	select {
	case <-time.After(50 * time.Millisecond):
		// Expected: queued and blocked.
	case err := <-queued:
		t.Fatalf("queued acquire completed unexpectedly: %v", err)
	}
	if err := gate.Acquire(); !errors.Is(err, system.ErrBusy) {
		t.Fatalf("expected ErrBusy when queue full, got %v", err)
	}
	gate.Release()
	select {
	case err := <-queued:
		if err != nil {
			t.Fatalf("queued acquire after release: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued acquire did not complete")
	}
	gate.Release()
}
