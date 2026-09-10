// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// client_test.go — R4-a, R4-b, ENV-b and ENV-c: how a credential is resolved,
// and every arm on which it must fail loudly instead of degrading.
//
// EVERY ARM ASSERTS ON THIS MODULE'S OWN LOOKUP, NEVER ON A LIBRARY'S VIEW OF
// THE ENVIRONMENT. Several runtimes synthesize a HOME for a process whose HOME
// is unset, so a test that reads the ambient environment cannot tell "the
// variable was absent" from "the variable was absent and something invented
// one". The resolver takes its environment as a function, and these tables
// supply it.
//
// THE IN-CLUSTER ARM IS COVERED ON BOTH SIDES BECAUSE THIS MODULE OWNS THE
// RESOLUTION. client-go's own in-cluster loader declares its token and CA paths
// as FUNCTION-LOCAL CONSTANTS with no parameter and no override, so a module
// calling it can construct only the NEGATIVE arm — files absent — and its
// positive arm has no venue anywhere in this project: not in-module, not at the
// live confirmation (which collects through a kubeconfig from outside the
// cluster), and there is no container venue in this tree. Taking the two paths
// as parameters that default to client-go's own values is what makes the
// positive arm its own known-positive.

func fakeEnv(pairs map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := pairs[name]
		return v, ok
	}
}

// writeKubeconfig writes a minimal but VALID kubeconfig naming the given
// contexts, with the first one current, and returns its path.
func writeKubeconfig(t *testing.T, dir string, contexts ...string) string {
	t.Helper()
	require.NotEmpty(t, contexts)
	var body strings.Builder
	body.WriteString("apiVersion: v1\nkind: Config\nclusters:\n")
	for _, c := range contexts {
		body.WriteString("- name: cluster-" + c + "\n  cluster:\n    server: https://" + c + ".example.invalid\n")
	}
	body.WriteString("users:\n")
	for _, c := range contexts {
		body.WriteString("- name: user-" + c + "\n  user:\n    token: not-a-real-token\n")
	}
	body.WriteString("contexts:\n")
	for _, c := range contexts {
		body.WriteString("- name: " + c + "\n  context:\n    cluster: cluster-" + c + "\n    user: user-" + c + "\n")
	}
	body.WriteString("current-context: " + contexts[0] + "\n")

	path := filepath.Join(dir, "config")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(body.String()), 0o600))
	return path
}

// TestResolve_KubeconfigViaKUBECONFIG is ENV-b arm 1, and the same-run
// known-positive the loud-failure arm rests on.
func TestResolve_KubeconfigViaKUBECONFIG(t *testing.T) {
	dir := t.TempDir()
	path := writeKubeconfig(t, dir, "alpha", "beta")

	got, err := resolveCredential("", credentialSources{
		lookupEnv: fakeEnv(map[string]string{"KUBECONFIG": path}),
	})
	require.NoError(t, err)
	assert.Equal(t, "alpha", got.ContextName, "no named context uses the currently selected one")
	assert.Equal(t, "https://alpha.example.invalid", got.Config.Host)
}

// TestResolve_KubeconfigViaHOME is ENV-b arm 2: KUBECONFIG absent, HOME at a
// directory holding .kube/config resolves that.
func TestResolve_KubeconfigViaHOME(t *testing.T) {
	home := t.TempDir()
	writeKubeconfig(t, filepath.Join(home, ".kube"), "gamma")

	got, err := resolveCredential("", credentialSources{
		lookupEnv: fakeEnv(map[string]string{"HOME": home}),
	})
	require.NoError(t, err)
	assert.Equal(t, "gamma", got.ContextName)
}

// TestResolve_NothingToResolveFailsLoud is ENV-b arm 3 and R4's "fails loud"
// clause. Neither variable is set and there is no in-cluster environment: the
// resolver must name what it could not resolve, not invent a home directory and
// not return an empty result.
func TestResolve_NothingToResolveFailsLoud(t *testing.T) {
	_, err := resolveCredential("", credentialSources{
		lookupEnv: fakeEnv(nil),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "KUBECONFIG")
	assert.Contains(t, err.Error(), "HOME")
	assert.Contains(t, err.Error(), "KUBERNETES_SERVICE_HOST",
		"the error names every input it looked for, including the in-cluster arm")
}

// TestResolve_NeverSynthesizesAHome is ENV-b's guard mutation made observable.
// A HOME the operator did not set must never be invented: on the live GKE
// target an ABSENT HOME authenticates and a HOME pointing at a scratch
// directory FAILS, so synthesizing one converts a working configuration into a
// broken one.
func TestResolve_NeverSynthesizesAHome(t *testing.T) {
	// A real home exists on this machine and holds a real kubeconfig for the
	// operator. If the resolver fell back to the ambient process environment it
	// would find it; with an empty lookup it must not.
	asked := map[string]bool{}
	_, err := resolveCredential("", credentialSources{
		lookupEnv: func(name string) (string, bool) {
			asked[name] = true
			return "", false
		},
	})
	require.Error(t, err)
	assert.True(t, asked["HOME"], "the resolver asks for HOME through its own lookup")
	assert.True(t, asked["KUBECONFIG"], "the resolver asks for KUBECONFIG through its own lookup")
}

// TestResolve_NamedContextThatDoesNotExistFailsLoud is R4-a. It must name the
// context, and — the control — the same kubeconfig with a context that DOES
// exist must resolve in the same run.
func TestResolve_NamedContextThatDoesNotExistFailsLoud(t *testing.T) {
	dir := t.TempDir()
	path := writeKubeconfig(t, dir, "alpha", "beta")
	env := fakeEnv(map[string]string{"KUBECONFIG": path})

	_, err := resolveCredential("does-not-exist", credentialSources{lookupEnv: env})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does-not-exist", "the refusal names the context that was asked for")
	assert.Contains(t, err.Error(), "alpha", "and names the contexts the kubeconfig does carry")

	// SAME-RUN CONTROL: the other context resolves, so the refusal above is
	// about the name and not about the fixture.
	got, err := resolveCredential("beta", credentialSources{lookupEnv: env})
	require.NoError(t, err)
	assert.Equal(t, "beta", got.ContextName)
	assert.Equal(t, "https://beta.example.invalid", got.Config.Host)
}

// TestResolve_InCluster_Positive is ENV-c's POSITIVE arm, reachable only
// because this module owns the resolution and takes the two file paths as
// parameters.
func TestResolve_InCluster_Positive(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "token")
	caFile := filepath.Join(dir, "ca.crt")
	require.NoError(t, os.WriteFile(tokenFile, []byte("a-service-account-token"), 0o600))
	require.NoError(t, os.WriteFile(caFile, []byte(testCAPEM), 0o600))

	got, err := resolveCredential("", credentialSources{
		lookupEnv: fakeEnv(map[string]string{
			"KUBERNETES_SERVICE_HOST": "10.0.0.1",
			"KUBERNETES_SERVICE_PORT": "443",
		}),
		inClusterTokenFile: tokenFile,
		inClusterCAFile:    caFile,
	})
	require.NoError(t, err)
	assert.Equal(t, "https://10.0.0.1:443", got.Config.Host)
	assert.Equal(t, inClusterContextName, got.ContextName)
	assert.Equal(t, "a-service-account-token", got.Config.BearerToken)
}

// TestResolve_InCluster_FilesAbsentFailsLoud is ENV-c's negative arm and its
// guard mutation: the in-cluster environment is set and the files are not
// there, and the resolver must NOT fall through to a kubeconfig search.
func TestResolve_InCluster_FilesAbsentFailsLoud(t *testing.T) {
	dir := t.TempDir()
	// A perfectly good kubeconfig is reachable. If the in-cluster arm fell
	// through, this is what it would silently succeed on.
	path := writeKubeconfig(t, dir, "alpha")

	_, err := resolveCredential("", credentialSources{
		lookupEnv: fakeEnv(map[string]string{
			"KUBERNETES_SERVICE_HOST": "10.0.0.1",
			"KUBERNETES_SERVICE_PORT": "443",
			"KUBECONFIG":              path,
		}),
		inClusterTokenFile: filepath.Join(dir, "no-such-token"),
		inClusterCAFile:    filepath.Join(dir, "no-such-ca"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no-such-token",
		"the refusal names the file it could not read")
	assert.NotContains(t, err.Error(), "alpha",
		"an in-cluster environment with unreadable files must NOT fall through to the kubeconfig")
}

// TestResolve_InClusterDefaultsAreClientGoPaths pins the defaults. The
// parameters exist so the arms are testable, not so the production paths can
// drift from the ones a Kubernetes pod actually mounts.
func TestResolve_InClusterDefaultsAreClientGoPaths(t *testing.T) {
	src := defaultCredentialSources()
	assert.Equal(t, "/var/run/secrets/kubernetes.io/serviceaccount/token", src.inClusterTokenFile)
	assert.Equal(t, "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt", src.inClusterCAFile)
	assert.NotNil(t, src.lookupEnv, "the production sources read the real environment")
}

// TestResolve_MalformedKubeconfigFailsLoud is the bad-input arm: a kubeconfig
// that is present but not parseable errors naming the file, and never yields an
// empty configuration that would then collect nothing and assert completeness.
func TestResolve_MalformedKubeconfigFailsLoud(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	require.NoError(t, os.WriteFile(path, []byte("this: is: not: a: kubeconfig\n\t\x00"), 0o600))

	_, err := resolveCredential("", credentialSources{
		lookupEnv: fakeEnv(map[string]string{"KUBECONFIG": path}),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), path, "the refusal names the file it could not read")
}

// testCAPEM is a syntactically valid, expired, self-signed certificate used
// only to satisfy the in-cluster CA loader. It authenticates nothing: no test
// here dials anything.
const testCAPEM = `-----BEGIN CERTIFICATE-----
MIIBcTCCARegAwIBAgIUJ7v0nEjmPPqLpNlmVEr7uqhx4SgwCgYIKoZIzj0EAwIw
FTETMBEGA1UEAwwKdGVzdC1jYS1jbjAeFw0yMDAxMDEwMDAwMDBaFw0yMDAxMDIw
MDAwMDBaMBUxEzARBgNVBAMMCnRlc3QtY2EtY24wWTATBgcqhkjOPQIBBggqhkjO
PQMBBwNCAAQ4gYYW9y+kZgAKwzUu1nMFTlrJ1oPtCkTLTRbBBIP5aLjQCJUvKdPT
0lRuoZ8U2LLKzWCPHR6zAWFCzZBHIDPDo1MwUTAdBgNVHQ4EFgQUqLcJ7RMDpHZL
0f0FpAWnMk6qcO0wHwYDVR0jBBgwFoAUqLcJ7RMDpHZL0f0FpAWnMk6qcO0wDwYD
VR0TAQH/BAUwAwEB/zAKBggqhkjOPQQDAgNIADBFAiEA5r2rXKfKZJ1234567890
-----END CERTIFICATE-----
`

// TestResolve_PartialInClusterEnvironmentFailsLoud is ENV-c2.
//
// THE DEFECT IT PINS. The first version required BOTH in-cluster names to be
// set and non-empty, and otherwise resolved a kubeconfig. Measured on that
// code, with a valid kubeconfig naming context "alpha" reachable: HOST set with
// PORT absent, HOST empty with PORT set, and HOST set with PORT empty all
// returned no error and context "alpha" — a completely different cluster's
// credential, under the id the operator asked for, producing a graph that looks
// entirely healthy.
//
// IT IS REACHABLE BECAUSE THE ENV BLOCK IS AN OPERATOR'S CONFIG CHOICE. The
// entry's env block is the child's whole environment, so which of the two names
// arrives is decided by whoever wrote the entry rather than by the kubelet, and
// declaring one and omitting the other is an ordinary typo.
//
// EACH ARM ASSERTS TWO THINGS: that it errors, and that the error does NOT name
// the reachable kubeconfig context — because an error mentioning it would mean
// the resolution had consulted the fallback this arm exists to refuse.
func TestResolve_PartialInClusterEnvironmentFailsLoud(t *testing.T) {
	dir := t.TempDir()
	kubeconfig := writeKubeconfig(t, dir, "alpha")

	cases := []struct {
		name  string
		env   map[string]string
		names string
	}{
		{
			name:  "host set, port absent",
			env:   map[string]string{"KUBERNETES_SERVICE_HOST": "10.0.0.1", "KUBECONFIG": kubeconfig},
			names: "KUBERNETES_SERVICE_PORT",
		},
		{
			name:  "host empty, port set",
			env:   map[string]string{"KUBERNETES_SERVICE_HOST": "", "KUBERNETES_SERVICE_PORT": "443", "KUBECONFIG": kubeconfig},
			names: "KUBERNETES_SERVICE_HOST",
		},
		{
			name:  "host set, port empty",
			env:   map[string]string{"KUBERNETES_SERVICE_HOST": "10.0.0.1", "KUBERNETES_SERVICE_PORT": "", "KUBECONFIG": kubeconfig},
			names: "KUBERNETES_SERVICE_PORT",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveCredential("", credentialSources{lookupEnv: fakeEnv(tc.env)})
			require.Error(t, err, "a half-declared in-cluster environment is bad input and must error")
			assert.Contains(t, err.Error(), tc.names, "the refusal names the missing half")
			assert.NotContains(t, err.Error(), "alpha",
				"the refusal must not name the reachable kubeconfig context; mentioning it would "+
					"mean the resolution consulted the fallback this arm exists to refuse")
		})
	}

	// SAME-RUN CONTROL: with BOTH names set the in-cluster arm is taken, and it
	// fails on the FILES rather than on the environment — a different refusal,
	// naming a path rather than a variable.
	_, err := resolveCredential("", credentialSources{
		lookupEnv: fakeEnv(map[string]string{
			"KUBERNETES_SERVICE_HOST": "10.0.0.1",
			"KUBERNETES_SERVICE_PORT": "443",
			"KUBECONFIG":              kubeconfig,
		}),
		inClusterTokenFile: filepath.Join(dir, "no-such-token"),
		inClusterCAFile:    filepath.Join(dir, "no-such-ca"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no-such-token", "with both names set the arm reaches the files")
	assert.NotContains(t, err.Error(), "half declared")

	// SECOND CONTROL: with NEITHER name set the kubeconfig resolves normally,
	// so the new gate has not swallowed the ordinary path.
	got, err := resolveCredential("", credentialSources{
		lookupEnv: fakeEnv(map[string]string{"KUBECONFIG": kubeconfig}),
	})
	require.NoError(t, err)
	assert.Equal(t, "alpha", got.ContextName)
}
