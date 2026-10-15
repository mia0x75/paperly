// Package config handles Box runtime configuration
// (config.json -> environment variables -> strongly typed struct).
//
// Box is self-hosted with no config center and no hot reload. All configuration is read once at
// startup; missing values fall back to defaults, but present values must be valid. Misconfiguration
// causes a startup crash.
//
// Principles:
//   - Config is JSON using the standard library only (no YAML/TOML).
//   - Priority: environment variables > config.json > code defaults.
//   - Secrets come only from env vars, never written to disk, logged, or exposed in DTOs.
package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// EnvPrefix is this project's environment variable prefix, avoiding collisions on the host.
const EnvPrefix = "PAPERLY_"

const (
	EnvConfigPath          = EnvPrefix + "CONFIG"
	EnvDataDir             = EnvPrefix + "DATA_DIR"
	EnvAddr                = EnvPrefix + "ADDR"
	EnvImageCompressPreset = EnvPrefix + "IMAGE_COMPRESS_PRESET"
)

// Image is the image processing configuration.
type Image struct {
	CompressPreset int
}

type Config struct {
	DataDir string
	Addr    string
	Image   Image
}

const (
	DefaultAddr = ":8080"
	// DefaultImageCompressPreset is the recommended preset (balanced lossy).
	DefaultImageCompressPreset = 3
)

// fileConfig is the structure of config.json.
type fileConfig struct {
	DataDir string    `json:"data_dir"`
	Addr    string    `json:"addr"`
	Image   fileImage `json:"image"`
	path    string
}

type fileImage struct {
	CompressPreset int `json:"compress_preset"`
}

var (
	fileTopFields   = map[string]bool{"data_dir": true, "addr": true, "image": true}
	fileImageFields = map[string]bool{"compress_preset": true}
)

// resolveExeDir returns the directory containing the executable, or "" if unavailable.
func resolveExeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
		exe = resolved
	}
	return filepath.Dir(exe)
}

func defaultDataDir(exeDir string) string {
	if exeDir == "" {
		return "./data"
	}
	return filepath.Join(exeDir, "data")
}

// configPath locates the config file.
func configPath(exeDir string) string {
	if v := strings.TrimSpace(os.Getenv(EnvConfigPath)); v != "" {
		return v
	}
	if exeDir == "" {
		return ""
	}
	return filepath.Join(exeDir, "config.json")
}

// loadFile reads the config file. A missing file returns defaults; a parse error is returned.
func loadFile(path string) (fileConfig, bool, error) {
	var fc fileConfig
	if path == "" {
		return fc, false, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fc, false, nil
		}
		return fc, false, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, &fc); err != nil {
		return fc, false, fmt.Errorf("parse %s: %w", path, err)
	}
	fc.path = path
	warnUnknownFields(path, raw)
	return fc, true, nil
}

// warnUnknownFields warns about unknown fields but does not reject.
func warnUnknownFields(path string, raw []byte) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return
	}
	for k := range top {
		if !fileTopFields[k] {
			slog.Warn("config.json has unknown field, ignored", "path", path, "field", k)
		}
	}
	warnSection(path, "image", top["image"], fileImageFields)
}

func warnSection(path, name string, raw json.RawMessage, known map[string]bool) {
	if len(raw) == 0 {
		return
	}
	var sec map[string]json.RawMessage
	if err := json.Unmarshal(raw, &sec); err != nil {
		return
	}
	for k := range sec {
		if !known[k] {
			slog.Warn("config.json has unknown field, ignored", "path", path, "section", name, "field", k)
		}
	}
}

// Load reads configuration from config.json and environment variables.
func Load() (*Config, error) {
	exeDir := resolveExeDir()
	fc, hasFile, err := loadFile(configPath(exeDir))
	if err != nil {
		return nil, err
	}

	cfg := defaults()
	cfg.DataDir = pickDataDir(exeDir, fc, hasFile)
	if err := applyFile(cfg, fc, hasFile); err != nil {
		return nil, err
	}
	if err := applyEnv(cfg); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(cfg.DataDir) {
		abs, aerr := filepath.Abs(cfg.DataDir)
		if aerr != nil {
			return nil, fmt.Errorf("parse data dir %q: %w", cfg.DataDir, aerr)
		}
		cfg.DataDir = abs
	}
	if err := validate(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func defaults() *Config {
	return &Config{
		Addr:  DefaultAddr,
		Image: Image{CompressPreset: DefaultImageCompressPreset},
	}
}

// pickDataDir determines the data directory by env > file > default.
func pickDataDir(exeDir string, fc fileConfig, hasFile bool) string {
	if v := strings.TrimSpace(os.Getenv(EnvDataDir)); v != "" {
		return v
	}
	if hasFile {
		if v := strings.TrimSpace(fc.DataDir); v != "" {
			if filepath.IsAbs(v) || exeDir == "" {
				return v
			}
			return filepath.Join(exeDir, v)
		}
	}
	return defaultDataDir(exeDir)
}

// applyFile overrides defaults with config.json when values are explicitly provided.
func applyFile(cfg *Config, fc fileConfig, hasFile bool) error {
	if !hasFile {
		return nil
	}
	cfg.Addr = firstNonEmpty(fc.Addr, cfg.Addr)
	if fc.Image.CompressPreset != 0 {
		if err := ValidateImagePreset(fc.Image.CompressPreset); err != nil {
			return fmt.Errorf("%s: %w", labelOf(fc.path, "image.compress_preset"), err)
		}
		cfg.Image.CompressPreset = fc.Image.CompressPreset
	}
	return nil
}

func applyEnv(cfg *Config) error {
	cfg.Addr = getenv(EnvAddr, cfg.Addr)

	if raw := strings.TrimSpace(os.Getenv(EnvImageCompressPreset)); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("%s=%q is not a valid integer", EnvImageCompressPreset, raw)
		}
		if err := ValidateImagePreset(n); err != nil {
			return fmt.Errorf("%s: %w", EnvImageCompressPreset, err)
		}
		cfg.Image.CompressPreset = n
	}
	return nil
}

func validate(cfg *Config) error {
	if cfg.Image.CompressPreset < 1 || cfg.Image.CompressPreset > 5 {
		return fmt.Errorf("image.compress_preset must be in the range 1-5")
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// labelOf builds the error label.
func labelOf(path, field string) string {
	if path == "" {
		return field
	}
	return path + " " + field
}

// getenv reads an environment variable; empty (including whitespace-only) is unset.
func getenv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// DefaultPath returns the config file path used at runtime.
func DefaultPath() string {
	return configPath(resolveExeDir())
}

// ValidateImagePreset validates the compression preset (1~5).
func ValidateImagePreset(p int) error {
	if p < 1 || p > 5 {
		return fmt.Errorf("image.compress_preset must be in the range 1-5")
	}
	return nil
}

// SaveImagePreset writes image.compress_preset back to the config file, preserving other fields.
//
// map[string]any is used instead of fileConfig so keys from future versions are not erased on
// re-serialization. An empty path returns an error.
func SaveImagePreset(path string, preset int) error {
	if err := ValidateImagePreset(preset); err != nil {
		return err
	}
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("no writable config file path found")
	}

	root := map[string]any{}
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &root); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	img, _ := root["image"].(map[string]any)
	if img == nil {
		img = map[string]any{}
	}
	img["compress_preset"] = preset
	root["image"] = img

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("serialize config: %w", err)
	}
	out = append(out, '\n')
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
