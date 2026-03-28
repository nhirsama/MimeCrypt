package imap

import "testing"

func TestEncodeMailboxNameKeepsInboxStable(t *testing.T) {
	t.Parallel()

	if got := encodeMailboxName(" inbox "); got != "INBOX" {
		t.Fatalf("encodeMailboxName() = %q, want INBOX", got)
	}
}

func TestEncodeMailboxNameEncodesAmpersand(t *testing.T) {
	t.Parallel()

	if got := encodeMailboxName("A&B"); got != "A&-B" {
		t.Fatalf("encodeMailboxName() = %q, want A&-B", got)
	}
}

func TestEncodeMailboxNameUsesModifiedUTF7(t *testing.T) {
	t.Parallel()

	if got := encodeMailboxName("台北"); got != "&U,BTFw-" {
		t.Fatalf("encodeMailboxName() = %q, want &U,BTFw-", got)
	}
}
