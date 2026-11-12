package web

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"inbrix/internal/agenttest"
	"inbrix/mailstore"
)

func chatTestOffice(t *testing.T, document string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, name := range []string{"[Content_Types].xml", document} {
		part, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write([]byte(`<?xml version="1.0"?><document>原文件内容</document>`))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestChatDocumentUploadsKeepOriginalBytes(t *testing.T) {
	h, app, account, other := outboxTestApp(t)
	ai := NewAISettingsHandler(h.store, h.config, h.mailDB)
	app.Post("/files", ai.HandleUploadAIChatAttachment)
	app.Get("/files/:id", ai.HandleAIChatAttachment)
	app.Delete("/files/:id", ai.HandleAIChatAttachment)
	for _, file := range []struct {
		name, mime string
		data       []byte
	}{
		{"扫描件.PDF", "application/pdf", []byte("%PDF-1.7\n1 0 obj << /Type /Catalog >> endobj\n%%EOF\n")},
		{"合同.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", chatTestOffice(t, "word/document.xml")},
		{"价格.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", chatTestOffice(t, "xl/workbook.xml")},
	} {
		t.Run(file.name, func(t *testing.T) {
			var body bytes.Buffer
			w := multipart.NewWriter(&body)
			_ = w.WriteField("accountEmail", account.Email)
			part, _ := w.CreateFormFile("file", file.name)
			_, _ = part.Write(file.data)
			_ = w.Close()
			req := httptest.NewRequest("POST", "/files", &body)
			req.Header.Set("Content-Type", w.FormDataContentType())
			res, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			var attachment mailstore.AIChatAttachment
			err = json.NewDecoder(res.Body).Decode(&attachment)
			res.Body.Close()
			if res.StatusCode != 201 || err != nil || attachment.ContentType != file.mime || attachment.Size != len(file.data) {
				t.Fatalf("upload failed: %d %+v %v", res.StatusCode, attachment, err)
			}
			res, err = app.Test(httptest.NewRequest("GET", "/files/"+attachment.ID+"?accountEmail="+account.Email, nil))
			if err != nil {
				t.Fatal(err)
			}
			data, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if res.StatusCode != 200 || !bytes.Equal(data, file.data) || !strings.HasPrefix(res.Header.Get("Content-Disposition"), "attachment;") || res.Header.Get("Content-Type") != file.mime || res.Header.Get("X-Content-Type-Options") != "nosniff" {
				t.Fatalf("download changed original file or headers: %d %v", res.StatusCode, res.Header)
			}
			if _, err := ai.resolveAgentAttachments(context.Background(), account.OwnerID, other.ID, []AIAgentChatMessage{{Role: "user", Attachments: []mailstore.AIChatAttachment{attachment}}}); err == nil {
				t.Fatal("cross-mailbox document accepted")
			}
		})
	}
	for _, file := range []struct {
		name string
		data []byte
	}{
		{"fake.pdf", []byte("<html>renamed file</html>")},
		{"broken.pdf", []byte("%PDF-1.7\ntruncated")},
		{"fake.docx", []byte("plain text")},
		{"renamed.xlsx", chatTestOffice(t, "word/document.xml")},
		{"broken.docx", chatTestOffice(t, "word/document.xml")[:30]},
		{"macro.exe", []byte("MZ")},
	} {
		if _, err := validateAIAttachment(file.name, file.data); err == nil {
			t.Fatal("invalid file accepted", file.name)
		}
	}
}

func TestChatFilePreflightHistoryAndToolTurns(t *testing.T) {
	for _, sessionChat := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "session"}[sessionChat], func(t *testing.T) {
			h, app, account, _ := agentContextTestApp(t, func(agentTestRequest) string { return "unused" })
			ctx := context.Background()
			model, err := h.mailDB.GetDefaultAIModel(ctx, account.OwnerID)
			if err != nil {
				t.Fatal(err)
			}
			disabled := false
			model.SupportsImages = &disabled // File input is independent of images.
			if _, err := h.mailDB.UpdateAIModel(ctx, model); err != nil {
				t.Fatal(err)
			}
			fileData := chatTestOffice(t, "xl/workbook.xml")
			attachment, err := h.mailDB.CreateAIChatAttachment(ctx, account.OwnerID, account.ID, "original.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", fileData)
			if err != nil {
				t.Fatal(err)
			}
			// Client metadata is untrusted; model input must use the stored record.
			ref := attachment
			ref.ContentType, ref.Filename, ref.Size = "image/png", "forged.png", 1
			input := AIAgentChatRequest{AccountEmail: account.Email, ModelID: model.ID, Messages: []AIAgentChatMessage{{Role: "user", Attachments: []mailstore.AIChatAttachment{ref}}}}
			if sessionChat {
				session, err := h.mailDB.CreateAIChatSession(ctx, account.OwnerID, account.ID, "document", "", "Files", "quotation")
				if err != nil {
					t.Fatal(err)
				}
				version := int64(0)
				input.SessionID, input.RequestID, input.Version = session.ID, "file-request", &version
			}
			calls := 0
			h.client = &http.Client{Transport: agentStreamTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				var payload struct {
					Input []struct{ Content []map[string]any }
				}
				if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				parts := payload.Input[0].Content
				if len(parts) != 3 || parts[2]["type"] != "input_file" || parts[2]["filename"] != attachment.Filename || parts[2]["file_data"] != "data:"+attachment.ContentType+";base64,"+base64.StdEncoding.EncodeToString(fileData) {
					t.Fatal("original file missing or client metadata trusted", parts)
				}
				answer := "已读取原文件"
				if calls == 1 {
					answer = "```tool_call\n{\"name\":\"search_products\",\"args\":{\"query\":\"valve\"}}\n```"
				}
				raw, _ := json.Marshal(agenttest.Response(answer, calls))
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(raw))}, nil
			})}
			status, _, raw := postAgentContext(t, app, input)
			if status != 200 || calls != 2 {
				t.Fatalf("file tool turns: status=%d calls=%d %s", status, calls, raw)
			}
			input.Messages = append(input.Messages, AIAgentChatMessage{Role: "assistant", Content: "已读"}, AIAgentChatMessage{Role: "user", Content: "继续分析刚才的 Excel"})
			if sessionChat {
				session, err := h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, input.SessionID)
				if err != nil {
					t.Fatal(err)
				}
				input.Messages = input.Messages[len(input.Messages)-1:]
				input.RequestID, input.Version = "file-follow-up", &session.Version
			}
			status, _, raw = postAgentContext(t, app, input)
			if status != 200 || calls != 3 {
				t.Fatalf("file follow-up: status=%d calls=%d %s", status, calls, raw)
			}
			model.SupportsFiles = &disabled
			if _, err := h.mailDB.UpdateAIModel(ctx, model); err != nil {
				t.Fatal(err)
			}
			input.Messages = []AIAgentChatMessage{{Role: "user", Attachments: []mailstore.AIChatAttachment{ref}}}
			var previous mailstore.AIChatSession
			if sessionChat {
				previous, err = h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, input.SessionID)
				if err != nil {
					t.Fatal(err)
				}
				input.RequestID, input.Version = "unsupported-file", &previous.Version
			}
			status, _, raw = postAgentContext(t, app, input)
			if status != 400 || calls != 3 || !strings.Contains(raw, "Enable file input") {
				t.Fatalf("disabled file capability: status=%d calls=%d %s", status, calls, raw)
			}
			if sessionChat {
				session, err := h.mailDB.GetAIChatSession(ctx, account.OwnerID, account.ID, input.SessionID)
				if err != nil || session.Version != previous.Version || len(session.Messages) != len(previous.Messages) {
					t.Fatal("unsupported file mutated session", err)
				}
				// Switching to a text-only model omits prior files from its projection.
				messages := []AIAgentChatMessage{{Role: "user", Attachments: []mailstore.AIChatAttachment{ref}}, {Role: "user", Content: "继续"}}
				files, err := h.resolveAgentContextAttachments(ctx, account.OwnerID, account.ID, messages, model)
				if err != nil || len(files) != 0 {
					t.Fatal("incompatible history file forwarded", err)
				}
			}
		})
	}
}
