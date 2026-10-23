package mailstore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type imageTestClient func(*http.Request) (*http.Response, error)

func (f imageTestClient) Do(r *http.Request) (*http.Response, error) { return f(r) }

func TestAIImagesProviderPayloads(t *testing.T) {
	for _, provider := range []string{"openai", "openai-compatible", "gemini"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", provider, stream), func(t *testing.T) {
				client := imageTestClient(func(req *http.Request) (*http.Response, error) {
					var payload map[string]json.RawMessage
					if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
						t.Fatal(err)
					}
					var parts []map[string]any
					switch provider {
					case "openai":
						var input []struct {
							Role    string
							Content []map[string]any
						}
						if err := json.Unmarshal(payload["input"], &input); err != nil {
							t.Fatal(err)
						}
						parts = input[0].Content
						if input[0].Role != "user" || parts[0]["type"] != "input_text" || parts[2]["type"] != "input_image" || parts[2]["image_url"] != "data:image/png;base64,aW1hZ2U=" {
							t.Fatalf("bad Responses image: %s", payload["input"])
						}
					case "openai-compatible":
						var input []struct {
							Role    string
							Content json.RawMessage
						}
						_ = json.Unmarshal(payload["messages"], &input)
						_ = json.Unmarshal(input[1].Content, &parts)
						if input[0].Role != "system" || input[1].Role != "user" || parts[2]["type"] != "image_url" || parts[2]["image_url"].(map[string]any)["url"] != "data:image/png;base64,aW1hZ2U=" {
							t.Fatalf("bad Chat image: %s", payload["messages"])
						}
					case "gemini":
						var input []struct {
							Role  string
							Parts []map[string]any
						}
						_ = json.Unmarshal(payload["contents"], &input)
						parts = input[0].Parts
						inline := parts[2]["inlineData"].(map[string]any)
						if inline["mimeType"] != "image/png" || inline["data"] != "aW1hZ2U=" || input[0].Role != "user" {
							t.Fatalf("bad Gemini image: %s", payload["contents"])
						}
					}
					if parts[1]["text"] != "User message 1 image" {
						t.Fatal("image lost original turn label")
					}
					if payload["temperature"] == nil {
						t.Fatal("extra parameters lost")
					}
					response := ""
					if stream {
						switch provider {
						case "openai":
							response = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"seen\"}\n\ndata: {\"type\":\"response.completed\"}\n\n"
						case "gemini":
							response = "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"seen\"}]},\"finishReason\":\"STOP\"}]}\n\n"
						default:
							response = "data: {\"choices\":[{\"delta\":{\"content\":\"seen\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
						}
					} else {
						switch provider {
						case "openai":
							response = `{"output_text":"seen"}`
						case "gemini":
							response = `{"candidates":[{"content":{"parts":[{"text":"seen"}]}}]}`
						default:
							response = `{"choices":[{"message":{"content":"seen"}}]}`
						}
					}
					headers := make(http.Header)
					if stream {
						headers.Set("Content-Type", "text/event-stream")
					} else {
						headers.Set("Content-Type", "application/json")
					}
					return &http.Response{StatusCode: 200, Header: headers, Body: io.NopCloser(strings.NewReader(response))}, nil
				})
				var delta func(string) error
				visible := ""
				if stream {
					delta = func(s string) error { visible += s; return nil }
				}
				text, err := CreateAIResponseWithImages(context.Background(), client, AIModelRecord{Provider: provider, BaseURL: "https://mock.invalid/v1", Model: "vision", ExtraParams: `{"temperature":0.3}`}, "fake", "instructions", "question", []AIInputImage{{Label: "User message 1 image", ContentType: "image/png", Base64: "aW1hZ2U="}}, 4096, "medium", delta)
				if err != nil || text != "seen" || (stream && visible != "seen") {
					t.Fatalf("text=%q delta=%q err=%v", text, visible, err)
				}
			})
		}
	}
	_, err := CreateAIResponseWithImages(context.Background(), imageTestClient(func(*http.Request) (*http.Response, error) { t.Fatal("unsupported images were sent"); return nil, nil }), AIModelRecord{Provider: "deepseek"}, "", "", "", []AIInputImage{{}}, 100, "", nil)
	if err == nil {
		t.Fatal("DeepSeek silently dropped images")
	}
}
