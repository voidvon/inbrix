package ai

// embedded_test.go — tests for the in-process llmux backend ([ai] mode =
// "embedded") and for the property that matters most in both modes: with
// [ai] enabled = false, LilMail's AI feature emits nothing at all.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"lilmail/config"

	"github.com/vul-os/llmux/core/gateway"
)

// ---------------------------------------------------------------------------
// Recording transport
// ---------------------------------------------------------------------------

// recordingTransport counts every HTTP round trip and delegates to the real
// transport, so a test can both prove that traffic did NOT happen and (in the
// positive controls below) that this recorder would have seen it if it had.
type recordingTransport struct {
	next  http.RoundTripper
	count atomic.Int64
	urls  []string
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.count.Add(1)
	r.urls = append(r.urls, req.URL.String())
	return r.next.RoundTrip(req)
}

// installRecorder swaps http.DefaultTransport for a recorder for the duration
// of the test. Every HTTP client on both AI paths ends up here: LilMail's
// remote completionClient uses &http.Client{Timeout: ...} with no Transport of
// its own, and llmux's provider adapters build their clients the same way
// (provider.NewHTTPClient sets only Timeout and CheckRedirect). So a round trip
// from either mode is counted.
func installRecorder(t *testing.T) *recordingTransport {
	t.Helper()
	prev := http.DefaultTransport
	rec := &recordingTransport{next: prev}
	http.DefaultTransport = rec
	t.Cleanup(func() { http.DefaultTransport = prev })
	return rec
}

// isolateLLMuxEnv blanks every environment variable llmux resolves config from,
// so a test's embedded gateway is built from its config file alone and cannot
// pick up the developer's real OPENAI_API_KEY, a shared DATABASE_URL, or a
// running Ollama.
func isolateLLMuxEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"OLLAMA_HOST", "LLMUX_LOCAL_BASE_URL", "LLMUX_LOCAL_API_KEY",
		"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GEMINI_API_KEY", "DEEPSEEK_API_KEY",
		"GROQ_API_KEY", "MISTRAL_API_KEY", "TOGETHER_API_KEY", "FIREWORKS_API_KEY",
		"XAI_API_KEY", "OPENROUTER_API_KEY", "COHERE_API_KEY",
		"LLMUX_POSTGRES", "DATABASE_URL", "VULOS_DATABASE_URL", "LLMUX_POSTGRES_SCHEMA",
		"LLMUX_REDIS", "LLMUX_ADDR", "LLMUX_SOCKET", "LLMUX_MASTER_KEY",
		"LLMUX_USAGE_LOG", "LLMUX_BYOK_KEK", "LLMUX_BYOK_STORE", "LLMUX_CP_URL",
		"LLMUX_LOG_LEVEL", "LLMUX_SYNC_INTERVAL_MIN",
	} {
		t.Setenv(k, "")
	}
}

// ---------------------------------------------------------------------------
// A fake OpenAI-shaped provider for the embedded gateway to dispatch to
// ---------------------------------------------------------------------------

// fakeProvider is an OpenAI-compatible upstream: llmux's passthrough adapter
// POSTs <base_url>/chat/completions to it. It records every request body it
// received, which is how a test asserts that mail content reached exactly this
// provider and nowhere else.
type fakeProvider struct {
	srv    *httptest.Server
	bodies []string
	calls  atomic.Int64
	reply  string
}

func newFakeProvider(t *testing.T, reply string) *fakeProvider {
	t.Helper()
	p := &fakeProvider{reply: reply}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.calls.Add(1)
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("fake provider: read body: %v", err)
		}
		p.bodies = append(p.bodies, string(raw))
		w.Header().Set("Content-Type", "application/json")
		msg, _ := json.Marshal(p.reply)
		fmt.Fprintf(w, `{"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"test-model",
			"choices":[{"index":0,"message":{"role":"assistant","content":%s},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`, string(msg))
	}))
	t.Cleanup(p.srv.Close)
	return p
}

// writeLLMuxConfig writes an llmux JSON config into a temp dir and returns its
// path. extra is merged over the base provider/route wiring.
func writeLLMuxConfig(t *testing.T, p *fakeProvider, extra map[string]any) string {
	t.Helper()
	cfg := map[string]any{
		"log_level": "error",
		"providers": []map[string]any{{
			"name": "fake", "type": "passthrough", "base_url": p.srv.URL,
		}},
		"routes": []map[string]any{{"model": "*", "provider": "fake"}},
	}
	for k, v := range extra {
		cfg[k] = v
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal llmux config: %v", err)
	}
	path := filepath.Join(t.TempDir(), "llmux.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write llmux config: %v", err)
	}
	return path
}

// embeddedCfg is the LilMail-side [ai] block for an embedded gateway pointed at
// the fake provider.
func embeddedCfg(t *testing.T, p *fakeProvider) config.AIConfig {
	t.Helper()
	return config.AIConfig{
		Enabled:     true,
		Mode:        config.AIModeEmbedded,
		Model:       "test-model",
		LLMuxConfig: writeLLMuxConfig(t, p, nil),
	}
}

// ---------------------------------------------------------------------------
// AI off is structurally silent
// ---------------------------------------------------------------------------

// aiRoutes is every route the package serves, with a body for each.
var aiRoutes = []struct {
	path string
	body any
}{
	{"/ai/compose", map[string]any{"context": "x", "draft_so_far": "y"}},
	{"/ai/summarize", map[string]any{"thread": "hello"}},
	{"/ai/reply", map[string]any{"thread": "hello"}},
	{"/ai/extract-actions", map[string]any{"thread": "hello"}},
	{"/ai/phishing", map[string]any{"message_body": "hello"}},
}

// settledGoroutines waits briefly for the runtime to drop back to want, then
// returns the final count. Fiber's app.Test spins a goroutine per request that
// exits shortly after the response, so a bare before/after comparison would be
// flaky in the direction of a FALSE FAILURE, never a false pass.
func settledGoroutines(want int) int {
	deadline := time.Now().Add(2 * time.Second)
	for {
		got := runtime.NumGoroutine()
		if got <= want || time.Now().After(deadline) {
			return got
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestAIDisabled_EmitsNothing is the "off means off" proof. With
// [ai] enabled = false — and deliberately with an EMBEDDED config that would
// otherwise build a real gateway — no completion backend is constructed, so
// every route 404s, not one HTTP round trip is attempted, and no goroutine is
// left behind.
//
// The zero is only meaningful because TestRecorder_SeesTraffic below runs the
// same recorder against both live modes and counts round trips: an assertion
// that nothing happened is worthless if the instrument cannot see anything.
func TestAIDisabled_EmitsNothing(t *testing.T) {
	isolateLLMuxEnv(t)
	provider := newFakeProvider(t, "should never be reached")
	rec := installRecorder(t)

	cfg := embeddedCfg(t, provider)
	cfg.Enabled = false

	baseline := runtime.NumGoroutine()

	h, err := NewHandler(cfg)
	if err != nil {
		t.Fatalf("NewHandler(disabled): %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })
	// Errorf, not Fatalf: the packet/goroutine assertions below are the ones
	// that matter and they must still run (and be seen to fail) if this one does.
	if h.client != nil {
		t.Errorf("disabled handler built a completion backend (%T); with AI off there must be nothing to call", h.client)
	}

	app := buildTestApp(t, cfg)
	for _, r := range aiRoutes {
		status, body := fiberPost(t, app, r.path, r.body)
		if status != http.StatusNotFound || !strings.Contains(body, "ai_disabled") {
			t.Errorf("%s: status = %d body = %s, want 404 ai_disabled", r.path, status, body)
		}
	}
	if status, _ := fiberGet(t, app, "/ai/capabilities"); status != http.StatusNotFound {
		t.Errorf("/ai/capabilities: status = %d, want 404", status)
	}

	if n := rec.count.Load(); n != 0 {
		t.Errorf("AI disabled made %d HTTP round trip(s): %v — the feature must be silent when off", n, rec.urls)
	}
	if n := provider.calls.Load(); n != 0 {
		t.Errorf("AI disabled reached the provider %d time(s)", n)
	}
	if got := settledGoroutines(baseline); got > baseline {
		t.Errorf("AI disabled left %d extra goroutine(s) (%d -> %d)", got-baseline, baseline, got)
	}
}

// TestRecorder_SeesTraffic is the positive control for the assertion above: the
// same recorder, the same routes, AI ON. It runs once per mode, because a
// recorder that only observed the remote path would silently excuse an embedded
// gateway that phoned home.
func TestRecorder_SeesTraffic(t *testing.T) {
	t.Run("remote", func(t *testing.T) {
		rec := installRecorder(t)
		srv := mockSSEServer(t, "hi")
		defer srv.Close()

		app := buildTestApp(t, config.AIConfig{Enabled: true, Endpoint: srv.URL, Model: "m"})
		if status, body := fiberPost(t, app, "/ai/summarize", map[string]any{"thread": "hello"}); status != http.StatusOK {
			t.Fatalf("summarize status = %d body = %s", status, body)
		}
		if n := rec.count.Load(); n == 0 {
			t.Fatal("recorder saw 0 round trips on the LIVE remote path — it cannot prove silence when off")
		}
	})

	t.Run("embedded", func(t *testing.T) {
		isolateLLMuxEnv(t)
		provider := newFakeProvider(t, "hi")
		rec := installRecorder(t)

		app := buildTestApp(t, embeddedCfg(t, provider))
		if status, body := fiberPost(t, app, "/ai/summarize", map[string]any{"thread": "hello"}); status != http.StatusOK {
			t.Fatalf("summarize status = %d body = %s", status, body)
		}
		if n := rec.count.Load(); n == 0 {
			t.Fatal("recorder saw 0 round trips on the LIVE embedded path — it cannot prove silence when off")
		}
	})
}

// TestEmbedded_ConstructionIsSilent checks the other half of "no surprise
// packets": building the gateway (which happens at startup, before any user
// action) must not talk to anything — no price feed, no provider probe.
func TestEmbedded_ConstructionIsSilent(t *testing.T) {
	isolateLLMuxEnv(t)
	provider := newFakeProvider(t, "unused")
	rec := installRecorder(t)
	baseline := runtime.NumGoroutine()

	h, err := NewHandler(embeddedCfg(t, provider))
	if err != nil {
		t.Fatalf("NewHandler(embedded): %v", err)
	}
	defer h.Close()

	if n := rec.count.Load(); n != 0 {
		t.Errorf("building the embedded gateway made %d round trip(s): %v", n, rec.urls)
	}
	if got := settledGoroutines(baseline); got > baseline {
		t.Errorf("building the embedded gateway started %d goroutine(s) (%d -> %d) — Run/Start must stay uncalled",
			got-baseline, baseline, got)
	}
}

// ---------------------------------------------------------------------------
// Embedded completion path
// ---------------------------------------------------------------------------

// TestEmbedded_CompletesInProcess exercises a real route end to end through the
// in-process gateway, and asserts the mail content reached the provider llmux's
// config selected — not some other endpoint.
func TestEmbedded_CompletesInProcess(t *testing.T) {
	isolateLLMuxEnv(t)
	summary := `{"summary":"Bob asks about Tuesday.","key_points":["meeting"],"action_items":["reply"]}`
	provider := newFakeProvider(t, summary)
	app := buildTestApp(t, embeddedCfg(t, provider))

	status, body := fiberPost(t, app, "/ai/summarize", map[string]any{
		"thread": "From: bob\nAre you free Tuesday at 3pm?",
	})
	if status != http.StatusOK {
		t.Fatalf("summarize status = %d, body = %s", status, body)
	}
	var resp struct {
		Summary   string   `json:"summary"`
		KeyPoints []string `json:"key_points"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("unmarshal: %v (body=%s)", err, body)
	}
	if resp.Summary != "Bob asks about Tuesday." || len(resp.KeyPoints) != 1 {
		t.Fatalf("parsed response = %+v", resp)
	}
	if provider.calls.Load() != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.calls.Load())
	}
	if !strings.Contains(provider.bodies[0], "Are you free Tuesday") {
		t.Fatalf("thread did not reach the configured provider: %s", provider.bodies[0])
	}
	if !strings.Contains(provider.bodies[0], `"model":"test-model"`) {
		t.Errorf("[ai] model was not forwarded to the provider: %s", provider.bodies[0])
	}
}

// TestEmbedded_PhishingRoute covers the one route that sends a user message in
// addition to the system prompt, so the two-message shape is exercised too.
func TestEmbedded_PhishingRoute(t *testing.T) {
	isolateLLMuxEnv(t)
	verdict := `{"verdict":"phishing","confidence":0.9,"reasons":["spoofed sender"],"suspicious_elements":["http://evil"]}`
	provider := newFakeProvider(t, verdict)
	app := buildTestApp(t, embeddedCfg(t, provider))

	status, body := fiberPost(t, app, "/ai/phishing", map[string]any{
		"message_headers": "From: paypal@evil.example",
		"message_body":    "Verify your account",
		"urls":            []string{"http://evil"},
	})
	if status != http.StatusOK {
		t.Fatalf("phishing status = %d, body = %s", status, body)
	}
	var resp phishingResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("unmarshal: %v (body=%s)", err, body)
	}
	if resp.Verdict != "phishing" || resp.Confidence != 0.9 {
		t.Fatalf("verdict = %+v", resp)
	}
	if !strings.Contains(provider.bodies[0], "Verify your account") {
		t.Errorf("message body did not reach the provider: %s", provider.bodies[0])
	}
	if !strings.Contains(provider.bodies[0], `"role":"user"`) {
		t.Errorf("user message missing from the embedded request: %s", provider.bodies[0])
	}
}

// TestEmbedded_ProviderErrorIs502 keeps the failure contract identical across
// modes: an upstream failure is a 502 with no upstream detail leaked.
func TestEmbedded_ProviderErrorIs502(t *testing.T) {
	isolateLLMuxEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "provider exploded", http.StatusInternalServerError)
	}))
	defer srv.Close()
	p := &fakeProvider{srv: srv}

	cfg := config.AIConfig{
		Enabled: true, Mode: config.AIModeEmbedded, Model: "test-model",
		LLMuxConfig: writeLLMuxConfig(t, p, map[string]any{
			"retry": map[string]any{"max_retries": 0},
		}),
	}
	app := buildTestApp(t, cfg)

	status, body := fiberPost(t, app, "/ai/summarize", map[string]any{"thread": "hi"})
	if status != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (body=%s)", status, body)
	}
	if strings.Contains(body, "provider exploded") {
		t.Errorf("upstream error text leaked to the client: %s", body)
	}
}

// ---------------------------------------------------------------------------
// Account-token forwarding, embedded
// ---------------------------------------------------------------------------

// TestEmbedded_AccountTokenGoesThroughAuthorize proves the account_header token
// is not merely accepted but actually gates the request: with llmux virtual keys
// configured, a known token completes and an unknown one is refused BEFORE the
// provider is called. That is the embedded counterpart of the remote mode's
// "forward the account's Bearer" contract.
func TestEmbedded_AccountTokenGoesThroughAuthorize(t *testing.T) {
	isolateLLMuxEnv(t)
	provider := newFakeProvider(t, "ok")
	cfg := config.AIConfig{
		Enabled:       true,
		Mode:          config.AIModeEmbedded,
		Model:         "test-model",
		AccountHeader: "X-Vulos-Account-Token",
		LLMuxConfig: writeLLMuxConfig(t, provider, map[string]any{
			"keys": []map[string]any{{"key": "sk-known", "name": "alice"}},
		}),
	}
	app := buildTestApp(t, cfg)

	post := func(token string) int {
		body, _ := json.Marshal(map[string]any{"thread": "hello"})
		req := httptest.NewRequest(http.MethodPost, "/ai/summarize", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("X-Vulos-Account-Token", token)
		}
		resp, err := app.Test(req, 5000)
		if err != nil {
			t.Fatalf("fiber test: %v", err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	if got := post("sk-known"); got != http.StatusOK {
		t.Errorf("known account token: status = %d, want 200", got)
	}
	if calls := provider.calls.Load(); calls != 1 {
		t.Fatalf("provider calls after known token = %d, want 1", calls)
	}

	if got := post("sk-bogus"); got != http.StatusBadGateway {
		t.Errorf("unknown account token: status = %d, want 502 (refused)", got)
	}
	if calls := provider.calls.Load(); calls != 1 {
		t.Errorf("an unauthorized request reached the provider: calls = %d, want still 1", calls)
	}
}

// ---------------------------------------------------------------------------
// The privacy decision: llmux's cache is off unless asked for
// ---------------------------------------------------------------------------

// TestEmbedded_CacheOffByDefault is the test behind the package doc's privacy
// paragraph. Embedding llmux brings its response cache into LilMail's process;
// by default LilMail switches it off, so nothing derived from a message is
// retained after the request. Two identical summarize calls must therefore both
// reach the provider.
func TestEmbedded_CacheOffByDefault(t *testing.T) {
	isolateLLMuxEnv(t)
	provider := newFakeProvider(t, "same answer")
	cfg := embeddedCfg(t, provider)
	// Even with the llmux config file explicitly ENABLING the cache, LilMail's
	// own [ai] llmux_cache = false wins: the privacy default is not overridable
	// from the gateway's side of the config.
	cfg.LLMuxConfig = writeLLMuxConfig(t, provider, map[string]any{
		"cache": map[string]any{"enabled": true, "ttl_seconds": 60},
	})
	app := buildTestApp(t, cfg)

	for i := 0; i < 2; i++ {
		if status, body := fiberPost(t, app, "/ai/summarize", map[string]any{"thread": "identical thread"}); status != http.StatusOK {
			t.Fatalf("call %d: status = %d body = %s", i, status, body)
		}
	}
	if got := provider.calls.Load(); got != 2 {
		t.Fatalf("provider calls = %d, want 2 — a repeated request was served from a cache that must be off by default", got)
	}
}

// TestEmbedded_CacheOptIn is the counterpart: with [ai] llmux_cache = true the
// second identical request IS served from memory. It pins the documented
// behaviour of the opt-in (and proves the default above is a real switch, not a
// coincidence of the cache never working).
func TestEmbedded_CacheOptIn(t *testing.T) {
	isolateLLMuxEnv(t)
	provider := newFakeProvider(t, "same answer")
	cfg := embeddedCfg(t, provider)
	cfg.LLMuxCache = true
	cfg.LLMuxConfig = writeLLMuxConfig(t, provider, map[string]any{
		"cache": map[string]any{"enabled": true, "ttl_seconds": 60},
	})
	app := buildTestApp(t, cfg)

	for i := 0; i < 2; i++ {
		if status, body := fiberPost(t, app, "/ai/summarize", map[string]any{"thread": "identical thread"}); status != http.StatusOK {
			t.Fatalf("call %d: status = %d body = %s", i, status, body)
		}
	}
	if got := provider.calls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want 1 — llmux_cache = true should serve the repeat from memory", got)
	}
}

// ---------------------------------------------------------------------------
// The embedded posture: what LilMail strips from llmux's config
// ---------------------------------------------------------------------------

// TestEmbedded_StripsRemoteStateAndPriceFeeds asserts the four overrides in
// newEmbeddedClient actually took effect on the gateway that got built. The
// Postgres DSN is the sharp one: gateway.New connects EAGERLY when it is set, so
// a config carrying an unreachable DSN would fail construction if LilMail did
// not clear it first.
func TestEmbedded_StripsRemoteStateAndPriceFeeds(t *testing.T) {
	isolateLLMuxEnv(t)
	provider := newFakeProvider(t, "unused")
	path := writeLLMuxConfig(t, provider, map[string]any{
		"postgres": "postgres://nobody:nobody@127.0.0.1:1/nope?sslmode=disable",
		"redis":    "127.0.0.1:1",
		// Asked for by the gateway's own config, and still refused: the
		// gw.Cache() assertion below would be vacuous without this.
		"cache": map[string]any{"enabled": true, "ttl_seconds": 60},
		"pricing": map[string]any{
			"sources":       []string{"https://openrouter.ai/api/v1/models"},
			"azure_pricing": true,
		},
	})
	rec := installRecorder(t)

	c, err := newEmbeddedClient(config.AIConfig{
		Enabled: true, Mode: config.AIModeEmbedded, Model: "test-model", LLMuxConfig: path,
	})
	if err != nil {
		t.Fatalf("newEmbeddedClient: %v — an unreachable postgres DSN must be dropped, not dialled", err)
	}
	defer c.Close()

	got := c.gw.Config()
	if got.Postgres != "" {
		t.Errorf("postgres DSN survived: %q", got.Postgres)
	}
	if got.Redis != "" {
		t.Errorf("redis address survived: %q", got.Redis)
	}
	if len(got.Pricing.Sources) != 0 || got.Pricing.Azure {
		t.Errorf("price feeds survived: sources=%v azure=%v", got.Pricing.Sources, got.Pricing.Azure)
	}
	if got.Server.Addr != "" || got.Server.SocketPath != "" {
		t.Errorf("a listen address survived: addr=%q socket=%q", got.Server.Addr, got.Server.SocketPath)
	}
	if c.gw.Cache() != nil {
		t.Error("response cache is on despite llmux_cache = false")
	}
	if n := rec.count.Load(); n != 0 {
		t.Errorf("construction made %d round trip(s): %v", n, rec.urls)
	}
}

// ---------------------------------------------------------------------------
// Misconfiguration fails at startup, not on the user's first request
// ---------------------------------------------------------------------------

func TestEmbedded_MisconfigurationFailsAtStartup(t *testing.T) {
	isolateLLMuxEnv(t)
	provider := newFakeProvider(t, "unused")
	good := writeLLMuxConfig(t, provider, nil)

	// An llmux config with providers but no route for the requested model.
	noRoute := writeLLMuxConfig(t, provider, map[string]any{
		"routes": []map[string]any{{"model": "some-other-model", "provider": "fake"}},
	})

	// A syntactically broken llmux config.
	broken := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(broken, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A config with no providers at all (llmux's defaults + a blanked env
	// resolve to nothing, so every request would 502).
	empty := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(empty, []byte(`{"log_level":"error"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		cfg  config.AIConfig
		want string
	}{
		{"no model", config.AIConfig{Enabled: true, Mode: config.AIModeEmbedded, LLMuxConfig: good}, "model is required"},
		{"unroutable model", config.AIConfig{Enabled: true, Mode: config.AIModeEmbedded, Model: "test-model", LLMuxConfig: noRoute}, "cannot route"},
		{"broken llmux config", config.AIConfig{Enabled: true, Mode: config.AIModeEmbedded, Model: "test-model", LLMuxConfig: broken}, "load llmux config"},
		{"no providers", config.AIConfig{Enabled: true, Mode: config.AIModeEmbedded, Model: "test-model", LLMuxConfig: empty}, "no llmux providers"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, err := NewHandler(tc.cfg)
			if err == nil {
				_ = h.Close()
				t.Fatalf("NewHandler accepted a %s config; startup must fail loudly", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// TestEmbedded_ModeIsNotRemote guards the seam itself: an embedded handler must
// not fall back to the [ai] endpoint. A config naming BOTH must never send mail
// to the endpoint.
func TestEmbedded_ModeIsNotRemote(t *testing.T) {
	isolateLLMuxEnv(t)
	provider := newFakeProvider(t, "from the embedded gateway")
	var endpointHits atomic.Int64
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		endpointHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer endpoint.Close()

	cfg := embeddedCfg(t, provider)
	cfg.Endpoint = endpoint.URL // present, and must be ignored
	app := buildTestApp(t, cfg)

	if status, body := fiberPost(t, app, "/ai/compose", map[string]any{"draft_so_far": "Hi "}); status != http.StatusOK {
		t.Fatalf("compose status = %d body = %s", status, body)
	}
	if n := endpointHits.Load(); n != 0 {
		t.Errorf("embedded mode called the remote [ai] endpoint %d time(s)", n)
	}
	if n := provider.calls.Load(); n != 1 {
		t.Errorf("embedded provider calls = %d, want 1", n)
	}
}

// TestEmbedded_NoBYOKOrControlPlane pins a documented LIMITATION rather than a
// feature: gateway.New builds neither a BYOK store nor a control-plane identity
// from config — llmux's own composition root wires those — so in embedded mode
// a `byok` or `cp` block in llmux_config is inert and every request uses the
// central provider keys. The docs say so; this fails if a future llmux quietly
// makes it untrue, which would turn accurate documentation into a lie.
func TestEmbedded_NoBYOKOrControlPlane(t *testing.T) {
	isolateLLMuxEnv(t)
	provider := newFakeProvider(t, "unused")
	path := writeLLMuxConfig(t, provider, map[string]any{
		"byok": map[string]any{"kek": strings.Repeat("a", 64)},
		"cp":   map[string]any{"cp_url": "https://cp.example.invalid"},
	})

	c, err := newEmbeddedClient(config.AIConfig{
		Enabled: true, Mode: config.AIModeEmbedded, Model: "test-model", LLMuxConfig: path,
	})
	if err != nil {
		t.Fatalf("newEmbeddedClient: %v", err)
	}
	defer c.Close()

	if c.gw.BYOK() != nil {
		t.Error("a BYOK store is wired embedded — the docs claim it is not; update them")
	}
	if _, ok := c.gw.Identity().(gateway.StaticIdentity); !ok {
		t.Errorf("identity resolver = %T, want the standalone gateway.StaticIdentity — no cp adapter is wired here", c.gw.Identity())
	}
	if c.gw.IdentityActive() {
		t.Error("the authenticated path is active with no virtual keys configured")
	}
}
