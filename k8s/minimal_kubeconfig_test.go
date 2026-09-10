// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// minimal_kubeconfig_test.go — A KUBECONFIG THAT RESOLVES, so an inertness sweep
// compares a BUILT CLIENT rather than a refusal string.
//
// WHY THE INERTNESS ROWS NEED ONE. The claim those rows make is that nothing on
// this collector's resolution path tells a name present-and-empty apart from
// absent. Taken against a tree with no kubeconfig anywhere, every arm returns at
// the file-not-found error, `clientcmd.NewDefaultClientConfig` is never called,
// and the one site where the package-level ClusterDefaults could enter a client
// is not exercised in any arm — so a name that moved a SUCCESSFULLY BUILT
// configuration and nothing else would be invisible, and the zero would be taken
// against a string that could not have moved anyway.
//
// SO THE ARMS RESOLVE. With this file in place the compared value is
// `resolved: context=... host=...`, which is what the validating research's own
// confirming arm compared and what makes the zero mean something.
//
// IT NEEDS NO CLUSTER AND MAKES NO CALL. Building a rest.Config from a kubeconfig
// is a local operation; nothing dials the host, which is why the server below is
// a `.invalid` name that cannot resolve in DNS.

// minimalKubeconfigServer is the host the fixture names, and the value an arm
// asserts it resolved. `.invalid` is reserved by RFC 2606 and never resolves, so
// a test that started dialing would fail loudly rather than reach a real cluster.
const minimalKubeconfigServer = "https://kubeconfig.example.invalid:6443"

// minimalKubeconfigContext is the context name the fixture selects.
const minimalKubeconfigContext = "fixture-context"

// minimalKubeconfig is one cluster, one context and one empty user — the
// smallest document `clientcmd.LoadFromFile` accepts that
// `NewDefaultClientConfig` will build a client from.
const minimalKubeconfig = `apiVersion: v1
kind: Config
clusters:
  - name: fixture-cluster
    cluster:
      server: ` + minimalKubeconfigServer + `
      insecure-skip-tls-verify: true
contexts:
  - name: ` + minimalKubeconfigContext + `
    context:
      cluster: fixture-cluster
      user: fixture-user
users:
  - name: fixture-user
    user: {}
current-context: ` + minimalKubeconfigContext + `
`

// writeMinimalKubeconfigUnderHome writes the fixture at home/.kube/config, the
// path this collector derives from HOME, and returns that path so a caller can
// also name it through KUBECONFIG.
func writeMinimalKubeconfigUnderHome(t *testing.T, home string) string {
	t.Helper()
	dir := filepath.Join(home, ".kube")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("creating %s: %v", dir, err)
	}
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte(minimalKubeconfig), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

// assertResolvesTheFixture is the guard every arm that depends on the fixture
// runs first: an outcome that is NOT a resolved host means the arm fell back to
// a refusal, and every comparison taken against it would be the weak one this
// fixture exists to replace.
func assertResolvesTheFixture(t *testing.T, label, outcome string) {
	t.Helper()
	if !strings.Contains(outcome, minimalKubeconfigServer) {
		t.Fatalf("%s did not resolve the fixture kubeconfig; the comparison would be taken against a refusal "+
			"rather than a built client.\ngot: %s\nwant a resolved host of %s", label, outcome, minimalKubeconfigServer)
	}
}
