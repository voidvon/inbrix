package api

import (
	"fmt"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
)

type requestSessionKey struct{}

// RequestSession uses the browser session normally. For an authenticated API
// key, it uses a separate store that exists only for this request. Legacy
// handlers may read and save mailbox selection without issuing a login cookie
// or modifying a browser session presented alongside the key.
func RequestSession(c *fiber.Ctx, browser *session.Store) (*session.Session, error) {
	if request, ok := c.Locals(requestSessionKey{}).(*session.Store); ok {
		return request.Get(c)
	}
	if browser == nil {
		return nil, fmt.Errorf("session store unavailable")
	}
	return browser.Get(c)
}

func IsAPIKeyRequest(c *fiber.Ctx) bool {
	_, ok := c.Locals(requestSessionKey{}).(*session.Store)
	return ok
}

const requestSessionHeader = "X-Inbrix-Request-Session"

// UseAPIKeySession must only be called after API key verification. Its storage
// is never shared with another request, and its ID cannot authenticate a client.
func UseAPIKeySession(c *fiber.Ctx, values map[string]any) error {
	store := session.New(session.Config{Storage: &requestSessionStorage{values: make(map[string][]byte)}, KeyLookup: "header:" + requestSessionHeader})
	c.Locals(requestSessionKey{}, store)
	sess, err := store.Get(c)
	if err != nil {
		return err
	}
	for key, value := range values {
		sess.Set(key, value)
	}
	err = sess.Save()
	ClearRequestSessionHeader(c)
	return err
}

func ClearRequestSessionHeader(c *fiber.Ctx) {
	c.Response().Header.Del(requestSessionHeader)
}

// A per-request Fiber storage adapter with no timers or background goroutines.
type requestSessionStorage struct {
	mu     sync.Mutex
	values map[string][]byte
}

func (s *requestSessionStorage) Get(key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.values[key]...), nil
}

func (s *requestSessionStorage) Set(key string, value []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = append([]byte(nil), value...)
	return nil
}

func (s *requestSessionStorage) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, key)
	return nil
}

func (s *requestSessionStorage) Reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.values)
	return nil
}

func (s *requestSessionStorage) Close() error { return s.Reset() }
