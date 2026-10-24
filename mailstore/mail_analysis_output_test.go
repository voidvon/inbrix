package mailstore

import (
	"errors"
	"strings"
	"testing"
)

func TestParseMailAnalysisOutputReportsSafeSchemaErrors(t *testing.T) {
	labels := []string{"客户", "需求", "要求", "问题"}
	for _, tc := range []struct{ name, raw, want string }{
		{"transcript", `[Agent Tool Call]: read_email({"uid":"1"})`, "invalid JSON"},
		{"truncated", `{"request":"private-mail-text"`, "incomplete JSON object"},
		{"missing", strings.Replace(standaloneAnalysisOutput, `"changes":"",`, "", 1), "missing required key changes"},
		{"null", strings.Replace(standaloneAnalysisOutput, `"actions":[]`, `"actions":null`, 1), "actions must not be null"},
		{"wrong type", strings.Replace(standaloneAnalysisOutput, `"actions":[]`, `"actions":"private-mail-text"`, 1), "actions must be a string array"},
		{"null item", strings.Replace(standaloneAnalysisOutput, `"actions":[]`, `"actions":[null]`, 1), "actions must contain only strings"},
		{"null field", strings.Replace(standaloneAnalysisOutput, `"客户":"测试客户"`, `"客户":null`, 1), "fields must contain only strings"},
		{"labels", strings.Replace(standaloneAnalysisOutput, `"客户":`, `"private-mail-text":`, 1), "fields must match"},
		{"trailing", standaloneAnalysisOutput + ` {}`, "trailing output"},
		{"unknown", strings.Replace(standaloneAnalysisOutput, `"changes":""`, `"changes":"","private-mail-text":0`, 1), "unexpected top-level keys"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseMailAnalysisOutput(tc.raw, labels)
			if !errors.Is(err, ErrInvalidMailAnalysis) || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "private-mail-text") {
				t.Fatalf("unsafe or imprecise error: %v", err)
			}
		})
	}
	if _, err := parseMailAnalysisOutput("```json\n"+standaloneAnalysisOutput+"\n```", labels); err != nil {
		t.Fatal(err)
	}
}
