package provider

import "testing"

func TestLookupDriverSpecReturnsBuiltins(t *testing.T) {
	t.Parallel()

	imap, ok := LookupDriverSpec("imap")
	if !ok {
		t.Fatalf("LookupDriverSpec(imap) = missing")
	}
	if imap.Source == nil || imap.Sink == nil {
		t.Fatalf("imap spec = %+v, want source and sink support", imap)
	}
	if imap.Source.DeleteSemantics != DeleteSemanticsHard {
		t.Fatalf("imap delete semantics = %q, want hard", imap.Source.DeleteSemantics)
	}
	if !imap.Auth.IMAP || imap.Source.ProbeKind != ProviderProbeFolderList {
		t.Fatalf("imap auth/probe = %+v / %q", imap.Auth, imap.Source.ProbeKind)
	}

	graph, ok := LookupDriverSpec("graph")
	if !ok {
		t.Fatalf("LookupDriverSpec(graph) = missing")
	}
	if graph.Source == nil || graph.Source.DeleteSemantics != DeleteSemanticsSoft {
		t.Fatalf("graph source spec = %+v, want soft delete source", graph.Source)
	}
	if !graph.Auth.Graph || graph.Source.ProbeKind != ProviderProbeIdentity {
		t.Fatalf("graph auth/probe = %+v / %q", graph.Auth, graph.Source.ProbeKind)
	}

	file, ok := LookupDriverSpec("file")
	if !ok {
		t.Fatalf("LookupDriverSpec(file) = missing")
	}
	if file.Source != nil || file.Sink == nil || !file.Sink.LocalConsumer || file.Sink.LocalConsumerKind != LocalConsumerFile {
		t.Fatalf("file spec = %+v, want local sink only", file)
	}
}

func TestSourceSpecModeSpecNormalizesMode(t *testing.T) {
	t.Parallel()

	spec, ok := LookupSourceSpec("imap")
	if !ok {
		t.Fatalf("LookupSourceSpec(imap) = missing")
	}

	mode, ok := spec.ModeSpec("POLL")
	if !ok {
		t.Fatalf("ModeSpec(POLL) = missing")
	}
	if !mode.RequiresStatePath || !mode.RequiresPollInterval || !mode.RequiresCycleTimeout {
		t.Fatalf("mode spec = %+v, want polling requirements", mode)
	}
}
