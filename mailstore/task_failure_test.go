package mailstore

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestMailTaskProviderErrorsPreserveStatusAndRetryAfter(t *testing.T) {
	for _, provider := range []string{"openai", "openai-compatible", "gemini", "deepseek"} {
		t.Run(provider, func(t *testing.T) {
			_, err := CreateAIResponse(context.Background(), taskHTTPClient{status: 429, retryAfter: "120", body: `{"error":{"message":"too many requests"}}`}, AIModelRecord{Provider: provider, BaseURL: "https://model.example.com/v1", Model: "test"}, "secret-key", "instructions", "input", 500, "medium")
			var upstream *UpstreamHTTPError
			if !errors.As(err, &upstream) || upstream.StatusCode != 429 || upstream.RetryAfter != 120*time.Second {
				t.Fatal(err)
			}
			policy := classifyMailTaskFailure(MailTask{Type: mailTaskAnalysis, Attempts: 1}, fmt.Errorf("wrapped: %w", err))
			if !policy.retryable || policy.kind != "rate_limit" || policy.delay != 120*time.Second {
				t.Fatal(policy)
			}
		})
	}
}

func TestMailTaskRetryPolicyDistinguishesPermanentTransientOutputAndCancellation(t *testing.T) {
	tests := []struct {
		name        string
		cause       error
		attempt     int
		retryable   bool
		kind        string
		interrupted bool
	}{
		{"auth", &UpstreamHTTPError{StatusCode: 401}, 1, false, "authentication", false},
		{"bad request", &UpstreamHTTPError{StatusCode: 400}, 1, false, "upstream_http", false},
		{"server", &UpstreamHTTPError{StatusCode: 503}, 1, true, "upstream_http", false},
		{"exhaustion", &UpstreamHTTPError{StatusCode: 503}, 5, false, "upstream_http", false},
		{"configuration", permanentMailTaskError("configuration", errors.New("missing key")), 1, false, "configuration", false},
		{"repair output", ErrInvalidMailAnalysis, 1, true, "invalid_output", false},
		{"invalid again", ErrInvalidMailAnalysis, 2, false, "invalid_output", false},
		{"timeout", context.DeadlineExceeded, 1, true, "timeout", false},
		{"shutdown", fmt.Errorf("request: %w", context.Canceled), 5, true, "cancelled", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := classifyMailTaskFailure(MailTask{Type: mailTaskAnalysis, Attempts: test.attempt}, test.cause)
			if p.retryable != test.retryable || p.kind != test.kind || p.interrupted != test.interrupted {
				t.Fatal(p)
			}
		})
	}
	seen := map[time.Duration]bool{}
	for _, token := range []string{"one", "two", "three", "four", "five"} {
		delay := mailTaskRetryDelayWithJitter(MailTask{Attempts: 1, Token: token})
		if delay < 12*time.Second || delay > 18*time.Second {
			t.Fatal(delay)
		}
		seen[delay] = true
	}
	if len(seen) < 2 {
		t.Fatal("retries have no jitter")
	}
}

func TestMailTaskTransportErrorsOmitSecretURLsAndKeepCancellation(t *testing.T) {
	err := upstreamTransportError("Feishu", &url.Error{Op: "Post", URL: "https://open.feishu.cn/open-apis/bot/v2/hook/secret-webhook", Err: context.Canceled})
	if strings.Contains(err.Error(), "secret-webhook") || !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	err = sendFeishuText(context.Background(), taskHTTPClient{status: 200, body: "<html>gateway page</html>"}, "https://open.feishu.cn/open-apis/bot/v2/hook/test", "message")
	if err == nil || !strings.Contains(err.Error(), "acknowledgement") {
		t.Fatal("malformed success acknowledged", err)
	}
	date := time.Now().Add(90 * time.Second).UTC().Format(http.TimeFormat)
	err = upstreamHTTPError("Feishu", &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{date}}}, "")
	var upstream *UpstreamHTTPError
	if !errors.As(err, &upstream) || upstream.RetryAfter < 88*time.Second || upstream.RetryAfter > 90*time.Second {
		t.Fatal(err)
	}
}
