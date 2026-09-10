// SPDX-License-Identifier: Apache-2.0

package main

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

// env_test.go — ENV-a: the declared environment allowlist, TABLE-DRIVEN OVER
// THE TARGET OS AS AN ARGUMENT.
//
// WHY THE OS IS AN ARGUMENT AND NOT runtime.GOOS READ INTERNALLY. Two entries
// of the list vary by platform: the Windows home fallbacks, and the two
// trust-root names whose only reader in the standard library is compiled under
// a unix-family build constraint that admits linux and excludes darwin and
// windows. Every job in this repository that reaches `go test` runs on Linux,
// so a composer reading runtime.GOOS internally would put its Windows branch
// and its unix-only branch beyond every gate this repository has. Taking the OS
// as data puts all three cases on an ordinary Linux runner.
//
// THE DARWIN CASE IS NOT DECORATION. It is the only case that distinguishes a
// correct target-OS function from one that returns the same list everywhere:
// darwin drops the trust-root pair that linux carries and carries none of the
// Windows trio.
//
// A DROPPED NAME IS A RED AND AN EXTRA NAME IS A RED. The block is the child
// process's WHOLE environment — the daemon copies nothing from its own and adds
// nothing — so a name missing from the list is a variable the collector never
// receives and no operator is told to set, and a name listed for no reader is a
// variable handed to a child for nothing.

// The names every target OS declares, with what reads each.
var wantEnvEveryOS = []string{
	// The kubeconfig resolution itself.
	"KUBECONFIG", // clientcmd's loader, through its RecommendedConfigPathEnvVar constant
	"HOME",       // where clientcmd looks for ~/.kube/config
	"PATH",       // LOAD-BEARING: the GKE credential plugin execs gcloud even
	//                though the kubeconfig names the plugin by absolute path
	"CLOUDSDK_CONFIG", // substitutes for HOME's gcloud-config role, never for its kubeconfig-location role

	// The in-cluster arm. The token and CA are FILE paths, not environment.
	"KUBERNETES_SERVICE_HOST",
	"KUBERNETES_SERVICE_PORT",

	// clientcmd's own two reads.
	"KUBERNETES_MASTER", // overrides the API server address
	"POD_NAMESPACE",     // the downward-API way to learn the pod's own namespace

	// The transport reads these on every API call, unconditionally: the rest
	// client's transport is built with a proxier that consults all six proxy
	// names, and the HTTP/2 knobs ride the same path as the off-switch.
	"DISABLE_HTTP2",
	"HTTP2_READ_IDLE_TIMEOUT_SECONDS",
	"HTTP2_PING_TIMEOUT_SECONDS",
	"HTTP_PROXY", "http_proxy",
	"HTTPS_PROXY", "https_proxy",
	"NO_PROXY", "no_proxy",
}

// The trust-root pair, read by crypto/x509 through constants under a build
// constraint that admits linux and excludes darwin and windows.
var wantEnvUnixOnly = []string{"SSL_CERT_FILE", "SSL_CERT_DIR"}

// The Windows home fallbacks. client-go's HomeDir() is guarded by a
// runtime.GOOS == "windows" check before it reads them, and HOME is routinely
// unset on Windows, so without them the standard kubeconfig resolution has
// nothing to resolve through.
var wantEnvWindowsOnly = []string{"HOMEDRIVE", "HOMEPATH", "USERPROFILE"}

func TestEnvAllowlist_PerTargetOS(t *testing.T) {
	cases := []struct {
		goos string
		want []string
	}{
		{goos: "linux", want: concat(wantEnvEveryOS, wantEnvUnixOnly)},
		{goos: "darwin", want: wantEnvEveryOS},
		{goos: "windows", want: concat(wantEnvEveryOS, wantEnvWindowsOnly)},
	}
	for _, tc := range cases {
		t.Run(tc.goos, func(t *testing.T) {
			got := envAllowlist(tc.goos)
			// ElementsMatch reds on a missing name AND on an extra one.
			assert.ElementsMatch(t, tc.want, got,
				"the declared environment for %s must be exactly this list", tc.goos)
		})
	}
}

// TestEnvAllowlist_DarwinDropsTheTrustRootPair is the case that distinguishes a
// real target-OS function from one returning a constant list.
func TestEnvAllowlist_DarwinDropsTheTrustRootPair(t *testing.T) {
	linux := set(envAllowlist("linux"))
	darwin := set(envAllowlist("darwin"))
	windows := set(envAllowlist("windows"))

	for _, name := range wantEnvUnixOnly {
		assert.True(t, linux[name], "%s is read on linux and must be declared there", name)
		assert.False(t, darwin[name], "%s has no reader compiled on darwin and must not be declared there", name)
		assert.False(t, windows[name], "%s has no reader compiled on windows and must not be declared there", name)
	}
	for _, name := range wantEnvWindowsOnly {
		assert.True(t, windows[name], "%s is read on windows and must be declared there", name)
		assert.False(t, linux[name], "%s is read only on windows and must not be declared on linux", name)
		assert.False(t, darwin[name], "%s is read only on windows and must not be declared on darwin", name)
	}
}

// TestEnvAllowlist_IsSortedAndFreeOfDuplicates keeps the shipped example entry
// stable: it is a documented artifact an operator diffs.
func TestEnvAllowlist_IsSortedAndFreeOfDuplicates(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		got := envAllowlist(goos)
		seen := make(map[string]bool, len(got))
		for i, name := range got {
			assert.False(t, seen[name], "%s: %q is declared twice", goos, name)
			seen[name] = true
			if i > 0 {
				assert.LessOrEqual(t, got[i-1], name, "%s: the list is not sorted at %q", goos, name)
			}
		}
	}
}

// TestEnvAllowlist_ProductionCallSitePassesRuntimeGOOS pins the ONE production
// call site to the host's own OS. The composer takes the OS as data so a test
// can reach every branch; production must still ask for the platform it is on.
func TestEnvAllowlist_ProductionCallSitePassesRuntimeGOOS(t *testing.T) {
	assert.Equal(t, envAllowlist(runtime.GOOS), DeclaredEnvironment(),
		"DeclaredEnvironment is the production call site and must pass runtime.GOOS")
}

// TestEnvAllowlist_KubernetesExecInfoIsNotAnInput records a measured NON-entry.
// client-go SETS KUBERNETES_EXEC_INFO on the credential plugin child and never
// reads it from the environment, so declaring it would hand a child a variable
// for no reader.
func TestEnvAllowlist_KubernetesExecInfoIsNotAnInput(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		assert.False(t, set(envAllowlist(goos))["KUBERNETES_EXEC_INFO"],
			"%s: KUBERNETES_EXEC_INFO is set by client-go on the plugin child, never read from the environment", goos)
	}
}

func concat(lists ...[]string) []string {
	var out []string
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}

func set(names []string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}
