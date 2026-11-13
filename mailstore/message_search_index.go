package mailstore

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

// The trigram index narrows substring searches before the existing SQL checks
// run. External content avoids storing a second copy of message bodies. Keep
// the original predicates for short terms, LIKE wildcards and exact semantics.
func (s *Store) migrateMessageSearch(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='messages_search')`).Scan(&exists); err != nil {
		return err
	}
	statements := []string{
		`CREATE VIRTUAL TABLE IF NOT EXISTS messages_search USING fts5(
			subject, from_addr, to_addrs, cc, preview, body, html,
			content='messages', content_rowid='rowid', tokenize='trigram', detail='column')`,
		`CREATE TRIGGER IF NOT EXISTS messages_search_insert AFTER INSERT ON messages BEGIN
			INSERT INTO messages_search(rowid, subject, from_addr, to_addrs, cc, preview, body, html)
			VALUES(new.rowid, new.subject, new.from_addr, new.to_addrs, new.cc, new.preview, new.body, new.html);
		END`,
		`CREATE TRIGGER IF NOT EXISTS messages_search_delete AFTER DELETE ON messages BEGIN
			INSERT INTO messages_search(messages_search, rowid, subject, from_addr, to_addrs, cc, preview, body, html)
			VALUES('delete', old.rowid, old.subject, old.from_addr, old.to_addrs, old.cc, old.preview, old.body, old.html);
		END`,
		`CREATE TRIGGER IF NOT EXISTS messages_search_update
		AFTER UPDATE OF subject, from_addr, to_addrs, cc, preview, body, html ON messages
		WHEN old.subject IS NOT new.subject OR old.from_addr IS NOT new.from_addr
			OR old.to_addrs IS NOT new.to_addrs OR old.cc IS NOT new.cc
			OR old.preview IS NOT new.preview OR old.body IS NOT new.body OR old.html IS NOT new.html
		BEGIN
			INSERT INTO messages_search(messages_search, rowid, subject, from_addr, to_addrs, cc, preview, body, html)
			VALUES('delete', old.rowid, old.subject, old.from_addr, old.to_addrs, old.cc, old.preview, old.body, old.html);
			INSERT INTO messages_search(rowid, subject, from_addr, to_addrs, cc, preview, body, html)
			VALUES(new.rowid, new.subject, new.from_addr, new.to_addrs, new.cc, new.preview, new.body, new.html);
		END`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("mailstore: migrate message search: %w", err)
		}
	}
	if !exists {
		// Populate existing mirrors once, in the same transaction as the triggers.
		if _, err := tx.ExecContext(ctx, `INSERT INTO messages_search(messages_search) VALUES('rebuild')`); err != nil {
			return fmt.Errorf("mailstore: build message search index: %w", err)
		}
	}
	return tx.Commit()
}

func (p *parsedSearch) addIndexTerm(term, column string, negated bool) {
	// Negated/short terms cannot select candidates. Wildcards retain their
	// historical LIKE meaning; NUL and invalid UTF-8 must use SQLite directly.
	if negated || !utf8.ValidString(term) || utf8.RuneCountInString(term) < 3 || strings.ContainsAny(term, "%_\x00") {
		return
	}
	// Positions are unnecessary because SQL verifies the complete substring.
	// AND individual trigrams to avoid expensive phrase-position merges for
	// common words in long/repetitive bodies. Cap the number of probes; any
	// subset remains a safe candidate filter for longer terms.
	runes := []rune(term)
	trigrams := make([]string, 0, 32)
	seen := make(map[string]bool)
	for i := 0; i+2 < len(runes) && len(trigrams) < 32; i++ {
		trigram := string(runes[i : i+3])
		if !seen[trigram] {
			seen[trigram] = true
			trigrams = append(trigrams, `"`+strings.ReplaceAll(trigram, `"`, `""`)+`"`)
		}
	}
	phrase := "(" + strings.Join(trigrams, " AND ") + ")"
	if column != "" {
		phrase = column + " : " + phrase
	}
	p.indexTerms = append(p.indexTerms, phrase)
}
