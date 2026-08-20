// Package storage defines inbrix's durable key-value seam and its backends.
//
// The seam is one small interface (KV) with two implementations:
//
//   - sqlite (DEFAULT): an embedded table in the local mail database.
//   - postgres (OPTIONAL): a shared SQL store, opt-in via [storage] config.
//     Use it when multiple instances must share state, or when an embedding host
//     (e.g. a Vulos OS box) needs to read the same data.
package storage

import (
	"errors"
	"fmt"

	"inbrix/config"
)

// ErrNotFound is returned by Get when a namespace/key pair does not exist.
var ErrNotFound = errors.New("storage: not found")

// KV is a namespaced byte-blob store. A namespace is an isolated keyspace
// (a logical partition of the SQL table). Implementations
// must be safe for concurrent use.
type KV interface {
	// Get returns the value for key in ns, or ErrNotFound.
	Get(ns, key string) ([]byte, error)
	// Set stores val for key in ns, creating the namespace if needed.
	Set(ns, key string, val []byte) error
	// Delete removes key from ns; deleting a missing key is not an error.
	Delete(ns, key string) error
	// List returns all key→value pairs in ns whose key has the given prefix
	// (pass "" for the whole namespace).
	List(ns, prefix string) (map[string][]byte, error)
	// Close releases the backend's resources.
	Close() error
}

// Open constructs the KV backend selected by cfg.Storage. sqlitePath is ignored
// by Postgres. An omitted backend selects SQLite.
func Open(cfg *config.Config, sqlitePath string) (KV, error) {
	switch cfg.Storage.Backend {
	case "", "sqlite":
		return OpenSQLite(sqlitePath)
	case "postgres":
		if cfg.Storage.PostgresDSN == "" {
			return nil, fmt.Errorf("storage: backend=postgres requires storage.postgres_dsn")
		}
		return OpenPostgres(cfg.Storage.PostgresDSN)
	default:
		return nil, fmt.Errorf("storage: unknown backend %q (want sqlite|postgres)", cfg.Storage.Backend)
	}
}
