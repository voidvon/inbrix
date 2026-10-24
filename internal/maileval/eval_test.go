package maileval

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"inbrix/internal/agenttest"
	"inbrix/mailstore"
)

type testClient struct {
	reply func(*http.Request) (string, error)
	calls int
}

func (c *testClient) Do(req *http.Request) (*http.Response, error) {
	c.calls++
	text, err := c.reply(req)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(text))}, nil
}

func suiteForTest(t *testing.T) Suite {
	t.Helper()
	suite, err := LoadSuite("")
	if err != nil {
		t.Fatal(err)
	}
	return suite
}

func TestSuiteValidationRejectsBrokenRubricsAndMissingEvidence(t *testing.T) {
	suite := suiteForTest(t)
	if len(suite.Cases) != 8 {
		t.Fatal(len(suite.Cases))
	}
	for _, mutate := range []func(*Suite){
		func(s *Suite) { s.Cases[1].ID = s.Cases[0].ID },
		func(s *Suite) { s.Cases[0].Rules[0].Patterns = []string{"["} },
		func(s *Suite) { s.Cases[1].Rules[2].References[0].UID = "999" },
		func(s *Suite) { s.Cases[1].History[0].BodyAvailable = false },
		func(s *Suite) { s.Cases[0].Trigger.PaddingRepeats = 1000000000 },
	} {
		candidate := suiteForTest(t)
		mutate(&candidate)
		if ValidateSuite(candidate) == nil {
			t.Fatal("bad suite accepted")
		}
	}
}

func TestScoringDetectsDroppedFactsWrongEvidenceAndBoundaryLeaks(t *testing.T) {
	c := suiteForTest(t).Cases[1]
	a := &mailstore.MailAnalysis{Request: "Q-CHANGE BV-25 数量20台", Background: "[Sent/41] 旧报价10台，USD25/台", Changes: "10台改20台", Actions: []string{"重新核实"}, Missing: []string{"新价格和交期待确认"}, Fields: map[string]string{"需求": "20台"}, Sources: []mailstore.MailAnalysisSource{{Folder: "Sent", UID: "41", TotalLength: 100, Ranges: []mailstore.MailAnalysisRange{{Start: 0, End: 100}}}}}
	checks, score := Evaluate(c, a, "", nil)
	if !AllChecksPass(checks) || score != 100 {
		t.Fatal(checks, score)
	}
	a.Request = "Q-CHANGE BV-25 数量10台"
	a.Sources[0].UID = "40"
	checks, score = Evaluate(c, a, "", []string{"hidden"})
	failed := map[string]bool{}
	for _, check := range checks {
		if !check.Passed {
			failed[check.ID] = true
		}
	}
	if score >= 100 || !failed["latest_quantity"] || !failed["history"] || !failed["mail_boundary"] {
		t.Fatal(checks, score)
	}
	checks, score = Evaluate(c, nil, "invalid output", nil)
	if AllChecksPass(checks) || score >= 100 {
		t.Fatal(checks, score)
	}
}

func TestMeterCountsKnownUsagePreservesResponseAndBlocksHiddenMail(t *testing.T) {
	client := &testClient{reply: func(*http.Request) (string, error) {
		return `{"choices":[{"message":{"content":"answer"}}],"usage":{"prompt_tokens":40,"completion_tokens":7}}`, nil
	}}
	meter := &Meter{Client: client, MaxRequests: 1}
	meter.BeginCase([]string{"HIDDEN_CANARY_99"})
	req, _ := http.NewRequest(http.MethodPost, "https://model.example.invalid", strings.NewReader(`{"input":"visible"}`))
	resp, err := meter.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(raw), "answer") {
		t.Fatal("response changed")
	}
	usage, output, violations := meter.Snapshot()
	if len(usage) != 1 || !usage[0].Known || usage[0].InputTokens != 40 || usage[0].OutputTokens != 7 || output != "answer" || len(violations) != 0 {
		t.Fatal(usage, output, violations)
	}
	req, _ = http.NewRequest(http.MethodPost, "https://model.example.invalid", strings.NewReader(`{"input":"HIDDEN_CANARY_99"}`))
	if _, err := meter.Do(req); err == nil || !strings.Contains(err.Error(), "boundary") || client.calls != 1 {
		t.Fatal("hidden mail escaped", err, client.calls)
	}
	meter.BeginCase(nil)
	req, _ = http.NewRequest(http.MethodPost, "https://model.example.invalid", strings.NewReader(`{}`))
	if _, err := meter.Do(req); err == nil || !strings.Contains(err.Error(), "budget") || client.calls != 1 {
		t.Fatal("request budget bypassed", err)
	}
	unknown, _ := parseUsage([]byte(`{"output_text":"ok"}`), RequestUsage{})
	if unknown.Known {
		t.Fatal("missing usage became zero-priced usage")
	}
	nested, text := parseUsage([]byte(`{"data":{"data":{"choices":[{"message":{"content":[{"type":"text","text":"nested answer"}]}}],"usage":{"prompt_tokens":55,"completion_tokens":9}}}}`), RequestUsage{})
	if !nested.Known || nested.InputTokens != 55 || nested.OutputTokens != 9 || text != "nested answer" {
		t.Fatal("gateway envelope not metered", nested, text)
	}
}

func TestReplayUsesProductionToolLoopWithIsolatedMailAndNoExpectedAnswers(t *testing.T) {
	suite := suiteForTest(t)
	c := suite.Cases[1]
	client := &testClient{reply: func(req *http.Request) (string, error) {
		var request struct {
			Input        json.RawMessage `json:"input"`
			Instructions string          `json:"instructions"`
		}
		if err := json.NewDecoder(req.Body).Decode(&request); err != nil {
			return "", err
		}
		if strings.Contains(agenttest.InputText(request.Input), "humanReview") || strings.Contains(agenttest.InputText(request.Input), "本次诉求保留") || strings.Contains(agenttest.InputText(request.Input), "patterns") {
			return "", errors.New("grading rubric leaked to model")
		}
		text := `{"request":"Q-CHANGE BV-25 数量20台","background":"[Sent/41] 原报价10台，USD25/台","changes":"10台改为20台","actions":["重新确认价格和交期"],"missing":["新价格和交期未确认"],"fields":{"客户":"测试客户","需求":"20台BV-25","要求":"重报价","问题":"新价格交期待确认"}}`
		if !strings.Contains(agenttest.InputText(request.Input), "[Tool Result (read_email)]") {
			text = `{"name":"read_email","arguments":{"folder":"Sent","uid":"41"}}`
		}
		response := agenttest.Response(text, 1)
		response["usage"] = map[string]int{"input_tokens": 100, "output_tokens": 40}
		raw, _ := json.Marshal(response)
		return string(raw), nil
	}}
	cfg := DefaultExecutionConfig()
	cfg.Model = mailstore.AIModelRecord{ID: "fake-model", Provider: "openai", Model: "fake", BaseURL: "https://model.example.invalid"}
	cfg.APIKey = "test-secret"
	report, err := Run(context.Background(), suite, Options{Config: cfg, Client: client, Repeats: 1, MaxRequests: 4, CaseIDs: []string{c.ID}})
	if err != nil || report.Totals.Completed != 1 || report.Totals.AllChecksPassed != 1 || report.Totals.Requests != 2 || !report.Totals.UsageComplete || !report.HumanReviewRequired {
		t.Fatal(report.Totals, report.Results, err)
	}
	if report.Results[0].Execution.PipelineVersion < 1 || len(report.Results[0].Execution.Analysis.Sources) != 2 {
		t.Fatal(report.Results[0])
	}
	report.Mode = "test-double" // Unit-test output is never a live-quality baseline.
	rescored, err := Rescore(suite, report)
	if err != nil || rescored.Totals.MeanAutomatedScore != report.Totals.MeanAutomatedScore {
		t.Fatal(rescored, err)
	}
	path := filepath.Join(t.TempDir(), "report")
	if err := WriteReport(path, report); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path + ".json")
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	if err := WriteReport(path, report); err == nil {
		t.Fatal("overwrote a baseline")
	}
	loaded, err := ReadReport(path + ".json")
	if err != nil {
		t.Fatal(err)
	}
	diff, err := Compare(loaded, report)
	if err != nil || diff.CommonCases != 1 || diff.MeanScoreDelta != 0 {
		t.Fatal(diff, err)
	}
	loaded.SuiteHash = "different"
	if _, err := Compare(loaded, report); err == nil {
		t.Fatal("compared different suites")
	}
}

func TestEstimatedCostRequiresBothPricesAndCompleteUsage(t *testing.T) {
	in, out := 2.0, 8.0
	report := Report{InputPricePerMillion: &in, OutputPricePerMillion: &out, Results: []CaseResult{{Usage: []RequestUsage{{Known: true, InputTokens: 1000000, OutputTokens: 1000000}}}}}
	report.Recalculate()
	if report.Totals.EstimatedCostUSD == nil || *report.Totals.EstimatedCostUSD != 10 {
		t.Fatal(report.Totals)
	}
	report.Results[0].Usage[0].Known = false
	report.Recalculate()
	if report.Totals.EstimatedCostUSD != nil {
		t.Fatal("unknown usage priced as zero")
	}
}

func TestRubricRepairRequiresIdenticalMailInputsAndRetainsCollectionHash(t *testing.T) {
	previous := suiteForTest(t)
	previous.Cases[1].Rules[3].Patterns[0] = "价格|报价|price"
	current := suiteForTest(t)
	a := &mailstore.MailAnalysis{Request: "BV-25 数量20台", Background: "旧数量10台 [Sent/41]", Changes: "10台改20台", Actions: []string{"重新核算"}, Missing: []string{"新单价、总价及交期待确认"}, Fields: map[string]string{"需求": "20台"}, Sources: []mailstore.MailAnalysisSource{{Folder: "Sent", UID: "41", TotalLength: 1, Ranges: []mailstore.MailAnalysisRange{{Start: 0, End: 1}}}}}
	report := Report{Version: 1, RubricVersion: RubricVersion, Mode: "test-double", SuiteHash: Fingerprint(previous), Results: []CaseResult{{ID: previous.Cases[1].ID, Iteration: 1, Execution: mailstore.MailAnalysisExecutionResult{Analysis: a}}}}
	report, err := Rescore(previous, report)
	if err != nil || report.Totals.AllChecksPassed != 0 {
		t.Fatal(report, err)
	}
	if _, err := Rescore(current, report); err == nil {
		t.Fatal("unverified rubric change accepted")
	}
	repaired, err := RescoreWithPreviousSuite(previous, current, report)
	if err != nil || repaired.Totals.AllChecksPassed != 1 || repaired.CollectedSuiteHash != Fingerprint(previous) || repaired.InputHash != InputFingerprint(current) {
		t.Fatal(repaired, err)
	}
	current.Cases[1].Trigger.Body += " Changed customer facts."
	if _, err := RescoreWithPreviousSuite(previous, current, report); err == nil {
		t.Fatal("changed mail inputs accepted as rubric-only repair")
	}
}

func TestCheckedInCandidateIsUnpublishedAndDoesNotChangeDefaultSkill(t *testing.T) {
	before := DefaultExecutionConfig()
	candidate, err := OverrideSkillFile(before, "testdata/candidate-skill.md")
	if err != nil || candidate.Skill.Revision != 0 || candidate.Skill.Fingerprint == before.Skill.Fingerprint {
		t.Fatal(candidate.Skill, err)
	}
	after := DefaultExecutionConfig()
	if after.Skill.Fingerprint != before.Skill.Fingerprint {
		t.Fatal("candidate changed built-in runtime instructions")
	}
}
