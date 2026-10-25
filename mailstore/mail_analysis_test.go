package mailstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"inbrix/internal/agentruntime"
	"inbrix/internal/agenttest"
	"inbrix/models"
)

type analysisScriptClient struct {
	inputs []string
	reply  func(int, string) string
}

func (c *analysisScriptClient) Do(req *http.Request) (*http.Response, error) {
	var request struct {
		Input        json.RawMessage `json:"input"`
		Instructions string          `json:"instructions"`
	}
	if err := json.NewDecoder(req.Body).Decode(&request); err != nil {
		return nil, err
	}
	if !strings.Contains(request.Instructions, "mail-analysis") {
		return nil, errors.New("skill was not loaded")
	}
	input := agenttest.InputText(request.Input)
	c.inputs = append(c.inputs, input)
	text := c.reply(len(c.inputs), input)
	raw, _ := json.Marshal(agenttest.Response(text, len(c.inputs)))
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw))), Header: make(http.Header)}, nil
}

const validAnalysisOutput = `{"request":"数量改为20台","background":"[Sent Messages/41] 上次报价10台，每台USD 25","changes":"数量10台改为20台","actions":["核实新数量价格与交期"],"missing":[],"summary":"测试客户，20台阀门，确认新报价。"}`

const standaloneAnalysisOutput = `{"request":"分析当前邮件","background":"","changes":"","actions":[],"missing":[],"summary":"测试客户，20台阀门，确认新报价。"}`

func TestMailSummaryRewritesOverlongTextInsteadOfTruncating(t *testing.T) {
	s, account, message, key := setupMailSummaryTest(t)
	client := &analysisScriptClient{reply: func(turn int, input string) string {
		if turn == 1 {
			return strings.Replace(standaloneAnalysisOutput, "测试客户，20台阀门，确认新报价。", strings.Repeat("重要信息", 51), 1)
		}
		if !strings.Contains(input, "summary must be at most 200 Unicode characters") {
			t.Fatal("model did not receive the length correction")
		}
		return standaloneAnalysisOutput
	}}
	result, err := GetOrCreateMailSummary(context.Background(), client, s, key, account, message, false)
	if err != nil || result.Record.Summary != "测试客户，20台阀门，确认新报价。" || len(client.inputs) != 2 {
		t.Fatal(result, len(client.inputs), err)
	}
	if result.Record.Analysis.Summary != result.Record.Summary || len(result.Record.Analysis.Fields) != 0 {
		t.Fatal("summary was reformatted into columns", result.Record.Analysis)
	}
}

func TestMailAnalysisRepairsToolTranscriptAndKeepsEvidenceHonest(t *testing.T) {
	s, account, message, key := setupMailSummaryTest(t)
	message.Body = "Please update the quotation to 20 valves."
	message.Date = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	message.InReplyTo = "<prior@example.com>"
	parent := models.Email{ID: "41", Folder: "Sent Messages", From: account.Email, To: message.From, Date: message.Date.Add(-time.Hour), MessageID: message.InReplyTo, Body: "10 valves at USD 25", BodyCached: true}
	if err := s.UpsertMessages(context.Background(), account.ID, parent.Folder, []models.Email{parent}); err != nil {
		t.Fatal(err)
	}
	client := &analysisScriptClient{reply: func(turn int, input string) string {
		switch turn {
		case 1:
			return `[Agent Tool Call]: read_email({"folder":"Sent Messages","uid":"41"})`
		case 2:
			if !strings.Contains(input, "invalid JSON") {
				t.Fatal("missing format correction feedback")
			}
			// Citing a preview-only email is accepted; the server records what
			// was actually read instead of rejecting the answer.
			return validAnalysisOutput
		default:
			t.Fatal("unexpected generation")
			return ""
		}
	}}
	result, err := GetOrCreateMailSummary(context.Background(), client, s, key, account, message, false)
	if err != nil {
		t.Fatal(err)
	}
	analysis := result.Record.Analysis
	if len(client.inputs) != 2 || analysis == nil {
		t.Fatalf("repair exceeded budget: calls=%d analysis=%+v", len(client.inputs), analysis)
	}
	if len(analysis.Sources) != 1 || analysis.Sources[0].Folder != "INBOX" || analysis.Sources[0].UID != "42" {
		t.Fatalf("sources must reflect actual reads only: %+v", analysis.Sources)
	}
}

func TestMailAnalysisAllowsTriggeringMailCitationWithoutBody(t *testing.T) {
	s, account, message, key := setupMailSummaryTest(t)
	ctx := context.Background()
	message.Body, message.HTML, message.BodyCached = "", "", false
	output := `{"request":"询价维护套件","background":"[INBOX/42] 客户发送扫描件询价","changes":"","actions":["核实附件内容"],"missing":[],"summary":"客户发送扫描件采购单，待核实。"}`
	client := &analysisScriptClient{reply: func(int, string) string { return output }}
	result, err := GetOrCreateMailSummary(ctx, client, s, key, account, message, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Record.Analysis.Sources) != 0 {
		t.Fatalf("unreadable body must not become a source: %+v", result.Record.Analysis.Sources)
	}
	joined := strings.Join(result.Record.Analysis.Missing, "\n")
	if !strings.Contains(joined, "正文尚未同步") {
		t.Fatalf("missing body was not reported: %v", result.Record.Analysis.Missing)
	}
}

func TestMailAnalysisSearchesReadsAndPersistsEvidence(t *testing.T) {
	s, account, message, key := setupMailSummaryTest(t)
	ctx := context.Background()
	message.Date = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	message.MessageID = "<new@example.com>"
	message.InReplyTo = "<quote@example.com>"
	message.References = []string{message.InReplyTo}
	message.Body = "Please change quantity to 20, based on your last quotation."
	parent := models.Email{ID: "41", Folder: "Sent Messages", From: account.Email, To: message.From, Date: message.Date.Add(-time.Hour), MessageID: message.InReplyTo, Subject: "Valve RFQ", Body: "We quote 10 valves at USD 25 per unit.", BodyCached: true}
	future := parent
	future.ID = "43"
	future.Date = message.Date.Add(time.Minute)
	future.Body = "FUTURE-SECRET"
	future.MessageID = "<later@example.com>"
	for _, email := range []models.Email{message, parent, future} {
		if err := s.UpsertMessages(ctx, account.ID, email.Folder, []models.Email{email}); err != nil {
			t.Fatal(err)
		}
	}
	other := testAccount(t, s, account.OwnerID, "other@example.com", false)
	private := parent
	private.Body = "OTHER-ACCOUNT-SECRET"
	private.ID = "99"
	if err := s.UpsertMessages(ctx, other.ID, private.Folder, []models.Email{private}); err != nil {
		t.Fatal(err)
	}
	client := &analysisScriptClient{reply: func(turn int, input string) string {
		if strings.Contains(input, "FUTURE-SECRET") || strings.Contains(input, "OTHER-ACCOUNT-SECRET") {
			t.Fatal("evidence boundary violated")
		}
		switch turn {
		case 1:
			if !strings.Contains(input, message.Body) {
				t.Fatal("latest email missing")
			}
			return "```tool_call\n{\"name\":\"search_emails\",\"arguments\":{\"scope\":\"thread\",\"query\":\"\"}}\n```"
		case 2:
			if !strings.Contains(input, `"uid":"41"`) {
				t.Fatalf("parent not found: %s", input)
			}
			return `{"name":"read_email","arguments":{"folder":"Sent Messages","uid":"41"}}`
		case 3:
			if !strings.Contains(input, parent.Body) {
				t.Fatal("historical body not read")
			}
			return validAnalysisOutput
		default:
			t.Fatal("unexpected generation")
			return ""
		}
	}}
	result, err := GetOrCreateMailSummary(ctx, client, s, key, account, message, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Record.Analysis == nil || len(result.Record.Analysis.Sources) != 2 || len(result.Record.Analysis.Activity) != 2 {
		t.Fatalf("analysis not persisted: %+v", result.Record.Analysis)
	}
	if result.Record.Summary != "测试客户，20台阀门，确认新报价。" {
		t.Fatal(result.Record.Summary)
	}
	stored, err := s.GetMessageSummary(ctx, result.Record.MessageSummaryKey)
	if err != nil || stored.Analysis == nil || stored.Analysis.Background != result.Record.Analysis.Background {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	cached, err := GetOrCreateMailSummary(ctx, client, s, key, account, message, false)
	if err != nil || !cached.Cached || cached.Stale || len(client.inputs) != 3 {
		t.Fatalf("cached=%+v err=%v", cached, err)
	}
	parent.Body = "Corrected quotation: USD 30 per unit."
	if err := s.UpsertMessages(ctx, account.ID, parent.Folder, []models.Email{parent}); err != nil {
		t.Fatal(err)
	}
	cached, err = GetOrCreateMailSummary(ctx, client, s, key, account, message, false)
	if err != nil || !cached.Stale || len(client.inputs) != 3 {
		t.Fatalf("evidence change not detected: %+v %v", cached, err)
	}
}

func TestMailAnalysisPreservesForwardedInquiryInsideQuote(t *testing.T) {
	s, account, message, key := setupMailSummaryTest(t)
	message.Subject = "Fwd: RFQ"
	message.Body = "Please quote THERMO DYNAMICS STEAM TRAP TD42 1/2 inch, 1 UNIT.\n-- \nRayon Hew"
	message.HTML = `<div class="gmail_quote"><div>Please quote THERMO DYNAMICS STEAM TRAP TD42 1/2 inch, 1 UNIT.</div></div><div class="gmail_signature">Rayon Hew</div>`
	parts := partitionMailAnalysisContent(message)
	if !strings.Contains(parts.Quoted, "TD42") || strings.Contains(parts.NewText, "TD42") || !strings.Contains(parts.Signature, "Rayon") {
		t.Fatalf("partitions=%+v", parts)
	}
	client := &analysisScriptClient{reply: func(_ int, input string) string {
		if !strings.Contains(input, "TD42") || !strings.Contains(input, "quotedOrForwardedText") {
			t.Fatal("forwarded content lost before model")
		}
		return `{"request":"转发询价 TD42 1/2 inch，1 UNIT","background":"","changes":"","actions":["核实报价"],"missing":[],"summary":"Rayon Hew，TD42 1/2 inch，1 UNIT，报价。"}`
	}}
	if _, err := SummarizeMail(context.Background(), client, s, key, account, message); err != nil {
		t.Fatal(err)
	}
}

func TestMailAnalysisToolBoundariesAndPagination(t *testing.T) {
	s, account, message, _ := setupMailSummaryTest(t)
	ctx := context.Background()
	message.Date = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= 4; i++ {
		email := message
		email.ID = fmt.Sprint(i)
		email.Date = message.Date.Add(-time.Duration(i) * time.Hour)
		if err := s.UpsertMessages(ctx, account.ID, email.Folder, []models.Email{email}); err != nil {
			t.Fatal(err)
		}
	}
	future := message
	future.ID = "99"
	future.Date = message.Date.Add(time.Hour)
	if err := s.UpsertMessages(ctx, account.ID, future.Folder, []models.Email{future}); err != nil {
		t.Fatal(err)
	}
	scope, err := newMailAnalysisScope(ctx, s, account, message)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := scope.execute(ctx, agentruntime.ToolCall{Name: "generate_document"}); err == nil {
		t.Fatal("write tool was permitted")
	}
	if _, err := scope.reader.Read(ctx, "INBOX", "99"); err == nil {
		t.Fatal("future mail was readable")
	}
	sameSecond := message
	sameSecond.ID = "100"
	if err := s.UpsertMessages(ctx, account.ID, sameSecond.Folder, []models.Email{sameSecond}); err != nil {
		t.Fatal(err)
	}
	if _, err := scope.reader.Read(ctx, "INBOX", "100"); err == nil {
		t.Fatal("later UID was readable")
	}
	page, err := scope.reader.Search(ctx, "mailbox", "", "", 2, nil)
	if err != nil || !page.HasMore {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	seen := map[string]bool{}
	for {
		for _, email := range page.Messages {
			if email.ID == "99" || email.ID == "100" || seen[email.ID] {
				t.Fatalf("bad page: %+v", page)
			}
			seen[email.ID] = true
		}
		if !page.HasMore {
			break
		}
		page, err = scope.reader.Search(ctx, "mailbox", "", page.NextCursor, 2, nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 5 {
		t.Fatalf("missing evidence: %v", seen)
	}
}

func TestMailAnalysisRejectsUnreadTriggerAndInvalidFields(t *testing.T) {
	s, account, message, key := setupMailSummaryTest(t)
	message.Body = strings.Repeat("Long content ", 1800) + "IMPORTANT-END"
	client := &analysisScriptClient{reply: func(_ int, _ string) string { return validAnalysisOutput }}
	if _, err := SummarizeMail(context.Background(), client, s, key, account, message); err == nil || !strings.Contains(err.Error(), "finish reading") {
		t.Fatalf("unread latest mail accepted: %v", err)
	}
	message.Body = "short mail"
	for _, output := range []string{`{"request":"ok","summary":"one。"}`, validAnalysisOutput + ` {"extra":true}`, strings.Replace(validAnalysisOutput, `"changes":`, `"unknown":`, 1)} {
		client := &analysisScriptClient{reply: func(_ int, _ string) string { return output }}
		if _, err := SummarizeMail(context.Background(), client, s, key, account, message); err == nil {
			t.Fatalf("invalid output accepted: %s", output)
		}
	}
}

func TestMailAnalysisReadsRemainingTriggerSegments(t *testing.T) {
	s, account, message, key := setupMailSummaryTest(t)
	message.Body = strings.Repeat("Long content ", 1300) + "IMPORTANT-END"
	if err := s.UpsertMessages(context.Background(), account.ID, message.Folder, []models.Email{message}); err != nil {
		t.Fatal(err)
	}
	client := &analysisScriptClient{reply: func(turn int, input string) string {
		if turn == 1 {
			return `{"name":"read_email","arguments":{"folder":"INBOX","uid":"42","offset":16000,"length":4000}}`
		}
		if !strings.Contains(input, "IMPORTANT-END") {
			t.Fatal("remaining current email was not supplied")
		}
		return standaloneAnalysisOutput
	}}
	result, err := GetOrCreateMailSummary(context.Background(), client, s, key, account, message, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Record.Analysis == nil || !sourceFullyRead(result.Record.Analysis.Sources[0]) {
		t.Fatalf("coverage not saved: %+v", result.Record.Analysis)
	}
}

func TestMailAnalysisMigrationKeepsExistingSummary(t *testing.T) {
	s, account, message, _ := setupMailSummaryTest(t)
	ctx := context.Background()
	claim := MessageSummaryRecord{MessageSummaryKey: MessageSummaryKey{AccountID: account.ID, FolderName: message.Folder, UID: message.ID}}
	record, acquired, err := s.ClaimMessageSummaryGeneration(ctx, claim, false, time.Minute)
	if err != nil || !acquired {
		t.Fatalf("claim: %v", err)
	}
	if _, err := s.CompleteMessageSummaryGeneration(ctx, claim, record.GenerationToken, "legacy summary"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`ALTER TABLE message_summaries DROP COLUMN analysis_json`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.ensureMessageAnalysisColumn(ctx); err != nil {
			t.Fatal(err)
		}
	}
	record, err = s.GetMessageSummary(ctx, claim.MessageSummaryKey)
	if err != nil || record.Summary != "legacy summary" || record.Analysis != nil {
		t.Fatalf("migration lost legacy data: %+v %v", record, err)
	}
}

func TestMailAnalysisAcceptsAnswerWithoutAnyHistoryRead(t *testing.T) {
	s, account, message, key := setupMailSummaryTest(t)
	// The durable record keeps the server-assigned sources even when the model
	// answers directly without historical evidence.
	client := &analysisScriptClient{reply: func(_ int, _ string) string { return standaloneAnalysisOutput }}
	result, err := SummarizeMail(context.Background(), client, s, key, account, message)
	if err != nil {
		t.Fatalf("answer without history citations rejected: %v", err)
	}
	if strings.TrimSpace(result) == "" {
		t.Fatal("summary was empty")
	}
}

func TestMailEvidenceHashNormalizesTimeZone(t *testing.T) {
	message := models.Email{Body: "same facts", Date: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}
	same := message
	same.Date = message.Date.In(time.FixedZone("local", 8*60*60))
	if mailEvidenceHash(message) != mailEvidenceHash(same) {
		t.Fatal("time zone changed the evidence fingerprint")
	}
}

type cancelledAnalysisClient struct{ entered chan struct{} }

func (c *cancelledAnalysisClient) Do(req *http.Request) (*http.Response, error) {
	close(c.entered)
	<-req.Context().Done()
	return nil, req.Context().Err()
}

func TestMailAnalysisCancellationPersistsFailure(t *testing.T) {
	s, account, message, key := setupMailSummaryTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &cancelledAnalysisClient{entered: make(chan struct{})}
	done := make(chan error, 1)
	go func() { _, err := GetOrCreateMailSummary(ctx, client, s, key, account, message, false); done <- err }()
	select {
	case <-client.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("analysis did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cancellation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("analysis did not stop")
	}
	record, err := s.GetMessageSummary(context.Background(), MessageSummaryKey{AccountID: account.ID, FolderName: message.Folder, UID: message.ID})
	if err != nil || record.Status != "failed" || record.GenerationToken != "" {
		t.Fatalf("failure not persisted: %+v %v", record, err)
	}
}
