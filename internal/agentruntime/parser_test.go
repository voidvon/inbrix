package agentruntime

import (
	"context"
	"strings"
	"testing"
)

const adjacentTools = `{"name":"search_emails","arguments":{"scope":"thread","query":"Q-MISSING"}}
{"name":"search_emails","arguments":{"scope":"conversation","query":"Synthetic RFQ"}}`

func TestParseAdjacentBareToolCallsAtomically(t *testing.T) {
	for _, raw := range []string{adjacentTools, strings.ReplaceAll(adjacentTools, "\n", ""), "```tool_call\n" + adjacentTools + "\n```"} {
		calls, err := ParseToolCalls(raw)
		if err != nil || len(calls) != 2 || calls[0].Args["scope"] != "thread" || calls[1].Args["scope"] != "conversation" {
			t.Fatal(calls, err)
		}
	}
	for _, raw := range []string{
		adjacentTools + ` trailing prose`,
		adjacentTools + ` {"name":"read_email","arguments":`,
		`{"name":"search_emails","arguments":{}} {"request":"final answer"}`,
		`{"name":"search_emails","arguments":{}} {"name":"read_email","arguments":{},"args":{}}`,
		`{"name":"search_emails","arguments":{}} {"name":"read_email","arguments":"not an object"}`,
		`{"name":"search_emails","arguments":{}} {"name":"read_email","arguments":{},"unexpected":true}`,
	} {
		calls, err := ParseToolCalls(raw)
		if err != nil || len(calls) != 0 {
			t.Fatal("partial or mixed stream executed", raw, calls, err)
		}
	}
}

func TestRunExecutesAdjacentToolsInsteadOfTreatingThemAsFinalOutput(t *testing.T) {
	turn, calls := 0, 0
	result, err := Run(context.Background(), Options{Limits: Limits{Turns: 2, CallsPerTurn: 4, Calls: 4, InputBytes: 16000, ResultBytes: 2000}, Respond: func(context.Context, string) (string, error) {
		turn++
		if turn == 1 {
			return adjacentTools, nil
		}
		return "final answer", nil
	}, Execute: func(context.Context, ToolCall) (any, string, error) {
		calls++
		return map[string]any{"messages": []string{}}, "searched", nil
	}})
	if err != nil || calls != 2 || turn != 2 || result.Text != "final answer" {
		t.Fatal(result, err, calls, turn)
	}
}
