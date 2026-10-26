package web

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

type aiRequestKey struct{ owner, account, session, request string }
type aiActiveRequest struct {
	cancel  context.CancelFunc
	done    chan struct{}
	stop    bool
	expires time.Time
}

// Stop markers cover a stop request arriving before the generation request.
// Entries are scoped to an owned account, bounded and expire after a turn's lifetime.
type aiRequestRegistry struct {
	mu       sync.Mutex
	requests map[aiRequestKey]*aiActiveRequest
}

func (r *aiRequestRegistry) prune() {
	if r.requests == nil {
		r.requests = make(map[aiRequestKey]*aiActiveRequest)
	}
	for key, request := range r.requests {
		if time.Now().After(request.expires) && request.cancel == nil {
			delete(r.requests, key)
		}
	}
}

func (r *aiRequestRegistry) begin(parent context.Context, key aiRequestKey) (context.Context, func(), error) {
	if key.request == "" {
		return parent, func() {}, nil
	} // Legacy callers have no stop identity.
	if len(key.request) > 128 {
		return nil, nil, fiber.ErrBadRequest
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prune()
	if previous := r.requests[key]; previous != nil {
		if previous.stop {
			return nil, nil, fiber.NewError(409, "AI generation was stopped")
		}
		return nil, nil, fiber.NewError(409, "AI request is already running")
	}
	if len(r.requests) >= 4096 {
		return nil, nil, fiber.ErrTooManyRequests
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Minute)
	request := &aiActiveRequest{cancel: cancel, done: make(chan struct{}), expires: time.Now().Add(11 * time.Minute)}
	r.requests[key] = request
	return ctx, func() {
		cancel()
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.requests[key] == request {
			if request.stop {
				request.cancel = nil
			} else {
				delete(r.requests, key)
			}
			close(request.done)
		}
	}, nil
}

func (r *aiRequestRegistry) stop(key aiRequestKey) (<-chan struct{}, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prune()
	if request := r.requests[key]; request != nil {
		request.stop = true
		if request.cancel != nil {
			request.cancel()
		}
		return request.done, nil
	}
	if len(r.requests) >= 4096 {
		return nil, fiber.ErrTooManyRequests
	}
	r.requests[key] = &aiActiveRequest{stop: true, expires: time.Now().Add(11 * time.Minute)}
	return nil, nil
}

func (h *AISettingsHandler) HandleStopAI(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	var input struct {
		AccountEmail string `json:"accountEmail"`
		SessionID    string `json:"sessionId,omitempty"`
		RequestID    string `json:"requestId"`
	}
	if c.BodyParser(&input) != nil || strings.TrimSpace(input.RequestID) == "" || len(input.RequestID) > 128 {
		return fiber.ErrBadRequest
	}
	account, err := h.mailDB.GetAccountByEmail(c.UserContext(), owner, strings.TrimSpace(input.AccountEmail))
	if err != nil {
		return chatStoreError(err)
	}
	done, err := h.aiRequests.stop(aiRequestKey{owner, account.ID, input.SessionID, input.RequestID})
	if err != nil {
		return err
	}
	if input.SessionID != "" {
		if err := h.mailDB.CancelAIChatRequest(c.UserContext(), owner, account.ID, input.SessionID, input.RequestID); err != nil {
			return fiber.ErrInternalServerError
		}
	}
	if done != nil {
		// Usually resolves as soon as upstream HTTP cancellation and run cleanup finish.
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
		case <-c.UserContext().Done():
		}
	}
	return c.SendStatus(fiber.StatusNoContent)
}
