package mailflow

import "testing"

func TestStoreRefEqualIgnoresMailbox(t *testing.T) {
	t.Parallel()

	left := StoreRef{
		Driver:  "imap",
		Account: "user@example.com",
		Mailbox: "INBOX",
	}
	right := StoreRef{
		Driver:  "imap",
		Account: "user@example.com",
		Mailbox: "Archive",
	}

	if !left.Equal(right) {
		t.Fatalf("Equal() = false, want true")
	}
}

func TestStoreRefEqualRequiresDriverAndAccountMatch(t *testing.T) {
	t.Parallel()

	base := StoreRef{
		Driver:  "imap",
		Account: "user@example.com",
	}
	if base.Equal(StoreRef{Driver: "graph", Account: "user@example.com"}) {
		t.Fatalf("Equal() = true with different driver, want false")
	}
	if base.Equal(StoreRef{Driver: "imap", Account: "other@example.com"}) {
		t.Fatalf("Equal() = true with different account, want false")
	}
}
