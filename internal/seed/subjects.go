package seed

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"paperly/internal/domain"
)

// subjectSeed is the on-disk shape of one subjects.json row. A dedicated struct
// (rather than unmarshalling straight into domain.Subject) is required because
// the domain layer forbids json tags (ADR 0002) and encoding/json cannot map
// snake_case keys like sort_order to CamelCase fields.
type subjectSeed struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
	Mask      int    `json:"mask"`
	Mandatory int    `json:"mandatory"`
}

// LoadSubjects reads the subject dictionary from <dataDir>/seed/subjects.json.
// A missing file yields an empty slice (the first start is handled by EnsureSeed).
func LoadSubjects(_ context.Context, dataDir string) ([]*domain.Subject, error) {
	path := filepath.Join(dataDir, "seed", "subjects.json")
	blob, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []*domain.Subject{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read subjects seed: %w", err)
	}
	var rows []subjectSeed
	if err := json.Unmarshal(blob, &rows); err != nil {
		return nil, fmt.Errorf("parse subjects seed: %w", err)
	}
	out := make([]*domain.Subject, 0, len(rows))
	for _, r := range rows {
		mask := r.Mask
		if mask == 0 {
			mask = domain.MaskAll
		}
		out = append(out, &domain.Subject{
			Code:      r.Code,
			Name:      r.Name,
			SortOrder: r.SortOrder,
			Source:    domain.SubjectSourceSystem,
			GradeMask: mask,
			Mandatory: r.Mandatory,
		})
	}
	return out, nil
}
