package web

import (
	"encoding/json"
	"fmt"
	"inbrix/handlers/api"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
)

type composeInlineAttachment struct {
	Field     string `json:"field"`
	ContentID string `json:"contentId"`
}

func parseComposeInlineAttachments(raw string, files map[string][]*multipart.FileHeader) (map[string]composeInlineAttachment, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]composeInlineAttachment{}, nil
	}

	var entries []composeInlineAttachment
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&entries); err != nil {
		return nil, fmt.Errorf("invalid inline attachment manifest: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("invalid inline attachment manifest")
	}

	byField := make(map[string]composeInlineAttachment, len(entries))
	contentIDs := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.Field == "" || entry.Field == "attachments" {
			return nil, fmt.Errorf("inline attachment field is invalid")
		}
		if err := api.ValidateContentID(entry.ContentID); err != nil {
			return nil, fmt.Errorf("inline attachment content ID is invalid: %w", err)
		}
		if _, exists := byField[entry.Field]; exists {
			return nil, fmt.Errorf("inline attachment field %q is duplicated", entry.Field)
		}
		if _, exists := contentIDs[entry.ContentID]; exists {
			return nil, fmt.Errorf("inline attachment content ID %q is duplicated", entry.ContentID)
		}
		if len(files[entry.Field]) != 1 {
			return nil, fmt.Errorf("inline attachment field %q must contain exactly one file", entry.Field)
		}
		byField[entry.Field] = entry
		contentIDs[entry.ContentID] = struct{}{}
	}
	return byField, nil
}

func sniffComposeInlineImage(fh *multipart.FileHeader) (string, error) {
	file, err := fh.Open()
	if err != nil {
		return "", fmt.Errorf("open inline image: %w", err)
	}
	defer file.Close()

	buffer := make([]byte, 512)
	n, err := io.ReadFull(file, buffer)
	if err != nil && err != io.ErrUnexpectedEOF {
		return "", fmt.Errorf("read inline image: %w", err)
	}
	contentType := http.DetectContentType(buffer[:n])
	switch contentType {
	case "image/jpeg", "image/png", "image/gif":
		return contentType, nil
	default:
		return "", fmt.Errorf("inline attachment %q is not a supported JPEG, PNG, or GIF image", fh.Filename)
	}
}
