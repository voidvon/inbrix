package mailstore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"inbrix/internal/agentruntime"
)

func TestAIOriginalFilesProviderPayloads(t *testing.T) {
	files := []AIInputAttachment{
		{Filename: "scan.pdf", ContentType: "application/pdf", Base64: "JVBERi0="},
		{Filename: "合同.docx", ContentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Base64: "UEsDBA=="},
		{Filename: "prices.xlsx", ContentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", Base64: "UEsDBAo="},
	}
	for _, provider := range []string{"openai", "gemini", "openai-compatible"} {
		for _, stream := range []bool{false, true} {
			for _, agent := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/stream=%v/agent=%v", provider, stream, agent), func(t *testing.T) {
					enabled, disabled := true, false
					model := AIModelRecord{Provider: provider, Model: "file-model", BaseURL: "https://mock.invalid/v1", SupportsFiles: &enabled, SupportsImages: &disabled}
					attachments := files
					if provider == "openai-compatible" {
						attachments = files[:1]
					}
					calls := 0
					client := nativeAgentClient(func(req *http.Request) (*http.Response, error) {
						calls++
						var payload map[string]any
						if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
							t.Fatal(err)
						}
						var parts []any
						switch provider {
						case "openai":
							parts = payload["input"].([]any)[0].(map[string]any)["content"].([]any)
						case "gemini":
							parts = payload["contents"].([]any)[0].(map[string]any)["parts"].([]any)
						default:
							parts = payload["messages"].([]any)[1].(map[string]any)["content"].([]any)
						}
						if len(parts) != 1+2*len(attachments) {
							t.Fatalf("missing file parts: %+v", parts)
						}
						for i, file := range attachments {
							part := parts[2+i*2].(map[string]any)
							switch provider {
							case "openai":
								if part["type"] != "input_file" || part["filename"] != file.Filename || part["file_data"] != "data:"+file.ContentType+";base64,"+file.Base64 {
									t.Fatalf("original file changed: %+v", part)
								}
							case "gemini":
								data := part["inlineData"].(map[string]any)
								if data["mimeType"] != file.ContentType || data["data"] != file.Base64 {
									t.Fatalf("original file changed: %+v", part)
								}
							default:
								data := part["file"].(map[string]any)
								if part["type"] != "file" || data["filename"] != file.Filename || data["file_data"] != "data:"+file.ContentType+";base64,"+file.Base64 {
									t.Fatalf("original PDF changed: %+v", part)
								}
							}
						}
						response, contentType := "", "application/json"
						if stream {
							response, contentType = nativeAgentSSE(provider, true), "text/event-stream"
						} else {
							raw, _ := json.Marshal(nativeAgentFixture(provider, true))
							response = string(raw)
						}
						return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(response))}, nil
					})
					var delta func(string) error
					if stream {
						delta = func(string) error { return nil }
					}
					for turn := 0; turn < 2; turn++ {
						if agent {
							messages := []agentruntime.Message{{Role: "user", Text: "Analyze original files"}}
							if turn == 1 {
								messages = append(messages, agentruntime.Message{Role: "assistant", ToolCalls: []agentruntime.ToolCall{{ID: "call", Name: "read_source", Args: map[string]any{}}}}, agentruntime.Message{Role: "tool", Name: "read_source", ToolCallID: "call", Text: `{}`})
							}
							_, err := CreateAgentResponse(context.Background(), client, model, "fake", "instructions", agentruntime.ModelRequest{Messages: messages}, attachments, 1000, "", delta)
							if err != nil {
								t.Fatal(err)
							}
						} else if _, err := CreateAIResponseWithImages(context.Background(), client, model, "fake", "instructions", "Analyze original files", attachments, 1000, "medium", delta); err != nil {
							t.Fatal(err)
						}
					}
					if calls != 2 {
						t.Fatal("files lost across requests")
					}
				})
			}
		}
	}
}

func TestAIFileCapabilityPersistenceAndMigration(t *testing.T) {
	s, ctx := openTestStore(t), context.Background()
	for _, provider := range []string{"openai", "gemini", "openai-compatible", "commandcode-go"} {
		model, err := s.CreateAIModel(ctx, AIModelRecord{OwnerID: "alice", Provider: provider, Model: provider, BaseURL: "https://mock.invalid", EncryptedAPIKey: "encrypted"})
		if err != nil {
			t.Fatal(err)
		}
		if model.SupportsFileInput() != (provider == "openai" || provider == "gemini") {
			t.Fatal("incorrect protocol default", provider)
		}
		for _, enabled := range []bool{false, true} {
			model.SupportsFiles = &enabled
			if _, err := s.UpdateAIModel(ctx, model); err != nil {
				t.Fatal(err)
			}
			model.SupportsFiles = nil
			model, err = s.UpdateAIModel(ctx, model)
			if err != nil || model.SupportsFiles == nil || *model.SupportsFiles != enabled || model.EncryptedAPIKey != "encrypted" {
				t.Fatalf("file setting lost: %+v %v", model, err)
			}
		}
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE ai_models DROP COLUMN supports_files`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.ensureAIModelSupportsFilesColumn(ctx); err != nil {
			t.Fatal(err)
		}
	}
	models, err := s.ListAIModels(ctx, "alice")
	if err != nil || len(models) != 4 {
		t.Fatal(models, err)
	}
}

func TestUnsupportedFilesNeverReachProvider(t *testing.T) {
	enabled, disabled := true, false
	for _, model := range []AIModelRecord{
		{Provider: "openai", SupportsFiles: &disabled},
		{Provider: "openai-compatible"},
		{Provider: "openai-compatible", SupportsFiles: &enabled},
		{Provider: "commandcode-go", SupportsFiles: &enabled},
	} {
		file := AIInputAttachment{Filename: "test.docx", ContentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Base64: "UEsDBA=="}
		client := nativeAgentClient(func(*http.Request) (*http.Response, error) {
			t.Fatal("unsupported file reached provider")
			return nil, nil
		})
		if _, err := CreateAgentResponse(context.Background(), client, model, "", "", agentruntime.ModelRequest{}, []AIInputAttachment{file}, 100, "", nil); err == nil {
			t.Fatal("unsupported file accepted", model.Provider)
		}
	}
}
