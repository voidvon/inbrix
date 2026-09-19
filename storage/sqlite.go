package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type sqliteKV struct {
	db    *sql.DB
	owned bool
}

// OpenSQLite stores namespaced values in the same SQLite file as the mail
// mirror. Separate sql.DB pools are safe because every connection uses WAL and
// the same busy timeout.
func OpenSQLite(path string) (KV, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("storage: sqlite path is empty")
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("storage: create sqlite directory: %w", err)
		}
	}
	dsn := "file:" + filepath.ToSlash(path) + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("storage: open sqlite %s: %w", path, err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	if err := initSQLiteKV(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &sqliteKV{db: db, owned: true}, nil
}

// OpenSQLiteFromDB initializes the inbrix_kv schema on an existing DB pool,
// sharing the connection pool and locks with the mail mirror.
func OpenSQLiteFromDB(db *sql.DB) (KV, error) {
	if db == nil {
		return nil, fmt.Errorf("storage: sqlite db is nil")
	}
	if err := initSQLiteKV(db); err != nil {
		return nil, err
	}
	return &sqliteKV{db: db, owned: false}, nil
}

func initSQLiteKV(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS inbrix_kv (
		ns TEXT NOT NULL,
		key TEXT NOT NULL,
		val BLOB NOT NULL,
		PRIMARY KEY (ns, key)
	)`); err != nil {
		return fmt.Errorf("storage: initialize sqlite kv: %w", err)
	}
	return nil
}

func (s *sqliteKV) Get(ns, key string) ([]byte, error) {
	var value []byte
	err := s.db.QueryRow(`SELECT val FROM inbrix_kv WHERE ns = ? AND key = ?`, ns, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return value, err
}

func (s *sqliteKV) Set(ns, key string, value []byte) error {
	_, err := s.db.Exec(`INSERT INTO inbrix_kv(ns, key, val) VALUES(?, ?, ?)
		ON CONFLICT(ns, key) DO UPDATE SET val = excluded.val`, ns, key, value)
	return err
}

func (s *sqliteKV) Delete(ns, key string) error {
	_, err := s.db.Exec(`DELETE FROM inbrix_kv WHERE ns = ? AND key = ?`, ns, key)
	return err
}

func (s *sqliteKV) List(ns, prefix string) (map[string][]byte, error) {
	rows, err := s.db.Query(`SELECT key, val FROM inbrix_kv WHERE ns = ?`, ns)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string][]byte)
	for rows.Next() {
		var key string
		var value []byte
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		if prefix == "" || strings.HasPrefix(key, prefix) {
			out[key] = value
		}
	}
	return out, rows.Err()
}

func (s *sqliteKV) Close() error {
	if s.owned && s.db != nil {
		return s.db.Close()
	}
	return nil
}
