package imap

import "testing"

func TestNormalizeMailboxKeepsInboxStable(t *testing.T) {
	t.Parallel()

	if got := normalizeMailbox(" inbox "); got != "INBOX" {
		t.Fatalf("normalizeMailbox() = %q, want INBOX", got)
	}
}

func TestNormalizeMailboxPreservesCustomFolder(t *testing.T) {
	t.Parallel()

	if got := normalizeMailbox(" Archive/Sub "); got != "Archive/Sub" {
		t.Fatalf("normalizeMailbox() = %q, want Archive/Sub", got)
	}
}
