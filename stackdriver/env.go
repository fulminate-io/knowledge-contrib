// SPDX-License-Identifier: Apache-2.0

package main

import "sort"

// env.go — THE ENVIRONMENT VARIABLES THIS COLLECTOR'S CONFIG ENTRY DECLARES,
// as a pure function of the target operating system.
//
// WHAT THIS TABLE IS AND IS NOT. It is NOT what the child process sees at run
// time: a stdio collector's environment is exactly the `env` block of its entry
// in the operator's collector config file, and the daemon copies nothing from
// its own environment and adds nothing. This table is the SOURCE that block is
// written from at install, and it is the module's own answer to "which names
// does this collector need in order to behave the way every other Google client
// on the host behaves".
//
// WHY A NAME IS LISTED. Because leaving it out changes behavior with no
// diagnostic. A name the entry does not carry is simply absent from the child,
// so an operator who set it in their own shell — a proxy, a CA bundle, a
// non-default universe — sees this one client quietly do something different
// from every other Google client on the same machine, with no error naming the
// cause. Under this repository's standing rule that bad input errors rather
// than degrading, a silently ignored operator-set name is the degrade; listing
// it costs one line and no behavior on a host that sets none of them.
//
// WHY IT TAKES THE OS AS A PARAMETER RATHER THAN READING runtime.GOOS. Every
// machine that runs this module's tests is Linux or macOS, so a runtime.GOOS
// branch would make the Windows arm unreachable on every runner that exists —
// present in the diff, never executed, and failing first on an operator's
// machine. As a parameter both arms are ordinary values a test computes.

// targetOS names the operating system an entry is being written for. It is a
// string rather than an enum because it is compared against Go's own GOOS
// spellings, and inventing a second vocabulary for the same values would be a
// mapping to keep in step.
type targetOS string

// The two arms this table has. There are exactly two because the credential
// path branches exactly once: the home directory the gcloud well-known
// credential file sits under is named differently on Windows.
const (
	targetOSWindows targetOS = "windows"
	targetOSUnix    targetOS = "unix"
)

// adcDiscoveryNames are the names Application Default Credentials resolution
// reads. HOME and APPDATA are the OS-dependent pair and are added by
// declaredEnvNames rather than listed here.
var adcDiscoveryNames = []string{
	"GOOGLE_APPLICATION_CREDENTIALS",
	"GCE_METADATA_HOST",
	"GOOGLE_CLOUD_PROJECT",
	"GOOGLE_CLOUD_QUOTA_PROJECT",
}

// proxyNames are the six the Go standard library and the gRPC transport read to
// decide whether to dial through a proxy. They are read by the STANDARD LIBRARY
// rather than by any Google package, which is why a dependency-closure census
// of this module cannot see them and why they are listed from the standard
// library's own source instead.
var proxyNames = []string{
	"HTTP_PROXY", "http_proxy",
	"HTTPS_PROXY", "https_proxy",
	"NO_PROXY", "no_proxy",
}

// trustRootNames decide where the TLS stack looks for a CA bundle.
//
// THEY ARE READ ONLY ON UNIX, by crypto/x509's unix trust-root file, whose build
// constraint excludes both darwin and windows — so on macOS and on Windows they
// are inert. They are listed on BOTH arms anyway: listing an inert name costs
// nothing, while splitting the table three ways to omit them would make the arm
// count a function of something other than the credential path.
//
// Leaving them out is what makes a collector fail on a host whose CA bundle is
// not at the compiled-in default — a container image, a corporate proxy install
// — with a transport error naming neither the cause nor the remedy.
var trustRootNames = []string{"SSL_CERT_FILE", "SSL_CERT_DIR"}

// chainSelectorNames choose between the current and the legacy Google auth
// chain. The DISABLE name is read FIRST and overrides the option the Cloud
// Logging client sets for itself, so an operator who set it expects the legacy
// chain and a collector that never sees the name gives them the other one.
var chainSelectorNames = []string{
	"GOOGLE_API_GO_EXPERIMENTAL_DISABLE_NEW_AUTH_LIB",
	"GOOGLE_API_GO_EXPERIMENTAL_ENABLE_NEW_AUTH_LIB",
}

// endpointNames decide which endpoint is dialed and how.
//
// ONE OF THEM FAILS LOUD AND THE REST DEGRADE SILENTLY, which is why they are
// all listed. GOOGLE_CLOUD_UNIVERSE_DOMAIN is validated and its mismatch is an
// error naming both values; the others simply take a different path and report
// nothing. The client-certificate pair is the sharpest case, because the default
// is ON for the default universe — so an entry that omits the name DROPS an
// operator's explicit "false" rather than leaving certificates off.
var endpointNames = []string{
	"GOOGLE_CLOUD_UNIVERSE_DOMAIN",
	"GOOGLE_API_USE_CLIENT_CERTIFICATE",
	"GOOGLE_API_CERTIFICATE_CONFIG",
	"GOOGLE_API_USE_MTLS_ENDPOINT",
	"GOOGLE_API_USE_MTLS",
	"EXPERIMENTAL_GOOGLE_API_USE_S2A",
	"S2A_TIMEOUT",
	"GOOGLE_AUTH_TRUST_BOUNDARY_ENABLED",
}

// excludedNames are names this collector's closure can read and this table
// deliberately does NOT declare, each with the cost of the omission. They are
// listed in source rather than merely absent so a reader can tell a decision
// from an oversight, and so a test can assert that none of them drifts into the
// declared set.
//
//   - S2A_ACCESS_TOKEN is CREDENTIAL MATERIAL rather than a switch. Cost: on a
//     host where S2A is the configured transport and its agent expects a token
//     from the environment, this collector negotiates ordinary TLS instead. The
//     two S2A switches ARE declared, so the operator can still see and set the
//     mode.
//   - GRPC_XDS_BOOTSTRAP and GRPC_XDS_BOOTSTRAP_CONFIG select a different
//     transport entirely. Cost: an operator cannot point this collector at an
//     xDS control plane from its entry.
//   - GRPC_GO_LOG_SEVERITY_LEVEL, GRPC_GO_LOG_VERBOSITY_LEVEL,
//     GRPC_GO_LOG_FORMATTER, GODEBUG, GOPROTODEBUG and DEBUG_HTTP2_GOROUTINES
//     raise diagnostics or change runtime behavior. Cost: an operator cannot
//     raise this child's gRPC log level from its entry and debugs it by other
//     means. The reason to withhold them is that a collector whose transport and
//     runtime an entry can reconfigure is a collector whose failures are not
//     reproducible from the entry alone.
var excludedNames = []string{
	"S2A_ACCESS_TOKEN",
	"GRPC_XDS_BOOTSTRAP",
	"GRPC_XDS_BOOTSTRAP_CONFIG",
	"GRPC_GO_LOG_SEVERITY_LEVEL",
	"GRPC_GO_LOG_VERBOSITY_LEVEL",
	"GRPC_GO_LOG_FORMATTER",
	"GODEBUG",
	"GOPROTODEBUG",
	"DEBUG_HTTP2_GOROUTINES",
}

// declaredEnvNames returns the names this collector's config entry declares for
// the given target OS, sorted.
//
// The two arms differ in exactly one name: the home directory the gcloud
// well-known credential file lives under.
func declaredEnvNames(os targetOS) []string {
	names := make([]string, 0, len(adcDiscoveryNames)+len(proxyNames)+
		len(trustRootNames)+len(chainSelectorNames)+len(endpointNames)+1)
	names = append(names, adcDiscoveryNames...)
	if os == targetOSWindows {
		names = append(names, "APPDATA")
	} else {
		names = append(names, "HOME")
	}
	names = append(names, proxyNames...)
	names = append(names, trustRootNames...)
	names = append(names, chainSelectorNames...)
	names = append(names, endpointNames...)
	sort.Strings(names)
	return names
}

// normalizeTargetOS maps a Go GOOS value to the arm that describes it. Every
// GOOS other than windows takes the unix arm, which is correct for the two that
// matter here and harmless for the rest: the arms differ only in the home
// directory name, and no non-Windows platform names it APPDATA.
func normalizeTargetOS(goos string) targetOS {
	if goos == string(targetOSWindows) {
		return targetOSWindows
	}
	return targetOSUnix
}
