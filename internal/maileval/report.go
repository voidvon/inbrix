package maileval

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Comparison struct {
	BaselineModel      string   `json:"baselineModel"`
	CommonCases        int      `json:"commonCases"`
	MeanScoreDelta     float64  `json:"meanScoreDelta"`
	MeanLatencyDeltaMS float64  `json:"meanLatencyDeltaMs"`
	NewFailures        []string `json:"newFailures"`
	ResolvedFailures   []string `json:"resolvedFailures"`
}

func ReadReport(path string) (Report, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Report{}, err
	}
	if len(raw) > 16<<20 {
		return Report{}, errors.New("report exceeds 16 MiB")
	}
	var report Report
	err = json.Unmarshal(raw, &report)
	if err == nil && (report.Version != 1 || report.RubricVersion != RubricVersion || report.SuiteHash == "" || len(report.Results) == 0) {
		err = errors.New("invalid replay report")
	}
	return report, err
}

// Rescore does not call any model. It only reapplies authored checks to outputs
// from an identical suite; evidence is the saved server-assigned projection.
func Rescore(suite Suite, report Report) (Report, error) {
	if report.Version != 1 || report.RubricVersion != RubricVersion || report.SuiteHash != Fingerprint(suite) {
		return report, errors.New("report and suite/rubric fingerprints differ")
	}
	cases := map[string]Case{}
	for _, c := range suite.Cases {
		cases[c.ID] = c
	}
	seen := map[string]bool{}
	for i, result := range report.Results {
		c, ok := cases[result.ID]
		key := fmt.Sprintf("%s/%d", result.ID, result.Iteration)
		if !ok || result.Iteration < 1 || seen[key] {
			return report, errors.New("report has unknown or duplicate case results")
		}
		seen[key] = true
		report.Results[i].Checks, report.Results[i].AutomatedScore = Evaluate(c, result.Execution.Analysis, result.Error, result.BoundaryViolations)
		report.Results[i].HumanReview = c.HumanReview
	}
	report.Mode = "rescore"
	report.HumanReviewRequired = true
	report.Comparison = nil
	report.Recalculate()
	return report, nil
}

func RescoreWithPreviousSuite(previous, current Suite, report Report) (Report, error) {
	if err := ValidateSuite(previous); err != nil {
		return report, err
	}
	if err := ValidateSuite(current); err != nil {
		return report, err
	}
	if report.SuiteHash != Fingerprint(previous) {
		return report, errors.New("previous suite does not match the collected report")
	}
	if InputFingerprint(previous) != InputFingerprint(current) {
		return report, errors.New("mail inputs or hidden-boundary controls changed; model replay is required")
	}
	if report.CollectedSuiteHash == "" {
		report.CollectedSuiteHash = report.SuiteHash
	}
	report.SuiteHash = Fingerprint(current)
	report.InputHash = InputFingerprint(current)
	report.SuiteName = current.Name
	return Rescore(current, report)
}

func Compare(baseline, current Report) (Comparison, error) {
	if baseline.SuiteHash != current.SuiteHash || baseline.RubricVersion != current.RubricVersion {
		return Comparison{}, errors.New("cannot compare different suites or rubrics")
	}
	previous := map[string]CaseResult{}
	for _, result := range baseline.Results {
		previous[fmt.Sprintf("%s/%d", result.ID, result.Iteration)] = result
	}
	diff := Comparison{BaselineModel: baseline.Model.Provider + "/" + baseline.Model.Model, NewFailures: []string{}, ResolvedFailures: []string{}}
	for _, result := range current.Results {
		key := fmt.Sprintf("%s/%d", result.ID, result.Iteration)
		old, ok := previous[key]
		if !ok {
			continue
		}
		diff.CommonCases++
		diff.MeanScoreDelta += result.AutomatedScore - old.AutomatedScore
		diff.MeanLatencyDeltaMS += float64(result.DurationMS - old.DurationMS)
		priorChecks := map[string]bool{}
		for _, check := range old.Checks {
			priorChecks[check.ID] = check.Passed
		}
		for _, check := range result.Checks {
			wasPassed, ok := priorChecks[check.ID]
			if !ok {
				continue
			}
			if wasPassed && !check.Passed {
				diff.NewFailures = append(diff.NewFailures, key+":"+check.ID)
			}
			if !wasPassed && check.Passed {
				diff.ResolvedFailures = append(diff.ResolvedFailures, key+":"+check.ID)
			}
		}
	}
	if diff.CommonCases == 0 {
		return diff, errors.New("reports have no common case iterations")
	}
	diff.MeanScoreDelta /= float64(diff.CommonCases)
	diff.MeanLatencyDeltaMS /= float64(diff.CommonCases)
	sort.Strings(diff.NewFailures)
	sort.Strings(diff.ResolvedFailures)
	return diff, nil
}

func Markdown(report Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# 邮件分析回放评测\n\n模式：%s；模型：`%s / %s`；流水线版本以各案例结果为准。\n\n", report.Mode, escapeCell(report.Model.Provider), escapeCell(report.Model.Model))
	b.WriteString("**以下是明确规则的自动检查结果，不等于语义正确率。全部案例仍需人工核对事实关联、遗漏和无依据承诺。内置案例是合成脱敏测试数据。**\n\n")
	if report.CollectedSuiteHash != "" && report.CollectedSuiteHash != report.SuiteHash {
		fmt.Fprintf(&b, "规则已离线复核；原采集套件指纹 `%s`，邮件输入保持一致，未新增模型请求。\n\n", report.CollectedSuiteHash)
	}
	fmt.Fprintf(&b, "套件：`%s`；指纹：`%s`；Skill：`%s/v%d`（%s）。\n\n", report.SuiteName, report.SuiteHash, report.Model.Skill.ID, report.Model.Skill.Revision, report.Model.SkillOrigin)
	t := report.Totals
	fmt.Fprintf(&b, "执行完成 %d/%d；全部检查通过 %d/%d；失败的关键检查 %d 项；平均自动检查分 %.1f/100。\n\nP50 %.2f 秒，P95 %.2f 秒；实际请求 %d 次。\n\n", t.Completed, t.Cases, t.AllChecksPassed, t.Cases, t.CriticalFailures, t.MeanAutomatedScore, float64(t.P50LatencyMS)/1000, float64(t.P95LatencyMS)/1000, t.Requests)
	fmt.Fprintf(&b, "基础设施失败 %d；输出校验失败 %d；旧报告未分类失败 %d。\n\n", t.InfrastructureFailures, t.OutputValidationFailures, t.UnclassifiedFailures)
	fmt.Fprintf(&b, "已报告 token：输入 %d，输出 %d；用量完整：%t。", t.InputTokens, t.OutputTokens, t.UsageComplete)
	if t.EstimatedCostUSD != nil {
		fmt.Fprintf(&b, "按传入的统一单价估算 USD %.6f；不代表最终账单。", *t.EstimatedCostUSD)
	} else {
		b.WriteString("未提供可完整计算的单价/用量，费用未知。")
	}
	b.WriteString("\n\n")
	b.WriteString("| 案例 | 执行 | 自动检查分 | 请求数 | 耗时 | 失败检查 |\n| --- | --- | ---: | ---: | ---: | --- |\n")
	for _, result := range report.Results {
		status := "完成"
		if result.Error != "" {
			status = "失败"
		}
		failed := []string{}
		for _, check := range result.Checks {
			if !check.Passed {
				failed = append(failed, check.ID)
			}
		}
		fmt.Fprintf(&b, "| %s #%d | %s | %.1f | %d | %.2fs | %s |\n", escapeCell(result.ID), result.Iteration, status, result.AutomatedScore, len(result.Usage), float64(result.DurationMS)/1000, escapeCell(strings.Join(failed, ", ")))
	}
	if report.Comparison != nil {
		d := report.Comparison
		fmt.Fprintf(&b, "\n## 与基线比较\n\n共同案例 %d；平均检查分变化 %+.1f；平均耗时变化 %+.2f 秒。\n\n新增失败：%s\n\n已解决：%s\n", d.CommonCases, d.MeanScoreDelta, d.MeanLatencyDeltaMS/1000, strings.Join(d.NewFailures, ", "), strings.Join(d.ResolvedFailures, ", "))
	}
	for _, result := range report.Results {
		fmt.Fprintf(&b, "\n## %s #%d：%s\n\n", result.ID, result.Iteration, escapeCell(result.Title))
		if result.Error != "" {
			fmt.Fprintf(&b, "执行错误：%s\n\n", escapeCell(result.Error))
		}
		if a := result.Execution.Analysis; a != nil {
			fmt.Fprintf(&b, "**本次诉求**：%s\n\n**背景**：%s\n\n**变化**：%s\n\n", escapeCell(a.Request), escapeCell(a.Background), escapeCell(a.Changes))
			b.WriteString("**待办**：\n\n")
			for _, item := range a.Actions {
				fmt.Fprintf(&b, "- %s\n", escapeCell(item))
			}
			b.WriteString("\n**缺失信息**：\n\n")
			for _, item := range a.Missing {
				fmt.Fprintf(&b, "- %s\n", escapeCell(item))
			}
			b.WriteString("\n**已读取证据**：\n\n")
			for _, source := range a.Sources {
				fmt.Fprintf(&b, "- `%s/%s`，全文读取：%t\n", source.Folder, source.UID, fullyRead(source))
			}
			b.WriteString("\n**精简输出**：\n\n")
			if a.Summary != "" {
				fmt.Fprintf(&b, "%s\n", escapeCell(a.Summary))
			}
			keys := []string{}
			for key := range a.Fields {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				fmt.Fprintf(&b, "- %s：%s\n", escapeCell(key), escapeCell(a.Fields[key]))
			}
		} else if result.LastOutput != "" {
			fmt.Fprintf(&b, "模型最后输出（JSON 字符串转义）：\n\n%s\n\n", strconvQuote(result.LastOutput))
		}
		b.WriteString("\n**自动检查**：\n\n")
		for _, check := range result.Checks {
			marker := "x"
			if !check.Passed {
				marker = " "
			}
			fmt.Fprintf(&b, "- [%s] %s：%s\n", marker, check.ID, escapeCell(check.Description+" — "+check.Detail))
		}
		b.WriteString("\n**人工复核（未自动判定）**：\n\n")
		for _, item := range result.HumanReview {
			fmt.Fprintf(&b, "- [ ] %s\n", escapeCell(item))
		}
	}
	return b.String()
}

func strconvQuote(s string) string { raw, _ := json.Marshal(s); return string(raw) }
func escapeCell(s string) string {
	return strings.NewReplacer("|", "\\|", "\n", "<br>", "<", "&lt;", ">", "&gt;").Replace(s)
}

func WriteReport(prefix string, report Report) error {
	if prefix == "" {
		return errors.New("report output prefix is empty")
	}
	if err := os.MkdirAll(filepath.Dir(prefix), 0700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	paths := []string{prefix + ".json", prefix + ".md"}
	for _, path := range paths {
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("report already exists: %s; choose a new -out prefix", path)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	for i, content := range [][]byte{append(raw, '\n'), []byte(Markdown(report))} {
		file, err := os.OpenFile(paths[i], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, writeErr := file.Write(content)
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
