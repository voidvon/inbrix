package mailstore

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"inbrix/internal/agentruntime"
)

// UpstreamHTTPError retains machine-readable status and backpressure without
// retaining request headers, API keys, or webhook URLs.
type UpstreamHTTPError struct {
	Service    string
	StatusCode int
	Message    string
	RetryAfter time.Duration
}

func (e *UpstreamHTTPError) Error() string {
	message := fmt.Sprintf("%s returned HTTP %d", e.Service, e.StatusCode)
	if e.Message != "" {
		message += ": " + e.Message
	}
	return message
}

func upstreamHTTPError(service string, resp *http.Response, message string) error {
	delay := time.Duration(0)
	value := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if seconds, err := strconv.ParseInt(value, 10, 32); err == nil && seconds > 0 {
		delay = time.Duration(seconds) * time.Second
	} else if stamp, err := http.ParseTime(value); err == nil {
		delay = max(time.Duration(0), time.Until(stamp))
	}
	return &UpstreamHTTPError{Service: service, StatusCode: resp.StatusCode, Message: message, RetryAfter: min(delay, 24*time.Hour)}
}

func upstreamTransportError(service string, err error) error {
	var requestError *url.Error
	if errors.As(err, &requestError) {
		err = requestError.Err
	}
	return fmt.Errorf("%s request failed: %w", service, err)
}

type mailTaskFailure struct {
	kind      string
	permanent bool
	cause     error
}

func (e *mailTaskFailure) Error() string { return e.cause.Error() }
func (e *mailTaskFailure) Unwrap() error { return e.cause }
func permanentMailTaskError(kind string, err error) error {
	return &mailTaskFailure{kind: kind, permanent: true, cause: err}
}

var ErrInvalidMailAnalysis = errors.New("invalid mail analysis output")

type mailTaskFailurePolicy struct {
	kind        string
	retryable   bool
	delay       time.Duration
	interrupted bool
}

func classifyMailTaskFailure(task MailTask, err error) mailTaskFailurePolicy {
	p := mailTaskFailurePolicy{kind: "transient", retryable: true}
	var upstream *UpstreamHTTPError
	var explicit *mailTaskFailure
	var network net.Error
	switch {
	case errors.Is(err, context.Canceled):
		p.kind, p.interrupted = "cancelled", true
	case errors.Is(err, context.DeadlineExceeded):
		p.kind = "timeout"
	case errors.As(err, &explicit):
		p.kind, p.retryable = explicit.kind, !explicit.permanent
	case errors.As(err, &upstream):
		p.kind = "upstream_http"
		p.retryable = upstream.StatusCode >= 500 || upstream.StatusCode == 408 || upstream.StatusCode == 409 || upstream.StatusCode == 425 || upstream.StatusCode == 429
		if upstream.StatusCode == 429 {
			p.kind = "rate_limit"
		}
		if upstream.StatusCode == 401 || upstream.StatusCode == 403 {
			p.kind = "authentication"
		}
		p.delay = upstream.RetryAfter
	case errors.Is(err, ErrInvalidAISkill), errors.Is(err, ErrAISkillInUse):
		p.kind, p.retryable = "configuration", false
	case errors.Is(err, ErrInvalidMailAnalysis), errors.Is(err, agentruntime.ErrIncomplete):
		p.kind = "invalid_output"
		p.retryable = task.Attempts < 2
	case errors.As(err, &network):
		p.kind = "network"
		if network.Timeout() {
			p.kind = "timeout"
		}
	}
	if task.Attempts >= mailTaskMaxAttempts(task.Type) && !p.interrupted {
		p.retryable = false
	}
	return p
}

func mailTaskRetryDelayWithJitter(task MailTask) time.Duration {
	base := mailTaskRetryDelay(task.Attempts)
	h := fnv.New64a()
	_, _ = h.Write([]byte(task.Token))
	// Stable for one attempt, varied across independently leased jobs: ±20%.
	return min(base*time.Duration(80+h.Sum64()%41)/100, 30*time.Minute)
}
