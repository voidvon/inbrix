package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Commands express intent. Identity comes from lineId, never a table position.
type DocumentCommand struct {
	Type   string                     `json:"type"`
	LineID string                     `json:"lineId,omitempty"`
	Fields map[string]json.RawMessage `json:"fields,omitempty"`
}

type documentEditReply struct {
	Commands []DocumentCommand `json:"commands"`
	Message  string            `json:"message,omitempty"`
}

// A conversation can answer a question without proposing a document mutation.
func decodeDocumentEditReply(body string) (documentEditReply, error) {
	var result documentEditReply
	decoder := json.NewDecoder(strings.NewReader(cleanJSONResponse(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, fmt.Errorf("文档助手回复无效: %w", err)
	}
	if result.Commands == nil {
		return result, fmt.Errorf("回复需要 commands 数组")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return result, fmt.Errorf("回复只能包含一个 JSON 对象")
	}
	result.Message = strings.TrimSpace(result.Message)
	return result, nil
}

func decodeDocumentCommands(body string) ([]DocumentCommand, error) {
	result, err := decodeDocumentEditReply(body)
	if err != nil {
		return nil, err
	}
	if len(result.Commands) == 0 {
		return nil, fmt.Errorf("请提供非空 commands")
	}
	return result.Commands, nil
}

func applyDocumentCommands(body string, commands []DocumentCommand) (map[string]string, []AIDocumentItem, error) {
	values, items, ok := canvasDocumentContent(body)
	if !ok {
		return nil, nil, fmt.Errorf("需要 version=2 商业快照")
	}
	if len(commands) == 0 || len(commands) > 1000 {
		return nil, nil, fmt.Errorf("修改命令数量必须在 1 到 1000 之间")
	}
	updates := map[string]string{}
	lineChanged := false
	for _, command := range commands {
		index := -1
		for i, item := range items {
			if item.LineID == command.LineID {
				index = i
				break
			}
		}
		switch command.Type {
		case "set_fields":
			if command.LineID != "" || len(command.Fields) == 0 {
				return nil, nil, fmt.Errorf("set_fields 需要非空 fields，不能指定 lineId")
			}
			for key, raw := range command.Fields {
				if retiredDocumentFields[key] || strings.HasPrefix(key, "item_") || documentCalculatedKeys[key] || strings.HasPrefix(key, "source_quotation_") {
					return nil, nil, fmt.Errorf("字段 %s 不能直接修改", key)
				}
				if _, exists := values[key]; !exists && !documentBusinessKeys[key] {
					return nil, nil, fmt.Errorf("文档没有字段 %s，请先配置模板", key)
				}
				var value string
				if bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &value) != nil {
					return nil, nil, fmt.Errorf("字段 %s 必须是字符串", key)
				}
				updates[key] = value
			}
		case "update_line":
			lineChanged = true
			if index < 0 || len(command.Fields) == 0 {
				return nil, nil, fmt.Errorf("update_line 需要有效 lineId 和非空 fields")
			}
			if err := patchDocumentLine(&items[index], command.Fields); err != nil {
				return nil, nil, err
			}
		case "add_line":
			lineChanged = true
			if command.LineID != "" || len(command.Fields) == 0 {
				return nil, nil, fmt.Errorf("add_line 由服务端生成 lineId，且需要非空 fields")
			}
			item := AIDocumentItem{}
			if err := patchDocumentLine(&item, command.Fields); err != nil {
				return nil, nil, err
			}
			next := []AIDocumentItem{item}
			ensureDocumentLineIDs(next)
			items = append(items, next[0])
		case "remove_line":
			lineChanged = true
			if index < 0 || len(command.Fields) > 0 {
				return nil, nil, fmt.Errorf("remove_line 需要有效 lineId，不能提供 fields")
			}
			items = append(items[:index], items[index+1:]...)
		default:
			return nil, nil, fmt.Errorf("未知文档修改命令 %s", command.Type)
		}
	}
	if !lineChanged {
		items = nil
	}
	return updates, items, nil
}

func patchDocumentLine(item *AIDocumentItem, fields map[string]json.RawMessage) error {
	encoded, _ := json.Marshal(item)
	var merged map[string]json.RawMessage
	_ = json.Unmarshal(encoded, &merged)
	for key, raw := range fields {
		switch key {
		case "model", "description", "qty", "price", "picture", "productId", "variantId", "priceId":
			var value string
			if bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &value) != nil {
				return fmt.Errorf("明细字段 %s 必须是字符串", key)
			}
		case "productVersion":
			var value int64
			if bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &value) != nil || value < 0 {
				return fmt.Errorf("产品版本无效")
			}
		default:
			return fmt.Errorf("明细字段 %s 不能直接修改", key)
		}
		merged[key] = raw
	}
	encoded, _ = json.Marshal(merged)
	return json.Unmarshal(encoded, item)
}
