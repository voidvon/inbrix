package i18n

import (
	"testing"
	"time"
)

func TestDetectPrefersCookieAndRecognizesChineseTags(t *testing.T) {
	if got := Detect("zh-CN", "en-US,en;q=0.9"); got != LocaleZhCN {
		t.Fatalf("cookie locale = %q, want %q", got, LocaleZhCN)
	}
	if got := Detect("", "zh-CN,en;q=0.9"); got != LocaleZhCN {
		t.Fatalf("Accept-Language locale = %q, want %q", got, LocaleZhCN)
	}
	if got := Detect("", "en-US,en;q=0.9"); got != LocaleEnglish {
		t.Fatalf("English locale = %q, want %q", got, LocaleEnglish)
	}
	if got := Detect("", "en-US,zh-CN;q=0.8"); got != LocaleEnglish {
		t.Fatalf("weighted English locale = %q, want %q", got, LocaleEnglish)
	}
	if got := Detect("", "en-US;q=0,zh-CN;q=0.9"); got != LocaleZhCN {
		t.Fatalf("weighted Chinese locale = %q, want %q", got, LocaleZhCN)
	}
}

func TestChineseFormatting(t *testing.T) {
	value := time.Date(2026, time.August, 11, 9, 5, 0, 0, time.Local)
	if got := FormatDate(LocaleZhCN, value); got != "2026年8月11日 09:05" {
		t.Fatalf("date = %q", got)
	}
	if got := FormatMonthTitle(LocaleZhCN, 8, 2026); got != "2026年8月" {
		t.Fatalf("month = %q", got)
	}
	if got := WeekdayName(LocaleZhCN, time.Tuesday); got != "星期二" {
		t.Fatalf("weekday = %q", got)
	}
	if got := WeekdayName(LocaleZhCN, 99); got != "星期一" {
		t.Fatalf("invalid weekday = %q", got)
	}
}

func TestTranslateFallsBackToSourceKey(t *testing.T) {
	if got := Translate(LocaleZhCN, "unknown source key"); got != "unknown source key" {
		t.Fatalf("missing translation = %q", got)
	}
	if got := Translate(LocaleZhCN, "Back to inbox"); got != "返回收件箱" {
		t.Fatalf("known translation = %q", got)
	}
}

func TestFolderLabelRecognizesQQSentMessages(t *testing.T) {
	if got := FolderLabel(LocaleZhCN, "Sent Messages"); got != "已发送" {
		t.Fatalf("QQ sent folder label = %q, want %q", got, "已发送")
	}
}
