package maileval

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"inbrix/mailstore"
)

type Check struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Passed      bool   `json:"passed"`
	Critical    bool   `json:"critical"`
	Weight      int    `json:"weight"`
	Detail      string `json:"detail"`
}

func targetText(a *mailstore.MailAnalysis, target string) string {
	if a == nil {
		return ""
	}
	switch target {
	case "request":
		return a.Request
	case "background":
		return a.Background
	case "changes":
		return a.Changes
	case "actions":
		return strings.Join(a.Actions, "\n")
	case "missing":
		return strings.Join(a.Missing, "\n")
	case "fields":
		keys := make([]string, 0, len(a.Fields))
		for key := range a.Fields {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var text strings.Builder
		for _, key := range keys {
			fmt.Fprintf(&text, "%s: %s\n", key, a.Fields[key])
		}
		return text.String()
	default:
		return a.Request + "\n" + a.Background + "\n" + a.Changes + "\n" + strings.Join(a.Actions, "\n") + "\n" + strings.Join(a.Missing, "\n") + "\n" + targetText(a, "fields")
	}
}

func fullyRead(source mailstore.MailAnalysisSource) bool {
	ranges := append([]mailstore.MailAnalysisRange{}, source.Ranges...)
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].Start < ranges[j].Start })
	end := 0
	for _, span := range ranges {
		if span.Start > end {
			return false
		}
		end = max(end, span.End)
	}
	return end >= source.TotalLength
}

// Evaluate uses only model-visible claims, server-assigned evidence, and the
// authored case rubric. It never reads expected facts into the model prompt.
func Evaluate(c Case, analysis *mailstore.MailAnalysis, executionError string, boundaryViolations []string) ([]Check, float64) {
	checks := []Check{
		{ID: "execution", Description: "生产分析流程成功完成并通过结构校验", Passed: executionError == "" && analysis != nil, Critical: true, Weight: 3, Detail: executionError},
		{ID: "mail_boundary", Description: "未向模型提供隐藏的未来/其他邮箱材料", Passed: len(boundaryViolations) == 0, Critical: true, Weight: 3, Detail: strings.Join(boundaryViolations, ", ")},
	}
	for _, rule := range c.Rules {
		check := Check{ID: rule.ID, Description: rule.Description, Critical: rule.Critical, Weight: rule.Weight, Passed: analysis != nil}
		text := targetText(analysis, rule.Target)
		if analysis == nil {
			check.Detail = "No valid structured analysis"
			checks = append(checks, check)
			continue
		}
		switch rule.Kind {
		case "contains_all", "contains_any", "excludes":
			matches := 0
			for _, pattern := range rule.Patterns {
				if regexp.MustCompile("(?i)" + pattern).MatchString(text) {
					matches++
					if rule.Kind == "excludes" {
						check.Detail += "Matched prohibited pattern: " + pattern + "; "
					}
				} else if rule.Kind != "excludes" {
					check.Detail += "Missing pattern: " + pattern + "; "
				}
			}
			check.Passed = (rule.Kind == "contains_all" && matches == len(rule.Patterns)) || (rule.Kind == "contains_any" && matches > 0) || (rule.Kind == "excludes" && matches == 0)
		case "evidence":
			for _, ref := range rule.References {
				found := false
				for _, source := range analysis.Sources {
					if source.Folder == ref.Folder && source.UID == ref.UID && (!rule.FullRead || fullyRead(source)) {
						found = true
					}
				}
				if !found {
					check.Passed = false
					check.Detail += "Unread/incomplete evidence: " + ref.Folder + "/" + ref.UID + "; "
				}
				if rule.Citation && !strings.Contains(analysis.Background+"\n"+analysis.Changes, "["+ref.Folder+"/"+ref.UID+"]") {
					check.Passed = false
					check.Detail += "Missing historical citation: " + ref.Folder + "/" + ref.UID + "; "
				}
			}
		case "read_attempt":
			for _, ref := range rule.References {
				found := false
				for _, activity := range analysis.Activity {
					if activity.Name != "read_email" || activity.Status != "success" {
						continue
					}
					folder, _ := activity.Args["folder"].(string)
					var uid string
					for _, key := range []string{"uid", "id", "message_id", "email_id"} {
						if value, ok := activity.Args[key]; ok {
							uid = fmt.Sprint(value)
							break
						}
					}
					if folder == ref.Folder && uid == ref.UID {
						found = true
					}
				}
				if !found {
					check.Passed = false
					check.Detail += "No body-read attempt: " + ref.Folder + "/" + ref.UID + "; "
				}
			}
		}
		if check.Passed {
			check.Detail = "Matched authored check; semantic review still required"
		}
		checks = append(checks, check)
	}
	earned, total := 0, 0
	for _, check := range checks {
		total += check.Weight
		if check.Passed {
			earned += check.Weight
		}
	}
	return checks, 100 * float64(earned) / float64(max(1, total))
}

func AllChecksPass(checks []Check) bool {
	for _, c := range checks {
		if !c.Passed {
			return false
		}
	}
	return true
}
