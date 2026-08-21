package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"

	"inbrix/storage"
)

type AccountEntry struct {
	Email             string `json:"email"`
	Label             string `json:"label"`
	Color             string `json:"color,omitempty"`
	IMAPServer        string `json:"imap_server"`
	IMAPPort          int    `json:"imap_port"`
	SMTPServer        string `json:"smtp_server"`
	SMTPPort          int    `json:"smtp_port"`
	EncryptedPassword string `json:"encrypted_password"`
}

type AccountStore struct {
	kv storage.KV
}

func NewAccountStore(kv storage.KV) *AccountStore { return &AccountStore{kv: kv} }

func (s *AccountStore) Close() error                  { return nil }
func (s *AccountStore) namespace(owner string) string { return "accounts:" + owner }

func (s *AccountStore) Save(owner string, entry AccountEntry) error {
	raw, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("accountstore: marshal: %w", err)
	}
	return s.kv.Set(s.namespace(owner), entry.Email, raw)
}

func (s *AccountStore) Get(owner, email string) (AccountEntry, error) {
	raw, err := s.kv.Get(s.namespace(owner), email)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return AccountEntry{}, storage.ErrNotFound
		}
		return AccountEntry{}, err
	}
	var entry AccountEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return AccountEntry{}, fmt.Errorf("accountstore: unmarshal: %w", err)
	}
	return entry, nil
}

func (s *AccountStore) Delete(owner, email string) error {
	return s.kv.Delete(s.namespace(owner), email)
}

func (s *AccountStore) List(owner string) ([]AccountEntry, error) {
	values, err := s.kv.List(s.namespace(owner), "")
	if err != nil {
		return nil, err
	}
	entries := make([]AccountEntry, 0, len(values))
	for _, value := range values {
		var entry AccountEntry
		if err := json.Unmarshal(value, &entry); err != nil {
			log.Printf("accountstore: unmarshal entry: %v", err)
			continue
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Email < entries[j].Email })
	return entries, nil
}
