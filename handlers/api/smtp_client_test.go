package api

import "testing"

func TestSMTPClientAuthenticationUsername(t *testing.T) {
	client := NewSMTPClient("smtp.example.com", 465, "alice@example.com", "secret", false)
	if got := client.Transport().AuthUsername; got != "alice@example.com" {
		t.Fatalf("default auth username = %q, want full email address", got)
	}

	client.SetAuthUsername(" alice ")
	if got := client.Transport().AuthUsername; got != "alice" {
		t.Fatalf("overridden auth username = %q, want %q", got, "alice")
	}

	client.SetAuthUsername("  ")
	if got := client.Transport().AuthUsername; got != "alice" {
		t.Fatalf("blank override changed auth username to %q", got)
	}
}
