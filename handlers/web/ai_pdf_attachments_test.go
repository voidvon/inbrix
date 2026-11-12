package web

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"inbrix/internal/agenttest"
	"inbrix/internal/pdfinput"
	"inbrix/mailstore"
)

func TestPDFChatCommandCodePreparation(t *testing.T) {
	// WASM compilation is much slower under -race; warm it before HTTP timeout assertions.
	if _, err := pdfinput.Default.Inspect(context.Background(), agenttest.PDF(1)); err != nil {
		t.Fatal(err)
	}
	for _, sessionChat := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "session"}[sessionChat], func(t *testing.T) {
			h, app, account, other := agentContextTestApp(t, func(agentTestRequest) string { return "unused" })
			ctx := context.Background()
			model, _ := h.mailDB.GetDefaultAIModel(ctx, account.OwnerID)
			model.Provider, model.Model = "commandcode-go", "vision-test"
			if _, err := h.mailDB.UpdateAIModel(ctx, model); err != nil {
				t.Fatal(err)
			}
			original := agenttest.PDF(2)
			attachment, err := h.mailDB.CreateAIChatAttachment(ctx, account.OwnerID, account.ID, "合同.pdf", "application/pdf", original)
			if err != nil {
				t.Fatal(err)
			}
			ref := attachment
			ref.Filename, ref.ContentType = "forged.png", "image/png"
			input := AIAgentChatRequest{AccountEmail: account.Email, ModelID: model.ID, Messages: []AIAgentChatMessage{{Role: "user", Attachments: []mailstore.AIChatAttachment{ref}}}}
			if sessionChat {
				session, err := h.mailDB.CreateAIChatSession(ctx, account.OwnerID, account.ID, "document", "", "PDF", "quotation")
				if err != nil {
					t.Fatal(err)
				}
				version := int64(0)
				input.SessionID, input.RequestID, input.Version = session.ID, "pdf-request", &version
			}
			calls := 0
			var firstImages []string
			h.client = &http.Client{Transport: agentStreamTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				var payload struct {
					Params struct {
						Messages []struct{ Content []map[string]any }
					}
				}
				if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				var images []string
				for _, message := range payload.Params.Messages {
					for index, part := range message.Content {
						if part["type"] != "image" {
							continue
						}
						imageURL, _ := part["image"].(string)
						images = append(images, imageURL)
						if !strings.HasPrefix(imageURL, "data:image/jpeg;base64,") {
							t.Fatal("PDF bytes were sent as image")
						}
						data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(imageURL, "data:image/jpeg;base64,"))
						if err != nil {
							t.Fatal(err)
						}
						if _, err := jpeg.Decode(bytes.NewReader(data)); err != nil {
							t.Fatal("not a rendered JPEG", err)
						}
						label, _ := message.Content[index-1]["text"].(string)
						if !strings.Contains(label, "合同.pdf") || !strings.Contains(label, "PDF page") || strings.Contains(label, "forged") {
							t.Fatal("untrusted filename or missing page provenance", label)
						}
					}
				}
				if len(images) != 2 {
					t.Fatal("PDF pages lost", len(images))
				}
				if calls == 1 {
					firstImages = images
				} else if strings.Join(firstImages, "\n") != strings.Join(images, "\n") {
					t.Fatal("pages changed across tool turns or history")
				}
				wire := "{\"type\":\"text-delta\",\"text\":\"已读取两页\"}\n{\"type\":\"finish\",\"finishReason\":\"stop\"}\n"
				if calls == 1 {
					wire = "{\"type\":\"tool-call\",\"toolCallId\":\"pdf-call\",\"toolName\":\"search_products\",\"input\":{\"query\":\"valve\"}}\n{\"type\":\"finish\",\"finishReason\":\"tool-calls\"}\n"
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(wire))}, nil
			})}
			status, _, raw := postAgentContext(t, app, input)
			if status != 200 || calls != 2 {
				t.Fatalf("PDF tool roundtrip: %d calls=%d %s", status, calls, raw)
			}
			stored, err := h.mailDB.GetAIChatAttachment(ctx, account.OwnerID, account.ID, attachment.ID)
			if err != nil || !bytes.Equal(stored.Data, original) {
				t.Fatal("conversion modified original")
			}
			if sessionChat {
				session, _ := h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, input.SessionID)
				input.RequestID, input.Version = "pdf-follow-up", &session.Version
				input.Messages = []AIAgentChatMessage{{Role: "user", Content: "继续看刚才的 PDF"}}
			} else {
				input.Messages = append(input.Messages, AIAgentChatMessage{Role: "assistant", Content: "已读"}, AIAgentChatMessage{Role: "user", Content: "继续"})
			}
			status, _, raw = postAgentContext(t, app, input)
			if status != 200 || calls != 3 {
				t.Fatalf("PDF follow-up: %d calls=%d %s", status, calls, raw)
			}
			if _, err := h.planAgentAttachments(ctx, account.OwnerID, other.ID, []AIAgentChatMessage{{Role: "user", Attachments: []mailstore.AIChatAttachment{attachment}}}, model, true); err == nil {
				t.Fatal("cached PDF bypassed mailbox permissions")
			}
			if sessionChat {
				session, _ := h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, input.SessionID)
				history, _ := json.Marshal(session.Messages)
				if strings.Contains(string(history), "base64") {
					t.Fatal("derived bytes entered durable history")
				}
			}
		})
	}
}

func TestPDFPreflightAndBudgets(t *testing.T) {
	h, app, account, _ := agentContextTestApp(t, func(agentTestRequest) string { t.Fatal("invalid PDF reached model"); return "" })
	ctx := context.Background()
	model, _ := h.mailDB.GetDefaultAIModel(ctx, account.OwnerID)
	model.PDFInputMode = "images"
	if _, err := h.mailDB.UpdateAIModel(ctx, model); err != nil {
		t.Fatal(err)
	}
	session, _ := h.mailDB.CreateAIChatSession(ctx, account.OwnerID, account.ID, "document", "", "PDF", "quotation")
	version := int64(0)
	for _, data := range [][]byte{agenttest.PDF(pdfinput.MaxPages + 1), []byte("%PDF-1.7\nbroken\n%%EOF")} {
		file, err := h.mailDB.CreateAIChatAttachment(ctx, account.OwnerID, account.ID, "bad.pdf", "application/pdf", data)
		if err != nil {
			t.Fatal(err)
		}
		status, _, raw := postAgentContext(t, app, AIAgentChatRequest{AccountEmail: account.Email, SessionID: session.ID, RequestID: "reject-pdf", Version: &version, Messages: []AIAgentChatMessage{{Role: "user", Attachments: []mailstore.AIChatAttachment{file}}}})
		if status != 400 {
			t.Fatalf("invalid PDF not rejected: %d %s", status, raw)
		}
		fresh, _ := h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, session.ID)
		if fresh.Version != 0 || fresh.LastRun != nil {
			t.Fatal("invalid PDF saved a user turn")
		}
	}
	file, _ := h.mailDB.CreateAIChatAttachment(ctx, account.OwnerID, account.ID, "many.pdf", "application/pdf", agenttest.PDF(13))
	message := AIAgentChatMessage{Role: "user", Attachments: []mailstore.AIChatAttachment{file, file}}
	if _, err := h.planAgentAttachments(ctx, account.OwnerID, account.ID, []AIAgentChatMessage{message}, model, true); err == nil {
		t.Fatal("26 pages bypassed 24-image budget")
	}
	plan, err := h.planAgentAttachments(ctx, account.OwnerID, account.ID, []AIAgentChatMessage{message, {Role: "user", Content: "follow-up"}}, model, true)
	if err != nil || len(plan.attachments) != 1 {
		t.Fatal("historical PDFs should be admitted whole", len(plan.attachments), err)
	}
	// The encoded budget must be applied after expansion, without partial PDF input.
	key := "test-budget"
	h.pdfCache.put(key, [][]byte{make([]byte, 13<<20), make([]byte, 13<<20)})
	if _, err := h.prepareAgentAttachments(ctx, aiAttachmentPlan{attachments: []aiPlannedAttachment{{key: key, pages: 2, required: true}}}, nil); err == nil {
		t.Fatal("encoded render budget not enforced")
	}
}

func TestPDFProgressAndCacheExpiry(t *testing.T) {
	h, app, account, _ := agentContextTestApp(t, func(agentTestRequest) string { return "完成" })
	ctx := context.Background()
	model, _ := h.mailDB.GetDefaultAIModel(ctx, account.OwnerID)
	model.PDFInputMode = "images"
	_, _ = h.mailDB.UpdateAIModel(ctx, model)
	file, _ := h.mailDB.CreateAIChatAttachment(ctx, account.OwnerID, account.ID, "scan.pdf", "application/pdf", agenttest.PDF(2))
	body, _ := json.Marshal(AIAgentChatRequest{AccountEmail: account.Email, Messages: []AIAgentChatMessage{{Role: "user", Attachments: []mailstore.AIChatAttachment{file}}}})
	req := httptest.NewRequest("POST", "/api/ai/agent-chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	res, err := app.Test(req, 10000)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !bytes.Contains(raw, []byte(`"type":"attachment_progress"`)) || !bytes.Contains(raw, []byte(`"page":2,"pages":2`)) || !bytes.Contains(raw, []byte(`"type":"done"`)) {
		t.Fatal("missing PDF progress", string(raw))
	}
	h.pdfCache.mu.Lock()
	for key, entry := range h.pdfCache.entries {
		entry.used = time.Now().Add(-16 * time.Minute)
		h.pdfCache.entries[key] = entry
	}
	h.pdfCache.mu.Unlock()
	plan, err := h.planAgentAttachments(ctx, account.OwnerID, account.ID, []AIAgentChatMessage{{Role: "user", Attachments: []mailstore.AIChatAttachment{file}}}, model, true)
	if err != nil {
		t.Fatal(err)
	}
	if pages := h.pdfCache.get(plan.attachments[0].key); pages != nil {
		t.Fatal("expired PDF cache retained")
	}
}
