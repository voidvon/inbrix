package mailstore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAPIKeysLifecycleAndUserPermissions(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	alice, err := s.CreateUser(ctx, "key-alice", "Alice", "hash")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := s.CreateUser(ctx, "key-bob", "Bob", "hash")
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	key, err := s.CreateAPIKey(ctx, alice.ID, "  automation  ", &expires, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if key.Name != "automation" || !strings.HasPrefix(key.Key, APIKeyPrefix) || len(key.Key) != len(APIKeyPrefix)+43 {
		t.Fatal("invalid generated credential")
	}
	var storedHash string
	if err := s.db.QueryRowContext(ctx, `SELECT key_hash FROM user_api_keys WHERE id = ?`, key.ID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if storedHash == key.Key || storedHash != hashAPIKey(key.Key) {
		t.Fatal("credential was not stored as a hash")
	}
	user, id, err := s.AuthenticateAPIKey(ctx, key.Key, "10.0.0.1")
	if err != nil || user.ID != alice.ID || id != key.ID || user.Role != RoleUser {
		t.Fatalf("key did not resolve its owner: %v", err)
	}
	if err := s.SetUserRole(ctx, alice.ID, RoleSuperAdmin); err != nil {
		t.Fatal(err)
	}
	user, _, err = s.AuthenticateAPIKey(ctx, key.Key, "10.0.0.1")
	if err != nil || user.Role != RoleSuperAdmin {
		t.Fatal("existing key did not inherit a role change")
	}
	if err := s.SetUserRole(ctx, alice.ID, RoleUser); err != nil {
		t.Fatal(err)
	}
	user, _, err = s.AuthenticateAPIKey(ctx, key.Key, "10.0.0.1")
	if err != nil || user.Role != RoleUser {
		t.Fatal("existing key retained privileges after demotion")
	}
	keys, err := s.ListAPIKeys(ctx, alice.ID)
	if err != nil || len(keys) != 1 || keys[0].LastUsedAt == nil || keys[0].LastUsedIP != "10.0.0.1" {
		t.Fatalf("missing key usage metadata: %+v %v", keys, err)
	}
	raw, _ := json.Marshal(keys)
	if strings.Contains(string(raw), key.Key) || strings.Contains(string(raw), storedHash) {
		t.Fatal("key list exposed a secret or hash")
	}
	others, err := s.ListAPIKeys(ctx, bob.ID)
	if err != nil || len(others) != 0 {
		t.Fatal("another user's keys were visible")
	}
	if _, err := s.RotateAPIKey(ctx, bob.ID, key.ID, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user rotation: %v", err)
	}
	if err := s.RevokeAPIKey(ctx, bob.ID, key.ID, "", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user revocation: %v", err)
	}
	if _, err := s.ListAPIKeyEvents(ctx, bob.ID, key.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user event access: %v", err)
	}
	rotated, err := s.RotateAPIKey(ctx, alice.ID, key.ID, "10.0.0.2")
	if err != nil || rotated.Key == key.Key || rotated.ID == key.ID || rotated.ExpiresAt == nil || !rotated.ExpiresAt.Equal(expires) {
		t.Fatalf("rotation failed or changed expiration: %v", err)
	}
	if _, _, err := s.AuthenticateAPIKey(ctx, key.Key, ""); !errors.Is(err, ErrNotFound) {
		t.Fatal("old credential still works after rotation")
	}
	if _, _, err := s.AuthenticateAPIKey(ctx, rotated.Key, ""); err != nil {
		t.Fatal("rotated credential does not work")
	}
	if _, err := s.RotateAPIKey(ctx, alice.ID, key.ID, ""); !errors.Is(err, ErrAPIKeyInactive) {
		t.Fatal("revoked credential can be rotated again")
	}
	events, err := s.ListAPIKeyEvents(ctx, alice.ID, key.ID)
	if err != nil || len(events) != 2 || events[0].Type != "rotated" || events[0].RelatedKeyID != rotated.ID || events[1].Type != "created" {
		t.Fatalf("rotation audit trail: %+v %v", events, err)
	}
	if err := s.RevokeAPIKey(ctx, alice.ID, rotated.ID, "10.0.0.3", "replaced integration"); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeAPIKey(ctx, alice.ID, rotated.ID, "10.0.0.3", "repeat"); err != nil {
		t.Fatal("revocation should be idempotent")
	}
	if _, _, err := s.AuthenticateAPIKey(ctx, rotated.Key, ""); !errors.Is(err, ErrNotFound) {
		t.Fatal("revoked credential is still accepted")
	}
	events, err = s.ListAPIKeyEvents(ctx, alice.ID, rotated.ID)
	if err != nil || len(events) != 2 || events[0].Type != "revoked" || events[0].Reason != "replaced integration" {
		t.Fatal("revocation audit trail was lost or duplicated")
	}
}

func TestAPIKeyValidationExpiryAndDeletedOwner(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	user, err := s.CreateUser(ctx, "key-validation", "Key test", "hash")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "   ", strings.Repeat("名", 121)} {
		if _, err := s.CreateAPIKey(ctx, user.ID, name, nil, ""); !errors.Is(err, ErrInvalidAPIKeyInput) {
			t.Fatalf("invalid name accepted: %v", err)
		}
	}
	past := time.Now().Add(-time.Hour)
	if _, err := s.CreateAPIKey(ctx, user.ID, "expired", &past, ""); !errors.Is(err, ErrInvalidAPIKeyInput) {
		t.Fatal("past expiration accepted")
	}
	if _, err := s.CreateAPIKey(ctx, "missing-user", "orphan", nil, ""); !errors.Is(err, ErrNotFound) {
		t.Fatal("orphan credential created")
	}
	key, err := s.CreateAPIKey(ctx, user.ID, "expiry", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"", "wrong", key.Key + "extra", APIKeyPrefix + strings.Repeat("a", 43)} {
		if _, _, err := s.AuthenticateAPIKey(ctx, token, ""); !errors.Is(err, ErrNotFound) {
			t.Fatal("invalid credential accepted")
		}
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE user_api_keys SET expires_at = ? WHERE id = ?`, past.Unix(), key.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AuthenticateAPIKey(ctx, key.Key, ""); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired credential accepted")
	}
	if _, err := s.RotateAPIKey(ctx, user.ID, key.ID, ""); !errors.Is(err, ErrAPIKeyInactive) {
		t.Fatal("expired credential can be rotated")
	}
	keys, err := s.ListAPIKeys(ctx, user.ID)
	if err != nil || keys[0].Status != "expired" {
		t.Fatal("expired status is incorrect")
	}
	active, err := s.CreateAPIKey(ctx, user.ID, "delete-owner", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, user.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AuthenticateAPIKey(ctx, active.Key, ""); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted user can still authenticate")
	}
	var remaining int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_api_keys WHERE owner_id = ?`, user.ID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("deleting a user left their credentials behind")
	}
}
