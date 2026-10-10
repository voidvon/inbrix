package mailstore

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"inbrix/models"
)

func TestMailTranslationPersistsAndStripsQuotes(t *testing.T) {
	s, account, baseMsg, encryptionKey := setupMailSummaryTest(t)
	ctx := context.Background()

	messageWithQuote := models.Email{
		ID:         "1001",
		Folder:     "INBOX",
		Body:       "Please find attached the quotation details.\r\n\r\nOn Oct 5, 2026, Alice wrote:\r\n> Old message that must not be translated",
		BodyCached: true,
	}
	if err := s.UpsertMessages(ctx, account.ID, messageWithQuote.Folder, []models.Email{messageWithQuote}); err != nil {
		t.Fatal(err)
	}

	var capturedInput string
	client := &recordingWebhookClient{
		status: http.StatusOK,
		body:   `{"output_text":"请查收附件中的报价详情。"}`,
	}

	result, err := GetOrCreateMailTranslation(ctx, client, s, encryptionKey, account, messageWithQuote, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Cached {
		t.Fatal("expected first call to not be cached")
	}
	if result.Record.Summary != "请查收附件中的报价详情。" {
		t.Fatalf("unexpected translation: %q", result.Record.Summary)
	}
	capturedInput = string(client.data)
	if strings.Contains(capturedInput, "Old message that must not be translated") {
		t.Fatalf("translation request should not contain quoted content, got: %s", capturedInput)
	}
	if !strings.Contains(capturedInput, "Please find attached the quotation details.") {
		t.Fatalf("translation request should contain message content, got: %s", capturedInput)
	}

	// Second call should be cached without hitting client
	client.data = nil
	second, err := GetOrCreateMailTranslation(ctx, client, s, encryptionKey, account, messageWithQuote, false)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Cached {
		t.Fatal("expected second call to be cached")
	}
	if second.Record.Summary != "请查收附件中的报价详情。" {
		t.Fatalf("unexpected cached translation: %q", second.Record.Summary)
	}
	if client.data != nil {
		t.Fatal("expected no HTTP call for cached translation")
	}

	// List translations
	listed, err := s.ListMessageTranslations(ctx, account.ID, []MessageSummaryKey{
		{AccountID: account.ID, FolderName: messageWithQuote.Folder, UID: messageWithQuote.ID},
		{AccountID: account.ID, FolderName: baseMsg.Folder, UID: baseMsg.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected 1 translation in list, got %d", len(listed))
	}
	if listed[MessageSummaryLookupKey(messageWithQuote.Folder, messageWithQuote.ID)].Summary != "请查收附件中的报价详情。" {
		t.Fatalf("unexpected summary in list: %+v", listed)
	}
}

func TestMailTranslationAllPersistsAndIncludesQuotes(t *testing.T) {
	s, account, _, encryptionKey := setupMailSummaryTest(t)
	ctx := context.Background()

	messageWithQuote := models.Email{
		ID:         "1002",
		Folder:     "INBOX",
		Body:       "Please find attached the quotation details.\r\n\r\nOn Oct 5, 2026, Alice wrote:\r\n> Old message that must not be stripped",
		BodyCached: true,
	}
	if err := s.UpsertMessages(ctx, account.ID, messageWithQuote.Folder, []models.Email{messageWithQuote}); err != nil {
		t.Fatal(err)
	}

	client := &recordingWebhookClient{
		status: http.StatusOK,
		body:   `{"output_text":"请查收附件中的报价详情。\n\n2026年10月5日，Alice写道：\n> 必须保留的老信息"}`,
	}

	result, err := GetOrCreateMailTranslationAll(ctx, client, s, encryptionKey, account, messageWithQuote, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Cached {
		t.Fatal("expected first call to not be cached")
	}
	if !strings.Contains(result.Record.Summary, "必须保留的老信息") {
		t.Fatalf("unexpected translation: %q", result.Record.Summary)
	}
	capturedInput := string(client.data)
	if !strings.Contains(capturedInput, "Old message that must not be stripped") {
		t.Fatalf("translate all request should contain quoted content, got: %s", capturedInput)
	}
	if !strings.Contains(capturedInput, "Please find attached the quotation details.") {
		t.Fatalf("translate all request should contain message content, got: %s", capturedInput)
	}

	// Verify caching
	client.data = nil
	second, err := GetOrCreateMailTranslationAll(ctx, client, s, encryptionKey, account, messageWithQuote, false)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Cached {
		t.Fatal("expected second call to be cached")
	}
	if second.Record.Summary != result.Record.Summary {
		t.Fatalf("unexpected cached translation: %q", second.Record.Summary)
	}
	if client.data != nil {
		t.Fatal("expected no HTTP call for cached translation")
	}

	// List translations all
	listed, err := s.ListMessageTranslationsAll(ctx, account.ID, []MessageSummaryKey{
		{AccountID: account.ID, FolderName: messageWithQuote.Folder, UID: messageWithQuote.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected 1 translation in list, got %d", len(listed))
	}
	if listed[MessageSummaryLookupKey(messageWithQuote.Folder, messageWithQuote.ID)].Summary != result.Record.Summary {
		t.Fatalf("unexpected summary in list: %+v", listed)
	}
}

func TestCleanTranslationText(t *testing.T) {
	input := "  尊敬的客户：  \r\n\r\n\r\n\r\n   您好！关于您咨询的报价，   我们已经完成核对。  \r\n相关文件已上传。   \r\n\r\n\r\n  祝好！  "
	expected := "尊敬的客户：\n\n您好！关于您咨询的报价， 我们已经完成核对。\n相关文件已上传。\n\n祝好！"
	got := CleanTranslationText(input)
	if got != expected {
		t.Fatalf("CleanTranslationText:\nexpected: %q\ngot:      %q", expected, got)
	}

	// Empty input
	if got := CleanTranslationText("   \r\n  \n  "); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}
