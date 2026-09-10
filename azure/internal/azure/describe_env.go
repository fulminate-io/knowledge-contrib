// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"slices"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// describe_env.go — WHAT AN INSTALLER MUST DO WITH EVERY ENVIRONMENT NAME THIS
// COLLECTOR READS, and the describe tool's environment declaration built from
// it.
//
// THE DECISION LIVES HERE, BESIDE THE COLLECTOR THAT READS THE NAME. It used to
// live in the installer as a hand-written per-collector table and in a
// disposition file beside it; both are now DERIVED from this map, so a variable
// this collector starts reading is one decision in one place rather than three
// edits in three repositories' worth of files. The disposition file remains the
// frozen review record, and the environment census pins this map against it.
//
// THE FOUR DISPOSITIONS ARE THE CONTRACT'S OWN, and each says what an installed
// entry carries: a path literal, a selector literal when set, a secret in NO
// state whatsoever, and not-carried for a name this collector reads that an
// installed entry deliberately does not declare. Declaring the last kind is the
// point: an operator whose variable is absent can tell a decision from an
// oversight.
//
// THE MODULE'S OWN EXCLUSION TABLE IS NOT REPRESENTED HERE, and its absence is
// the point rather than an omission. This module publishes a table of names it
// records as DELIBERATELY NOT DECLARED — a name a dependency's documentation
// mentions that this collector's own code path never reads. Those names are not
// this collector's to describe at all: declaring them, even as not-carried,
// would put a name in the operator's entry that the module's own source says it
// does not read, and the installer suite asserts they reach no entry.
// EnvDispositions is that decision, one row per name.
var EnvDispositions = map[string]string{
	"AZURE_ADDITIONALLY_ALLOWED_TENANTS":  framework.EnvClassSelector,
	"AZURE_AUTHORITY_HOST":                framework.EnvClassSelector,
	"AZURE_CLIENT_CERTIFICATE_PASSWORD":   framework.EnvClassSecret,
	"AZURE_CLIENT_CERTIFICATE_PATH":       framework.EnvClassSelector,
	"AZURE_CLIENT_ID":                     framework.EnvClassSelector,
	"AZURE_CLIENT_SECRET":                 framework.EnvClassSecret,
	"AZURE_CLIENT_SEND_CERTIFICATE_CHAIN": framework.EnvClassSelector,
	"AZURE_FEDERATED_TOKEN_FILE":          framework.EnvClassSelector,
	"AZURE_PASSWORD":                      framework.EnvClassSecret,
	"AZURE_POD_IDENTITY_AUTHORITY_HOST":   framework.EnvClassSelector,
	"AZURE_REGIONAL_AUTHORITY_NAME":       framework.EnvClassSelector,
	"AZURE_SDK_GO_LOGGING":                framework.EnvClassSelector,
	"AZURE_SUBSCRIPTION_ID":               framework.EnvClassSelector,
	"AZURE_TENANT_ID":                     framework.EnvClassSelector,
	"AZURE_TOKEN_CREDENTIALS":             framework.EnvClassSelector,
	"AZURE_USERNAME":                      framework.EnvClassSecret,
	"DEFAULT_IDENTITY_CLIENT_ID":          framework.EnvClassSelector,
	"HOME":                                framework.EnvClassPath,
	"HTTPS_PROXY":                         framework.EnvClassSelector,
	"HTTP_PROXY":                          framework.EnvClassSelector,
	"IDENTITY_ENDPOINT":                   framework.EnvClassSelector,
	"IDENTITY_HEADER":                     framework.EnvClassSecret,
	"IDENTITY_SERVER_THUMBPRINT":          framework.EnvClassSelector,
	"IMDS_ENDPOINT":                       framework.EnvClassSelector,
	"MSAL_FORCE_REGION":                   framework.EnvClassSelector,
	"MSI_ENDPOINT":                        framework.EnvClassSelector,
	"MSI_SECRET":                          framework.EnvClassSecret,
	"NO_PROXY":                            framework.EnvClassSelector,
	"PATH":                                framework.EnvClassPath,
	"REGION_NAME":                         framework.EnvClassSelector,
	"SSL_CERT_DIR":                        framework.EnvClassSelector,
	"SSL_CERT_FILE":                       framework.EnvClassSelector,
	"SYSTEMROOT":                          framework.EnvClassNotCarried,
	"http_proxy":                          framework.EnvClassSelector,
	"https_proxy":                         framework.EnvClassSelector,
	"no_proxy":                            framework.EnvClassSelector,
}

// EmptySensitiveNames are the names this collector tells PRESENT AND EMPTY apart
// from ABSENT. There is exactly one, and it is not one this module reads itself.
//
// AZURE_TOKEN_CREDENTIALS IS REFUSED BY azidentity AT CONSTRUCTION. The name
// selects which arms of the default credential chain are tried, and a value
// outside its vocabulary — the empty string included — fails
// NewDefaultAzureCredential before any token request, so a collect that carries
// it present and empty never starts. Absent means the full chain, which is the
// baseline every install expects.
//
// WHY THAT MAKES IT UNSAFE IN A WORKED ENTRY. A `${NAME:-}` reference resolves to
// the empty string in the process serving the collect, so an operator copying such
// an entry gets a collector whose credential chain will not construct. The
// installer's documentation gate refuses the defaulted form for a marked name and
// admits it for the rest, and this is where it reads the answer.
//
// THE MODULE'S OWN READ IS UNMARKED AND MEASURED SO. AZURE_SUBSCRIPTION_ID is
// resolved with an `ok && v != ""` guard, so present-and-empty takes the same arm
// as absent. empty_sensitive_test.go drives every declared name for the LINUX
// target through the chain's constructor, and drives the subscription read
// separately; it fails if this set and the measured one disagree in either
// direction.
var EmptySensitiveNames = map[string]bool{
	"AZURE_TOKEN_CREDENTIALS": true,
}

// DescribedEnvironment renders the describe tool's environment declaration: every
// name above, sorted, each with its disposition and its empty-sensitivity.
//
// SORTED so one unchanged collector renders one byte-identical declaration, and
// so the installer table generated from it does not reorder itself between runs.
func DescribedEnvironment() []framework.EnvDeclaration {
	names := make([]string, 0, len(EnvDispositions))
	for name := range EnvDispositions {
		names = append(names, name)
	}
	slices.Sort(names)
	out := make([]framework.EnvDeclaration, 0, len(names))
	for _, name := range names {
		out = append(out, framework.EnvDeclaration{
			Name:           name,
			Class:          EnvDispositions[name],
			EmptySensitive: EmptySensitiveNames[name],
		})
	}
	return out
}
