package mailstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"inbrix/models"
)

// Retrieval is a dependency manifest, not a claim that all matching bodies were
// read. Sources.Ranges remains the authority for actual reading coverage.
type MailAnalysisRetrieval struct {
	Version   int                 `json:"version"`
	ScopeHash string              `json:"scopeHash"`
	Queries   []MailAnalysisQuery `json:"queries"`
}

type MailAnalysisQuery struct {
	Scope       string `json:"scope"`
	Query       string `json:"query"`
	Fingerprint string `json:"fingerprint"`
}

// Stream a stable header projection over the entire matching set, including
// matches outside pages shown to the model. Read flags do not affect this hash.
// The existing query parser and account/as-of/UID filters are shared with reads.
func mailAnalysisHeaderFingerprint(ctx context.Context, store *Store, clauses []string, args []any) (string, error) {
	rows, err := store.db.QueryContext(ctx, "SELECT "+messageHeaderColumns+" FROM messages WHERE "+strings.Join(clauses, " AND ")+" ORDER BY folder_name,uid", args...)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	h := sha256.New()
	encoder := json.NewEncoder(h)
	for rows.Next() {
		mail, err := scanMessageHeader(rows)
		if err != nil {
			return "", err
		}
		if err := encoder.Encode([]any{analysisMailPreview(mail), mail.BodyCached, mail.Attachments, mail.References}); err != nil {
			return "", err
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (scope *mailAnalysisScope) queryFingerprint(ctx context.Context, requested, query string) (string, error) {
	keys := scope.thread
	switch requested {
	case "conversation":
		keys = scope.conversation
	case "mailbox":
		keys = nil
	}
	clauses, args := messageSearchConditions(scope.reader.AccountID, query, keys, scope.reader.AsOf, scope.reader.Anchor)
	return mailAnalysisHeaderFingerprint(ctx, scope.reader.Store, clauses, args)
}

func (scope *mailAnalysisScope) recordQuery(ctx context.Context, requested, query string) error {
	fingerprint, err := scope.queryFingerprint(ctx, requested, query)
	if err != nil {
		return err
	}
	for _, old := range scope.retrieval.Queries {
		if old.Scope == requested && old.Query == query {
			if old.Fingerprint != fingerprint {
				return errors.New("mail search results changed during analysis; retry")
			}
			return nil
		}
	}
	scope.retrieval.Queries = append(scope.retrieval.Queries, MailAnalysisQuery{Scope: requested, Query: query, Fingerprint: fingerprint})
	return nil
}

func currentMailAnalysisRetrieval(ctx context.Context, store *Store, account Account, message models.Email, previous MailAnalysisRetrieval) (MailAnalysisRetrieval, error) {
	scope, err := newMailAnalysisScope(ctx, store, account, message)
	if err != nil {
		return MailAnalysisRetrieval{}, err
	}
	for _, query := range previous.Queries {
		if err := scope.recordQuery(ctx, query.Scope, query.Query); err != nil {
			return MailAnalysisRetrieval{}, err
		}
	}
	return scope.retrieval, nil
}

func mailAnalysisCompleteSourceHash(account Account, message models.Email, sources []MailAnalysisSource, retrieval MailAnalysisRetrieval) string {
	base := mailAnalysisSourceHash(account, message, sources)
	if retrieval.Version == 0 {
		return base
	}
	raw, _ := json.Marshal(retrieval)
	return hashMailSummaryValue(base + "\x00" + string(raw))
}
