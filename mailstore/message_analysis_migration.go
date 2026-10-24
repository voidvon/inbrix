package mailstore

import (
	"context"
	"database/sql"
	"fmt"
)

func (s *Store) ensureMessageAnalysisColumn(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(message_summaries)`)
	if err != nil {
		return err
	}
	hasColumn := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		hasColumn = hasColumn || name == "analysis_json"
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if hasColumn {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE message_summaries ADD COLUMN analysis_json TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("mailstore: add analysis column: %w", err)
	}
	return nil
}
