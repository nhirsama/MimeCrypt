package appconfig

import "strings"

const (
	CredentialKindOAuth         = "oauth"
	CredentialKindSharedSession = "shared-session"
)

type CredentialKindSpec struct {
	RequiresRemoteRevoke bool
}

var builtinCredentialKindSpecs = map[string]CredentialKindSpec{
	CredentialKindOAuth: {
		RequiresRemoteRevoke: true,
	},
	CredentialKindSharedSession: {
		RequiresRemoteRevoke: false,
	},
}

func NormalizeCredentialKind(kind string) string {
	return strings.ToLower(strings.TrimSpace(kind))
}

func LookupCredentialKindSpec(kind string) (CredentialKindSpec, bool) {
	spec, ok := builtinCredentialKindSpecs[NormalizeCredentialKind(kind)]
	return spec, ok
}
