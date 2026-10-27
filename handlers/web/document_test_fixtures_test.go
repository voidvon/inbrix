package web

import (
	"context"
	"encoding/json"
	"inbrix/mailstore"
	"strings"
)

// Fixture construction is the only place a layout is read to seed business
// data. Production reads require a canonical snapshot and never do this.
func seedCanonicalTestSnapshot(payload map[string]any, values map[string]string, items []AIDocumentItem) {
	copy := map[string]string{}
	for k, v := range values {
		copy[k] = v
	}
	if copy["tax_rate"] != "" && copy["tax_mode"] == "" {
		copy["tax_mode"] = "excluded"
	}
	ensureDocumentLineIDs(items)
	_ = setCanvasBusiness(payload, copy, items)
}

func testCommands(body string, values map[string]string, targets []AIDocumentItem) []DocumentCommand {
	commands := []DocumentCommand{}
	fields := map[string]json.RawMessage{}
	for key, value := range values {
		if documentCalculatedKeys[key] {
			continue
		}
		raw, _ := json.Marshal(value)
		fields[documentFieldKey(key)] = raw
	}
	if len(fields) > 0 {
		commands = append(commands, DocumentCommand{Type: "set_fields", Fields: fields})
	}
	if targets != nil {
		_, current, _ := canvasDocumentContent(body)
		for i, item := range targets {
			raw, _ := json.Marshal(item)
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(raw, &fields)
			for _, key := range []string{"lineId", "amount", "taxRate", "taxMode"} {
				delete(fields, key)
			}
			if i < len(current) {
				commands = append(commands, DocumentCommand{Type: "update_line", LineID: current[i].LineID, Fields: fields})
			} else {
				commands = append(commands, DocumentCommand{Type: "add_line", Fields: fields})
			}
		}
		if len(targets) < len(current) {
			for _, line := range current[len(targets):] {
				commands = append(commands, DocumentCommand{Type: "remove_line", LineID: line.LineID})
			}
		}
	}
	return commands
}

func executeDocumentUpdateTest(h *AISettingsHandler, ctx context.Context, owner string, args map[string]any, model mailstore.AIModelRecord, key string) (any, string, error) {
	converted := map[string]any{}
	for k, v := range args {
		converted[k] = v
	}
	if record, _ := h.mailDB.GetDocument(ctx, owner, parseDocumentIDArg(args)); record != nil {
		values := parseStringMapArg(args, "values")
		items := parseItemsArg(args, "items")
		_, current, _ := canvasDocumentContent(record.HTML)
		// Scenario fixture lines omit unchanged catalog identity by design.
		for i := range items {
			if i < len(current) && items[i].Model == current[i].Model {
				items[i].ProductID = current[i].ProductID
				items[i].ProductVersion = current[i].ProductVersion
				items[i].VariantID = current[i].VariantID
				items[i].PriceID = current[i].PriceID
			}
		}
		commands := testCommands(record.HTML, values, items)
		delete(converted, "values")
		delete(converted, "items")
		if len(commands) > 0 {
			converted["commands"] = commands
		}
		converted["baseRevision"] = record.Revision
	}
	return h.executeUpdateDocument(ctx, owner, converted, model, key)
}

func canonicalTestCanvas(body string) string {
	var payload map[string]any
	if json.Unmarshal([]byte(strings.TrimPrefix(body, canvasDocumentPrefix)), &payload) != nil {
		return body
	}
	var parsed canvasPayload
	raw, _ := json.Marshal(payload)
	_ = json.Unmarshal(raw, &parsed)
	values := map[string]string{}
	for _, elements := range [][]canvasElement{parsed.Data.Main, parsed.Data.Header, parsed.Data.Footer} {
		collectCanvasControls(elements, values)
		collectCanvasProductImages(elements, values)
	}
	seedCanonicalTestSnapshot(payload, values, canvasDocumentItems(values))
	raw, _ = json.Marshal(payload)
	return canvasDocumentPrefix + string(raw)
}
