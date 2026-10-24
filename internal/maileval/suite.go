// Package maileval replays anonymized mail cases through the production mail
// analysis service. Automated checks are explicit heuristics, not an LLM judge
// or proof of factual correctness; every report retains a human-review rubric.
package maileval

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"inbrix/mailstore"
	"inbrix/models"
)

//go:embed testdata/suite.json
var builtinSuite []byte

type Message struct {
	models.Email
	BodyAvailable  bool   `json:"bodyAvailable"`
	Padding        string `json:"padding,omitempty"`
	PaddingRepeats int    `json:"paddingRepeats,omitempty"`
}

func (m Message) EmailValue() models.Email {
	out := m.Email
	out.BodyCached = m.BodyAvailable
	if m.PaddingRepeats > 0 {
		out.Body = strings.Repeat(m.Padding, m.PaddingRepeats) + out.Body
	}
	return out
}

type Reference struct {
	Folder string `json:"folder"`
	UID    string `json:"uid"`
}
type Rule struct {
	ID          string      `json:"id"`
	Description string      `json:"description"`
	Kind        string      `json:"kind"`
	Target      string      `json:"target,omitempty"`
	Patterns    []string    `json:"patterns,omitempty"`
	References  []Reference `json:"references,omitempty"`
	FullRead    bool        `json:"fullRead,omitempty"`
	Citation    bool        `json:"citation,omitempty"`
	Critical    bool        `json:"critical"`
	Weight      int         `json:"weight"`
}
type Case struct {
	ID             string    `json:"id"`
	Title          string    `json:"title"`
	Trigger        Message   `json:"trigger"`
	History        []Message `json:"history"`
	ForeignHistory []Message `json:"foreignHistory,omitempty"`
	HiddenCanaries []string  `json:"hiddenCanaries,omitempty"`
	Rules          []Rule    `json:"rules"`
	HumanReview    []string  `json:"humanReview"`
}
type Suite struct {
	Version      int    `json:"version"`
	Name         string `json:"name"`
	AccountEmail string `json:"accountEmail"`
	Provenance   string `json:"provenance"`
	Cases        []Case `json:"cases"`
}

func LoadSuite(path string) (Suite, error) {
	raw := builtinSuite
	if path != "" {
		var err error
		raw, err = os.ReadFile(path)
		if err != nil {
			return Suite{}, err
		}
	}
	if len(raw) > 4<<20 {
		return Suite{}, errors.New("suite exceeds 4 MiB")
	}
	var suite Suite
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&suite); err != nil {
		return suite, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return suite, errors.New("suite has trailing JSON")
	}
	return suite, ValidateSuite(suite)
}

func Fingerprint(value any) string {
	raw, _ := json.Marshal(value)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

// InputFingerprint excludes the rubric, which is never sent to the model.
// This allows audited offline rubric repairs without replaying paid requests.
func InputFingerprint(suite Suite) string {
	type inputCase struct {
		ID             string
		Trigger        Message
		History        []Message
		ForeignHistory []Message
		HiddenCanaries []string
	}
	cases := make([]inputCase, 0, len(suite.Cases))
	for _, c := range suite.Cases {
		cases = append(cases, inputCase{c.ID, c.Trigger, c.History, c.ForeignHistory, c.HiddenCanaries})
	}
	return Fingerprint(struct {
		Account string
		Cases   []inputCase
	}{suite.AccountEmail, cases})
}

func ValidateSuite(suite Suite) error {
	if suite.Version != 1 || suite.Name == "" || suite.Provenance == "" || suite.AccountEmail == "" || len(suite.Cases) == 0 || len(suite.Cases) > 100 {
		return errors.New("invalid suite metadata or case count")
	}
	ids := map[string]bool{}
	for _, c := range suite.Cases {
		if !regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`).MatchString(c.ID) || ids[c.ID] || c.Title == "" || len(c.Rules) == 0 || len(c.HumanReview) == 0 {
			return fmt.Errorf("invalid case %q", c.ID)
		}
		ids[c.ID] = true
		seen := map[string]Message{}
		messages := append([]Message{c.Trigger}, c.History...)
		if len(messages) > 100 {
			return fmt.Errorf("case %s has too many messages", c.ID)
		}
		for _, m := range append(messages, c.ForeignHistory...) {
			if m.ID == "" || m.Folder == "" || m.Date.IsZero() || m.PaddingRepeats < 0 || m.PaddingRepeats > 10000 || len(m.Padding) > 1000 || len(m.Body)+len(m.HTML)+len(m.Padding)*m.PaddingRepeats > 512000 {
				return fmt.Errorf("case %s has invalid mail", c.ID)
			}
			if _, err := positiveUID(m.ID); err != nil {
				return fmt.Errorf("case %s: %w", c.ID, err)
			}
		}
		for _, m := range messages {
			key := m.Folder + "/" + m.ID
			if _, ok := seen[key]; ok {
				return fmt.Errorf("case %s duplicates mail %s", c.ID, key)
			}
			seen[key] = m
		}
		for _, canary := range c.HiddenCanaries {
			if len(canary) < 8 {
				return fmt.Errorf("case %s has a weak hidden canary", c.ID)
			}
			if strings.Contains(c.Trigger.EmailValue().Body+c.Trigger.HTML, canary) {
				return fmt.Errorf("case %s hides a visible canary", c.ID)
			}
			hidden := false
			anchor, _ := positiveUID(c.Trigger.ID)
			for _, m := range c.History {
				if !strings.Contains(m.EmailValue().Body+m.HTML+m.Preview, canary) {
					continue
				}
				uid, _ := positiveUID(m.ID)
				if m.Date.After(c.Trigger.Date) || (m.Folder == c.Trigger.Folder && uid > anchor) {
					hidden = true
				} else {
					return fmt.Errorf("case %s canary occurs in visible history", c.ID)
				}
			}
			for _, m := range c.ForeignHistory {
				if strings.Contains(m.EmailValue().Body+m.HTML+m.Preview, canary) {
					hidden = true
				}
			}
			if !hidden {
				return fmt.Errorf("case %s hidden canary has no hidden source", c.ID)
			}
		}
		ruleIDs := map[string]bool{}
		for _, rule := range c.Rules {
			if rule.ID == "" || ruleIDs[rule.ID] || rule.Description == "" || rule.Weight < 1 || rule.Weight > 100 {
				return fmt.Errorf("case %s has invalid rule %s", c.ID, rule.ID)
			}
			ruleIDs[rule.ID] = true
			switch rule.Kind {
			case "contains_all", "contains_any", "excludes":
				if len(rule.Patterns) == 0 {
					return fmt.Errorf("rule %s has no patterns", rule.ID)
				}
				switch rule.Target {
				case "request", "background", "changes", "actions", "missing", "fields", "all":
				default:
					return fmt.Errorf("rule %s has unknown target", rule.ID)
				}
				for _, pattern := range rule.Patterns {
					if _, err := regexp.Compile("(?i)" + pattern); err != nil {
						return fmt.Errorf("rule %s: %w", rule.ID, err)
					}
				}
			case "evidence", "read_attempt":
				if len(rule.References) == 0 {
					return fmt.Errorf("rule %s has no mail references", rule.ID)
				}
				for _, ref := range rule.References {
					m, ok := seen[ref.Folder+"/"+ref.UID]
					if !ok || m.Date.After(c.Trigger.Date) {
						return fmt.Errorf("rule %s references unavailable/future mail", rule.ID)
					}
					if rule.Kind == "evidence" && !m.BodyAvailable {
						return fmt.Errorf("rule %s requires unavailable evidence", rule.ID)
					}
				}
			default:
				return fmt.Errorf("unknown rule kind %q", rule.Kind)
			}
		}
	}
	return nil
}

func DefaultExecutionConfig() mailstore.MailAnalysisExecutionConfig {
	skill, _ := mailstore.DefaultMailAnalysisSkillSnapshot()
	return mailstore.MailAnalysisExecutionConfig{Skill: skill, Agent: mailstore.AIAgentRecord{ID: "mail-eval-agent", Name: "Mail analysis evaluation", Prompt: "分析最新邮件及相关历史；清楚区分已有事实、变化和待确认信息。", OutputLabels: []string{"客户", "需求", "要求", "问题"}}}
}
