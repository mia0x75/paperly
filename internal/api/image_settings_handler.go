package api

import (
	"errors"

	"github.com/labstack/echo/v5"

	"paperly/internal/api/dto"
	"paperly/internal/domain"
	"paperly/internal/service"
)

// ImageSettingsHandler handles reading/writing the upload image-compression tiers.
//
// There is only one setting, at route /api/settings/image. The reason it is not put in AdminHandler:
// the settings page's "image compression" is a preference parents adjust routinely, whereas AdminHandler
// is all backup/health maintenance actions; mixing them would blur both permissions and semantics.
type ImageSettingsHandler struct {
	svc *service.ImageSettings
}

// NewImageSettingsHandler constructs the image-settings HTTP handler.
func NewImageSettingsHandler(svc *service.ImageSettings) *ImageSettingsHandler {
	return &ImageSettingsHandler{svc: svc}
}

// RegisterRoutes registers the image-settings routes on the given group.
func (h *ImageSettingsHandler) RegisterRoutes(g *echo.Group) {
	g.GET("/settings/image", h.Get)
	g.PUT("/settings/image", h.Update)
}

// Get handles GET /api/settings/image.
func (h *ImageSettingsHandler) Get(c *echo.Context) error {
	preset, err := h.svc.GetImageCompressPreset(c.Request().Context())
	if err != nil {
		return fail(c, CodeInternal, "failed to read image settings", err.Error())
	}
	return ok(c, dto.NewImageSettings(preset))
}

// updateImageSettingsReq is the PUT request body.
type updateImageSettingsReq struct {
	CompressPreset int `json:"compress_preset"`
}

// Update handles PUT /api/settings/image.
func (h *ImageSettingsHandler) Update(c *echo.Context) error {
	var req updateImageSettingsReq
	if err := c.Bind(&req); err != nil {
		return fail(c, CodeInvalidBody, "invalid request body", err.Error())
	}
	if err := h.svc.SetImageCompressPreset(c.Request().Context(), req.CompressPreset); err != nil {
		if errors.Is(err, domain.ErrInvalidInput) {
			return fail(c, CodeInvalidInput, err.Error(), "")
		}
		return fail(c, CodeInternal, "failed to save image settings", err.Error())
	}
	return ok(c, dto.NewImageSettings(req.CompressPreset))
}
