package main

import (
	"inbrix/config"
	"path/filepath"
	"testing"
)

func TestResolveRuntimePathsAt(t *testing.T) {
	baseDir := t.TempDir()
	cfg := &config.Config{}
	cfg.Cache.Folder = "./data"
	cfg.MailSync.Database = "./data/mail.db"
	cfg.Notifications.VAPIDKeyFile = "vapid.json"
	cfg.Accounts.StoreFile = "accounts.db"

	resolveRuntimePathsAt(cfg, baseDir)

	wantData := filepath.Join(baseDir, "data")
	checks := map[string]struct {
		got  string
		want string
	}{
		"data directory": {cfg.Cache.Folder, wantData},
		"mail database":  {cfg.MailSync.Database, filepath.Join(wantData, "mail.db")},
		"VAPID keys":     {cfg.Notifications.VAPIDKeyFile, filepath.Join(wantData, "vapid.json")},
		"account store":  {cfg.Accounts.StoreFile, filepath.Join(wantData, "accounts.db")},
	}
	for name, check := range checks {
		if check.got != check.want {
			t.Errorf("%s = %q, want %q", name, check.got, check.want)
		}
	}
}

func TestResolveRuntimePathsAtPreservesAbsolutePaths(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(t.TempDir(), "custom-data")
	database := filepath.Join(t.TempDir(), "mail.db")
	cfg := &config.Config{}
	cfg.Cache.Folder = dataDir
	cfg.MailSync.Database = database

	resolveRuntimePathsAt(cfg, baseDir)

	if cfg.Cache.Folder != dataDir {
		t.Fatalf("absolute data directory changed to %q", cfg.Cache.Folder)
	}
	if cfg.MailSync.Database != database {
		t.Fatalf("absolute database path changed to %q", cfg.MailSync.Database)
	}
}

func TestResolveRuntimePathsUsesRuntimeDirectoryOverride(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("INBRIX_RUNTIME_DIR", baseDir)
	cfg := &config.Config{}
	cfg.Cache.Folder = "./data"
	cfg.MailSync.Database = "./data/mail.db"

	resolveRuntimePaths(cfg)

	if cfg.Cache.Folder != filepath.Join(baseDir, "data") {
		t.Fatalf("data directory = %q, want it under runtime override %q", cfg.Cache.Folder, baseDir)
	}
	if cfg.MailSync.Database != filepath.Join(baseDir, "data", "mail.db") {
		t.Fatalf("mail database = %q, want it under runtime override %q", cfg.MailSync.Database, baseDir)
	}
}
