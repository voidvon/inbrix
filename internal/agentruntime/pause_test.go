package agentruntime

import (
	"context"
	"testing"
)

func TestPauseToolStopsWithoutAnotherModelTurn(t *testing.T) {
	requests := 0
	out, err := Run(context.Background(), Options{
		PauseTool: "ask_user", Input: "报价", Limits: Limits{Turns: 3, CallsPerTurn: 4, Calls: 8, InputBytes: 16000, ResultBytes: 2000},
		Tools: []ToolSpec{{Name: "ask_user", Parameters: map[string]any{"type": "object"}}},
		Complete: func(context.Context, ModelRequest) (ModelResponse, error) {
			requests++
			return ModelResponse{Message: Message{ToolCalls: []ToolCall{{ID: "q1", Name: "ask_user"}}}}, nil
		},
		Execute: func(context.Context, ToolCall) (any, string, error) {
			return "awaiting_user", "需要多少件？", nil
		},
	})
	if err != nil || requests != 1 || out.Text != "需要多少件？" || len(out.Calls) != 1 || out.Calls[0].Status != "success" {
		t.Fatalf("pause failed: %+v %v requests=%d", out, err, requests)
	}
}

func TestInvalidPauseCallCanBeCorrected(t *testing.T) {
	requests := 0
	executions := 0
	_, err := Run(context.Background(), Options{
		PauseTool: "ask_user", Input: "报价", Limits: Limits{Turns: 3, CallsPerTurn: 4, Calls: 8, InputBytes: 16000, ResultBytes: 2000},
		Tools: []ToolSpec{{Name: "ask_user", Parameters: map[string]any{"type": "object"}}},
		Complete: func(_ context.Context, r ModelRequest) (ModelResponse, error) {
			requests++
			if requests == 1 {
				return ModelResponse{Message: Message{ToolCalls: []ToolCall{{ID: "bad", Name: "ask_user", ArgumentError: "invalid JSON"}}}}, nil
			}
			if r.Messages[len(r.Messages)-1].Role != "tool" {
				t.Fatal("invalid question was not returned as a tool error")
			}
			return ModelResponse{Message: Message{ToolCalls: []ToolCall{{ID: "valid", Name: "ask_user"}}}}, nil
		},
		Execute: func(context.Context, ToolCall) (any, string, error) {
			executions++
			return "awaiting_user", "需要多少件？", nil
		},
	})
	if err != nil || requests != 2 || executions != 1 {
		t.Fatalf("correction: %v requests=%d executions=%d", err, requests, executions)
	}
}
