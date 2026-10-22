package mailstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// AIChatContext stores derived memory only. Full chat history and original
// emails remain authoritative and are never replaced by the summary.
type AIChatContext struct {
	HistoryHash    string
	HistoryCount   int
	HistorySummary string
	MailKeys       string
}

func (s *Store) GetAIChatContext(ctx context.Context, accountID, conversationID string) (AIChatContext, error) {
	var record AIChatContext
	err := s.db.QueryRowContext(ctx, `SELECT history_hash, history_count, history_summary, mail_keys FROM conversation_ai_context WHERE account_id = ? AND conversation_id = ?`, accountID, conversationID).
		Scan(&record.HistoryHash, &record.HistoryCount, &record.HistorySummary, &record.MailKeys)
	if errors.Is(err, sql.ErrNoRows) {
		return record, nil
	}
	return record, err
}

func (s *Store) SaveAIChatContext(ctx context.Context, accountID, conversationID string, record AIChatContext) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO conversation_ai_context(account_id, conversation_id, history_hash, history_count, history_summary, mail_keys)
		VALUES(?, ?, ?, ?, ?, ?) ON CONFLICT(account_id, conversation_id) DO UPDATE SET
		history_hash=excluded.history_hash, history_count=excluded.history_count, history_summary=excluded.history_summary, mail_keys=excluded.mail_keys`,
		accountID, conversationID, record.HistoryHash, record.HistoryCount, record.HistorySummary, record.MailKeys)
	if err != nil {
		return fmt.Errorf("mailstore: save AI chat context: %w", err)
	}
	return nil
}

func (s *Store) DeleteAIChatContext(ctx context.Context, accountID, conversationID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM conversation_ai_context WHERE account_id = ? AND conversation_id = ?`, accountID, conversationID)
	return err
}
