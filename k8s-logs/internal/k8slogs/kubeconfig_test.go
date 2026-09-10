// SPDX-License-Identifier: Apache-2.0

package k8slogs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/client-go/rest"
)

// kubeconfig_test.go — the two credential sources and the three failures
// between them.
//
// THE IN-CLUSTER ARM NEEDS A SEAM THIS MODULE OWNS, and that is a measured
// constraint rather than a preference: client-go's rest.InClusterConfig reads
// its token and CA from paths that are FUNCTION-LOCAL CONSTANTS, so no
// variable, option or temporary directory makes it read anything else. Since
// this module writes its own resolver, the loader is its own dependency and the
// arm becomes a known positive.

// writeKubeconfig writes a minimal kubeconfig holding the named contexts and
// points KUBECONFIG at it for the duration of the test.
func writeKubeconfig(t *testing.T, contexts ...string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("apiVersion: v1\nkind: Config\nclusters:\n")
	b.WriteString("- cluster:\n    server: https://127.0.0.1:1\n  name: c\n")
	b.WriteString("users:\n- name: u\n  user:\n    token: t\n")
	b.WriteString("contexts:\n")
	for _, name := range contexts {
		fmt.Fprintf(&b, "- context:\n    cluster: c\n    user: u\n  name: %s\n", name)
	}
	if len(contexts) > 0 {
		fmt.Fprintf(&b, "current-context: %s\n", contexts[0])
	}
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBECONFIG", path)
}

// TestNamedContextResolves is the known positive the refusals below rest on.
func TestNamedContextResolves(t *testing.T) {
	writeKubeconfig(t, "gke_p_r_main", "other")
	cfg, err := Resolver{}.Config("other")
	if err != nil {
		t.Fatalf("a context the kubeconfig holds was refused: %v", err)
	}
	if cfg == nil {
		t.Fatal("the resolver returned no configuration and no error")
	}
}

// TestAMissingNamedContextFailsLoudNamingIt — never a silent fall-through to
// the current context, which would collect the wrong cluster under the name the
// operator asked for.
func TestAMissingNamedContextFailsLoudNamingIt(t *testing.T) {
	writeKubeconfig(t, "gke_p_r_main")
	_, err := Resolver{}.Config("staging")
	if err == nil {
		t.Fatal("a context the kubeconfig does not hold resolved; client-go's own override falls through to " +
			"the current context, which collects a different cluster under the requested name")
	}
	if !strings.Contains(err.Error(), "staging") {
		t.Fatalf("the refusal does not name the context that is missing: %v", err)
	}
	if !strings.Contains(err.Error(), "gke_p_r_main") {
		t.Fatalf("the refusal does not name the contexts that ARE available: %v", err)
	}
}

// TestNoKubeconfigAndNotInClusterNamesBothModes.
func TestNoKubeconfigAndNotInClusterNamesBothModes(t *testing.T) {
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "does-not-exist"))
	r := Resolver{InCluster: func() (*rest.Config, error) {
		return nil, fmt.Errorf("no service account is mounted")
	}}
	_, err := r.Config("")
	if err == nil {
		t.Fatal("resolution succeeded with neither source available")
	}
	msg := err.Error()
	if !strings.Contains(msg, "kubeconfig") {
		t.Errorf("the combined refusal does not name the kubeconfig mode: %v", err)
	}
	if !strings.Contains(msg, "no service account is mounted") {
		t.Errorf("the combined refusal does not carry the in-cluster reason: %v", err)
	}
}

// TestInClusterConfigIsReturnedAndNotFallenThroughFrom is the KNOWN POSITIVE
// for the in-cluster arm, asserted on the returned configuration rather than on
// a network call.
func TestInClusterConfigIsReturnedAndNotFallenThroughFrom(t *testing.T) {
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "does-not-exist"))
	want := &rest.Config{Host: "https://in-cluster.example:443"}
	r := Resolver{InCluster: func() (*rest.Config, error) { return want, nil }}

	got, err := r.Config("")
	if err != nil {
		t.Fatalf("resolution failed with the in-cluster loader succeeding: %v", err)
	}
	if got.Host != want.Host {
		t.Fatalf("resolution returned the host %q, want the in-cluster loader's %q; "+
			"the kubeconfig arm must not win once it has failed", got.Host, want.Host)
	}
}

// TestInClusterFailureIsDistinguishableFromTheCombinedMessage. A deployed pod
// with a broken service-account mount otherwise reports the same text as a
// laptop with no kubeconfig, and an operator reaches for the wrong lever.
func TestInClusterFailureIsDistinguishableFromTheCombinedMessage(t *testing.T) {
	r := Resolver{InCluster: func() (*rest.Config, error) {
		return nil, fmt.Errorf("open /var/run/secrets: no such file or directory")
	}}
	_, err := r.InClusterConfig()
	if err == nil {
		t.Fatal("the in-cluster arm reported success with its loader failing")
	}
	if strings.Contains(err.Error(), "no kubeconfig") {
		t.Fatalf("the in-cluster refusal is the COMBINED message: %v", err)
	}
	if !strings.Contains(err.Error(), "in-cluster") {
		t.Fatalf("the in-cluster refusal does not say which mode failed: %v", err)
	}
	if !strings.Contains(err.Error(), EnvKubernetesServiceHost) {
		t.Fatalf("the in-cluster refusal does not name the variables its entry must list: %v", err)
	}

	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "does-not-exist"))
	_, combinedErr := r.Config("")
	if combinedErr == nil {
		t.Fatal("the combined arm reported success")
	}
	if combinedErr.Error() == err.Error() {
		t.Fatal("the two refusals are the same string; the control is that they are distinguishable")
	}
}

// TestParseKubeContext — the GKE shape, and the honest empty answer for every
// other provider.
func TestParseKubeContext(t *testing.T) {
	for _, tc := range []struct{ in, project, cluster string }{
		{"gke_fulminate-services_us-central1_main-us-central1", "fulminate-services", "main-us-central1"},
		{"gke_p_us-central1-a_c", "p", "c"},
		{"arn:aws:eks:us-east-1:1234:cluster/prod", "", ""},
		{"minikube", "", ""},
		{"gke_toofew", "", ""},
		{"", "", ""},
	} {
		project, cluster := ParseKubeContext(tc.in)
		if project != tc.project || cluster != tc.cluster {
			t.Errorf("ParseKubeContext(%q) = (%q, %q), want (%q, %q)", tc.in, project, cluster, tc.project, tc.cluster)
		}
	}
}

// TestAPodWithBrokenCredentialsGetsTheInClusterMessage — the routing that makes
// Resolver.InClusterConfig reachable from the walk.
//
// The method existed and named the right failure, and NOTHING CALLED IT: the
// collector's client always went through Config, which always produced the
// combined message. So the deployed pod with a broken service-account mount that
// the method's own doc names still read exactly like a laptop with no
// kubeconfig. Config now tells the two apart by the one thing that distinguishes
// them, the pod's own service-host variable.
func TestAPodWithBrokenCredentialsGetsTheInClusterMessage(t *testing.T) {
	broken := func() (*rest.Config, error) {
		return nil, fmt.Errorf("open /var/run/secrets: no such file or directory")
	}

	t.Run("in a pod", func(t *testing.T) {
		t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "does-not-exist"))
		t.Setenv(EnvKubernetesServiceHost, "10.0.0.1")
		_, err := Resolver{InCluster: broken}.Config("")
		if err == nil {
			t.Fatal("resolution succeeded with both sources failing")
		}
		if strings.Contains(err.Error(), "no credentials") {
			t.Fatalf("a pod whose service account is broken got the COMBINED message: %v", err)
		}
		if !strings.Contains(err.Error(), "service account") {
			t.Fatalf("the refusal does not point at the mount: %v", err)
		}
	})

	// THE CONTROL, same resolver and same failure, differing only in whether the
	// process is in a pod: a laptop still gets the combined message, so the
	// routing above is the service-host variable and not a blanket change.
	t.Run("not in a pod", func(t *testing.T) {
		t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "does-not-exist"))
		_, err := Resolver{InCluster: broken}.Config("")
		if err == nil {
			t.Fatal("resolution succeeded with both sources failing")
		}
		if !strings.Contains(err.Error(), "no credentials") {
			t.Fatalf("a process outside a pod did not get the combined message: %v", err)
		}
	})
}
