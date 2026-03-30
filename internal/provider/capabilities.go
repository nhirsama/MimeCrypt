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
	LocalConsumerBackup  LocalConsumerKind = "backup"
	LocalConsumerFile    LocalConsumerKind = "file"
)

type SourceModeSpec struct {
	RequiresStatePath    bool
	RequiresPollInterval bool
	RequiresCycleTimeout bool
}

type SourceModeCapabilities = SourceModeSpec

type SourceCapabilities struct {
	RequiresCredential bool
	SupportsDelete     bool
	DeleteSemantics    DeleteSemantics
	ProbeKind          ProviderProbeKind
	Modes              map[string]SourceModeSpec
}

func (s SourceCapabilities) ModeSpec(mode string) (SourceModeSpec, bool) {
	if s.Modes == nil {
		return SourceModeSpec{}, false
	}
	modeSpec, ok := s.Modes[strings.ToLower(strings.TrimSpace(mode))]
	return modeSpec, ok
}

type SourceSpec = SourceCapabilities

type SinkCapabilities struct {
	RequiresCredential bool
	RequiresOutputDir  bool
	SupportsVerify     bool
	SupportsReconcile  bool
	SupportsHealth     bool
	LocalConsumer      bool
	LocalConsumerKind  LocalConsumerKind
}

type SinkSpec = SinkCapabilities

type DriverInfo struct {
	Name   string
	Auth   AuthRequirement
	Source *SourceCapabilities
	Sink   *SinkCapabilities
}

type DriverSpec = DriverInfo
