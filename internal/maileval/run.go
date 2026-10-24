package maileval

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"inbrix/mailstore"
	"inbrix/models"
)

const RubricVersion = 1

type ModelMetadata struct {
	ID              string                     `json:"id"`
	Provider        string                     `json:"provider"`
	Model           string                     `json:"model"`
	Effort          string                     `json:"effort"`
	SkillOrigin     string                     `json:"skillOrigin"`
	Skill           mailstore.AISkillExecution `json:"skill"`
	AgentPromptHash string                     `json:"agentPromptHash"`
	AgentPrompt     string                     `json:"agentPrompt"`
	OutputLabels    []string                   `json:"outputLabels"`
	ExtraParamsHash string                     `json:"extraParamsHash"`
	EndpointHash    string                     `json:"endpointHash"`
}
type CaseResult struct {
	ID                 string                                `json:"id"`
	Title              string                                `json:"title"`
	Iteration          int                                   `json:"iteration"`
	DurationMS         int64                                 `json:"durationMs"`
	Execution          mailstore.MailAnalysisExecutionResult `json:"execution"`
	Error              string                                `json:"error,omitempty"`
	FailureKind        string                                `json:"failureKind,omitempty"`
	LastOutput         string                                `json:"lastOutput,omitempty"`
	Usage              []RequestUsage                        `json:"usage"`
	BoundaryViolations []string                              `json:"boundaryViolations"`
	Checks             []Check                               `json:"checks"`
	AutomatedScore     float64                               `json:"automatedScore"`
	HumanReview        []string                              `json:"humanReview"`
}
type Totals struct {
	Cases                    int      `json:"cases"`
	Completed                int      `json:"completed"`
	AllChecksPassed          int      `json:"allChecksPassed"`
	CriticalFailures         int      `json:"criticalFailures"`
	InfrastructureFailures   int      `json:"infrastructureFailures"`
	OutputValidationFailures int      `json:"outputValidationFailures"`
	UnclassifiedFailures     int      `json:"unclassifiedFailures"`
	MeanAutomatedScore       float64  `json:"meanAutomatedScore"`
	P50LatencyMS             int64    `json:"p50LatencyMs"`
	P95LatencyMS             int64    `json:"p95LatencyMs"`
	Requests                 int      `json:"requests"`
	InputTokens              int64    `json:"inputTokens"`
	OutputTokens             int64    `json:"outputTokens"`
	UsageComplete            bool     `json:"usageComplete"`
	EstimatedCostUSD         *float64 `json:"estimatedCostUsd,omitempty"`
}
type Report struct {
	Version               int           `json:"version"`
	RubricVersion         int           `json:"rubricVersion"`
	Mode                  string        `json:"mode"`
	SuiteName             string        `json:"suiteName"`
	SuiteHash             string        `json:"suiteHash"`
	CollectedSuiteHash    string        `json:"collectedSuiteHash,omitempty"`
	InputHash             string        `json:"inputHash,omitempty"`
	StartedAt             time.Time     `json:"startedAt"`
	FinishedAt            time.Time     `json:"finishedAt"`
	Model                 ModelMetadata `json:"model"`
	InputPricePerMillion  *float64      `json:"inputPricePerMillion,omitempty"`
	OutputPricePerMillion *float64      `json:"outputPricePerMillion,omitempty"`
	Results               []CaseResult  `json:"results"`
	Totals                Totals        `json:"totals"`
	Comparison            *Comparison   `json:"comparison,omitempty"`
	HumanReviewRequired   bool          `json:"humanReviewRequired"`
}
type Options struct {
	Config                mailstore.MailAnalysisExecutionConfig
	SkillOrigin           string
	Client                mailstore.HTTPClient
	Repeats               int
	CaseIDs               []string
	MaxRequests           int
	InputPricePerMillion  *float64
	OutputPricePerMillion *float64
	Progress              func(string)
}

func positiveUID(value string) (uint64, error) {
	n, err := strconv.ParseUint(value, 10, 32)
	if err != nil || n == 0 {
		return 0, fmt.Errorf("invalid UID %q", value)
	}
	return n, nil
}

func Run(ctx context.Context, suite Suite, options Options) (Report, error) {
	if err := ValidateSuite(suite); err != nil {
		return Report{}, err
	}
	if options.Client == nil || options.Repeats < 1 || options.Repeats > 10 || options.MaxRequests < 1 || options.MaxRequests > 10000 {
		return Report{}, fmt.Errorf("invalid replay options")
	}
	if strings.TrimSpace(options.Config.APIKey) == "" || options.Config.Model.Model == "" {
		return Report{}, fmt.Errorf("model and API key are required for a live replay")
	}
	if options.Config.Model.ReasoningEffort == "" {
		options.Config.Model.ReasoningEffort = "medium"
	}
	selected := map[string]bool{}
	for _, id := range options.CaseIDs {
		selected[id] = false
	}
	for _, c := range suite.Cases {
		if _, ok := selected[c.ID]; ok {
			selected[c.ID] = true
		}
	}
	for id, found := range selected {
		if !found {
			return Report{}, fmt.Errorf("unknown case %q", id)
		}
	}
	report := Report{
		Version: 1, RubricVersion: RubricVersion, Mode: "live",
		SuiteName: suite.Name, SuiteHash: Fingerprint(suite), StartedAt: time.Now().UTC(),
		HumanReviewRequired:  true,
		InputPricePerMillion: options.InputPricePerMillion, OutputPricePerMillion: options.OutputPricePerMillion,
		Model: ModelMetadata{
			ID: options.Config.Model.ID, Provider: options.Config.Model.Provider,
			Model: options.Config.Model.Model, Effort: options.Config.Model.ReasoningEffort,
			SkillOrigin: options.SkillOrigin, Skill: options.Config.Skill,
			AgentPromptHash: Fingerprint([]any{options.Config.Agent.Prompt, options.Config.Agent.OutputLabels}),
			AgentPrompt:     options.Config.Agent.Prompt, OutputLabels: append([]string{}, options.Config.Agent.OutputLabels...),
			ExtraParamsHash: Fingerprint(options.Config.Model.ExtraParams), EndpointHash: Fingerprint(options.Config.Model.BaseURL),
		},
	}
	report.InputHash = InputFingerprint(suite)
	meter := &Meter{Client: options.Client, MaxRequests: options.MaxRequests}
	for _, c := range suite.Cases {
		if len(selected) > 0 {
			if _, ok := selected[c.ID]; !ok {
				continue
			}
		}
		for iteration := 1; iteration <= options.Repeats; iteration++ {
			if err := ctx.Err(); err != nil {
				report.FinishedAt = time.Now().UTC()
				report.Recalculate()
				return report, err
			}
			if options.Progress != nil {
				options.Progress(fmt.Sprintf("START %s %d/%d", c.ID, iteration, options.Repeats))
			}
			meter.BeginCase(c.HiddenCanaries)
			started := time.Now()
			execution, err := runIsolated(ctx, suite.AccountEmail, c, meter, options.Config)
			result := CaseResult{ID: c.ID, Title: c.Title, Iteration: iteration, DurationMS: time.Since(started).Milliseconds(), Execution: execution, HumanReview: c.HumanReview}
			if err != nil {
				result.Error = strings.ReplaceAll(err.Error(), options.Config.APIKey, "[redacted]")
				result.FailureKind = executionFailureKind(err)
			}
			result.Usage, result.LastOutput, result.BoundaryViolations = meter.Snapshot()
			result.Checks, result.AutomatedScore = Evaluate(c, execution.Analysis, result.Error, result.BoundaryViolations)
			report.Results = append(report.Results, result)
			if options.Progress != nil {
				options.Progress(fmt.Sprintf("DONE  %s score=%.1f checks=%t requests=%d elapsed=%.1fs", c.ID, result.AutomatedScore, AllChecksPass(result.Checks), len(result.Usage), float64(result.DurationMS)/1000))
			}
		}
	}
	report.FinishedAt = time.Now().UTC()
	report.Recalculate()
	return report, nil
}

func runIsolated(ctx context.Context, email string, c Case, client mailstore.HTTPClient, cfg mailstore.MailAnalysisExecutionConfig) (mailstore.MailAnalysisExecutionResult, error) {
	dir, err := os.MkdirTemp("", "inbrix-mail-eval-")
	if err != nil {
		return mailstore.MailAnalysisExecutionResult{}, err
	}
	defer os.RemoveAll(dir)
	store, err := mailstore.Open(filepath.Join(dir, "replay.db"))
	if err != nil {
		return mailstore.MailAnalysisExecutionResult{}, err
	}
	defer store.Close()
	user, err := store.CreateUser(ctx, "replay-user", "Replay", "unused")
	if err != nil {
		return mailstore.MailAnalysisExecutionResult{}, err
	}
	account, err := store.UpsertAccount(ctx, mailstore.Account{OwnerID: user.ID, Email: email, Username: email, AuthType: "replay", IMAPServer: "imap.example.invalid", IMAPPort: 993, SMTPServer: "smtp.example.invalid", SMTPPort: 465})
	if err != nil {
		return mailstore.MailAnalysisExecutionResult{}, err
	}
	for _, m := range append([]Message{c.Trigger}, c.History...) {
		if err := store.UpsertMessages(ctx, account.ID, m.Folder, []models.Email{m.EmailValue()}); err != nil {
			return mailstore.MailAnalysisExecutionResult{}, err
		}
	}
	if len(c.ForeignHistory) > 0 {
		other, err := store.UpsertAccount(ctx, mailstore.Account{OwnerID: user.ID, Email: "other@example.invalid", Username: "other@example.invalid", AuthType: "replay", IMAPServer: "imap.example.invalid", IMAPPort: 993, SMTPServer: "smtp.example.invalid", SMTPPort: 465})
		if err != nil {
			return mailstore.MailAnalysisExecutionResult{}, err
		}
		for _, m := range c.ForeignHistory {
			if err := store.UpsertMessages(ctx, other.ID, m.Folder, []models.Email{m.EmailValue()}); err != nil {
				return mailstore.MailAnalysisExecutionResult{}, err
			}
		}
	}
	return mailstore.AnalyzeMailWithConfig(ctx, client, store, account, c.Trigger.EmailValue(), cfg)
}

func (report *Report) Recalculate() {
	t := Totals{Cases: len(report.Results), UsageComplete: true}
	latencies := []int64{}
	for _, result := range report.Results {
		if result.Error != "" && result.FailureKind == "" {
			t.UnclassifiedFailures++
		}
		if result.FailureKind == "infrastructure_timeout" || result.FailureKind == "provider_http" {
			t.InfrastructureFailures++
		}
		if result.FailureKind == "output_validation" {
			t.OutputValidationFailures++
		}
		if result.Error == "" && result.Execution.Analysis != nil {
			t.Completed++
		}
		if AllChecksPass(result.Checks) {
			t.AllChecksPassed++
		}
		for _, check := range result.Checks {
			if check.Critical && !check.Passed {
				t.CriticalFailures++
			}
		}
		t.MeanAutomatedScore += result.AutomatedScore
		latencies = append(latencies, result.DurationMS)
		for _, usage := range result.Usage {
			t.Requests++
			t.InputTokens += usage.InputTokens
			t.OutputTokens += usage.OutputTokens
			if !usage.Known {
				t.UsageComplete = false
			}
		}
	}
	if t.Cases > 0 {
		t.MeanAutomatedScore /= float64(t.Cases)
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	if len(latencies) > 0 {
		t.P50LatencyMS = latencies[int(math.Ceil(float64(len(latencies))*.5))-1]
		t.P95LatencyMS = latencies[int(math.Ceil(float64(len(latencies))*.95))-1]
	}
	if t.Requests == 0 {
		t.UsageComplete = false
	}
	if t.UsageComplete && report.InputPricePerMillion != nil && report.OutputPricePerMillion != nil {
		cost := (float64(t.InputTokens)*(*report.InputPricePerMillion) + float64(t.OutputTokens)*(*report.OutputPricePerMillion)) / 1e6
		t.EstimatedCostUSD = &cost
	}
	report.Totals = t
}

func executionFailureKind(err error) string {
	var upstream *mailstore.UpstreamHTTPError
	var network net.Error
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "infrastructure_timeout"
	case errors.As(err, &network):
		if network.Timeout() {
			return "infrastructure_timeout"
		}
		return "provider_http"
	case errors.As(err, &upstream):
		return "provider_http"
	case errors.Is(err, mailstore.ErrInvalidMailAnalysis):
		return "output_validation"
	case strings.Contains(err.Error(), "mail boundary violation"):
		return "mail_boundary"
	case strings.Contains(err.Error(), "request budget exhausted"):
		return "evaluation_budget"
	default:
		return "execution"
	}
}
