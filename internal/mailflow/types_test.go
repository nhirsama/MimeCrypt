package mailflow

import (
	"strings"
	"testing"
)

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

func TestExecutionPlanValidateRequiresRequiredTarget(t *testing.T) {
	t.Parallel()

	err := (ExecutionPlan{
		Targets: []DeliveryTarget{{
			Name:     "archive-main",
			Consumer: "archive",
			Artifact: "primary",
		}},
	}).Validate()
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("Validate() error = %v, want required target error", err)
	}
}
