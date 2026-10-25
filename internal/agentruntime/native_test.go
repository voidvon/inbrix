package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func nativeOptions() Options {
	return Options{Input: "investigate", Tools: []ToolSpec{{Name: "read_source", Parameters: map[string]any{"type": "object"}}},
		Limits:  Limits{Turns: 4, CallsPerTurn: 4, Calls: 8, InputBytes: 16000, ResultBytes: 2000, ValidationRetries: 2},
		Execute: func(context.Context, ToolCall) (any, string, error) { return "evidence", "read", nil }}
}

func TestNativeLoopPairsEveryCallIncludingRefusedCalls(t *testing.T) {
	opts := nativeOptions()
	opts.Limits.CallsPerTurn = 1
	executed, turn := 0, 0
	opts.Execute = func(context.Context, ToolCall) (any, string, error) { executed++; return "evidence", "read", nil }
	opts.Complete = func(_ context.Context, req ModelRequest) (ModelResponse, error) {
		turn++
		if turn == 1 {
			return ModelResponse{Message: Message{ProviderData: json.RawMessage(`{"continuation":"opaque"}`), ToolCalls: []ToolCall{
				{ID: "a", Name: "read_source", Args: map[string]any{}}, {ID: "b", Name: "read_source", Args: map[string]any{}},
			}}}, nil
		}
		if !req.DisableTools || len(req.Messages) != 5 {
			t.Fatalf("missing synthesis boundary or call results: %+v", req)
		}
		if string(req.Messages[1].ProviderData) != `{"continuation":"opaque"}` || req.Messages[2].ToolCallID != "a" || req.Messages[3].ToolCallID != "b" || !strings.Contains(req.Messages[3].Text, "NOT executed") {
			t.Fatal("lost continuation or tool pairing", req.Messages)
		}
		return ModelResponse{Message: Message{Text: "partial coverage"}}, nil
	}
	out, err := Run(context.Background(), opts)
	if err != nil || out.Text != "partial coverage" || executed != 1 || turn != 2 {
		t.Fatal(out, executed, turn, err)
	}
}

func TestNativeLoopReturnsRegistryAndArgumentErrorsAsToolResults(t *testing.T) {
	opts := nativeOptions()
	turn, executed := 0, 0
	opts.Execute = func(context.Context, ToolCall) (any, string, error) { executed++; return nil, "", nil }
	opts.Complete = func(_ context.Context, req ModelRequest) (ModelResponse, error) {
		turn++
		if turn == 1 {
			return ModelResponse{Message: Message{ToolCalls: []ToolCall{{ID: "a", Name: "unavailable"}, {ID: "b", Name: "read_source", ArgumentError: "invalid JSON arguments"}}}}, nil
		}
		if len(req.Messages) != 4 || req.Messages[2].Role != "tool" || !strings.Contains(req.Messages[2].Text, "not available") || !strings.Contains(req.Messages[3].Text, "invalid JSON") {
			t.Fatal(req.Messages)
		}
		return ModelResponse{Message: Message{Text: "unable to read"}}, nil
	}
	out, err := Run(context.Background(), opts)
	if err != nil || executed != 0 || len(out.Calls) != 2 || out.Calls[0].Status != "error" || out.Calls[1].Status != "error" {
		t.Fatal(out, executed, err)
	}
}

func TestNativeSubmissionValidationReturnsMatchingToolFeedback(t *testing.T) {
	opts := nativeOptions()
	turn, executed := 0, 0
	opts.OutputSchema = map[string]any{"type": "object"}
	opts.Execute = func(context.Context, ToolCall) (any, string, error) { executed++; return "evidence", "read", nil }
	opts.Validate = func(text string) error {
		if !strings.Contains(text, "verified") || executed == 0 {
			return errors.New("read evidence first")
		}
		return nil
	}
	opts.Complete = func(_ context.Context, req ModelRequest) (ModelResponse, error) {
		turn++
		switch turn {
		case 1:
			return ModelResponse{Message: Message{ToolCalls: []ToolCall{{ID: "submit1", Name: "submit_result", Args: map[string]any{"answer": "guess"}}}}}, nil
		case 2:
			last := req.Messages[len(req.Messages)-1]
			if last.Role != "tool" || last.ToolCallID != "submit1" || !strings.Contains(last.Text, "read evidence first") {
				t.Fatal(last)
			}
			return ModelResponse{Message: Message{ToolCalls: []ToolCall{{ID: "read1", Name: "read_source", Args: map[string]any{}}}}}, nil
		default:
			return ModelResponse{Message: Message{ToolCalls: []ToolCall{{ID: "submit2", Name: "submit_result", Args: map[string]any{"answer": "verified"}}}}}, nil
		}
	}
	out, err := Run(context.Background(), opts)
	if err != nil || out.Text != `{"answer":"verified"}` || len(out.Calls) != 1 || executed != 1 || turn != 3 {
		t.Fatal(out, executed, turn, err)
	}
}

func TestNativeLoopRejectsInvalidCallIDsAndMalformedSubmissions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		calls  []ToolCall
		schema bool
	}{
		{"missing ID", []ToolCall{{Name: "read_source"}}, false},
		{"duplicate IDs", []ToolCall{{ID: "a", Name: "read_source"}, {ID: "a", Name: "read_source"}}, false},
		{"missing submission ID", []ToolCall{{Name: "submit_result", Args: map[string]any{}}}, true},
		{"malformed submission", []ToolCall{{ID: "s", Name: "submit_result", ArgumentError: "invalid arguments"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := nativeOptions()
			opts.Limits.ValidationRetries = 0
			if tc.schema {
				opts.OutputSchema = map[string]any{"type": "object"}
			}
			opts.Execute = func(context.Context, ToolCall) (any, string, error) {
				t.Fatal("executed invalid call")
				return nil, "", nil
			}
			opts.Complete = func(context.Context, ModelRequest) (ModelResponse, error) {
				return ModelResponse{Message: Message{ToolCalls: tc.calls}}, nil
			}
			if _, err := Run(context.Background(), opts); err == nil {
				t.Fatal("invalid call accepted")
			}
		})
	}
}

func TestNativeLoopDoesNotExecuteToolLookingText(t *testing.T) {
	opts := nativeOptions()
	opts.Complete = func(context.Context, ModelRequest) (ModelResponse, error) {
		return ModelResponse{Message: Message{Text: `{"name":"read_source","args":{}}`}}, nil
	}
	opts.Execute = func(context.Context, ToolCall) (any, string, error) { t.Fatal("executed prose"); return nil, "", nil }
	out, err := Run(context.Background(), opts)
	if err != nil || len(out.Calls) != 0 {
		t.Fatal(out, err)
	}
}

func TestNativeLoopFeedsMalformedSubmissionBackAsToolError(t *testing.T) {
	opts := nativeOptions()
	opts.OutputSchema = map[string]any{"type": "object"}
	turn := 0
	opts.Complete = func(_ context.Context, req ModelRequest) (ModelResponse, error) {
		turn++
		switch turn {
		case 1:
			return ModelResponse{Message: Message{ToolCalls: []ToolCall{{ID: "s1", Name: "submit_result", ArgumentError: "tool arguments must be a JSON object: unexpected end of JSON input"}}}}, nil
		case 2:
			last := req.Messages[len(req.Messages)-1]
			if last.Role != "tool" || last.ToolCallID != "s1" || !strings.Contains(last.Text, "must be a JSON object") {
				t.Fatalf("missing parse-error feedback: %+v", last)
			}
			return ModelResponse{Message: Message{ToolCalls: []ToolCall{{ID: "s2", Name: "submit_result", Args: map[string]any{"answer": "ok"}}}}}, nil
		default:
			t.Fatal("unexpected generation")
			return ModelResponse{}, nil
		}
	}
	out, err := Run(context.Background(), opts)
	if err != nil || out.Text != `{"answer":"ok"}` || turn != 2 {
		t.Fatal(out, turn, err)
	}
}

func TestNativeStopPathRecoversMalformedSubmissionOnce(t *testing.T) {
	opts := nativeOptions()
	opts.OutputSchema = map[string]any{"type": "object"}
	opts.Limits.Turns = 2
	turn := 0
	opts.Execute = func(context.Context, ToolCall) (any, string, error) { return "evidence", "read", nil }
	opts.Complete = func(_ context.Context, req ModelRequest) (ModelResponse, error) {
		turn++
		switch turn {
		case 1:
			return ModelResponse{Message: Message{ToolCalls: []ToolCall{{ID: "r1", Name: "read_source", Args: map[string]any{}}}}}, nil
		case 2:
			return ModelResponse{Message: Message{ToolCalls: []ToolCall{{ID: "s1", Name: "submit_result", ArgumentError: "tool arguments must be a JSON object: unexpected end of JSON input"}}}}, nil
		case 3:
			if !req.DisableTools {
				t.Fatal("expected forced final without tools")
			}
			return ModelResponse{Message: Message{ToolCalls: []ToolCall{{ID: "s2", Name: "submit_result", ArgumentError: "tool arguments must be a JSON object: unexpected end of JSON input"}}}}, nil
		case 4:
			last := req.Messages[len(req.Messages)-1]
			if last.Role != "tool" || last.ToolCallID != "s2" || !strings.Contains(last.Text, "must be a JSON object") {
				t.Fatalf("missing corrective tool result: %+v", last)
			}
			return ModelResponse{Message: Message{ToolCalls: []ToolCall{{ID: "s3", Name: "submit_result", Args: map[string]any{"answer": "ok"}}}}}, nil
		default:
			t.Fatal("unexpected generation")
			return ModelResponse{}, nil
		}
	}
	out, err := Run(context.Background(), opts)
	if err != nil || out.Text != `{"answer":"ok"}` || turn != 4 {
		t.Fatal(out, turn, err)
	}
}
