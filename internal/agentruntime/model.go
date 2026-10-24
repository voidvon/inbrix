package agentruntime

import "encoding/json"

// ToolSpec describes a capability granted by the application for this run.
// Skills can explain capabilities, but cannot register or grant them.
type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// Message is provider independent. ProviderData retains opaque continuation
// items (reasoning, signatures) in memory only, never in UI/tool activity logs.
type Message struct {
	Role         string          `json:"role"`
	Text         string          `json:"content,omitempty"`
	ToolCalls    []ToolCall      `json:"toolCalls,omitempty"`
	ToolCallID   string          `json:"toolCallId,omitempty"`
	Name         string          `json:"name,omitempty"`
	ProviderData json.RawMessage `json:"providerData,omitempty"`
}

type ModelRequest struct {
	Messages []Message  `json:"messages"`
	Tools    []ToolSpec `json:"tools,omitempty"`
	// DisableTools reserves the last model request for synthesis only.
	DisableTools bool `json:"disableTools,omitempty"`
}

type ModelResponse struct {
	Message Message
}
