package provider

import "strings"

type AuthRequirement struct {
	Graph bool
	EWS   bool
	IMAP  bool
}

type ProviderProbeKind string

const (
	ProviderProbeIdentity   ProviderProbeKind = "identity"
	ProviderProbeFolderList ProviderProbeKind = "folder-list"
)

type LocalConsumerKind string

const (
	LocalConsumerNone    LocalConsumerKind = ""
	LocalConsumerDiscard LocalConsumerKind = "discard"
	LocalConsumerFile    LocalConsumerKind = "file"
)

type SourceModeSpec struct {
	RequiresStatePath    bool
	RequiresPollInterval bool
	RequiresCycleTimeout bool
}

type SourceSpec struct {
	RequiresCredential bool
	SupportsDelete     bool
	DeleteSemantics    DeleteSemantics
	ProbeKind          ProviderProbeKind
	Modes              map[string]SourceModeSpec
}

func (s *SourceSpec) ModeSpec(mode string) (SourceModeSpec, bool) {
	if s == nil {
		return SourceModeSpec{}, false
	}
	spec, ok := s.Modes[normalizeDriverName(mode)]
	return spec, ok
}

type SinkSpec struct {
	RequiresCredential bool
	RequiresOutputDir  bool
	SupportsVerify     bool
	SupportsReconcile  bool
	SupportsHealth     bool
	LocalConsumer      bool
	LocalConsumerKind  LocalConsumerKind
}

type DriverSpec struct {
	Name   string
	Auth   AuthRequirement
	Source *SourceSpec
	Sink   *SinkSpec
}

var builtinDriverSpecs = map[string]DriverSpec{
	"discard": {
		Name: "discard",
		Sink: &SinkSpec{
			LocalConsumer:     true,
			LocalConsumerKind: LocalConsumerDiscard,
		},
	},
	"ews": {
		Name: "ews",
		Auth: AuthRequirement{
			Graph: true,
			EWS:   true,
		},
		Sink: &SinkSpec{
			RequiresCredential: true,
			SupportsVerify:     true,
			SupportsReconcile:  true,
			SupportsHealth:     true,
		},
	},
	"file": {
		Name: "file",
		Sink: &SinkSpec{
			RequiresOutputDir: true,
			LocalConsumer:     true,
			LocalConsumerKind: LocalConsumerFile,
		},
	},
	"graph": {
		Name: "graph",
		Auth: AuthRequirement{
			Graph: true,
		},
		Source: &SourceSpec{
			RequiresCredential: true,
			SupportsDelete:     true,
			DeleteSemantics:    DeleteSemanticsSoft,
			ProbeKind:          ProviderProbeIdentity,
			Modes: map[string]SourceModeSpec{
				"poll": {
					RequiresStatePath:    true,
					RequiresPollInterval: true,
					RequiresCycleTimeout: true,
				},
			},
		},
		Sink: &SinkSpec{
			RequiresCredential: true,
			SupportsVerify:     true,
			SupportsReconcile:  true,
			SupportsHealth:     true,
		},
	},
	"imap": {
		Name: "imap",
		Auth: AuthRequirement{
			IMAP: true,
		},
		Source: &SourceSpec{
			RequiresCredential: true,
			SupportsDelete:     true,
			DeleteSemantics:    DeleteSemanticsHard,
			ProbeKind:          ProviderProbeFolderList,
			Modes: map[string]SourceModeSpec{
				"poll": {
					RequiresStatePath:    true,
					RequiresPollInterval: true,
					RequiresCycleTimeout: true,
				},
			},
		},
		Sink: &SinkSpec{
			RequiresCredential: true,
			SupportsVerify:     true,
			SupportsReconcile:  true,
			SupportsHealth:     true,
		},
	},
}

func LookupDriverSpec(driver string) (DriverSpec, bool) {
	spec, ok := builtinDriverSpecs[normalizeDriverName(driver)]
	return spec, ok
}

func LookupSourceSpec(driver string) (*SourceSpec, bool) {
	spec, ok := LookupDriverSpec(driver)
	if !ok || spec.Source == nil {
		return nil, false
	}
	return spec.Source, true
}

func LookupSinkSpec(driver string) (*SinkSpec, bool) {
	spec, ok := LookupDriverSpec(driver)
	if !ok || spec.Sink == nil {
		return nil, false
	}
	return spec.Sink, true
}

func normalizeDriverName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
