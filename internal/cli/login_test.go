package cli

import (
	"reflect"
	"testing"

	"mimecrypt/internal/appruntime"
)

func TestLoginDriversForConfigUsesBindingsWhenPresent(t *testing.T) {
	t.Parallel()

	plan := appruntime.CredentialPlan{
		AuthDrivers: []string{"imap", "graph"},
		Bindings: []appruntime.CredentialBinding{
			{Kind: appruntime.CredentialBindingSource, Name: "inbox", Driver: "imap"},
		},
	}

	got := loginDriversForConfig(plan)
	if !reflect.DeepEqual(got, []string{"imap", "graph"}) {
		t.Fatalf("loginDriversForConfig() = %#v, want [imap graph]", got)
	}
}

func TestLoginDriversForConfigAllowsInteractiveReconfigureWithoutBindings(t *testing.T) {
	t.Parallel()

	plan := appruntime.CredentialPlan{
		AuthDrivers: []string{"imap"},
	}

	if got := loginDriversForConfig(plan); got != nil {
		t.Fatalf("loginDriversForConfig() = %#v, want nil", got)
	}
}
