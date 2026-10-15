package worker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
)

func dataDirWithExam(t *testing.T, id string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "assets", "exams", id)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "page_1.jpg"), []byte("x"), 0o644))
	return filepath.Dir(filepath.Dir(filepath.Dir(dir)))
}

func TestCleanupRemovesDir(t *testing.T) {
	base := dataDirWithExam(t, "14")
	deps := testDeps()
	deps.DataDir = base

	err := handleCleanupStorage(context.Background(), deps,
		&domain.Task{ID: 1, Kind: domain.TaskKindCleanupStorage, Detail: `{"dir":"assets/exams/14"}`})
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(base, "assets", "exams", "14"))
	require.True(t, os.IsNotExist(err), "试卷目录应被整个移除")
}

func TestCleanupIdempotentOnMissingDir(t *testing.T) {
	deps := testDeps()
	deps.DataDir = t.TempDir()

	err := handleCleanupStorage(context.Background(), deps,
		&domain.Task{ID: 2, Kind: domain.TaskKindCleanupStorage, Detail: `{"dir":"assets/exams/99"}`})
	require.NoError(t, err, "目录不存在视为成功，重跑安全")
}

func TestCleanupRejectsPathTraversal(t *testing.T) {
	deps := testDeps()
	deps.DataDir = t.TempDir()

	for _, dir := range []string{
		"../../etc",            // prefix entirely wrong
		"assets/exams/../evil", // escapes after Clean
		"/etc/passwd",          // absolute path
		"assets/exams",         // the root directory itself is not cleaned
	} {
		err := handleCleanupStorage(context.Background(), deps,
			&domain.Task{ID: 3, Kind: domain.TaskKindCleanupStorage, Detail: `{"dir":"` + dir + `"}`})
		requireNonRetryable(t, err)
	}
	if _, err := examDirWithinAssets("assets/exams/1"); err != nil {
		t.Fatalf("正常子目录应放行: %v", err)
	}
}

func TestCleanupMalformedDetail(t *testing.T) {
	deps := testDeps()

	err := handleCleanupStorage(context.Background(), deps,
		&domain.Task{ID: 4, Kind: domain.TaskKindCleanupStorage, Detail: "nope"})
	requireNonRetryable(t, err)
}
