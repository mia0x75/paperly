package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

// getJSON issues a GET request and decodes the response body.
func getJSON(t *testing.T, e *echo.Echo, target string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	var body map[string]any
	if rec.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	}
	return rec.Code, body
}

// putJSON issues a PUT request and decodes the response body.
func putJSON(t *testing.T, e *echo.Echo, target string, payload map[string]any) (int, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPut, target, bytes.NewReader(raw))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	var body map[string]any
	if rec.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	}
	return rec.Code, body
}

// body2int converts a JSON number (float64) to int.
func body2int(t *testing.T, v any) int {
	t.Helper()
	f, ok := v.(float64)
	require.True(t, ok, "期望数字，实际 %T", v)
	return int(f)
}
