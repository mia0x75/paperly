package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// clearEnv blanks this package's environment variables.
//
// t.Setenv(k, "") is used instead of os.Unsetenv because readers treat an empty string as unset,
// and t.Setenv restores automatically without polluting other tests in the package.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{EnvImageCompressPreset} {
		t.Setenv(k, "")
	}
}

// Having no config file is the normal state: defaults must be used.
func TestNoConfigFileAllDefaults(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvConfigPath, filepath.Join(t.TempDir(), "不存在.json"))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("没有配置文件不该报错：%v", err)
	}
	if cfg.Addr != DefaultAddr {
		t.Errorf("Addr = %q, 期望默认 %q", cfg.Addr, DefaultAddr)
	}
	if cfg.Image.CompressPreset != DefaultImageCompressPreset {
		t.Errorf("CompressPreset = %d, 期望默认 %d", cfg.Image.CompressPreset, DefaultImageCompressPreset)
	}
}

func TestConfigFileApplies(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, `{
		"addr": ":9090",
		"image": {"compress_preset": 4}
	}`)
	t.Setenv(EnvConfigPath, path)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addr != ":9090" {
		t.Errorf("Addr = %q, 期望取自文件", cfg.Addr)
	}
	if cfg.Image.CompressPreset != 4 {
		t.Errorf("CompressPreset = %d, 期望取自文件 4", cfg.Image.CompressPreset)
	}
}

func TestEnvOverridesConfigFile(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, `{
		"addr": ":9090",
		"image": {"compress_preset": 4}
	}`)
	t.Setenv(EnvConfigPath, path)
	t.Setenv(EnvAddr, ":7070")
	t.Setenv(EnvImageCompressPreset, "2")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addr != ":7070" {
		t.Errorf("Addr = %q, env 应赢", cfg.Addr)
	}
	if cfg.Image.CompressPreset != 2 {
		t.Errorf("CompressPreset = %d, env 应赢", cfg.Image.CompressPreset)
	}
}

// Unknown fields do not block startup, so a typo does not stop Box from starting.
func TestConfigFileUnknownFieldTolerated(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, `{
		"addr": ":9090",
		"typo_field": 1,
		"image": {"compress_preset": 3, "nmae": "x"}
	}`)
	t.Setenv(EnvConfigPath, path)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("未知字段不该阻断启动：%v", err)
	}
	if cfg.Addr != ":9090" {
		t.Errorf("Addr = %q", cfg.Addr)
	}
}

// A syntax error is a misconfiguration and must fail at startup.
func TestConfigFileBadJSONFailsFast(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, `{"addr": ":9090",}`)
	t.Setenv(EnvConfigPath, path)

	if _, err := Load(); err == nil {
		t.Fatal("JSON 语法错应在启动时报错")
	} else if !strings.Contains(err.Error(), path) {
		t.Errorf("错误信息应点名配置文件路径：%v", err)
	}
}

func TestConfigFileBadValuesFailFast(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, `{"image": {"compress_preset": 9}}`)
	t.Setenv(EnvConfigPath, path)
	if _, err := Load(); err == nil {
		t.Fatal("非法档位应启动失败")
	}
}

// A relative data_dir in the file is based on the executable directory, not cwd.
func TestConfigFileRelativeDataDirResolvesToExeDir(t *testing.T) {
	clearEnv(t)
	exeDir := resolveExeDir()
	if exeDir == "" {
		t.Skip("取不到可执行文件目录")
	}
	path := writeConfig(t, `{"data_dir": "custom-data"}`)
	t.Setenv(EnvConfigPath, path)
	// Cleared after writeConfig: let the file's data_dir win (env has higher priority).
	t.Setenv(EnvDataDir, "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := filepath.Join(exeDir, "custom-data")
	if cfg.DataDir != want {
		t.Errorf("DataDir = %q, 期望 %q（相对路径以 exeDir 为基准）", cfg.DataDir, want)
	}
	if !filepath.IsAbs(cfg.DataDir) {
		t.Errorf("DataDir 必须绝对：%q", cfg.DataDir)
	}
}

func TestEnvDataDirBeatsConfigFile(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, `{"data_dir": "from-file"}`)
	t.Setenv(EnvConfigPath, path)
	envDir := t.TempDir()
	t.Setenv(EnvDataDir, envDir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DataDir != envDir {
		t.Errorf("DataDir = %q, 期望 env 的 %q", cfg.DataDir, envDir)
	}
}

func TestLoadRelativeDataDirBecomesAbsolute(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvDataDir, "./rel-data")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !filepath.IsAbs(cfg.DataDir) {
		t.Errorf("DataDir = %q, 相对路径会让「双击启动」时数据落到意外位置", cfg.DataDir)
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	// Blanking EnvConfigPath is key: otherwise it leaks to the previous temporary file.
	t.Setenv(EnvConfigPath, "")
	t.Setenv(EnvDataDir, t.TempDir())
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("写配置: %v", err)
	}
	return path
}

func TestImagePresetDefault(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvConfigPath, filepath.Join(t.TempDir(), "不存在.json"))
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Image.CompressPreset != DefaultImageCompressPreset {
		t.Errorf("CompressPreset = %d, 期望默认 %d", cfg.Image.CompressPreset, DefaultImageCompressPreset)
	}
}

func TestImagePresetFromFileAndEnv(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, `{"image": {"compress_preset": 2}}`)
	t.Setenv(EnvConfigPath, path)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Image.CompressPreset != 2 {
		t.Errorf("文件档位 = %d, 期望 2", cfg.Image.CompressPreset)
	}

	t.Setenv(EnvImageCompressPreset, "5")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Image.CompressPreset != 5 {
		t.Errorf("env 档位 = %d, 期望 5", cfg.Image.CompressPreset)
	}
}

func TestImagePresetInvalidFailsFast(t *testing.T) {
	clearEnv(t)
	path := writeConfig(t, `{"image": {"compress_preset": 9}}`)
	t.Setenv(EnvConfigPath, path)
	if _, err := Load(); err == nil {
		t.Fatal("非法档位应启动失败")
	}
}

func TestSaveImagePresetPreservesOtherFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	original := `{
  "addr": ":9090",
  "unknown_future_key": {"keep": true},
  "image": {"compress_preset": 3}
}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("写: %v", err)
	}

	if err := SaveImagePreset(path, 5); err != nil {
		t.Fatalf("SaveImagePreset: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读: %v", err)
	}
	body := string(raw)
	if !strings.Contains(body, ":9090") {
		t.Errorf("addr 被抹掉了：%s", body)
	}
	if !strings.Contains(body, "unknown_future_key") {
		t.Errorf("未知字段被抹掉了：%s", body)
	}
	if !strings.Contains(body, `"compress_preset": 5`) {
		t.Errorf("档位没写进去：%s", body)
	}

	// After write-back it must be parsed correctly by Load.
	clearEnv(t)
	t.Setenv(EnvConfigPath, path)
	t.Setenv(EnvDataDir, t.TempDir())
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Image.CompressPreset != 5 {
		t.Errorf("回读档位 = %d, 期望 5", cfg.Image.CompressPreset)
	}
}

func TestSaveImagePresetCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := SaveImagePreset(path, 1); err != nil {
		t.Fatalf("SaveImagePreset: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("文件未创建: %v", err)
	}
}

func TestSaveImagePresetRejectsBadInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := SaveImagePreset(path, 0); err == nil {
		t.Error("档位 0 应被拒")
	}
	if err := SaveImagePreset(path, 6); err == nil {
		t.Error("档位 6 应被拒")
	}
	if err := SaveImagePreset("", 3); err == nil {
		t.Error("空路径应被拒")
	}
}
