package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"inbrix/internal/maileval"
)

func TestValidateModeNeedsNoCredentialsOrOutputFiles(t *testing.T) {
	var out, stderr bytes.Buffer
	prefix := filepath.Join(t.TempDir(), "unused")
	if status := run([]string{"-mode", "validate", "-out", prefix}, &out, &stderr); status != 0 || !strings.Contains(out.String(), "No model requests; no quality score") {
		t.Fatal(status, out.String(), stderr.String())
	}
	if _, err := os.Stat(prefix + ".json"); !os.IsNotExist(err) {
		t.Fatal("validation fabricated a scored run")
	}
}

func TestRunModeRejectsMissingCredentialsWithoutSendingRequests(t *testing.T) {
	t.Setenv("INBRIX_TEST_EMPTY_EVAL_KEY", "")
	var out, stderr bytes.Buffer
	status := run([]string{"-mode", "run", "-model", "test-model", "-base-url", "https://invalid.example", "-key-env", "INBRIX_TEST_EMPTY_EVAL_KEY"}, &out, &stderr)
	if status != 2 || !strings.Contains(stderr.String(), "configure a model") {
		t.Fatal(status, out.String(), stderr.String())
	}
}

func TestRescoreModeReusesSavedOutputAndRetainsFailures(t *testing.T) {
	suite, err := maileval.LoadSuite("")
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "input.json")
	report := maileval.Report{Version: 1, RubricVersion: maileval.RubricVersion, Mode: "test-double", SuiteName: suite.Name, SuiteHash: maileval.Fingerprint(suite), Results: []maileval.CaseResult{{ID: suite.Cases[0].ID, Iteration: 1, Error: "model did not complete"}}}
	raw, _ := json.Marshal(report)
	if err := os.WriteFile(input, raw, 0600); err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Join(t.TempDir(), "rescored")
	var out, stderr bytes.Buffer
	status := run([]string{"-mode", "rescore", "-report", input, "-out", prefix}, &out, &stderr)
	if status != 1 {
		t.Fatal(status, out.String(), stderr.String())
	}
	saved, err := maileval.ReadReport(prefix + ".json")
	if err != nil || saved.Mode != "rescore" || saved.Totals.Completed != 0 || !saved.HumanReviewRequired {
		t.Fatal(saved, err)
	}
}
