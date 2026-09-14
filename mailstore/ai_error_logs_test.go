package mailstore

import (
	"context"
	"testing"
)

func TestAIErrorLogsRecordListAndClear(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	err := s.RecordAIError(ctx, AIErrorLogRecord{
		OwnerID:      "alice",
		TaskType:     MailSummaryTask,
		AccountEmail: "alice@example.com",
		ModelName:    "gpt-5.6-sol",
		AgentName:    "默认分析",
		ErrorMessage: "OpenAI returned HTTP 502: Bad gateway",
	})
	if err != nil {
		t.Fatalf("RecordAIError: %v", err)
	}

	err = s.RecordAIError(ctx, AIErrorLogRecord{
		OwnerID:      "alice",
		TaskType:     EmailDraftTask,
		AccountEmail: "alice@example.com",
		ModelName:    "deepseek-chat",
		AgentName:    "草稿助手",
		ErrorMessage: "context deadline exceeded",
	})
	if err != nil {
		t.Fatalf("RecordAIError second: %v", err)
	}

	// Bob should have no logs
	bobLogs, err := s.ListAIErrorLogs(ctx, "bob", 100)
	if err != nil {
		t.Fatalf("ListAIErrorLogs bob: %v", err)
	}
	if len(bobLogs) != 0 {
		t.Fatalf("expected 0 logs for bob, got %d", len(bobLogs))
	}

	// Alice should have 2 logs, newest first
	aliceLogs, err := s.ListAIErrorLogs(ctx, "alice", 100)
	if err != nil {
		t.Fatalf("ListAIErrorLogs alice: %v", err)
	}
	if len(aliceLogs) != 2 {
		t.Fatalf("expected 2 logs for alice, got %d", len(aliceLogs))
	}
	if aliceLogs[0].TaskType != EmailDraftTask {
		t.Fatalf("expected newest log to be draft, got %s", aliceLogs[0].TaskType)
	}
	if aliceLogs[1].TaskType != MailSummaryTask {
		t.Fatalf("expected second log to be summary, got %s", aliceLogs[1].TaskType)
	}

	// Clear logs
	if err := s.ClearAIErrorLogs(ctx, "alice"); err != nil {
		t.Fatalf("ClearAIErrorLogs: %v", err)
	}

	clearedLogs, err := s.ListAIErrorLogs(ctx, "alice", 100)
	if err != nil {
		t.Fatalf("ListAIErrorLogs after clear: %v", err)
	}
	if len(clearedLogs) != 0 {
		t.Fatalf("expected 0 logs after clear, got %d", len(clearedLogs))
	}
}
