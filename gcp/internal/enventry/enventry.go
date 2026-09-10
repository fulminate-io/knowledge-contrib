// SPDX-License-Identifier: Apache-2.0

// Package enventry computes the ENVIRONMENT BLOCK an operator writes into this
// collector's config entry, and renders the worked example the guide shows.
//
// WHY THIS IS CODE AND NOT PROSE. A stdio collector receives EXACTLY the
// environment its config entry declares: the daemon copies nothing and adds
// nothing, so a name the credential chain reads and the entry omits is a name
// the child does not have. The consequences of omitting one are not uniform —
// some fail loudly at the first call, one fails silently by taking a default the
// operator meant to override, and two fail only on a host whose certificate
// bundle is somewhere unusual — so the set is derived here, tested, and rendered
// into the documentation rather than retyped into it.
//
// THE SET IS A FUNCTION OF THE TARGET OPERATING SYSTEM, not of the running one.
// Exactly one name differs: the credential chain looks for the logged-in
// account's file under a different variable on Windows than everywhere else.
// Taking the target as an ARGUMENT rather than reading the running platform is
// what makes both cases testable: no runner in this repository runs tests on
// Windows, so a branch on the running platform would be a branch no test could
// ever enter.
package enventry

import (
	"fmt"
	"slices"
	"strings"
)

// GOOSWindows is the target-OS argument that selects the Windows case.
const GOOSWindows = "windows"

// windowsHomeVariable and unixHomeVariable name the variable the credential
// chain joins the logged-in account's well-known file path onto. It is the ONE
// name that differs between the two cases.
const (
	windowsHomeVariable = "APPDATA"
	unixHomeVariable    = "HOME"
)

// commonNames are read on every target. They fall into four groups, and the
// grouping is the reason each is here rather than a preference.
var commonNames = []string{
	// The credential chain's own discovery names.
	"GOOGLE_APPLICATION_CREDENTIALS",
	"GCE_METADATA_HOST",
	"GOOGLE_CLOUD_PROJECT",
	"GOOGLE_CLOUD_QUOTA_PROJECT",

	// The auth stack's own switches. Two of them are a PAIR that selects which
	// of two credential implementations runs, and the DISABLE half is read
	// FIRST and overrides the option every client constructor sets — so an
	// operator who omits it finds their override silently ignored.
	"GOOGLE_API_GO_EXPERIMENTAL_DISABLE_NEW_AUTH_LIB",
	"GOOGLE_API_GO_EXPERIMENTAL_ENABLE_NEW_AUTH_LIB",
	// The universe this credential belongs to. Omitting it is the one name in
	// this group that fails LOUDLY: on a non-default universe the client falls
	// back to the public endpoint while the credential carries the real one, and
	// the mismatch is reported naming both values.
	"GOOGLE_CLOUD_UNIVERSE_DOMAIN",
	// Client-certificate authentication. Its default is ON for the public
	// universe, so omitting it DROPS an operator's explicit "off".
	"GOOGLE_API_USE_CLIENT_CERTIFICATE",
	"GOOGLE_API_CERTIFICATE_CONFIG",
	"GOOGLE_API_USE_MTLS_ENDPOINT",
	"GOOGLE_API_USE_MTLS",
	// The secure-transport switches.
	"EXPERIMENTAL_GOOGLE_API_USE_S2A",
	"S2A_TIMEOUT",
	// The trust-boundary feature, whose read site also validates the value, so
	// omitting it removes the validation as well as the feature.
	"GOOGLE_AUTH_TRUST_BOUNDARY_ENABLED",

	// The proxy family, in both spellings, because the standard library reads
	// both. Behind a corporate proxy an operator who omits these gets a
	// collector that cannot reach the provider a browser on the same host can,
	// and the failure is a connection error naming no cause.
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY",
	"http_proxy", "https_proxy", "no_proxy",

	// The trust roots. On a host whose certificate bundle is not at a
	// compiled-in default, an operator who omits these gets a collector that
	// cannot complete a secure connection at all. They are read only on the unix
	// family — the platform's own store is used on the others — so DECLARING
	// them is inert on a target that ignores them and load-bearing on one that
	// does not, which is why they are in the common set rather than a third case.
	"SSL_CERT_FILE", "SSL_CERT_DIR",
}

// DeliberatelyOmitted are the names this collector does NOT declare, with the
// reason each is left out. It is exported and asserted so that a name added to
// or dropped from the set below is a decision on the record rather than a
// silent edit.
var DeliberatelyOmitted = map[string]string{
	// CREDENTIAL MATERIAL rather than a switch. Declaring it would put a token
	// in an operator-edited configuration file, and the cost of leaving it out
	// is narrow and nameable: an operator relying on it gets that one token
	// acquisition failing inside the collector and working outside it.
	"S2A_ACCESS_TOKEN": "carries an access token; a credential belongs in the credential chain, " +
		"not in a config file",

	// UNREACHABLE on this collector's path: the read site returns before the
	// variable is consulted unless an option this collector never sets is set.
	"GOOGLE_CLOUD_DISABLE_DIRECT_PATH":    "its read site is unreachable on this collector's path",
	"GOOGLE_CLOUD_ENABLE_DIRECT_PATH_XDS": "its read site is unreachable on this collector's path",

	// READ BY NOTHING in this collector's dependency closure. They are the
	// names an operator most often expects to matter, which is why their
	// absence is stated rather than left to be discovered.
	"CLOUDSDK_CONFIG":       "read by no package in this collector's dependency closure",
	"GCLOUD_PROJECT":        "read by no package in this collector's dependency closure",
	"CLOUDSDK_CORE_PROJECT": "read by no package in this collector's dependency closure",
	"GCE_METADATA_IP":       "read by no package in this collector's dependency closure",
}

// Names returns the environment variable names this collector's config entry
// should declare for the named target operating system, sorted.
func Names(goos string) []string {
	out := slices.Clone(commonNames)
	if goos == GOOSWindows {
		out = append(out, windowsHomeVariable)
	} else {
		out = append(out, unixHomeVariable)
	}
	slices.Sort(out)
	return out
}

// Example renders the environment block of a worked config entry for the named
// target, as the lines an operator writes.
//
// EVERY VALUE IS A PLACEHOLDER. The block is a map of NAME to VALUE and the
// values are the operator's own; rendering a real one here would either be
// wrong or, worse, be a credential.
func Example(goos string) string {
	var b strings.Builder
	for _, name := range Names(goos) {
		fmt.Fprintf(&b, "%s = \"<%s>\"\n", name, strings.ToLower(name))
	}
	return b.String()
}
