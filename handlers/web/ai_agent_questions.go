package web

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

type AIUserQuestion struct {
	Question string   `json:"question"`
	Options  []string `json:"options"`
}

// The question is persisted as ordinary assistant content, so answering uses
// the existing authenticated, versioned chat turn and survives a reload.
func executeAskUser(args map[string]any) (any, string, error) {
	question, ok := args["question"].(string)
	question = strings.TrimSpace(question)
	if !ok || question == "" || utf8.RuneCountInString(question) > 2000 {
		return nil, "", fmt.Errorf("question must contain 1 to 2000 characters")
	}
	options := []string{}
	if raw, exists := args["options"]; exists {
		encoded, err := json.Marshal(raw)
		if err != nil || len(encoded) == 0 || encoded[0] != '[' || json.Unmarshal(encoded, &options) != nil || len(options) > 6 {
			return nil, "", fmt.Errorf("options must be an array of up to 6 strings")
		}
		seen := map[string]bool{}
		for i, option := range options {
			option = strings.TrimSpace(option)
			if option == "" || utf8.RuneCountInString(option) > 200 || seen[option] {
				return nil, "", fmt.Errorf("options must be distinct, non-empty strings of up to 200 characters")
			}
			options[i], seen[option] = option, true
		}
	}
	return &AIUserQuestion{Question: question, Options: options}, question, nil
}
