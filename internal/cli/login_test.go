package cli

import (
	"reflect"
	"testing"

	"mimecrypt/internal/appconfig"
	"mimecrypt/internal/appruntime"
)

func TestCredentialPlanSuggestedAuthHintsReturnsStoredProfileHints(t *testing.T) {
	t.Parallel()

	plan := appruntime.CredentialPlan{
		LocalConfig: appconfig.LocalConfig{AuthProfile: "graph+imap"},
		Bindings: []appruntime.CredentialBinding{
			{Kind: appruntime.CredentialBindingSource, Name: "inbox", Driver: "imap"},
		},
		BindingDrivers: []string{"imap"},
	}

	got := plan.SuggestedAuthHints()
	if !reflect.DeepEqual(got, []string{"graph", "imap"}) {
		t.Fatalf("SuggestedAuthHints() = %#v, want [graph imap]", got)
	}
}

func TestCredentialPlanSuggestedAuthHintsTranslatesBindingDriversToAuthHints(t *testing.T) {
	t.Parallel()

	plan := appruntime.CredentialPlan{
		BindingDrivers: []string{"ews"},
	}

	if got := plan.SuggestedAuthHints(); !reflect.DeepEqual(got, []string{"graph", "ews"}) {
		t.Fatalf("SuggestedAuthHints() = %#v, want [graph ews]", got)
	}
}

func TestCredentialPlanSuggestedAuthHintsReturnsNilWithoutProfileOrBindings(t *testing.T) {
	t.Parallel()

	plan := appruntime.CredentialPlan{}

	if got := plan.SuggestedAuthHints(); got != nil {
		t.Fatalf("SuggestedAuthHints() = %#v, want nil", got)
	}
}
