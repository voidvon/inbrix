package mailstore

import (
	"errors"
	"strings"
)

// AIInputAttachment carries original file bytes, never extracted document text.
// Labels retain the source turn when the transcript is compacted.
type AIInputAttachment struct {
	Label       string
	Filename    string
	ContentType string
	Base64      string
}

// Keep callers of the original image-only API source compatible.
type AIInputImage = AIInputAttachment

func (a AIInputAttachment) IsImage() bool { return strings.HasPrefix(a.ContentType, "image/") }

func (model AIModelRecord) SupportsFileInput() bool {
	if model.Provider == "commandcode-go" {
		return false
	}
	if model.SupportsFiles != nil {
		return *model.SupportsFiles
	}
	return model.Provider == "" || model.Provider == "openai" || model.Provider == "gemini"
}

// ValidateInputAttachment checks the protocol independently of image support.
// Compatible gateways must opt in; their standard file content part is PDF only.
func (model AIModelRecord) ValidateInputAttachment(a AIInputAttachment) error {
	if a.IsImage() || a.ContentType == "" { // Preserve existing image capability checks.
		if !model.SupportsImageInput() {
			return errors.New("当前模型未启用图片输入，请在编辑模型中开启“支持图片”或选择其他模型 / Enable image input in model settings or select a vision model")
		}
		return nil
	}
	if !model.SupportsFileInput() {
		return errors.New("当前模型未启用文件输入，请在编辑模型中开启“支持文件”或选择 OpenAI Responses / Gemini 模型 / Enable file input or select a Responses or Gemini model")
	}
	switch model.Provider {
	case "", "openai", "gemini":
	case "openai-compatible":
		if a.ContentType != "application/pdf" {
			return errors.New("Chat Completions 文件输入仅支持 PDF；Word、Excel 请使用 OpenAI Responses 或 Gemini / Use Responses or Gemini for Word and Excel")
		}
	default:
		return errors.New("当前接口不支持原文件输入 / This API does not support file input")
	}
	switch a.ContentType {
	case "application/pdf", "application/msword", "application/vnd.ms-excel",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return nil
	default:
		return errors.New("不支持的 AI 文件类型 / Unsupported AI file type")
	}
}

func validateAIInputAttachments(model AIModelRecord, attachments []AIInputAttachment) error {
	for _, a := range attachments {
		if err := model.ValidateInputAttachment(a); err != nil {
			return err
		}
	}
	return nil
}

func (a AIInputAttachment) dataURL() string {
	return "data:" + a.ContentType + ";base64," + a.Base64
}

func (a AIInputAttachment) responsesPart() map[string]any {
	if a.IsImage() {
		return map[string]any{"type": "input_image", "image_url": a.dataURL()}
	}
	return map[string]any{"type": "input_file", "filename": a.Filename, "file_data": a.dataURL()}
}

func (a AIInputAttachment) chatCompletionsPart() map[string]any {
	if a.IsImage() {
		return map[string]any{"type": "image_url", "image_url": map[string]string{"url": a.dataURL()}}
	}
	return map[string]any{"type": "file", "file": map[string]string{"filename": a.Filename, "file_data": a.dataURL()}}
}

// PDFInputRouting separates native file capability from document preparation policy.
// The serializer still validates the prepared image or original file, independently.
func ValidatePDFInputMode(mode string) error {
	switch mode {
	case "", "auto", "native", "images", "disabled":
		return nil
	default:
		return errors.New("PDF input mode must be auto, native, images, or disabled")
	}
}

func (model AIModelRecord) EffectivePDFInputMode() string {
	if model.PDFInputMode == "" {
		return "auto"
	}
	return model.PDFInputMode
}

func (model AIModelRecord) PDFInputRouting() (string, error) {
	switch model.EffectivePDFInputMode() {
	case "disabled":
		return "", errors.New("当前模型已禁用 PDF 输入 / PDF input is disabled for this model")
	case "native":
		if !model.SupportsFileInput() {
			return "", errors.New("当前接口未启用原生 PDF 输入，请选择页面图片模式 / Native PDF input is unavailable; select page images")
		}
		return "native", nil
	case "images":
		if !model.SupportsImageInput() {
			return "", errors.New("PDF 页面图片模式需要支持图片的模型 / PDF page images require a vision model")
		}
		return "images", nil
	case "auto":
		if model.SupportsFileInput() {
			return "native", nil
		}
		if model.SupportsImageInput() {
			return "images", nil
		}
		return "", errors.New("当前模型不支持 PDF 原文件或图片，请更换模型 / Select a model supporting PDF or images")
	default:
		return "", ValidatePDFInputMode(model.PDFInputMode)
	}
}
