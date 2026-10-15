package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHandlerServesIndexAtRoot(t *testing.T) {
	h, err := Handler()
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, strings.ToLower(rec.Header().Get("Content-Type")), "text/html")
	require.Contains(t, rec.Body.String(), "<!doctype html")
}

func TestHandlerSPAFallback(t *testing.T) {
	h, err := Handler()
	require.NoError(t, err)

	// Unknown paths (frontend routes) should fall back to index.html, not 404.
	req := httptest.NewRequest(http.MethodGet, "/kids/123", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "<!doctype html")
}
