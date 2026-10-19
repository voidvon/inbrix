package web

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// Stored quotations and contracts are canvas documents: the client serializes
// the editor's element tree and stores it behind this prefix. Reading the
// element tree is what lets the agent see and change a document's fields
// (customer, line items, totals, terms) instead of guessing at raw HTML.
const canvasDocumentPrefix = "__INBRIX_CANVAS_DOCUMENT__:"

type canvasControl struct {
	ConceptID   string          `json:"conceptId"`
	Placeholder string          `json:"placeholder"`
	Value       []canvasElement `json:"value"`
}

type canvasElement struct {
	Value   string         `json:"value"`
	Control *canvasControl `json:"control"`
	TrList  []canvasRow    `json:"trList"`
}

type canvasRow struct {
	TdList []canvasCell `json:"tdList"`
}

type canvasCell struct {
	Value []canvasElement `json:"value"`
}

type canvasPayload struct {
	Template string `json:"template"`
	Data     struct {
		Main []canvasElement `json:"main"`
	} `json:"data"`
}

// parseStoredCanvasDocument decodes a stored document body, reporting whether it
// really is a canvas document. Legacy plain-HTML documents return false.
func parseStoredCanvasDocument(body string) (canvasPayload, bool) {
	body = strings.TrimSpace(body)
	if !strings.HasPrefix(body, canvasDocumentPrefix) {
		return canvasPayload{}, false
	}
	var payload canvasPayload
	if err := json.Unmarshal([]byte(strings.TrimPrefix(body, canvasDocumentPrefix)), &payload); err != nil {
		return canvasPayload{}, false
	}
	if len(payload.Data.Main) == 0 {
		return canvasPayload{}, false
	}
	return payload, true
}

// collectCanvasControls walks the element tree and records every control's
// concept id and current text.
func collectCanvasControls(elements []canvasElement, out map[string]string) {
	for _, element := range elements {
		if element.Control != nil && strings.TrimSpace(element.Control.ConceptID) != "" {
			out[element.Control.ConceptID] = canvasControlText(element.Control)
		}
		for _, row := range element.TrList {
			for _, cell := range row.TdList {
				collectCanvasControls(cell.Value, out)
			}
		}
	}
}

// canvasControlText joins the text runs of a control. A control with no runs is
// empty rather than the literal placeholder.
func canvasControlText(control *canvasControl) string {
	if control == nil || len(control.Value) == 0 {
		return ""
	}
	var builder strings.Builder
	for _, run := range control.Value {
		builder.WriteString(run.Value)
	}
	return strings.TrimSpace(builder.String())
}

// itemField describes the concept ids a template uses for one line item. Single
// item documents drop the numeric suffix.
var itemFieldConcept = map[string]string{
	"item_model":       "model",
	"item_description": "description",
	"item_qty":         "qty",
	"item_price":       "price",
	"item_amount":      "amount",
}

// canvasDocumentItems rebuilds the line items from the flat control map, keeping
// the template's own ordering.
func canvasDocumentItems(values map[string]string) []AIDocumentItem {
	type slot struct {
		index int
		item  AIDocumentItem
	}
	byIndex := map[int]*AIDocumentItem{}
	single := AIDocumentItem{}
	hasSingle := false
	hasIndexed := false

	for key, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if field, ok := itemFieldConcept[key]; ok {
			assignItemField(&single, field, value)
			hasSingle = true
			continue
		}
		for concept, field := range itemFieldConcept {
			prefix := concept + "_"
			if !strings.HasPrefix(key, prefix) {
				continue
			}
			index, err := strconv.Atoi(strings.TrimPrefix(key, prefix))
			if err != nil || index < 0 || index > 500 {
				continue
			}
			if byIndex[index] == nil {
				byIndex[index] = &AIDocumentItem{}
			}
			assignItemField(byIndex[index], field, value)
			hasIndexed = true
		}
	}

	items := make([]AIDocumentItem, 0, len(byIndex)+1)
	if hasIndexed {
		indexes := make([]int, 0, len(byIndex))
		for index := range byIndex {
			indexes = append(indexes, index)
		}
		sort.Ints(indexes)
		for _, index := range indexes {
			if item := *byIndex[index]; !itemIsEmpty(item) {
				items = append(items, item)
			}
		}
		return items
	}
	if hasSingle && !itemIsEmpty(single) {
		items = append(items, single)
	}
	return items
}

func assignItemField(item *AIDocumentItem, field, value string) {
	switch field {
	case "model":
		item.Model = value
	case "description":
		item.Description = value
	case "qty":
		item.Qty = value
	case "price":
		item.Price = value
	case "amount":
		item.Amount = value
	}
}

func itemIsEmpty(item AIDocumentItem) bool {
	return strings.TrimSpace(item.Model) == "" && strings.TrimSpace(item.Description) == "" &&
		strings.TrimSpace(item.Qty) == "" && strings.TrimSpace(item.Price) == "" && strings.TrimSpace(item.Amount) == ""
}

// canvasDocumentContent returns the editable fields of a stored document. ok is
// false for documents this build cannot read structurally, which callers must
// report instead of guessing.
func canvasDocumentContent(body string) (map[string]string, []AIDocumentItem, bool) {
	payload, ok := parseStoredCanvasDocument(body)
	if !ok {
		return nil, nil, false
	}
	values := make(map[string]string)
	collectCanvasControls(payload.Data.Main, values)
	// Empty controls are noise for the model; drop them so a variable list stays
	// readable and an update cannot "clear" a field by accident.
	for key, value := range values {
		if strings.TrimSpace(value) == "" {
			delete(values, key)
		}
	}
	return values, canvasDocumentItems(values), true
}

// documentValueKeys returns the field names in a stable order for prompts.
func documentValueKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
