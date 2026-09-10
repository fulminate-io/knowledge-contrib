// SPDX-License-Identifier: Apache-2.0

package main

import (
	"runtime"
	"sort"
)

// env.go — THE DECLARED ENVIRONMENT: every variable this collector's dependency
// closure reads, computed from a TARGET-OS ARGUMENT.
//
// WHAT THE LIST IS. A stdio collector's environment is scrubbed to exactly the
// variables its config entry declares: the daemon copies nothing from its own
// and adds nothing, and there is no platform baseline. So this list IS the
// entry's `env` block, and a name missing from it is a variable this process
// never receives — silently, because an absent variable looks exactly like an
// unset one. The list is shipped as a value so the documented example entry and
// this collector's own expectations cannot drift apart.
//
// WHY THE OS IS A PARAMETER RATHER THAN runtime.GOOS READ INTERNALLY. Two
// groups vary by platform: the Windows home fallbacks, and the two trust-root
// names whose only standard-library reader is compiled under a build constraint
// that admits linux and excludes darwin and windows. Every job in this
// repository that reaches `go test` runs on Linux, so a composer reading
// runtime.GOOS internally would put its Windows branch and its unix-only branch
// beyond every gate this repository has. A list of names takes a parameter; it
// is not the case for the build-tag idiom this tree uses for platform-varying
// CODE.
//
// HOW THE LIST WAS DERIVED, AND WHAT THE DERIVATION CANNOT SEE. It is the union
// of a CALL-SITE CENSUS over the dependency closure — every read resolved
// through its constant or its variadic argument, not only string literals — and
// a LIVE MATRIX measured against a real GKE cluster with a fresh kubeconfig per
// row. Neither instrument is the list on its own: a call-site census sees every
// Go read and no spawned binary, and a live matrix sees only the names someone
// thought to vary. PATH and CLOUDSDK_CONFIG are on this list because they were
// MEASURED, not because a census found them — the GKE credential plugin hands
// the whole environment to `gcloud`, whose own reads are in no Go closure.
//
// A HAZARD MEASURED ON THE LIVE CLUSTER, RECORDED HERE BECAUSE IT IS THE
// REVERSE OF THE INTUITIVE ONE: an ABSENT HOME works and a HOME pointing at a
// scratch directory FAILS. Nothing here synthesizes a HOME when the variable is
// missing; see resolveKubeconfig in client.go.

// envKubeconfig is the set of names every target OS declares.
var envEveryOS = []string{
	// The kubeconfig resolution.
	"KUBECONFIG",      // clientcmd's loader reads it through a named constant
	"HOME",            // where clientcmd looks for ~/.kube/config
	"PATH",            // the GKE credential plugin execs gcloud from it
	"CLOUDSDK_CONFIG", // substitutes for HOME's gcloud-config role only

	// The in-cluster arm. Only the host and port come from the environment; the
	// token and the CA bundle are file paths the library holds as constants.
	"KUBERNETES_SERVICE_HOST",
	"KUBERNETES_SERVICE_PORT",

	// clientcmd's own two reads.
	"KUBERNETES_MASTER",
	"POD_NAMESPACE",

	// The transport reads these on EVERY API call. The proxier is installed on
	// the transport every rest client is built with, and it consults all six
	// proxy names; the two HTTP/2 knobs are read on the same path the
	// off-switch is, when the off-switch is unset, which is the ordinary case.
	//
	// ALL SIX PROXY NAMES ARE DECLARED TOGETHER, never three. The upper- and
	// lower-case spellings are two DIFFERENT lookups tried in order, so
	// declaring one of a pair changes behavior rather than halving the cost.
	"DISABLE_HTTP2",
	"HTTP2_READ_IDLE_TIMEOUT_SECONDS",
	"HTTP2_PING_TIMEOUT_SECONDS",
	"HTTP_PROXY", "http_proxy",
	"HTTPS_PROXY", "https_proxy",
	"NO_PROXY", "no_proxy",
}

// envUnixOnly is the trust-root pair. crypto/x509 reads both through named
// constants in a file whose build constraint is
// `aix || dragonfly || freebsd || (js && wasm) || linux || netbsd || openbsd ||
// solaris || wasip1`. linux is in it; darwin and windows are not, and a name a
// target OS never compiles a reader for is a name that OS does not reach.
var envUnixOnly = []string{"SSL_CERT_DIR", "SSL_CERT_FILE"}

// envWindowsOnly is the Windows home fallback trio. client-go's home-directory
// resolution is guarded by a windows check and then prefers whichever of HOME,
// HOMEDRIVE+HOMEPATH and USERPROFILE contains .kube\config. HOME is routinely
// unset on Windows, so without these the standard kubeconfig resolution has
// nothing to resolve through.
var envWindowsOnly = []string{"HOMEDRIVE", "HOMEPATH", "USERPROFILE"}

// envAllowlist returns the environment variable names this collector declares
// for the given target OS, sorted and free of duplicates.
//
// goos takes the values `runtime.GOOS` takes. An unrecognized value is not an
// error: it yields the platform-independent names alone, which is the correct
// answer for a platform neither special case names.
func envAllowlist(goos string) []string {
	out := append([]string(nil), envEveryOS...)
	switch goos {
	case "windows":
		out = append(out, envWindowsOnly...)
	case "darwin":
		// Deliberately nothing: darwin's certificate verification does not
		// compile the readers of the trust-root pair.
	default:
		// The unix family, which is where the trust-root readers compile.
		out = append(out, envUnixOnly...)
	}
	sort.Strings(out)
	return out
}

// DeclaredEnvironment is the ONE production call site of [envAllowlist]: the
// list for the platform this process is actually running on. The documented
// config entry is generated from it, so the example an operator copies and the
// names this process can receive are one list.
func DeclaredEnvironment() []string { return envAllowlist(runtime.GOOS) }
