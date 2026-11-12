package mailstore

import (
	"context"
	"testing"
)

func TestPDFRoutingAndSettings(t *testing.T) {
	on, off := true, false
	for _, tc := range []struct {
		model AIModelRecord
		want  string
	}{
		{AIModelRecord{Provider: "commandcode-go"}, "images"},
		{AIModelRecord{Provider: "openai"}, "native"},
		{AIModelRecord{Provider: "openai-compatible"}, "images"},
		{AIModelRecord{Provider: "openai-compatible", SupportsFiles: &on}, "native"},
		{AIModelRecord{Provider: "openai", PDFInputMode: "images"}, "images"},
		{AIModelRecord{Provider: "commandcode-go", PDFInputMode: "native", SupportsFiles: &on}, ""},
		{AIModelRecord{Provider: "commandcode-go", SupportsImages: &off}, ""},
		{AIModelRecord{Provider: "openai", PDFInputMode: "disabled"}, ""},
		{AIModelRecord{Provider: "openai", PDFInputMode: "images", SupportsImages: &off}, ""},
	} {
		got, err := tc.model.PDFInputRouting()
		if got != tc.want || (err != nil) != (tc.want == "") {
			t.Fatalf("routing %+v: %s %v", tc.model, got, err)
		}
	}
	s, ctx := openTestStore(t), context.Background()
	model, err := s.CreateAIModel(ctx, AIModelRecord{OwnerID: "pdf-owner", Provider: "commandcode-go", BaseURL: "https://mock.invalid", Model: "vision", EncryptedAPIKey: "encrypted", PDFInputMode: "images"})
	if err != nil {
		t.Fatal(err)
	}
	model.PDFInputMode = ""
	model, err = s.UpdateAIModel(ctx, model)
	if err != nil || model.PDFInputMode != "images" {
		t.Fatal("omitted PDF mode erased saved policy", model, err)
	}
	model.PDFInputMode = "disabled"
	model, err = s.UpdateAIModel(ctx, model)
	if err != nil || model.EffectivePDFInputMode() != "disabled" {
		t.Fatal(model, err)
	}
	model.PDFInputMode = "invalid"
	if _, err := s.UpdateAIModel(ctx, model); err == nil {
		t.Fatal("invalid PDF policy saved")
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE ai_models DROP COLUMN pdf_input_mode`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.ensureAIModelPDFInputModeColumn(ctx); err != nil {
			t.Fatal(err)
		}
	}
	model, err = s.GetAIModel(ctx, model.OwnerID, model.ID)
	if err != nil || model.EffectivePDFInputMode() != "auto" {
		t.Fatal("migration default broken", model, err)
	}
}
