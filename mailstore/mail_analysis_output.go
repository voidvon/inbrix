package mailstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const mailSummaryMaxCharacters = 200

type mailAnalysisOutput struct {
	Request    string   `json:"request"`
	Background string   `json:"background"`
	Changes    string   `json:"changes"`
	Actions    []string `json:"actions"`
	Missing    []string `json:"missing"`
	Summary    string   `json:"summary"`
}

// Report schema failures without copying model output (which can contain mail
// bodies) into persistent error logs or API error messages.
func parseMailAnalysisOutput(raw string) (mailAnalysisOutput, error) {
	var result mailAnalysisOutput
	invalid := func(reason string) (mailAnalysisOutput, error) {
		return result, fmt.Errorf("%w: mail-analysis skill returned invalid structured output: %s", ErrInvalidMailAnalysis, reason)
	}
	decoder := json.NewDecoder(strings.NewReader(CleanJSONFence(raw)))
	var fields map[string]json.RawMessage
	if err := decoder.Decode(&fields); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return invalid("incomplete JSON object")
		}
		var syntax *json.SyntaxError
		if errors.As(err, &syntax) {
			return invalid(fmt.Sprintf("invalid JSON at byte %d", syntax.Offset))
		}
		return invalid("expected a JSON object")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return invalid("trailing output after JSON object")
	}
	for _, name := range []string{"request", "background", "changes", "actions", "missing", "summary"} {
		value, ok := fields[name]
		if !ok {
			return invalid("missing required key " + name)
		}
		if string(value) == "null" {
			return invalid(name + " must not be null")
		}
	}
	if len(fields) != 6 {
		return invalid("unexpected top-level keys")
	}
	for _, field := range []struct {
		name string
		dst  any
		kind string
	}{
		{"request", &result.Request, "a string"},
		{"background", &result.Background, "a string"},
		{"changes", &result.Changes, "a string"},
		{"actions", &result.Actions, "a string array"},
		{"missing", &result.Missing, "a string array"},
		{"summary", &result.Summary, "a string"},
	} {
		if err := json.Unmarshal(fields[field.name], field.dst); err != nil {
			return invalid(field.name + " must be " + field.kind)
		}
	}
	if strings.TrimSpace(result.Request) == "" {
		return invalid("request must be non-empty")
	}
	result.Summary = strings.TrimSpace(result.Summary)
	if result.Summary == "" {
		return invalid("summary must be non-empty")
	}
	if utf8.RuneCountInString(result.Summary) > mailSummaryMaxCharacters {
		return invalid("summary must be at most 200 Unicode characters; rewrite it concisely without truncating key facts")
	}
	// encoding/json accepts null for string destinations, so check nested
	// values explicitly rather than silently turning them into empty strings.
	var shape struct {
		Actions []any `json:"actions"`
		Missing []any `json:"missing"`
	}
	encoded, _ := json.Marshal(fields)
	_ = json.Unmarshal(encoded, &shape)
	for _, array := range []struct {
		name   string
		values []any
	}{{"actions", shape.Actions}, {"missing", shape.Missing}} {
		for _, value := range array.values {
			if _, ok := value.(string); !ok {
				return invalid(array.name + " must contain only strings")
			}
		}
	}
	return result, nil
}
