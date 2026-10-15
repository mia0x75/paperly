package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"

	"paperly/internal/backup"
	"paperly/internal/domain"
	"paperly/internal/health"
	"paperly/internal/seed"
	"paperly/internal/service"
)

type adminOpsRepo struct {
	verifyErr error
	integrity string
	size      int64
	schemaTag string
}

func (f *adminOpsRepo) VacuumInto(_ context.Context, p string) error {
	return os.WriteFile(p, []byte("SQLite format 3\x00"), 0o600)
}
func (f *adminOpsRepo) VerifyFileIntegrity(_ context.Context, _ string) (string, error) {
	if f.verifyErr != nil {
		return "", f.verifyErr
	}
	return "ok", nil
}
func (f *adminOpsRepo) IntegrityCheck(context.Context) (string, error) { return f.integrity, nil }
func (f *adminOpsRepo) DBSize(context.Context) (int64, error)          { return f.size, nil }
func (f *adminOpsRepo) ListSchemaMigrations(context.Context) ([]*domain.SchemaMigration, error) {
	return []*domain.SchemaMigration{}, nil
}

func (f *adminOpsRepo) SchemaDisplayTag(context.Context) (string, error) {
	return f.schemaTag, nil
}
func (f *adminOpsRepo) Optimize(context.Context) error { return nil }

type adminMetricRepo struct {
	lastAt time.Time
	hasAt  bool
}

func (f *adminMetricRepo) Inc(context.Context, string) error             { return nil }
func (f *adminMetricRepo) SetMax(context.Context, string, int) error     { return nil }
func (f *adminMetricRepo) SetGauge(context.Context, string, int64) error { return nil }
func (f *adminMetricRepo) SumSince(context.Context, string, int) (int, error) {
	return 0, nil
}
func (f *adminMetricRepo) MaxSince(context.Context, string, int) (int, error) {
	return 0, nil
}
func (f *adminMetricRepo) LatestAt(context.Context, string) (time.Time, bool, error) {
	return f.lastAt, f.hasAt, nil
}
func (f *adminMetricRepo) DeleteOlderThan(context.Context, string) (int64, error) { return 0, nil }

func (f *adminMetricRepo) DeleteGaugeOlderThan(context.Context, string) (int64, error) { return 0, nil }
func (f *adminMetricRepo) GaugeSeries(context.Context, []string, int) ([]domain.GaugePoint, error) {
	return nil, nil
}

type adminCursorRepo struct {
	byName map[string]*domain.MaintenanceCursor
}

func newAdminCursorRepo() *adminCursorRepo {
	return &adminCursorRepo{byName: map[string]*domain.MaintenanceCursor{}}
}

func (f *adminCursorRepo) Get(_ context.Context, n string) (*domain.MaintenanceCursor, error) {
	c, ok := f.byName[n]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *c
	return &cp, nil
}
func (f *adminCursorRepo) Upsert(_ context.Context, c *domain.MaintenanceCursor) error {
	cp := *c
	f.byName[c.TaskName] = &cp
	return nil
}
func (f *adminCursorRepo) List(context.Context) ([]*domain.MaintenanceCursor, error) { return nil, nil }

type adminTestEnv struct {
	echo    *echo.Echo
	ops     *adminOpsRepo
	metric  *adminMetricRepo
	cursor  *adminCursorRepo
	dataDir string
}

func newAdminTestEnv(t *testing.T) *adminTestEnv {
	t.Helper()
	ops := &adminOpsRepo{integrity: "ok", size: 12 << 20, schemaTag: "v1.0.0"}
	metric := &adminMetricRepo{hasAt: true, lastAt: time.Now().Add(-time.Hour)}
	cursor := newAdminCursorRepo()
	dataDir := t.TempDir()

	bk := backup.NewService(ops, metric, dataDir)
	hl := health.NewService(ops, metric, cursor, dataDir)

	e := echo.New()
	NewAdminHandler(bk, hl, service.NewSystemOpsService(ops, cursor), dataDir).
		RegisterRoutes(e.Group("/api"))
	return &adminTestEnv{echo: e, ops: ops, metric: metric, cursor: cursor, dataDir: dataDir}
}

func TestAdminRoutesRegistered(t *testing.T) {
	env := newAdminTestEnv(t)
	routes := map[string]bool{}
	for _, r := range env.echo.Router().Routes() {
		routes[r.Method+" "+r.Path] = true
	}
	for _, want := range []string{
		"POST /api/admin/backup",
		"GET /api/admin/backups",
		"GET /api/admin/health",
		"POST /api/admin/seed/ensure",
		"GET /api/admin/seed",
	} {
		require.True(t, routes[want], "missing route %s", want)
	}
}

func TestAdminCreateBackup(t *testing.T) {
	env := newAdminTestEnv(t)
	// The assets directory must exist for there to be copyable content
	require.NoError(t, os.MkdirAll(filepath.Join(env.dataDir, "assets", "pages"), 0o750))

	status, resp := doJSON(t, env.echo, http.MethodPost, "/api/admin/backup", "")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, float64(CodeOK), resp["code"])
	data := resp["data"].(map[string]any)
	require.Contains(t, []string{"daily", "weekly", "monthly"}, data["tier"])
	require.NotEmpty(t, data["file_name"], "the receipt must give the backup file name")
	require.NotEmpty(t, data["timestamp"])
	require.Equal(t, "ok", data["integrity"], "the backup must carry an integrity check conclusion")
	require.Greater(t, data["bytes"], float64(0))
	require.Equal(t, float64(0), data["pruned"])
	// DTO must not leak absolute paths
	blob, _ := json.Marshal(data)
	require.NotContains(t, string(blob), env.dataDir, "absolute paths must not appear in DTO")

	// The file really landed on disk
	dir := filepath.Join(env.dataDir, "backup", data["tier"].(string))
	ts := data["timestamp"].(string)
	_, err := os.Stat(filepath.Join(dir, "paperly_"+ts+".db"))
	require.NoError(t, err, "the backup DB must really land on disk")
	_, err = os.Stat(filepath.Join(dir, "paperly_"+ts+".manifest.json"))
	require.NoError(t, err, "the manifest must land together, otherwise backup integrity cannot be judged")
}

func TestAdminCreateBackupFailure(t *testing.T) {
	env := newAdminTestEnv(t)
	env.ops.verifyErr = domain.ErrInvalidInput
	status, resp := doJSON(t, env.echo, http.MethodPost, "/api/admin/backup", "")
	require.Equal(t, http.StatusInternalServerError, status)
	require.Equal(t, float64(CodeInternal), resp["code"])
	require.NotEmpty(t, resp["message"])
}

func TestAdminListBackups(t *testing.T) {
	env := newAdminTestEnv(t)
	env.metric.hasAt = false
	status, resp := doJSON(t, env.echo, http.MethodGet, "/api/admin/backups", "")
	require.Equal(t, http.StatusOK, status)
	data := resp["data"].(map[string]any)
	require.Equal(t, float64(0), data["total"])
	require.False(t, data["has_backup"].(bool), "has_backup=false is what means \"never backed up\"")

	env.metric.hasAt = true
	status, resp = doJSON(t, env.echo, http.MethodGet, "/api/admin/backups", "")
	require.Equal(t, http.StatusOK, status)
	data = resp["data"].(map[string]any)
	require.True(t, data["has_backup"].(bool))
	require.NotEmpty(t, data["last_backup_at"], "a time must be given when a backup exists")
}

func TestAdminHealth(t *testing.T) {
	env := newAdminTestEnv(t)
	status, resp := doJSON(t, env.echo, http.MethodGet, "/api/admin/health", "")
	require.Equal(t, http.StatusOK, status)
	data := resp["data"].(map[string]any)
	require.Equal(t, string(health.StatusWarn), data["status"], "no backup in this case -> warning")
	checks := data["checks"].([]any)
	require.Len(t, checks, 6)
	for _, raw := range checks {
		c := raw.(map[string]any)
		require.NotEmpty(t, c["key"])
		require.NotEmpty(t, c["title"])
		require.Contains(t, []any{"ok", "warn", "error"}, c["status"])
	}
	require.NotEmpty(t, data["summary"], "there must be a readable conclusion, not just a pile of raw counts")
}

func TestAdminHealthDegradesOnError(t *testing.T) {
	env := newAdminTestEnv(t)
	env.ops.integrity = "database disk image is malformed"
	status, resp := doJSON(t, env.echo, http.MethodGet, "/api/admin/health", "")
	require.Equal(t, http.StatusOK, status, "the health check itself is a read-only diagnostic and must be 200 even when it finds problems")
	require.Equal(t, string(health.StatusError), resp["data"].(map[string]any)["status"])
}

func TestAdminSeedInfo(t *testing.T) {
	env := newAdminTestEnv(t)
	status, resp := doJSON(t, env.echo, http.MethodGet, "/api/admin/seed", "")
	require.Equal(t, http.StatusOK, status)
	data := resp["data"].(map[string]any)
	require.Equal(t, float64(10), data["builtin_count"])
	require.Len(t, data["builtin_files"].([]any), 10)
}

func TestAdminSeedEnsure(t *testing.T) {
	env := newAdminTestEnv(t)
	// Seed a value the parent expects to keep
	require.NoError(t, os.MkdirAll(filepath.Join(env.dataDir, "seed"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(env.dataDir, "seed", "knowledge_edges_数学.json"),
		[]byte(`{"seed_version":"家长改的"}`), 0o600))

	status, resp := doJSON(t, env.echo, http.MethodPost, "/api/admin/seed/ensure", "")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, float64(CodeOK), resp["code"])

	blob, err := os.ReadFile(filepath.Join(env.dataDir, "seed", "knowledge_edges_数学.json"))
	require.NoError(t, err)
	require.Contains(t, string(blob), "家长改的", "ensure must never overwrite the parent's expected value")

	files, err := seed.BuiltinFiles()
	require.NoError(t, err)
	for _, f := range files {
		if f.Name == "knowledge_edges_数学.json" {
			continue
		}
		_, err := os.Stat(filepath.Join(env.dataDir, "seed", f.Name))
		require.NoError(t, err, f.Name)
	}
}

func TestParseBoolParam(t *testing.T) {
	for _, v := range []string{"", "0", "false", "no", "off"} {
		got, err := parseBoolParam(v)
		require.NoError(t, err)
		require.False(t, got, v)
	}
	for _, v := range []string{"1", "true", "yes", "on", "TRUE"} {
		got, err := parseBoolParam(v)
		require.NoError(t, err)
		require.True(t, got, v)
	}
	_, err := parseBoolParam("2")
	require.Error(t, err)
}

func TestSanitizeHeaderValue(t *testing.T) {
	require.Equal(t, "a b c", sanitizeHeaderValue("a\r\nb\nc"))
	require.NotContains(t, sanitizeHeaderValue("x\r\nSet-Cookie: y"), "\n")
}

func (m *adminOpsRepo) WithTx(tx domain.Tx) domain.OpsRepository { return m }

func (m *adminMetricRepo) WithTx(tx domain.Tx) domain.SystemMetricsRepository { return m }

func (m *adminCursorRepo) WithTx(tx domain.Tx) domain.MaintenanceCursorRepository { return m }
