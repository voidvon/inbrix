package web

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"inbrix/internal/agenttest"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	mailapi "inbrix/handlers/api"
	"inbrix/mailstore"
)

func chatTestPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 32, 24))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestChatImageUploadOwnershipAndValidation(t *testing.T) {
	h, app, account, other := outboxTestApp(t)
	ai := NewAISettingsHandler(h.store, h.config, h.mailDB)
	app.Post("/images", ai.HandleUploadAIChatAttachment)
	app.Get("/images/:id", ai.HandleAIChatAttachment)
	app.Delete("/images/:id", ai.HandleAIChatAttachment)
	outer := fiber.New(fiber.Config{BodyLimit: 25 << 20})
	outer.Mount("/", app)
	app = outer
	imageBytes := chatTestPNG(t)
	upload := func(email, name string, data []byte, status int) mailstore.AIChatAttachment {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		_ = writer.WriteField("accountEmail", email)
		part, _ := writer.CreateFormFile("file", name)
		_, _ = part.Write(data)
		_ = writer.Close()
		req := httptest.NewRequest("POST", "/images", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		if res.StatusCode != status {
			t.Fatalf("upload status=%d body=%s want=%d", res.StatusCode, raw, status)
		}
		var result mailstore.AIChatAttachment
		if status == 201 {
			_ = json.Unmarshal(raw, &result)
		}
		return result
	}
	// A generic multipart Content-Type and a misleading extension are ignored.
	attachment := upload(account.Email, "photo.txt", imageBytes, 201)
	if attachment.ContentType != "image/png" || attachment.Size != len(imageBytes) || attachment.ID == "" {
		t.Fatal(attachment)
	}
	upload(other.Email, "photo.png", imageBytes, 404)
	upload(account.Email, "photo.png", []byte("<svg onload='alert(1)'/>"), 400)
	upload(account.Email, "truncated.png", imageBytes[:len(imageBytes)-15], 400)
	upload(account.Email, "large.png", make([]byte, mailstore.MaxAIImageBytes+1), 413)
	request := func(method, email string, outsider bool, status int) []byte {
		t.Helper()
		req := httptest.NewRequest(method, "/images/"+attachment.ID+"?accountEmail="+email, nil)
		if outsider {
			req.Header.Set("X-Test-Outsider", "1")
		}
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		if res.StatusCode != status {
			t.Fatalf("%s status=%d want=%d: %s", method, res.StatusCode, status, raw)
		}
		if status == 200 && (res.Header.Get("Content-Type") != "image/png" || res.Header.Get("Cache-Control") != "private, no-store") {
			t.Fatal("unsafe response headers")
		}
		return raw
	}
	if !bytes.Equal(request("GET", account.Email, false, 200), imageBytes) {
		t.Fatal("image bytes changed")
	}
	request("GET", other.Email, true, 404)
	request("DELETE", other.Email, true, 404)
	// A second mailbox owned by the same user cannot use another mailbox's image.
	second, err := h.mailDB.UpsertAccount(context.Background(), mailstore.Account{OwnerID: account.OwnerID, Email: "second@example.com", Username: "second", EncryptedPassword: "enc"})
	if err != nil {
		t.Fatal(err)
	}
	request("GET", second.Email, false, 404)
	_, err = ai.resolveAgentImages(context.Background(), account.OwnerID, second.ID, []AIAgentChatMessage{{Role: "user", Attachments: []mailstore.AIChatAttachment{attachment}}})
	if err == nil {
		t.Fatal("cross-mailbox reference accepted")
	}
	request("DELETE", account.Email, false, 204)
	request("GET", account.Email, false, 404)
}

func TestAgentChatImagesRemainAcrossToolTurnsAndHistory(t *testing.T) {
	h, app, account, _ := outboxTestApp(t)
	ai := NewAISettingsHandler(h.store, h.config, h.mailDB)
	h.config.Encryption.Key = "0123456789abcdef0123456789abcdef"
	key, err := mailapi.EncryptJSON("fake", h.config.Encryption.Key)
	if err != nil {
		t.Fatal(err)
	}
	model, err := h.mailDB.CreateAIModel(context.Background(), mailstore.AIModelRecord{OwnerID: account.OwnerID, Provider: "openai", BaseURL: "https://mock.invalid/v1", Model: "vision", EncryptedAPIKey: key})
	if err != nil {
		t.Fatal(err)
	}
	imageBytes := chatTestPNG(t)
	attachment, err := h.mailDB.CreateAIChatAttachment(context.Background(), account.OwnerID, account.ID, "需求.png", "image/png", imageBytes)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	ai.client = &http.Client{Transport: agentStreamTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		var payload struct {
			Input []struct{ Content []map[string]any }
		}
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		parts := payload.Input[0].Content
		if len(parts) != 3 || parts[2]["image_url"] != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(imageBytes) || !strings.Contains(parts[1]["text"].(string), attachment.ID) {
			t.Fatal("image missing or changed during tool turn")
		}
		answer := "看到了需求图片"
		if calls == 1 {
			answer = "```tool_call\n{\"name\":\"search_products\",\"args\":{\"query\":\"valve\"}}\n```"
		}
		body, _ := json.Marshal(agenttest.Response(answer, calls))
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(body))}, nil
	})}
	app.Post("/api/ai/agent-chat", ai.HandleAgentChat)
	input := AIAgentChatRequest{AccountEmail: account.Email, ModelID: model.ID, Messages: []AIAgentChatMessage{{Role: "user", Attachments: []mailstore.AIChatAttachment{attachment}}}}
	status, _, raw := postAgentContext(t, app, input)
	if status != 200 || calls != 2 {
		t.Fatalf("status=%d calls=%d: %s", status, calls, raw)
	}
	// A text-only follow-up still passes prior user images, for references like “the previous image”.
	input.Messages = append(input.Messages, AIAgentChatMessage{Role: "assistant", Content: "已看过"}, AIAgentChatMessage{Role: "user", Content: "上一张图片中是什么型号？"})
	status, _, raw = postAgentContext(t, app, input)
	if status != 200 || calls != 3 {
		t.Fatalf("follow-up: %d %s", status, raw)
	}
	input.Messages[0].Attachments[0].ID = "missing"
	status, _, _ = postAgentContext(t, app, input)
	if status != 400 || calls != 3 {
		t.Fatal("unavailable image reached provider")
	}
}

func TestAgentChatImageCapabilityOverride(t *testing.T) {
	for _, provider := range []string{"openai-compatible"} {
		for _, sessionChat := range []bool{false, true} {
			name := provider + "/legacy"
			if sessionChat {
				name = provider + "/session"
			}
			t.Run(name, func(t *testing.T) {
				h, app, account, _ := agentContextTestApp(t, func(agentTestRequest) string {
					t.Fatal("unexpected Responses request")
					return ""
				})
				ctx := context.Background()
				model, err := h.mailDB.GetDefaultAIModel(ctx, account.OwnerID)
				if err != nil {
					t.Fatal(err)
				}
				enabled := false
				model.Provider, model.Model, model.SupportsImages = provider, "custom-vision", &enabled
				if _, err := h.mailDB.UpdateAIModel(ctx, model); err != nil {
					t.Fatal(err)
				}
				img, err := h.mailDB.CreateAIChatAttachment(ctx, account.OwnerID, account.ID, "test.png", "image/png", chatTestPNG(t))
				if err != nil {
					t.Fatal(err)
				}
				input := AIAgentChatRequest{AccountEmail: account.Email, ModelID: model.ID, Messages: []AIAgentChatMessage{{Role: "user", Attachments: []mailstore.AIChatAttachment{img}}}}
				if sessionChat {
					session, err := h.mailDB.CreateAIChatSession(ctx, account.OwnerID, account.ID, "document", "", "New", "quotation")
					if err != nil {
						t.Fatal(err)
					}
					version := int64(0)
					input.SessionID, input.RequestID, input.Version = session.ID, "image-request", &version
				}
				calls := 0
				h.client = &http.Client{Transport: agentStreamTransport(func(req *http.Request) (*http.Response, error) {
					calls++
					var payload struct {
						Messages []struct {
							Role    string
							Content json.RawMessage
						}
					}
					if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
						t.Fatal(err)
					}
					if !strings.HasSuffix(req.URL.Path, "/chat/completions") || len(payload.Messages) != 2 || payload.Messages[1].Role != "user" || !strings.Contains(string(payload.Messages[1].Content), "data:image/png;base64,") {
						t.Fatalf("image did not reach Chat Completions: %+v", payload)
					}
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"Image received"},"finish_reason":"stop"}]}`))}, nil
				})}
				status, _, raw := postAgentContext(t, app, input)
				if status != 400 || calls != 0 || !strings.Contains(raw, "Enable image input") {
					t.Fatalf("disabled capability: %d calls=%d %s", status, calls, raw)
				}
				if sessionChat {
					saved, err := h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, input.SessionID)
					if err != nil || saved.Version != 0 || len(saved.Messages) != 0 {
						t.Fatalf("disabled image mutated session: %+v %v", saved, err)
					}
				}
				enabled = true
				if _, err := h.mailDB.UpdateAIModel(ctx, model); err != nil {
					t.Fatal(err)
				}
				status, response, raw := postAgentContext(t, app, input)
				if status != 200 || calls != 1 || response.Content != "Image received" {
					t.Fatalf("enabled capability: %d calls=%d %s", status, calls, raw)
				}
			})
		}
	}
}
