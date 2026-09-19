package storage

import (
	"bytes"
	"path/filepath"
	"testing"

	"inbrix/config"
)

// Exercises the KV contract against the default SQLite backend. The Postgres
// backend satisfies the same interface and is covered by integration tests
// where a database is available.
func TestSQLiteKVRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kv.db")
	kv, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer kv.Close()

	if _, err := kv.Get("threads", "missing"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}

	if err := kv.Set("threads", "a", []byte("1")); err != nil {
		t.Fatal(err)
	}
	if err := kv.Set("threads", "ab", []byte("2")); err != nil {
		t.Fatal(err)
	}
	if err := kv.Set("recipients", "a", []byte("x")); err != nil {
		t.Fatal(err)
	}

	v, err := kv.Get("threads", "a")
	if err != nil || !bytes.Equal(v, []byte("1")) {
		t.Fatalf("get a: %q %v", v, err)
	}

	// List honours namespace isolation and prefix.
	all, err := kv.List("threads", "")
	if err != nil || len(all) != 2 {
		t.Fatalf("list threads: %v len=%d", err, len(all))
	}
	pre, err := kv.List("threads", "ab")
	if err != nil || len(pre) != 1 {
		t.Fatalf("list prefix ab: %v len=%d", err, len(pre))
	}

	if err := kv.Delete("threads", "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := kv.Get("threads", "a"); err != ErrNotFound {
		t.Fatalf("after delete want ErrNotFound, got %v", err)
	}
}

// Open() must default to SQLite when no backend is configured, preserving the
// standalone single-binary behaviour.
func TestOpenDefaultsToSQLite(t *testing.T) {
	cfg := &config.Config{} // empty: Storage.Backend == ""
	kv, err := Open(cfg, filepath.Join(t.TempDir(), "kv.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer kv.Close()
	if err := kv.Set("ns", "k", []byte("v")); err != nil {
		t.Fatal(err)
	}
}

func TestOpenWithDBReusesConnectionPool(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.db")
	kv1, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer kv1.Close()

	sqlKV, ok := kv1.(*sqliteKV)
	if !ok {
		t.Fatal("expected *sqliteKV")
	}

	cfg := &config.Config{}
	kv2, err := OpenWithDB(cfg, path, sqlKV.db)
	if err != nil {
		t.Fatal(err)
	}
	// Closing unowned kv2 must not close shared DB
	if err := kv2.Close(); err != nil {
		t.Fatal(err)
	}

	// kv1 should still be completely functional
	if err := kv1.Set("shared_ns", "key", []byte("value")); err != nil {
		t.Fatalf("kv1 after kv2 close failed: %v", err)
	}
	val, err := kv1.Get("shared_ns", "key")
	if err != nil || string(val) != "value" {
		t.Fatalf("get shared: %q %v", val, err)
	}
}
