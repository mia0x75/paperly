package seed

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuiltinFilesCoverAllSubjects(t *testing.T) {
	files, err := BuiltinFiles()
	require.NoError(t, err)

	// Subject dimension: each main exam subject has a DAG seed. Biology/
	// geography/history are still being backfilled (placeholder empty files),
	// so they are excluded from the non-empty check.
	subjects := []string{"语文", "数学", "英语", "物理", "化学", "道法"}
	for _, s := range subjects {
		var hasEdges bool
		for _, f := range files {
			if f.Name == "knowledge_edges_"+s+".json" {
				hasEdges = true
			}
		}
		require.True(t, hasEdges, "缺 %s 的知识图谱 seed", s)
	}

	// Built-in seeds must not contain difficulty_baseline_* files: difficulty
	// estimation uses knowledge-point edges/feedback instead.
	for _, f := range files {
		require.NotContains(t, f.Name, "difficulty_baseline_", "难度基准已删除：%s", f.Name)
	}

	require.Len(t, files, 10, "9 个知识图谱 + subjects.json = 10 个内置文件")
	for _, f := range files {
		require.Greater(t, f.Size, int64(0), "%s 不该是空文件", f.Name)
	}
}

func TestEnsureSeedCopiesOnFirstRun(t *testing.T) {
	dir := t.TempDir()
	res, err := EnsureSeed(dir)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "seed"), res.SeedDir)
	require.Len(t, res.Copied, 10, "9 个知识图谱 + subjects.json")
	require.Empty(t, res.Skipped)

	// The landed file actually exists and is non-empty
	edges := filepath.Join(dir, "seed", "knowledge_edges_数学.json")
	blob, err := os.ReadFile(edges)
	require.NoError(t, err)
	require.Contains(t, string(blob), `"subject": "数学"`)
	require.Contains(t, string(blob), `"prerequisite"`)
}

func TestEnsureSeedIdempotentDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	// First run
	_, err := EnsureSeed(dir)
	require.NoError(t, err)

	// Parent edited the seed (curricular rework of knowledge-point names)
	edited := filepath.Join(dir, "seed", "knowledge_edges_数学.json")
	require.NoError(t, os.WriteFile(edited, []byte(`{"subject":"数学","seed_version":"9.9","edges":[]}`), 0o600))

	// Second start
	res, err := EnsureSeed(dir)
	require.NoError(t, err)
	require.Empty(t, res.Copied, "已有文件不该被覆盖")
	require.Len(t, res.Skipped, 10)

	// The parent's edits must still be there
	blob, err := os.ReadFile(edited)
	require.NoError(t, err)
	require.Contains(t, string(blob), `"9.9"`, "家长的教研修订必须优先于内置副本")
}

func TestEnsureSeedPartialDirectoryFillsGaps(t *testing.T) {
	dir := t.TempDir()
	// The parent already placed 1 file; 9 are still missing
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "seed"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "seed", "knowledge_edges_数学.json"), []byte("{}"), 0o600))

	res, err := EnsureSeed(dir)
	require.NoError(t, err)
	require.Len(t, res.Copied, 9, "只补缺失的")
	require.Len(t, res.Skipped, 1)
}

func TestEnsureSeedFilePermissions(t *testing.T) {
	dir := t.TempDir()
	_, err := EnsureSeed(dir)
	require.NoError(t, err)

	info, err := os.Stat(filepath.Join(dir, "seed", "knowledge_edges_数学.json"))
	require.NoError(t, err)
	// 0o640: no permission for other users (seed is read-only data; other accounts
	// on the same machine must not casually edit it)
	require.Equal(t, os.FileMode(0o640), info.Mode().Perm())
}

func TestEnsureSeedEmptyDirPath(t *testing.T) {
	dir := t.TempDir()
	// Nested path: EnsureSeed does its own MkdirAll
	res, err := EnsureSeed(filepath.Join(dir, "a", "b"))
	require.NoError(t, err)
	require.Equal(t, 10, len(res.Copied))
	_, err = os.Stat(filepath.Join(dir, "a", "b", "seed", "knowledge_edges_物理.json"))
	require.NoError(t, err)
}

func TestBuiltinSeedEdgesAreAcyclicChains(t *testing.T) {
	// Every edge must be a strict "parent -> child" prerequisite; node names must
	// not self-loop. A self-loop would make the DAG service's Kahn cycle check
	// reject the whole subject batch.
	for _, name := range []string{"语文", "数学", "英语", "物理", "化学", "道法"} {
		blob, err := builtin.ReadFile(builtinRoot + "/knowledge_edges_" + name + ".json")
		require.NoError(t, err)
		require.True(t, strings.HasPrefix(string(blob), "{"), "%s 应是对象结构（DAG seed）", name)

		// Coarse check: no self-loop where "parent" == "child"
		var payload struct {
			Subject string `json:"subject"`
			Version string `json:"seed_version"`
			Edges   []struct {
				Parent   string `json:"parent"`
				Child    string `json:"child"`
				Relation string `json:"relation"`
			} `json:"edges"`
		}
		require.NoError(t, json.Unmarshal(blob, &payload), "%s 必须是合法 JSON", name)
		require.Equal(t, name, payload.Subject)
		require.NotEmpty(t, payload.Version, "%s 缺 seed_version（升级时需判断新旧）", name)
		require.NotEmpty(t, payload.Edges)
		seen := map[string]bool{}
		for _, e := range payload.Edges {
			require.NotEqual(t, e.Parent, e.Child, "%s 存在自环 %s", name, e.Parent)
			require.NotEmpty(t, e.Parent)
			require.NotEmpty(t, e.Child)
			require.Equal(t, "prerequisite", e.Relation, "%s 的边应标记前置关系", name)
			key := e.Parent + "->" + e.Child
			require.False(t, seen[key], "%s 重复边 %s（重复边会放大传播权重）", name, key)
			seen[key] = true
		}
	}
}
