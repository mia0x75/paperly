package dto

// ImageSettings is the response body for GET/PUT /api/settings/image.
//
// Returns only compress_preset (1~5). The display text of the five tiers (lossless / lossy balance…)
// belongs to the UI layer and is mapped by the frontend — the backend does not ship Chinese display
// text, keeping the same tier descriptions from being duplicated across frontend and backend and drifting.
type ImageSettings struct {
	CompressPreset int `json:"compress_preset"`
}

// NewImageSettings maps the image settings.
func NewImageSettings(preset int) ImageSettings {
	return ImageSettings{CompressPreset: preset}
}
