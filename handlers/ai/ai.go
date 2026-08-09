// Package ai provides mail-specific AI endpoints for LilMail.
//
// It exposes five Fiber routes plus a capability probe:
//
//	POST /ai/compose           — smart compose / continue / rewrite
//	POST /ai/summarize         — thread summary + key points + action items
//	POST /ai/reply             — 3 reply suggestions with different tones
//	POST /ai/extract-actions   — action items with optional due dates
//	POST /ai/phishing          — phishing / suspicious / clean classification
//
// All routes are gated on AIConfig.Enabled. When disabled they return
// {"error":"ai_disabled","hint":"set [ai] enabled=true in config.toml"}, and
// NO completion backend is constructed at all — with AI off this package holds
// no client, no gateway and no goroutine, so it can emit no packets.
//
// # Two modes
//
// [ai] mode selects where completions happen. Both are first-class; neither is
// a fallback for the other.
//
//   - "remote" (default, completionClient in this file) POSTs to a configurable
//     OpenAI-compatible SSE endpoint — the Vulos OS airouter's /api/ai/chat, a
//     central llmux's /v1/chat/completions, or a provider directly:
//
//     data: {"choices":[{"delta":{"content":"..."}}]}
//     data: [DONE]
//
//   - "embedded" (embeddedClient, embedded.go) runs llmux in-process as a Go
//     library. There is no gateway to deploy and no completion hop: llmux's own
//     config decides which provider serves, and its sovereignty gate refuses any
//     off-box provider the operator has not explicitly opted in.
//
// Account context: when [ai] account_header is configured, the value of that
// inbound request header is the caller's account token. In remote mode it is
// forwarded as "Authorization: Bearer <token>" so a central gateway (llmux) can
// resolve it to an account and apply BYOK-vs-central key selection plus
// metering; in embedded mode it is handed to the in-process gateway's Authorize,
// which is the same auth path llmux's HTTP shell runs. When no per-request token
// is present, the static [ai] api_key is used. LilMail does not decide BYOK vs
// central — it only forwards the account's token.
//
// # Privacy
//
// Mail content is never written to any persistent store in this package, in
// either mode.
//
// In remote mode it is forwarded to the configured endpoint and discarded.
//
// In embedded mode it is handed to the in-process llmux gateway, dispatched to
// the provider llmux's config selects, and discarded. Embedding llmux brings its
// response cache into LilMail's process, so LilMail builds the gateway with that
// cache DISABLED by default: nothing derived from a message outlives the request.
// Setting [ai] llmux_cache = true opts in explicitly, and then model responses to
// your mail are held in an in-memory, TTL-bounded, size-bounded LRU keyed by a
// SHA-256 of the request until they expire or are evicted. Even then nothing is
// written to disk: LilMail also strips llmux's Redis and Postgres settings on
// this path, so the disk/network-backed cache and key stores are unreachable.
package ai

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"lilmail/config"

	"github.com/gofiber/fiber/v2"
)

// ---------------------------------------------------------------------------
// Embedded prompt templates
// ---------------------------------------------------------------------------

//go:embed prompts/mail_compose.txt
var mailComposePrompt string

//go:embed prompts/mail_summarize.txt
var mailSummarizePrompt string

//go:embed prompts/mail_reply_suggestions.txt
var mailReplyPrompt string

//go:embed prompts/mail_extract_actions.txt
var mailExtractActionsPrompt string

//go:embed prompts/phishing.txt
var phishingPrompt string

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// completer performs one completion, whatever the backend. The two
// implementations are completionClient (remote HTTP endpoint) and embeddedClient
// (in-process llmux). bearer is the caller's account token for this request.
type completer interface {
	complete(ctx context.Context, bearer, systemPrompt, userContent string) (string, error)
	// Close releases whatever the backend holds open.
	Close() error
}

// Handler serves the /ai/* endpoints.
type Handler struct {
	cfg config.AIConfig
	// client is nil when cfg.Enabled is false. That is the structural form of
	// "AI off is silent": with no backend object there is nothing to call, so
	// the disabled path cannot reach a network no matter what a route does.
	client completer
}

// NewHandler creates a Handler backed by the given AI config.
//
// With cfg.Enabled false it builds no backend and cannot fail. With AI on it
// returns an error for a configuration that could not serve a request —
// notably an embedded gateway with no providers, an unroutable model, or an
// unreadable llmux config — so a misconfiguration surfaces at startup instead
// of as a 502 on the user's first summarize.
func NewHandler(cfg config.AIConfig) (*Handler, error) {
	h := &Handler{cfg: cfg}
	if !cfg.Enabled {
		return h, nil
	}
	if cfg.EmbeddedAI() {
		c, err := newEmbeddedClient(cfg)
		if err != nil {
			return nil, err
		}
		h.client = c
		return h, nil
	}
	h.client = newCompletionClient(cfg)
	return h, nil
}

// Close releases the completion backend (the embedded llmux gateway's Redis
// client and key store, when one was built). Safe on a disabled handler.
func (h *Handler) Close() error {
	if h.client == nil {
		return nil
	}
	return h.client.Close()
}

// complete runs one completion for an inbound request, forwarding the caller's
// account token. It is the single place both modes are reached from.
func (h *Handler) complete(c *fiber.Ctx, systemPrompt, userContent string) (string, error) {
	if h.client == nil {
		// Unreachable while every route checks cfg.Enabled first; kept so a
		// future route that forgets the check fails closed instead of panicking.
		return "", fmt.Errorf("ai: disabled — no completion backend")
	}
	return h.client.complete(c.Context(), h.resolveBearer(c), systemPrompt, userContent)
}

// resolveBearer determines the Bearer token to forward to the completion
// endpoint for this request. When [ai] account_header is configured and that
// header is present on the inbound request, its value (the caller's account
// token) is forwarded so a central gateway such as llmux can resolve it to an
// account and apply BYOK-vs-central + metering. Otherwise it falls back to the
// static [ai] api_key. Returns "" when neither is available (no Authorization
// header is then sent).
func (h *Handler) resolveBearer(c *fiber.Ctx) string {
	if h.cfg.AccountHeader != "" {
		if v := strings.TrimSpace(c.Get(h.cfg.AccountHeader)); v != "" {
			return v
		}
	}
	return h.cfg.APIKey
}

// disabledResponse writes the standard "AI disabled" JSON body.
func disabledResponse(c *fiber.Ctx) error {
	return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
		"error": "ai_disabled",
		"hint":  "set [ai] enabled=true in config.toml",
	})
}

// Register mounts all AI routes onto the given Fiber router group.
// Call after applying the SessionMiddleware to the group.
func (h *Handler) Register(grp fiber.Router) {
	grp.Get("/ai/capabilities", h.HandleCapabilities)
	grp.Post("/ai/compose", h.HandleCompose)
	grp.Post("/ai/summarize", h.HandleSummarize)
	grp.Post("/ai/reply", h.HandleReply)
	grp.Post("/ai/extract-actions", h.HandleExtractActions)
	grp.Post("/ai/phishing", h.HandlePhishing)
}

// ---------------------------------------------------------------------------
// GET /ai/capabilities
// ---------------------------------------------------------------------------

// HandleCapabilities is a cheap, LLM-free capability probe the client calls on
// mount to decide whether to surface the AI affordances (summarize / smart-reply
// / help-me-write / phishing). It NEVER contacts the completion endpoint or the
// model — it only reports which features are wired.
//
// When AI is disabled it returns the standard 404 {"error":"ai_disabled"} (same
// as every other AI route), so the client's honest capability probe degrades the
// whole surface on any non-2xx. When enabled it returns the per-feature flags so
// the client can light up exactly the affordances the backend serves.
func (h *Handler) HandleCapabilities(c *fiber.Ctx) error {
	if !h.cfg.Enabled {
		return disabledResponse(c)
	}
	return c.JSON(fiber.Map{
		"enabled":        true,
		"compose":        true,
		"summarize":      true,
		"reply":          true,
		"extractActions": true,
		"phishing":       true,
	})
}

// ---------------------------------------------------------------------------
// POST /ai/compose
// ---------------------------------------------------------------------------

type composeRequest struct {
	Context     string `json:"context"`
	DraftSoFar  string `json:"draft_so_far"`
	Instruction string `json:"instruction"` // continue|finish|rewrite-formal|rewrite-casual
}

// HandleCompose handles POST /ai/compose.
func (h *Handler) HandleCompose(c *fiber.Ctx) error {
	if !h.cfg.Enabled {
		return disabledResponse(c)
	}
	var req composeRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_json: " + err.Error()})
	}
	if req.Instruction == "" {
		req.Instruction = "continue"
	}

	prompt := strings.NewReplacer(
		"{{CONTEXT}}", escapeForPrompt(req.Context),
		"{{DRAFT}}", escapeForPrompt(req.DraftSoFar),
		"{{INSTRUCTION}}", escapeForPrompt(req.Instruction),
	).Replace(mailComposePrompt)

	completion, err := h.complete(c, prompt, "")
	if err != nil {
		log.Printf("[mail_ai] compose error: %v", err)
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "service_unavailable"})
	}
	return c.JSON(fiber.Map{"completion": completion})
}

// ---------------------------------------------------------------------------
// POST /ai/summarize
// ---------------------------------------------------------------------------

type summarizeRequest struct {
	Thread string `json:"thread"`
}

type summarizeResponse struct {
	Summary     string   `json:"summary"`
	KeyPoints   []string `json:"key_points"`
	ActionItems []string `json:"action_items"`
}

// HandleSummarize handles POST /ai/summarize.
func (h *Handler) HandleSummarize(c *fiber.Ctx) error {
	if !h.cfg.Enabled {
		return disabledResponse(c)
	}
	var req summarizeRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_json: " + err.Error()})
	}

	prompt := strings.ReplaceAll(mailSummarizePrompt, "{{THREAD}}", escapeForPrompt(req.Thread))
	raw, err := h.complete(c, prompt, "")
	if err != nil {
		log.Printf("[mail_ai] summarize error: %v", err)
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "service_unavailable"})
	}

	var resp summarizeResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &resp); err != nil {
		resp = summarizeResponse{
			Summary:     strings.TrimSpace(raw),
			KeyPoints:   []string{},
			ActionItems: []string{},
		}
	}
	if resp.KeyPoints == nil {
		resp.KeyPoints = []string{}
	}
	if resp.ActionItems == nil {
		resp.ActionItems = []string{}
	}
	return c.JSON(resp)
}

// ---------------------------------------------------------------------------
// POST /ai/reply
// ---------------------------------------------------------------------------

type replySuggestion struct {
	Tone string `json:"tone"`
	Text string `json:"text"`
}

type replyResponse struct {
	Suggestions []replySuggestion `json:"suggestions"`
}

// HandleReply handles POST /ai/reply.
func (h *Handler) HandleReply(c *fiber.Ctx) error {
	if !h.cfg.Enabled {
		return disabledResponse(c)
	}
	var req struct {
		Thread string `json:"thread"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_json: " + err.Error()})
	}

	prompt := strings.ReplaceAll(mailReplyPrompt, "{{THREAD}}", escapeForPrompt(req.Thread))
	raw, err := h.complete(c, prompt, "")
	if err != nil {
		log.Printf("[mail_ai] reply error: %v", err)
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "service_unavailable"})
	}

	var resp replyResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &resp); err != nil || len(resp.Suggestions) != 3 {
		resp = replyResponse{
			Suggestions: []replySuggestion{
				{Tone: "concise", Text: strings.TrimSpace(raw)},
				{Tone: "detailed", Text: strings.TrimSpace(raw)},
				{Tone: "decline", Text: strings.TrimSpace(raw)},
			},
		}
	}
	return c.JSON(resp)
}

// ---------------------------------------------------------------------------
// POST /ai/extract-actions
// ---------------------------------------------------------------------------

type actionItem struct {
	Verb    string  `json:"verb"`
	Subject string  `json:"subject"`
	DueISO  *string `json:"due_iso"`
}

type extractActionsResponse struct {
	Actions []actionItem `json:"actions"`
}

// HandleExtractActions handles POST /ai/extract-actions.
func (h *Handler) HandleExtractActions(c *fiber.Ctx) error {
	if !h.cfg.Enabled {
		return disabledResponse(c)
	}
	var req struct {
		Thread string `json:"thread"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_json: " + err.Error()})
	}

	prompt := strings.ReplaceAll(mailExtractActionsPrompt, "{{THREAD}}", escapeForPrompt(req.Thread))
	raw, err := h.complete(c, prompt, "")
	if err != nil {
		log.Printf("[mail_ai] extract-actions error: %v", err)
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "service_unavailable"})
	}

	var resp extractActionsResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &resp); err != nil {
		resp = extractActionsResponse{Actions: []actionItem{}}
	}
	if resp.Actions == nil {
		resp.Actions = []actionItem{}
	}
	return c.JSON(resp)
}

// ---------------------------------------------------------------------------
// POST /ai/phishing
// ---------------------------------------------------------------------------

type phishingRequest struct {
	MessageHeaders string   `json:"message_headers"`
	MessageBody    string   `json:"message_body"`
	URLs           []string `json:"urls"`
}

type phishingResponse struct {
	Verdict            string   `json:"verdict"`
	Confidence         float64  `json:"confidence"`
	Reasons            []string `json:"reasons"`
	SuspiciousElements []string `json:"suspicious_elements"`
}

// HandlePhishing handles POST /ai/phishing.
func (h *Handler) HandlePhishing(c *fiber.Ctx) error {
	if !h.cfg.Enabled {
		return disabledResponse(c)
	}
	var req phishingRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid_json: " + err.Error()})
	}

	userContent := buildPhishingUserContent(req)
	completion, err := h.complete(c, phishingPrompt, userContent)
	if err != nil {
		log.Printf("[mail_ai] phishing error: %v", err)
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "service_unavailable"})
	}

	verdict, parseErr := parsePhishingVerdict(completion)
	if parseErr != nil {
		verdict = &phishingResponse{
			Verdict:            "suspicious",
			Confidence:         0.0,
			Reasons:            []string{"llm_parse_error"},
			SuspiciousElements: []string{},
		}
	}
	return c.JSON(verdict)
}

// buildPhishingUserContent assembles a compact representation of the message for the LLM.
func buildPhishingUserContent(req phishingRequest) string {
	var sb strings.Builder
	if req.MessageHeaders != "" {
		hdr := req.MessageHeaders
		if len(hdr) > 1500 {
			hdr = hdr[:1500] + "...[truncated]"
		}
		sb.WriteString("HEADERS:\n")
		sb.WriteString(hdr)
		sb.WriteString("\n\n")
	}
	if req.MessageBody != "" {
		body := req.MessageBody
		if len(body) > 2000 {
			body = body[:2000] + "...[truncated]"
		}
		sb.WriteString("BODY:\n")
		sb.WriteString(body)
		sb.WriteString("\n\n")
	}
	if len(req.URLs) > 0 {
		sb.WriteString("URLS:\n")
		for i, u := range req.URLs {
			if i >= 10 {
				sb.WriteString(fmt.Sprintf("...[%d more URLs]\n", len(req.URLs)-10))
				break
			}
			sb.WriteString(u)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// parsePhishingVerdict attempts to parse the LLM completion as a phishingResponse JSON object.
func parsePhishingVerdict(completion string) (*phishingResponse, error) {
	s := strings.TrimSpace(completion)
	if idx := strings.Index(s, "{"); idx > 0 {
		s = s[idx:]
	}
	if idx := strings.LastIndex(s, "}"); idx >= 0 && idx < len(s)-1 {
		s = s[:idx+1]
	}

	var resp phishingResponse
	if err := json.Unmarshal([]byte(s), &resp); err != nil {
		return nil, fmt.Errorf("phishing: parse verdict JSON: %w", err)
	}

	switch resp.Verdict {
	case "phishing", "suspicious", "clean":
		// OK.
	default:
		return nil, fmt.Errorf("phishing: unknown verdict %q", resp.Verdict)
	}

	if resp.Confidence < 0 {
		resp.Confidence = 0
	}
	if resp.Confidence > 1 {
		resp.Confidence = 1
	}
	if resp.Reasons == nil {
		resp.Reasons = []string{}
	}
	if resp.SuspiciousElements == nil {
		resp.SuspiciousElements = []string{}
	}
	return &resp, nil
}

// ---------------------------------------------------------------------------
// Completion client
// ---------------------------------------------------------------------------

// completionClient calls a configurable OpenAI-compatible chat-completion
// endpoint and drains the SSE response into a plain string.
type completionClient struct {
	endpoint string // e.g. "http://localhost:8080/api/ai/chat"
	apiKey   string // sent as "Authorization: Bearer <key>" when non-empty
	model    string // model slug forwarded to the endpoint
	http     *http.Client
}

// chatMessage mirrors the wire format expected by the endpoint.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatRequest is the body sent to the completion endpoint.
type chatRequest struct {
	Model    string        `json:"model,omitempty"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

func newCompletionClient(cfg config.AIConfig) *completionClient {
	return &completionClient{
		endpoint: cfg.Endpoint,
		apiKey:   cfg.APIKey,
		model:    cfg.Model,
		http:     &http.Client{Timeout: 90 * time.Second},
	}
}

// Close implements completer. The remote client holds nothing to release: its
// http.Client uses the shared default transport.
func (c *completionClient) Close() error { return nil }

// complete sends systemPrompt (and optional userContent) to the configured
// endpoint and returns the concatenated completion text.
//
// It mirrors the drainSSEText logic from the vulos airouter mail_handler.go,
// parsing the OpenAI-compatible SSE wire format used by both POST /api/ai/chat
// and llmux's POST /v1/chat/completions:
//
//	data: {"choices":[{"delta":{"content":"..."}}]}
//	data: [DONE]
//
// bearer is the per-request account token forwarded as "Authorization: Bearer
// <bearer>"; when empty, the client's static api_key is used instead, and when
// that is also empty no Authorization header is sent.
//
// When userContent is empty, only the system message is sent (the prompt
// itself carries all the necessary context for mail AI tasks).
func (c *completionClient) complete(ctx context.Context, bearer, systemPrompt, userContent string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if c.endpoint == "" {
		return "", fmt.Errorf("ai: no endpoint configured")
	}

	msgs := []chatMessage{
		{Role: "system", Content: systemPrompt},
	}
	if userContent != "" {
		msgs = append(msgs, chatMessage{Role: "user", Content: userContent})
	}

	body := chatRequest{
		Model:    c.model,
		Messages: msgs,
		Stream:   true,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("ai: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("ai: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer == "" {
		bearer = c.apiKey
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("ai: endpoint request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("ai: endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	text, err := drainSSEText(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ai: drain SSE: %w", err)
	}
	return text, nil
}

// drainSSEText reads an SSE stream and concatenates all content delta fields.
// It parses the OpenAI-compatible SSE format emitted by the vulos airouter
// /api/ai/chat endpoint:
//
//	data: {"choices":[{"delta":{"content":"chunk"}}]}
//	data: [DONE]
//
// It also handles a non-streaming fallback where the entire content is in a
// top-level "content" field.
func drainSSEText(r io.Reader) (string, error) {
	var sb strings.Builder
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := line[6:]
		if payload == "[DONE]" {
			break
		}
		var ev struct {
			Choices []struct {
				Delta struct{ Content string } `json:"delta"`
			} `json:"choices"`
			Content string `json:"content,omitempty"`
		}
		if err := json.Unmarshal([]byte(payload), &ev); err == nil {
			if len(ev.Choices) > 0 {
				sb.WriteString(ev.Choices[0].Delta.Content)
			} else if ev.Content != "" {
				sb.WriteString(ev.Content)
			}
		}
	}
	return sb.String(), scanner.Err()
}

// ---------------------------------------------------------------------------
// Prompt-injection guard
// ---------------------------------------------------------------------------

// escapeForPrompt neutralises {{...}} marker syntax in user-supplied strings
// before they are substituted into LLM prompt templates. Replaces "{{" → "{ {"
// and "}}" → "} }" so the resulting text no longer forms a valid {{KEY}} marker.
func escapeForPrompt(s string) string {
	s = strings.ReplaceAll(s, "{{", "{ {")
	s = strings.ReplaceAll(s, "}}", "} }")
	return s
}
