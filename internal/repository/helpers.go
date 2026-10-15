package repository

import (
	"database/sql"
	"fmt"

	"paperly/internal/domain"
)

// affectedOrNotFound maps RowsAffected==0 to domain.ErrNotFound.
func affectedOrNotFound(res sql.Result, entity string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected for %s: %w", entity, err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}
