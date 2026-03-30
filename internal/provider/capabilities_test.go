package provider_test

import (
	"testing"

	"mimecrypt/internal/provider"
	"mimecrypt/internal/providers"
)

func TestSourceSpecModeSpecNormalizesMode(t *testing.T) {
	t.Parallel()

	spec := provider.SourceSpec{
		Modes: map[string]provider.SourceModeSpec{
			"poll": {
				RequiresStatePath:    true,
				RequiresPollInterval: true,
				RequiresCycleTimeout: true,
			},
		},
	}

	mode, ok := spec.ModeSpec("POLL")
	if !ok {
		t.Fatalf("ModeSpec(POLL) = missing")
	}
	if !mode.RequiresStatePath || !mode.RequiresPollInterval || !mode.RequiresCycleTimeout {
		t.Fatalf("mode spec = %+v, want polling requirements", mode)
	}
}

func TestBuiltinDriverSpecsFromRegistry(t *testing.T) {
	t.Parallel()

	imap, ok := providers.LookupDriverSpec("imap")
	if !ok {
		t.Fatalf("LookupDriverSpec(imap) = missing")
	}
	if imap.Source == nil || imap.Sink == nil {
		t.Fatalf("imap spec = %+v, want source and sink support", imap)
	}
	if imap.Source.DeleteSemantics != provider.DeleteSemanticsHard {
		t.Fatalf("imap delete semantics = %q, want hard", imap.Source.DeleteSemantics)
	}
	if !imap.Auth.IMAP || imap.Source.ProbeKind != provider.ProviderProbeFolderList {
		t.Fatalf("imap auth/probe = %+v / %q", imap.Auth, imap.Source.ProbeKind)
	}

	webhook, ok := providers.LookupDriverSpec("webhook")
	if !ok {
		t.Fatalf("LookupDriverSpec(webhook) = missing")
	}
	if webhook.Sink != nil {
		t.Fatalf("webhook sink spec = %+v, want nil", webhook.Sink)
	}
	if webhook.Source == nil {
		t.Fatalf("webhook source spec = nil")
	}
	if _, ok := webhook.Source.ModeSpec("push"); !ok {
		t.Fatalf("webhook ModeSpec(push) = missing")
	}
}
