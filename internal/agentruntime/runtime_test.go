package agentruntime

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRunContinuesAfterToolsAndReportsErrors(t *testing.T) {
	turn := 0
	var events []string
	out, err := Run(context.Background(), Options{
		Input: "analyze", Limits: Limits{Turns: 3, CallsPerTurn: 4, Calls: 8, InputBytes: 16000, ResultBytes: 4000},
		Respond: func(ctx context.Context, input string) (string, error) {
			turn++
			if turn == 1 {
				return "```tool_call\n[{\"name\":\"read_email\",\"args\":{\"uid\":\"1\"}},{\"name\":\"unknown\",\"args\":{}}]\n```", nil
			}
			if !strings.Contains(input, "verified-price") || !strings.Contains(input, "not allowed") {
				t.Fatalf("tool results missing: %s", input)
			}
			return "done", nil
		},
		Execute: func(ctx context.Context, call ToolCall) (any, string, error) {
			if call.Name == "unknown" {
				return nil, "", errors.New("not allowed")
			}
			return map[string]string{"price": "verified-price"}, "read", nil
		},
		OnTool: func(kind string, call ToolCall) error { events = append(events, kind+":"+call.Name); return nil },
	})
	if err != nil || out.Text != "done" || len(out.Calls) != 2 || out.Calls[1].Status != "error" || len(events) != 4 {
		t.Fatalf("result=%+v events=%v err=%v", out, events, err)
	}
}

func TestRunRejectsToolRequestsAfterBudget(t *testing.T) {
	executed := 0
	_, err := Run(context.Background(), Options{
		Limits:  Limits{Turns: 1, CallsPerTurn: 1, Calls: 1, InputBytes: 12000, ResultBytes: 2000},
		Respond: func(context.Context, string) (string, error) { return `{"name":"read_email","args":{}}`, nil },
		Execute: func(context.Context, ToolCall) (any, string, error) { executed++; return "body", "read", nil },
	})
	if !errors.Is(err, ErrIncomplete) || executed != 1 {
		t.Fatalf("executed=%d err=%v", executed, err)
	}
}

func TestRunOmitsLargeResultsAndHonorsCancellation(t *testing.T) {
	turn := 0
	out, err := Run(context.Background(), Options{
		Limits: Limits{Turns: 2, CallsPerTurn: 1, Calls: 2, InputBytes: 12000, ResultBytes: 2000},
		Respond: func(ctx context.Context, input string) (string, error) {
			turn++
			if len(input) > 12000 {
				t.Fatal("input budget exceeded")
			}
			if turn == 1 {
				return `{"name":"read_email","args":{}}`, nil
			}
			if !strings.Contains(input, `"outputOmitted":true`) || strings.Contains(input, strings.Repeat("secret", 1000)) {
				t.Fatal("oversized result was exposed")
			}
			return "partial coverage", nil
		},
		Execute: func(context.Context, ToolCall) (any, string, error) {
			return strings.Repeat("secret", 1000), "too large", nil
		},
	})
	if err != nil || out.Text != "partial coverage" {
		t.Fatalf("%+v %v", out, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Run(ctx, Options{Limits: Limits{Turns: 1, CallsPerTurn: 1, Calls: 1, InputBytes: 12000, ResultBytes: 2000}, Respond: func(context.Context, string) (string, error) {
		t.Fatal("called model after cancellation")
		return "", nil
	}, Execute: func(context.Context, ToolCall) (any, string, error) { return nil, "", nil }})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestRunRepairsFinalAnswerWithEvidenceWithinBudget(t *testing.T) {
	turn, executed := 0, 0
	out, err := Run(context.Background(), Options{
		Input: "analyze", Limits: Limits{Turns: 4, CallsPerTurn: 1, Calls: 1, InputBytes: 16000, ResultBytes: 2000, ValidationRetries: 2},
		Respond: func(ctx context.Context, input string) (string, error) {
			turn++
			switch turn {
			case 1:
				return "unread citation", nil
			case 2:
				if !strings.Contains(input, "read evidence first") || !strings.Contains(input, "unread citation") {
					t.Fatal("missing rejected answer or validation feedback")
				}
				return `{"name":"read_email","args":{}}`, nil
			default:
				return "valid", nil
			}
		},
		Execute: func(context.Context, ToolCall) (any, string, error) { executed++; return "evidence", "read", nil },
		Validate: func(s string) error {
			if s != "valid" || executed == 0 {
				return errors.New("read evidence first")
			}
			return nil
		},
	})
	if err != nil || out.Text != "valid" || executed != 1 || turn != 3 || len(out.Calls) != 1 {
		t.Fatalf("out=%+v turn=%d executed=%d err=%v", out, turn, executed, err)
	}
}

func TestRunFinalValidationCannotBypassBudgets(t *testing.T) {
	rejected := errors.New("invalid final answer")
	for _, tc := range []struct {
		name                  string
		turns, retries, calls int
		tool                  bool
	}{
		{"retry limit", 6, 2, 3, false},
		{"turn limit", 1, 2, 1, false},
		{"forced final", 1, 2, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			_, err := Run(context.Background(), Options{
				Limits: Limits{Turns: tc.turns, CallsPerTurn: 1, Calls: 1, InputBytes: 16000, ResultBytes: 2000, ValidationRetries: tc.retries},
				Respond: func(context.Context, string) (string, error) {
					calls++
					if tc.tool && calls == 1 {
						return `{"name":"read_email","args":{}}`, nil
					}
					return "invalid", nil
				},
				Execute:  func(context.Context, ToolCall) (any, string, error) { return "body", "read", nil },
				Validate: func(string) error { return rejected },
			})
			if !errors.Is(err, rejected) || calls != tc.calls {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
		})
	}
}
