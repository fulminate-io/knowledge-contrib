// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// transport_test.go — THE TWO TRANSPORT VARIABLE GROUPS, observed in a child
// whose whole environment the test decided.
//
// WHY THESE TWO ARE DIFFERENT FROM EVERY OTHER DECLARED NAME. The credential
// names are read by the Azure SDK, whose behaviour a unit test can observe; the
// proxy and trust-store names are read by the Go runtime BENEATH this
// collector, once per process, before any of this collector's code runs. The
// only way to see what declaring or omitting one does is to declare or omit it
// on a real child and watch the request.
//
// THEY DO NOT BEHAVE ALIKE ACROSS PLATFORMS, which is why they are two rows
// rather than one. The proxy names are read on every platform. The trust-store
// pair is read by crypto/x509's unix arm, whose build constraint covers Linux
// and the BSDs and EXCLUDES darwin and windows — so on a macOS host the pair is
// read by nothing, and a test that scrubbed them there and watched a request
// succeed would have observed nothing at all. That arm SKIPS rather than
// passing, so a local run reports it as not exercised.

// probeChild runs the probing child against a URL with exactly the given
// environment, and returns its lines.
func probeChild(t *testing.T, url string, block map[string]string, extra ...string) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	env := append([]string{
		childModeEnv + "=" + childModeProbeHTTP,
		childURLEnv + "=" + url,
	}, extra...)
	for name, value := range block {
		env = append(env, name+"="+value)
	}

	cmd := exec.CommandContext(ctx, testBinary(t))
	cmd.Env = env
	cmd.Stderr = os.Stderr
	// THE CHILD RUNS OUTSIDE THE WORKSPACE. It is not isolation for its own
	// sake: the workspace's cache-blindness census asks whether a spawned
	// child reads anything a cache key should have covered, and a child whose
	// working directory is a scratch tree cannot resolve a workspace-relative
	// path at all. A child that needed one would fail here rather than pass
	// quietly against bytes no key sees.
	cmd.Dir = t.TempDir()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("running the probing child: %v", err)
	}
	var lines []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// TestTransport_TheProxyNamesDecideWhetherTheCollectorCanReachAHost.
//
// THE MEASUREMENT IS ARRANGED SO THE PROXY IS THE ONLY WAY THROUGH: the child
// requests a host that does not resolve, and the test's own proxy is the only
// thing that can answer for it. With the name in the block the request
// succeeds; with the name omitted it fails at name resolution. That is the
// operator's experience on a proxied host in both directions.
func TestTransport_TheProxyNamesDecideWhetherTheCollectorCanReachAHost(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A proxy receives the absolute URL as the request target. Answering
		// anything at all is enough: what is under test is whether the child
		// went through here.
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(proxy.Close)

	// A host that cannot resolve, so nothing but the proxy can answer for it.
	const unreachable = "http://collector-test.invalid/status"

	through := probeChild(t, unreachable, map[string]string{"HTTP_PROXY": proxy.URL})
	if len(through) != 1 || !strings.HasPrefix(through[0], "ok:") {
		t.Errorf("with the proxy name in the block the request did not go through it: %v", through)
	}

	without := probeChild(t, unreachable, nil)
	if len(without) != 1 || !strings.HasPrefix(without[0], "err:") {
		t.Errorf("with the proxy name omitted the request succeeded anyway, so the row above proves nothing: %v", without)
	}
	if !strings.Contains(without[0], "collector-test.invalid") {
		t.Errorf("the failure does not name the host the collector could not reach: %s", without[0])
	}
}

// TestTransport_TheProxyResolutionIsMemoizedOncePerProcess. It is why the entry
// that spawned the collector is what decides its proxy: a variable changed
// after the first request changes nothing, so there is no way to configure a
// running collector.
func TestTransport_TheProxyResolutionIsMemoizedOncePerProcess(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(proxy.Close)

	const unreachable = "http://collector-test.invalid/status"
	lines := probeChild(t, unreachable, map[string]string{"HTTP_PROXY": proxy.URL},
		childProbeAgain+"=1",
		childProbeMutate+"=HTTP_PROXY=http://127.0.0.1:1/",
	)
	if len(lines) != 2 {
		t.Fatalf("expected two attempts, got %v", lines)
	}
	if !strings.HasPrefix(lines[0], "ok:") {
		t.Fatalf("the first request did not go through the proxy: %s", lines[0])
	}
	if !strings.HasPrefix(lines[1], "ok:") {
		t.Errorf("changing the proxy variable inside the running process changed the transport: %s", lines[1])
	}
}

// TestTransport_TheTrustStoreNamesDecideWhetherTLSVerifies.
//
// LINUX ONLY, WITH AN EXPLICIT SKIP EVERYWHERE ELSE. crypto/x509's
// root_unix.go, which is the only place in crypto/x509 that reads an
// environment variable, is built for Linux and the BSDs and excludes darwin and
// windows; on those two the names are read by nothing, and a passing assertion
// would be an assertion about nothing.
func TestTransport_TheTrustStoreNamesDecideWhetherTLSVerifies(t *testing.T) {
	if runtime.GOOS != OSLinux {
		t.Skipf("the trust-store variables are read by crypto/x509's unix arm, which excludes %s; "+
			"this row is exercised on the Linux test leg", runtime.GOOS)
	}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	// The server's own certificate, written where a CA bundle would be.
	bundle := filepath.Join(t.TempDir(), "ca.pem")
	writeCertificate(t, bundle, server.Certificate())

	trusted := probeChild(t, server.URL, map[string]string{"SSL_CERT_FILE": bundle})
	if len(trusted) != 1 || !strings.HasPrefix(trusted[0], "ok:") {
		t.Errorf("with the trust store in the block, TLS did not verify: %v", trusted)
	}

	untrusted := probeChild(t, server.URL, nil)
	if len(untrusted) != 1 || !strings.HasPrefix(untrusted[0], "err:") {
		t.Fatalf("with the trust store omitted, TLS verified anyway, so the row above proves nothing: %v", untrusted)
	}
	// The failure is a CERTIFICATE failure rather than a credential one, which
	// is what an operator has to be able to tell apart.
	if !strings.Contains(strings.ToLower(untrusted[0]), "certificate") {
		t.Errorf("the failure does not read as a certificate failure: %s", untrusted[0])
	}
}

// TestTransport_TheTrustStoreNamesAreReadByNothingOnDarwin is the same row's
// other half, and it is what makes the skip above honest rather than a hole:
// on darwin the pair is not declared AND a verification failure there is about
// the system keychain rather than about a bundle.
func TestTransport_TheTrustStoreNamesAreReadByNothingOnDarwin(t *testing.T) {
	if runtime.GOOS != OSDarwin {
		t.Skipf("this row is about darwin's behaviour; this host is %s", runtime.GOOS)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	bundle := filepath.Join(t.TempDir(), "ca.pem")
	writeCertificate(t, bundle, server.Certificate())

	// Declaring the name changes NOTHING here: the request fails either way,
	// because darwin verifies through the system keychain and reads no bundle.
	withName := probeChild(t, server.URL, map[string]string{"SSL_CERT_FILE": bundle})
	if len(withName) != 1 || !strings.HasPrefix(withName[0], "err:") {
		t.Errorf("on darwin the trust-store variable was honored, which contradicts the build constraint "+
			"this collector's environment table is built on: %v", withName)
	}
}

// writeCertificate stores a certificate as PEM, which is the shape a CA bundle
// takes.
func writeCertificate(t *testing.T, path string, cert *x509.Certificate) {
	t.Helper()
	encoded := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	if encoded == nil {
		t.Fatal("encoding the test certificate")
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("writing the test bundle: %v", err)
	}
}

// unusedTLSConfig keeps the tls import honest if the rows above are skipped on
// this host; it documents the shape the server above serves.
var _ = tls.VersionTLS12
