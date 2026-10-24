package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"inbrix/internal/maileval"
	"inbrix/mailstore"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("mail-eval", flag.ContinueOnError)
	flags.SetOutput(stderr)
	mode := flags.String("mode", "validate", "validate (offline), run (live model), rescore (offline), compare (offline)")
	suitePath := flags.String("suite", "", "JSON suite path; blank uses embedded synthetic cases")
	configPath := flags.String("config", "", "read model/skill settings from this app config and read-only SQLite")
	owner := flags.String("owner", "", "owner id for configured model selection")
	modelID := flags.String("model-id", "", "configured model id; defaults to the selected owner's default model")
	agentID := flags.String("agent-id", "", "configured agent id; defaults to the selected owner's summary agent")
	provider := flags.String("provider", "openai-compatible", "provider for environment-key runs")
	modelName := flags.String("model", "", "model name for environment-key runs")
	baseURL := flags.String("base-url", "", "base URL for environment-key runs")
	keyEnv := flags.String("key-env", "INBRIX_EVAL_API_KEY", "environment variable containing the model API key")
	effort := flags.String("effort", "", "override configured reasoning effort")
	skillFile := flags.String("skill-file", "", "candidate root Skill instructions; inherited dependencies stay frozen; revision=0")
	cases := flags.String("cases", "", "comma-separated case ids; blank runs all")
	repeats := flags.Int("repeat", 1, "iterations per case, 1..10")
	maxRequests := flags.Int("max-requests", 64, "maximum model HTTP requests for the entire run")
	timeout := flags.Duration("timeout", 15*time.Minute, "overall run deadline")
	output := flags.String("out", "", "fresh output prefix for .json and .md reports")
	input := flags.String("report", "", "saved report for rescore or compare")
	previousSuitePath := flags.String("previous-suite", "", "original collected suite for offline rubric repairs; mail inputs must be identical")
	baseline := flags.String("baseline", "", "saved baseline for run or compare")
	inputRate := flags.Float64("input-usd-per-million", -1, "optional flat input-token price; estimation only")
	outputRate := flags.Float64("output-usd-per-million", -1, "optional flat output-token price; estimation only")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "unexpected positional arguments")
		return 2
	}
	suite, err := maileval.LoadSuite(*suitePath)
	if err != nil {
		fmt.Fprintln(stderr, "suite:", err)
		return 2
	}
	if *mode == "validate" {
		fmt.Fprintf(stdout, "Validated %d authored cases; suite=%s fingerprint=%s. No model requests; no quality score.\n", len(suite.Cases), suite.Name, maileval.Fingerprint(suite))
		for _, c := range suite.Cases {
			fmt.Fprintf(stdout, "  %s: %s\n", c.ID, c.Title)
		}
		return 0
	}
	if *output == "" {
		*output = filepath.Join("output", "mail-eval", time.Now().UTC().Format("20060102T150405.000000000Z"))
	}
	var report maileval.Report
	switch *mode {
	case "rescore", "compare":
		if *input == "" {
			fmt.Fprintln(stderr, "-report is required")
			return 2
		}
		report, err = maileval.ReadReport(*input)
		if err == nil && *mode == "rescore" {
			if *previousSuitePath != "" {
				previous, loadErr := maileval.LoadSuite(*previousSuitePath)
				if loadErr != nil {
					err = loadErr
				} else {
					report, err = maileval.RescoreWithPreviousSuite(previous, suite, report)
				}
			} else {
				report, err = maileval.Rescore(suite, report)
			}
		}
	case "run":
		if *timeout < time.Second || *timeout > time.Hour || *repeats < 1 || *repeats > 10 || *maxRequests < 1 || *maxRequests > 10000 {
			fmt.Fprintln(stderr, "invalid timeout, repeat or request budget")
			return 2
		}
		if math.IsNaN(*inputRate) || math.IsNaN(*outputRate) || math.IsInf(*inputRate, 0) || math.IsInf(*outputRate, 0) || (*inputRate < 0) != (*outputRate < 0) {
			fmt.Fprintln(stderr, "provide both finite nonnegative prices, or omit both")
			return 2
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		ctx, deadlineCancel := context.WithTimeout(ctx, *timeout)
		defer deadlineCancel()
		cfg := maileval.DefaultExecutionConfig()
		origin := "builtin-default"
		if *configPath != "" {
			cfg, origin, err = maileval.LoadConfiguredExecution(ctx, *configPath, *owner, *modelID, *agentID)
		} else {
			cfg.Model = mailstore.AIModelRecord{ID: "environment-model", Provider: *provider, Model: *modelName, BaseURL: *baseURL, ExtraParams: "{}", ReasoningEffort: "medium"}
			cfg.APIKey = os.Getenv(*keyEnv)
		}
		if err != nil {
			fmt.Fprintln(stderr, "configuration:", err)
			return 2
		}
		if cfg.APIKey == "" || cfg.Model.Model == "" || cfg.Model.BaseURL == "" {
			fmt.Fprintln(stderr, "configure a model via -config or provide -model, -base-url and the key environment variable")
			return 2
		}
		if *effort != "" {
			cfg.Model.ReasoningEffort = *effort
		}
		if *skillFile != "" {
			cfg, err = maileval.OverrideSkillFile(cfg, *skillFile)
			origin = "candidate-file (unpublished revision 0)"
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 2
			}
		}
		options := maileval.Options{Config: cfg, SkillOrigin: origin, Client: &http.Client{Timeout: 65 * time.Second}, Repeats: *repeats, MaxRequests: *maxRequests, Progress: func(line string) { fmt.Fprintln(stderr, line) }}
		if *cases != "" {
			for _, id := range strings.Split(*cases, ",") {
				options.CaseIDs = append(options.CaseIDs, strings.TrimSpace(id))
			}
		}
		if *inputRate >= 0 {
			options.InputPricePerMillion = inputRate
			options.OutputPricePerMillion = outputRate
		}
		fmt.Fprintf(stderr, "Live model: %s / %s; Skill: %s/v%d; only fixture mail is replayed.\n", cfg.Model.Provider, cfg.Model.Model, cfg.Skill.ID, cfg.Skill.Revision)
		report, err = maileval.Run(ctx, suite, options)
	default:
		fmt.Fprintln(stderr, "unknown -mode")
		return 2
	}
	if err != nil && len(report.Results) == 0 {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if *mode == "compare" && *baseline == "" {
		fmt.Fprintln(stderr, "-baseline is required")
		return 2
	}
	if *baseline != "" {
		previous, readErr := maileval.ReadReport(*baseline)
		if readErr != nil {
			fmt.Fprintln(stderr, readErr)
			return 2
		}
		diff, compareErr := maileval.Compare(previous, report)
		if compareErr != nil {
			fmt.Fprintln(stderr, compareErr)
			return 2
		}
		report.Comparison = &diff
	}
	if writeErr := maileval.WriteReport(*output, report); writeErr != nil {
		fmt.Fprintln(stderr, writeErr)
		return 2
	}
	fmt.Fprintf(stdout, "Reports: %s.json and %s.md\nCompleted %d/%d; all-checks-pass %d/%d; automated score %.1f. Human semantic review is still required.\n", *output, *output, report.Totals.Completed, report.Totals.Cases, report.Totals.AllChecksPassed, report.Totals.Cases, report.Totals.MeanAutomatedScore)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if report.Totals.AllChecksPassed != report.Totals.Cases {
		return 1
	}
	return 0
}
