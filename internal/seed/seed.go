// Package seed bundles built-in sample data.
//
// Rationale: the Box edition is a "copy and use" single-machine app, so a parent
// should not have to locate a knowledge graph before mastery propagation can be
// seen. For this reason the built-in knowledge-graph DAG (knowledge_edges_*.json)
// and preset subjects (subjects.json) are embedded into the binary via embed.FS,
// auto-landed under <dataDir>/seed/ on first start, then imported normally by
// DAGService.
//
// Why not feed embed.FS directly to the import logic: the seed files are also
// **parent-editable artifacts** — after curricular rework of knowledge-point
// names, what is edited is <dataDir>/seed/*.json. Keeping the "built-in copy"
// separate from the "runtime directory" makes the parent's edited copy the
// authoritative one, so a future upgrade is not silently overwritten.
package seed

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed data/*.json
var builtin embed.FS

const builtinRoot = "data"

// filePerm is the on-disk permission: seed files are read-only; 0o640 keeps
// other users on the same machine from casually editing them.
const filePerm = 0o640

const dirPerm = 0o750

type FileMeta struct {
	Name string
	Size int64
}

// BuiltinFiles returns the list of built-in seed files (for health/debugging).
func BuiltinFiles() ([]FileMeta, error) {
	entries, err := fs.ReadDir(builtin, builtinRoot)
	if err != nil {
		return nil, fmt.Errorf("read builtin seed: %w", err)
	}
	out := make([]FileMeta, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, fmt.Errorf("stat builtin seed %s: %w", e.Name(), err)
		}
		out = append(out, FileMeta{Name: e.Name(), Size: info.Size()})
	}
	return out, nil
}

type EnsureResult struct {
	SeedDir string   `json:"seed_dir"`
	Copied  []string `json:"copied"`
	Skipped []string `json:"skipped"`
}

// EnsureSeed guarantees usable seed data exists under <dataDir>/seed/.
//
// Idempotency strategy (key: **never overwrite files the parent has edited**):
//   - File already exists -> skip. Even if the built-in version was updated it
//     is not overwritten: the parent's curricular revisions take precedence.
//     Ship new seeds as "new files" or via manual parent merge.
//   - File missing -> copy from embed.FS.
//
// Rationale: silently overwriting parent-edited seeds is far more harmful than
// not shipping new seeds.
func EnsureSeed(dataDir string) (*EnsureResult, error) {
	seedDir := filepath.Join(dataDir, "seed")
	if err := os.MkdirAll(seedDir, dirPerm); err != nil {
		return nil, fmt.Errorf("create seed dir: %w", err)
	}

	entries, err := fs.ReadDir(builtin, builtinRoot)
	if err != nil {
		return nil, fmt.Errorf("read builtin seed: %w", err)
	}

	res := &EnsureResult{SeedDir: seedDir}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		target := filepath.Join(seedDir, e.Name())
		if _, err := os.Stat(target); err == nil {
			res.Skipped = append(res.Skipped, e.Name())
			continue
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("stat seed file %s: %w", target, err)
		}
		if err := copyEmbedded(builtinRoot+"/"+e.Name(), target); err != nil {
			return nil, err
		}
		res.Copied = append(res.Copied, e.Name())
	}
	return res, nil
}

func copyEmbedded(name, target string) error {
	blob, err := builtin.ReadFile(name)
	if err != nil {
		return fmt.Errorf("read builtin seed %s: %w", name, err)
	}
	if err := os.WriteFile(target, blob, filePerm); err != nil {
		return fmt.Errorf("write seed file %s: %w", target, err)
	}
	return nil
}
