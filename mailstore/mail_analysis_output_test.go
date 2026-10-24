package mailstore

import (
	"errors"
	"strings"
	"testing"
)

func TestParseMailAnalysisOutputReportsSafeSchemaErrors(t *testing.T) {
	for _, tc := range []struct{ name, raw, want string }{
		{"transcript", `[Agent Tool Call]: read_email({"uid":"1"})`, "invalid JSON"},
		{"truncated", `{"request":"private-mail-text"`, "incomplete JSON object"},
		{"missing", strings.Replace(standaloneAnalysisOutput, `"changes":"",`, "", 1), "missing required key changes"},
		{"null", strings.Replace(standaloneAnalysisOutput, `"actions":[]`, `"actions":null`, 1), "actions must not be null"},
		{"wrong type", strings.Replace(standaloneAnalysisOutput, `"actions":[]`, `"actions":"private-mail-text"`, 1), "actions must be a string array"},
		{"null item", strings.Replace(standaloneAnalysisOutput, `"actions":[]`, `"actions":[null]`, 1), "actions must contain only strings"},
		{"null summary", strings.Replace(standaloneAnalysisOutput, `"summary":"测试客户，20台阀门，确认新报价。"`, `"summary":null`, 1), "summary must not be null"},
		{"summary type", strings.Replace(standaloneAnalysisOutput, `"summary":"测试客户，20台阀门，确认新报价。"`, `"summary":{}`, 1), "summary must be a string"},
		{"empty summary", strings.Replace(standaloneAnalysisOutput, `测试客户，20台阀门，确认新报价。`, `  `, 1), "summary must be non-empty"},
		{"long summary", strings.Replace(standaloneAnalysisOutput, `测试客户，20台阀门，确认新报价。`, strings.Repeat("字", 201), 1), "summary must be at most 200 Unicode characters"},
		{"trailing", standaloneAnalysisOutput + ` {}`, "trailing output"},
		{"unknown", strings.Replace(standaloneAnalysisOutput, `"changes":""`, `"changes":"","private-mail-text":0`, 1), "unexpected top-level keys"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseMailAnalysisOutput(tc.raw)
			if !errors.Is(err, ErrInvalidMailAnalysis) || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "private-mail-text") {
				t.Fatalf("unsafe or imprecise error: %v", err)
			}
		})
	}
	if _, err := parseMailAnalysisOutput("```json\n" + standaloneAnalysisOutput + "\n```"); err != nil {
		t.Fatal(err)
	}
}

func TestFreeTextSummaryCountsUnicodeCharactersWithoutTruncating(t *testing.T) {
	for _, summary := range []string{strings.Repeat("中", 200), strings.Repeat("🙂", 200), "需要重新报价。\n数量已改为20台，请核实交期。"} {
		raw := strings.Replace(standaloneAnalysisOutput, "测试客户，20台阀门，确认新报价。", summary, 1)
		// Encode paragraph breaks as JSON rather than imposing a display format.
		if strings.Contains(summary, "\n") {
			raw = strings.Replace(raw, "\n", `\n`, -1)
		}
		got, err := parseMailAnalysisOutput(raw)
		if err != nil || got.Summary != summary {
			t.Fatalf("free text was rejected or altered: %q %v", got.Summary, err)
		}
	}
}
