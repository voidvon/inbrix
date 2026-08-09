package ai

// embedded.go — the in-process completion backend: llmux
// (github.com/vul-os/llmux) linked into LilMail as a Go library rather than
// called over HTTP.
//
// The library contract this relies on (core/gateway's own doc):
//
//   - gateway.New starts NO goroutines and reads no environment of its own. The
//     one qualification is that it connects EAGERLY when a Postgres DSN is set —
//     which is exactly why newEmbeddedClient clears that DSN below.
//   - Background work (the price-catalog syncer, the key-spend flusher, the
//     Redis ping) only ever begins in Run/Start. LilMail calls NEITHER, so an
//     embedded gateway makes no outbound call that a mail action did not cause.
//   - Chat/ChatStream/Embed work without any of that.

import (
	"context"
	"fmt"

	"lilmail/config"

	llmuxconfig "github.com/vul-os/llmux/core/config"
	"github.com/vul-os/llmux/core/gateway"
	"github.com/vul-os/llmux/core/openai"
)

// embeddedClient completes through an in-process llmux gateway.
//
// It uses the non-streaming Chat path: every LilMail AI route buffers the whole
// completion before answering (a summary, three reply suggestions, a phishing
// verdict — none of them stream to the browser), so the streaming callback would
// only reassemble what Chat already returns.
//
// Not available embedded: per-account BYOK and the llmux control-plane
// integration. gateway.New does NOT build either from config — llmux's own
// composition root wires them with SetBYOKStore / SetIdentity, and
// integration/cp is a package the core deliberately never imports. So a `byok`
// or `cp` block in llmux_config is inert here and every request uses the
// central provider keys that config holds. A deployment that needs each
// account's own keys, or central metering, wants mode = "remote" pointed at a
// full llmux. TestEmbedded_NoBYOKOrControlPlane pins this so the claim cannot
// go quietly stale under a future llmux.
type embeddedClient struct {
	gw    *gateway.Gateway
	model string
}

// newEmbeddedClient builds the in-process gateway from llmux's own JSON config
// at cfg.LLMuxConfig (empty = llmux's defaults plus its environment
// auto-detection), then removes everything a mail client must not host.
func newEmbeddedClient(cfg config.AIConfig) (*embeddedClient, error) {
	if cfg.Model == "" {
		return nil, fmt.Errorf("ai: [ai] model is required when mode = %q (llmux refuses a request with no model)", config.AIModeEmbedded)
	}

	lcfg, err := llmuxconfig.Load(cfg.LLMuxConfig)
	if err != nil {
		return nil, fmt.Errorf("ai: load llmux config %q: %w", cfg.LLMuxConfig, err)
	}

	// --- The embedded posture ------------------------------------------------
	// Each line below removes a behaviour that is reasonable for a standalone
	// gateway process and surprising inside a mail client. They are applied
	// AFTER Load so they override the file, the environment, and llmux's
	// defaults alike — none of them is negotiable from configuration.

	// No listener. Nothing in this path serves HTTP; blank the address so no
	// future shell can read a port out of the config and bind it.
	lcfg.Server.Addr = ""
	lcfg.Server.SocketPath = ""

	// No price-feed sync. llmux's default sources are openrouter.ai and a
	// GitHub raw URL; they are only fetched by Start/Run, which is never called
	// here, but clearing them means even a mistaken future Start cannot make a
	// mail client phone a price list. The built-in seed catalog still prices
	// requests offline.
	lcfg.Pricing.Sources = nil
	lcfg.Pricing.Azure = false

	// No shared remote state. Postgres is the one thing gateway.New connects
	// eagerly, and llmux resolves its DSN from DATABASE_URL / VULOS_DATABASE_URL
	// — so in a Vulos deployment, leaving this alone would have LilMail open a
	// database pool for LLM key spend merely because a shared DSN was exported.
	// Redis is the same class of surprise. Cross-replica key/spend state is a
	// reason to run llmux as a service and use mode = "remote".
	lcfg.Postgres = ""
	lcfg.PostgresSchema = ""
	lcfg.Redis = ""

	// No response cache unless the operator opted in. llmux's cache would retain
	// model output derived from mail in this process after the request ends; see
	// the package doc's Privacy section. With Redis already cleared above, the
	// opt-in cache is the in-memory LRU only — it can never reach a disk.
	if !cfg.LLMuxCache {
		lcfg.Cache.Enabled = false
		lcfg.Cache.Semantic = false
	}

	if len(lcfg.Providers) == 0 {
		return nil, fmt.Errorf("ai: embedded mode has no llmux providers — set [ai] llmux_config to a file that configures at least one provider (llmux_config = %q)", cfg.LLMuxConfig)
	}

	gw, err := gateway.New(lcfg)
	if err != nil {
		return nil, fmt.Errorf("ai: build embedded llmux gateway: %w", err)
	}

	// Fail loudly at startup on a model no provider can serve. Prepare only
	// resolves the route (no dispatch, no I/O), and it is the same check every
	// request runs — so a green startup means [ai] model is actually routable
	// rather than a 502 on the user's first summarize.
	if _, err := gw.Prepare(context.Background(), cfg.Model); err != nil {
		_ = gw.Close()
		return nil, fmt.Errorf("ai: embedded mode cannot route [ai] model %q: %w", cfg.Model, err)
	}

	return &embeddedClient{gw: gw, model: cfg.Model}, nil
}

// complete implements completer against the in-process gateway.
//
// bearer is the caller's account token (from [ai] account_header, else the
// static api_key). It goes through the gateway's Authorize, which is the same
// single auth path llmux's HTTP shell uses — an embedded host cannot get a laxer
// check than a network client. With no virtual keys configured Authorize is a
// no-op, which is the standalone posture: an in-process caller is already
// trusted. The release it returns frees any budget reservation and must always
// be called.
func (c *embeddedClient) complete(ctx context.Context, bearer, systemPrompt, userContent string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	ctx, release, err := c.gw.Authorize(ctx, bearer)
	if err != nil {
		return "", fmt.Errorf("ai: llmux authorize: %w", err)
	}
	defer release()

	msgs := []openai.Message{{Role: "system", Content: openai.Str(systemPrompt)}}
	if userContent != "" {
		msgs = append(msgs, openai.Message{Role: "user", Content: openai.Str(userContent)})
	}

	res, err := c.gw.Chat(ctx, &openai.ChatCompletionRequest{Model: c.model, Messages: msgs})
	if err != nil {
		return "", fmt.Errorf("ai: llmux chat: %w", err)
	}
	if res == nil || res.Response == nil || len(res.Response.Choices) == 0 {
		return "", fmt.Errorf("ai: llmux returned no completion choices")
	}
	return res.Response.Choices[0].Message.Content.String(), nil
}

// Close implements completer, releasing the gateway's resources. There is no
// background work to stop: LilMail never calls Run or Start.
func (c *embeddedClient) Close() error { return c.gw.Close() }
