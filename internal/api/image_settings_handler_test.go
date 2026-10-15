package api

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"

	"paperly/internal/service"
)

// newImageSettingsServer builds a test server mounting only the image-settings routes.
func newImageSettingsServer(t *testing.T) *echo.Echo {
	t.Helper()
	svc := service.NewImageSettings(3, filepath.Join(t.TempDir(), "config.json"))
	e := echo.New()
	NewImageSettingsHandler(svc).RegisterRoutes(e.Group("/api"))
	return e
}

func TestImageSettingsRoutesRegistered(t *testing.T) {
	e := newImageSettingsServer(t)
	routes := map[string]bool{}
	for _, r := range e.Router().Routes() {
		routes[r.Method+" "+r.Path] = true
	}
	for _, want := range []string{"GET /api/settings/image", "PUT /api/settings/image"} {
		require.True(t, routes[want], "缺少路由 %s", want)
	}
}

func TestImageSettingsGet(t *testing.T) {
	e := newImageSettingsServer(t)
	rec := doSettingsJSON(t, e, http.MethodGet, "/api/settings/image", "")
	require.Equal(t, http.StatusOK, rec.Code)
	data := decodeData(t, rec)
	require.EqualValues(t, 3, data["compress_preset"])
}

func TestImageSettingsUpdateRoundTrip(t *testing.T) {
	e := newImageSettingsServer(t)
	rec := doSettingsJSON(t, e, http.MethodPut, "/api/settings/image", `{"compress_preset":5}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.EqualValues(t, 5, decodeData(t, rec)["compress_preset"])

	// Read-back is consistent.
	rec = doSettingsJSON(t, e, http.MethodGet, "/api/settings/image", "")
	require.EqualValues(t, 5, decodeData(t, rec)["compress_preset"])
}

func TestImageSettingsUpdateRejectsOutOfRange(t *testing.T) {
	e := newImageSettingsServer(t)
	rec := doSettingsJSON(t, e, http.MethodPut, "/api/settings/image", `{"compress_preset":9}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, CodeInvalidInput, decodeErr(t, rec).Code)
}

func TestImageSettingsUpdateRejectsBadBody(t *testing.T) {
	e := newImageSettingsServer(t)
	rec := doSettingsJSON(t, e, http.MethodPut, "/api/settings/image", `{"compress_preset":"x"}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
