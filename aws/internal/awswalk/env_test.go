// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// env_test.go — THE ENVIRONMENT CENSUS, run against the SDK's ACTUAL read set
// rather than against a transcribed list.
//
// WHY A CENSUS AND NOT A GOLDEN LIST. The names in env.go are what an operator
// copies into a config entry's `env` block, and that block is the collector's
// WHOLE environment: a name the SDK reads and the block omits is simply absent
// from the child, and the symptom is an operator who set AWS_PROFILE and got no
// credentials with nothing to act on. A golden list drifts the moment the SDK is
// bumped and nothing notices.
//
// THE CENSUS IS OVER THE MODULE'S PINNED DEPENDENCY CLOSURE, not its import list,
// and that is the half a naive census loses: `config` pulls in sts, sso, ssooidc
// and signin to resolve the credential chain, and each reads its own
// AWS_ENDPOINT_URL_<SERVICE> name. An operator pointing STS at a private endpoint
// is authenticating through exactly the client an import-list census never sees.
//
// IT DIFFS IN BOTH DIRECTIONS. Closure-minus-listed finds what the entry would
// fail to supply; listed-minus-closure finds what is listed for no reader. Either
// is a defect and either fails.

// fenceTestCacheOnPinnedModules is the cache fence for the census below.
//
// WHAT IT IS FOR. `go test` keys a package's stored result on the files a run
// opened, and it DROPS any opened name that does not resolve inside the tested
// package's own module root. This census reads the pinned SDK sources out of
// GOMODCACHE, which is a machine-dependent path outside every module — so
// without a fence the census is cacheable against a dependency set it never read,
// and the symptom is a stored PASS after an SDK bump that added a name.
//
// THE IN-MODULE SYMLINK REMEDY THIS REPOSITORY USES ELSEWHERE DOES NOT TRANSFER:
// GOMODCACHE is not in the repository and cannot be linked from it. What CAN be
// fenced is the thing that DECIDES the closure — this module's own go.mod and
// go.sum, which name every dependency and its version. Opening them puts the
// pinned version set into this package's cache key, so any change to the closure
// re-runs the census. A dependency whose CONTENT changed without its version
// changing is outside that guarantee, and the module proxy's checksum database
// is what makes that case not arise.
//
// IT IS CALLED FROM EACH TEST AND NEVER FROM TestMain: the go tool installs the
// opened-file hook inside m.Run, so a fence in TestMain opens its files outside
// the recording window and reaches no cache key at all — while looking, in the
// diff and in a green run, exactly like a fence that works.
func fenceTestCacheOnPinnedModules(t *testing.T) {
	t.Helper()
	opened := 0
	for _, name := range []string{"go.mod", "go.sum"} {
		// The module root is two directories up from internal/awswalk.
		path := filepath.Join("..", "..", name)
		if _, err := os.ReadFile(path); err != nil {
			t.Fatalf("test-cache fence: %s: %v", path, err)
		}
		opened++
	}
	// THE FLOOR IS THE KNOWN POSITIVE. A fence that opened nothing is
	// indistinguishable from no fence and fails exactly as silently.
	if opened != 2 {
		t.Fatalf("test-cache fence: opened %d of the 2 files that decide this module's dependency closure", opened)
	}
}

// TestEnv_ListedSetMatchesTheConfigPackagesReadSet is the first direction: every
// AWS_ name the pinned config package reads must be listed.
func TestEnv_ListedSetMatchesTheConfigPackagesReadSet(t *testing.T) {
	fenceTestCacheOnPinnedModules(t)

	dir := moduleDir(t, "github.com/aws/aws-sdk-go-v2/config")
	read := awsNamesInPackage(t, dir)
	if len(read) < 20 {
		t.Fatalf("the census found only %d AWS_ names in the pinned config package at %s; that is far below "+
			"what the credential chain reads, so the census is broken rather than the list", len(read), dir)
	}

	listed := map[string]bool{}
	for _, n := range EnvAllowlist("linux") {
		listed[n] = true
	}
	var missing []string
	for _, n := range read {
		if !listed[n] {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		t.Errorf("the pinned aws-sdk-go-v2/config package reads %d name(s) this collector's environment list "+
			"does not carry: %v.\nAn operator whose entry omits one has a collector that silently ignores a "+
			"setting they made.", len(missing), missing)
	}
}

// TestEnv_NothingIsListedForNoReader is the SECOND direction, and it is what
// keeps the list from growing by accretion: a name nothing reads is a line an
// operator is asked to configure for no effect.
func TestEnv_NothingIsListedForNoReader(t *testing.T) {
	fenceTestCacheOnPinnedModules(t)

	reads := map[string]bool{}
	for _, n := range awsNamesInPackage(t, moduleDir(t, "github.com/aws/aws-sdk-go-v2/config")) {
		reads[n] = true
	}
	for _, name := range serviceEndpointEnvNames {
		reads[name] = true
	}
	var orphans []string
	for _, listed := range EnvAllowlist("linux") {
		if !strings.HasPrefix(listed, "AWS_") {
			continue // HOME and the stdlib names have their own tests below.
		}
		if !reads[listed] {
			orphans = append(orphans, listed)
		}
	}
	if len(orphans) > 0 {
		t.Errorf("this collector's environment list carries %d AWS_ name(s) nothing in its dependency closure "+
			"reads: %v", len(orphans), orphans)
	}
}

// TestEnv_EveryServiceInTheClosureHasItsEndpointName is the TRANSITIVE half, and
// it is the one an import-list census loses.
//
// It enumerates the service packages in this module's `go list -deps` closure and
// reads each one's own endpoints.go for the AWS_ENDPOINT_URL_<SERVICE> literal it
// composes at runtime. A service reached only through the credential chain —
// sts, sso, ssooidc, signin — is in that closure and in no import of this module.
func TestEnv_EveryServiceInTheClosureHasItsEndpointName(t *testing.T) {
	fenceTestCacheOnPinnedModules(t)

	services := closureServicePackages(t)
	if len(services) < 30 {
		t.Fatalf("the closure census found only %d service packages; this module builds clients for thirty and "+
			"the credential chain adds four more, so the census is broken rather than the list", len(services))
	}

	listed := map[string]bool{}
	for _, n := range serviceEndpointEnvNames {
		listed[n] = true
	}
	var missing []string
	for _, pkg := range services {
		for _, name := range awsNamesInFile(t, filepath.Join(moduleDir(t, pkg), "endpoints.go")) {
			if !strings.HasPrefix(name, "AWS_ENDPOINT_URL_") {
				continue
			}
			if !listed[name] {
				missing = append(missing, name+" (from "+pkg+")")
			}
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d service endpoint override name(s) are read by a package in this module's closure and are "+
			"not listed: %v.\nA missing one leaves an operator's private-endpoint override SILENTLY unset on "+
			"exactly the client the credential chain authenticates through.", len(missing), missing)
	}

	// THE TRANSITIVE ARM, ASSERTED BY NAME rather than left to the count: these
	// four are reached only through `config`, and a census built from this
	// module's import list would carry none of them.
	for _, transitive := range []string{
		"AWS_ENDPOINT_URL_STS", "AWS_ENDPOINT_URL_SSO",
		"AWS_ENDPOINT_URL_SSO_OIDC", "AWS_ENDPOINT_URL_SIGNIN",
	} {
		if !listed[transitive] {
			t.Errorf("%s is absent; it is read by a client this module never imports directly and reaches only "+
				"through the credential chain", transitive)
		}
	}
}

// TestEnv_HomeIsListedAndIsTargetOSDependent covers the one name that is not an
// AWS_ variable and is required all the same.
//
// WITHOUT IT no profile, SSO session or shared-file assumed role is reachable,
// however many AWS_ names the block carries — the SDK resolves both shared files
// through os.UserHomeDir.
func TestEnv_HomeIsListedAndIsTargetOSDependent(t *testing.T) {
	unix := EnvAllowlist("linux")
	if !slices.Contains(unix, "HOME") {
		t.Error("HOME is absent from the unix list; without it the SDK reaches no shared config or credentials file")
	}
	if slices.Contains(unix, "USERPROFILE") {
		t.Error("USERPROFILE is on the unix list, where nothing reads it")
	}
	windows := EnvAllowlist("windows")
	if !slices.Contains(windows, "USERPROFILE") {
		t.Error("USERPROFILE is absent from the windows list; os.UserHomeDir reads it there")
	}
	if slices.Contains(windows, "HOME") {
		t.Error("HOME is on the windows list, where os.UserHomeDir does not read it")
	}
	// AN UNKNOWN TARGET IS TREATED AS UNIX, which is the shape every non-Windows
	// target takes; it is asserted rather than assumed because it is the arm a
	// caller passing a new GOOS lands in.
	if !slices.Contains(EnvAllowlist("freebsd"), "HOME") {
		t.Error("an unknown target OS must be treated as unix-like")
	}
}

// TestEnv_StdlibNamesAreListed covers the four-name group no closure census can
// reach: the standard library is outside every module closure by construction.
func TestEnv_StdlibNamesAreListed(t *testing.T) {
	listed := map[string]bool{}
	for _, n := range EnvAllowlist("linux") {
		listed[n] = true
	}
	// THE PROXY PAIR IS SIX NAMES, upper and lower case, because
	// net/http.ProxyFromEnvironment reads both spellings and the AWS SDK's default
	// HTTP client installs exactly that proxy function.
	for _, name := range []string{
		"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy",
	} {
		if !listed[name] {
			t.Errorf("%s is absent; an operator who has not listed it gets a collector that cannot dial an "+
				"endpoint the AWS CLI on the same host reaches, surfacing as a transport error rather than as "+
				"an explanation", name)
		}
	}
	// THE TRUST ROOTS, because AWS_CA_BUNDLE is already listed and overrides the
	// SDK's OWN bundle without reaching crypto/x509's system pool.
	for _, name := range []string{"SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if !listed[name] {
			t.Errorf("%s is absent, leaving a half-covered trust story: AWS_CA_BUNDLE is listed and does not "+
				"reach the standard library's system pool", name)
		}
	}
}

// TestEnv_TheListIsSortedAndFreeOfDuplicates pins the property the install
// documentation depends on: it is generated from this list, so an unstable order
// would produce a spurious diff on every regeneration.
func TestEnv_TheListIsSortedAndFreeOfDuplicates(t *testing.T) {
	got := EnvAllowlist("linux")
	if !slices.IsSorted(got) {
		t.Error("the environment list is not sorted; the install documentation is generated from it")
	}
	seen := map[string]bool{}
	for _, n := range got {
		if seen[n] {
			t.Errorf("%s appears twice", n)
		}
		seen[n] = true
	}
}
