package web

import "strings"

// This is a calculation result, not a certification that a contract is ready
// to send. Never persist it as an input: it must follow the current calculation.
type DocumentPricingValidation struct {
	Status string                 `json:"status"` // complete | incomplete
	Issues []DocumentPricingIssue `json:"issues"`
}

type DocumentPricingIssue struct {
	Code    string `json:"code"`
	Field   string `json:"field"`
	Line    int    `json:"line,omitempty"` // one-based; absent for document-level issues
	Message string `json:"message"`
}

func (v *DocumentPricingValidation) add(code, field string, line int, message string) {
	v.Status = "incomplete"
	v.Issues = append(v.Issues, DocumentPricingIssue{Code: code, Field: field, Line: line, Message: message})
}

func (v *DocumentPricingValidation) summary() string {
	if v == nil {
		return ""
	}
	messages := make([]string, 0, len(v.Issues))
	for _, issue := range v.Issues {
		messages = append(messages, issue.Message)
	}
	return strings.Join(messages, "；")
}
