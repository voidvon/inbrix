// handlers/api/threadstore.go — durable message-header cache for JWZ threading.
//
// The store persists per-folder header metadata (UID → msgMeta JSON) in a
// shared KV store so that threads can be rebuilt over a larger window than the
// current 50-message IMAP page.
//
// Usage:
//
//	store := NewThreadStore(kv, account)
//	threads, err := store.BuildThreads(folder, emails)
//
// If the DB is missing, corrupt, or otherwise unusable BuildThreads falls back
// to threading just the in-memory emails.
package api

import (
	"encoding/json"
	"inbrix/models"
	"inbrix/storage"
	"log"
	"time"
)

// msgMeta is the slim record persisted per message in the durable store.
type msgMeta struct {
	MessageID  string    `json:"mid,omitempty"`
	InReplyTo  string    `json:"irt,omitempty"`
	References []string  `json:"refs,omitempty"`
	Subject    string    `json:"subj,omitempty"`
	From       string    `json:"from,omitempty"`
	FromName   string    `json:"fromName,omitempty"`
	Date       time.Time `json:"date"`
	Preview    string    `json:"preview,omitempty"`
	Flags      []string  `json:"flags,omitempty"`
}

type ThreadStore struct {
	kv    storage.KV
	scope string
	owned bool
}

func NewThreadStore(kv storage.KV, scope string) *ThreadStore {
	return &ThreadStore{kv: kv, scope: scope}
}

func (s *ThreadStore) Close() error {
	if s.owned {
		return s.kv.Close()
	}
	return nil
}

func (s *ThreadStore) namespace(folder string) string {
	return "threads:" + s.scope + ":" + folder
}

// BuildThreads upserts emails into the store, loads all cached headers for
// the folder, runs ThreadMessages over the union, and returns threads.
// folder is the IMAP folder name used as the bucket name.
//
// On any DB error the function logs the error and falls back to threading
// only the supplied in-memory emails.
func (s *ThreadStore) BuildThreads(folder string, emails []models.Email) ([]models.Thread, error) {
	ns := s.namespace(folder)
	var upsertErr error
	for i := range emails {
		e := &emails[i]
		uid := e.ID
		if uid == "" {
			continue
		}
		meta := msgMeta{
			MessageID:  e.MessageID,
			InReplyTo:  e.InReplyTo,
			References: e.References,
			Subject:    e.Subject,
			From:       e.From,
			FromName:   e.FromName,
			Date:       e.Date,
			Preview:    e.Preview,
			Flags:      e.Flags,
		}
		raw, err := json.Marshal(meta)
		if err != nil {
			continue
		}
		if err := s.kv.Set(ns, uid, raw); err != nil {
			upsertErr = err
			break
		}
	}

	if upsertErr != nil {
		log.Printf("threadstore: upsert %s/%s: %v", s.scope, folder, upsertErr)
		// Still attempt to read whatever was previously stored.
	}

	// ---- load all cached headers for this folder ------------------------
	// Build a set of UIDs already in the in-memory slice so we use the
	// richer in-memory version (with body, attachments etc.) when available.
	inMem := make(map[string]int, len(emails)) // uid → index in emails
	for i := range emails {
		inMem[emails[i].ID] = i
	}

	var union []models.Email
	// Seed with in-memory emails first (they have the most fields populated).
	union = append(union, emails...)

	stored, readErr := s.kv.List(ns, "")
	if readErr == nil {
		for uid, v := range stored {
			if _, ok := inMem[uid]; ok {
				continue
			}
			var meta msgMeta
			if err := json.Unmarshal(v, &meta); err != nil {
				continue
			}
			union = append(union, models.Email{
				ID:         uid,
				MessageID:  meta.MessageID,
				InReplyTo:  meta.InReplyTo,
				References: meta.References,
				Subject:    meta.Subject,
				From:       meta.From,
				FromName:   meta.FromName,
				Date:       meta.Date,
				Preview:    meta.Preview,
				Flags:      meta.Flags,
			})
		}
	}
	if readErr != nil {
		log.Printf("threadstore: read %s/%s: %v — using in-memory only", s.scope, folder, readErr)
		return ThreadMessages(emails), nil
	}

	return ThreadMessages(union), nil
}
