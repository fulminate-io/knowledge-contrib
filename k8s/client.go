// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// client.go — HOW A CREDENTIAL IS RESOLVED, and why every arm of it fails
// loudly rather than degrading.
//
// THE COLLECTOR IS HANDED NO CREDENTIAL. The client passes an id and this
// collector's params, and nothing else; the credential comes from the standard
// Kubernetes resolution, reading the environment the operator's config entry
// declared. That is the whole story, and it is why this file reads variables
// rather than accepting them as parameters.
//
// THE ORDER IS IN-CLUSTER FIRST, THEN KUBECONFIG. A process running inside a
// cluster has an unambiguous identity mounted for it; a kubeconfig found beside
// it would be a second, contradictory answer. Once the in-cluster environment
// is present this file NEVER falls through to a kubeconfig search: a pod whose
// service-account files are missing has a broken deployment, and silently
// authenticating as whoever's kubeconfig happens to be on the image is a worse
// outcome than a refusal that names the missing file.
//
// THIS MODULE OWNS THE IN-CLUSTER RESOLUTION RATHER THAN CALLING client-go's.
// That library's in-cluster loader declares its token and CA paths as
// function-local constants, with no parameter and no override, so a module
// calling it can construct only the NEGATIVE arm in a test and the positive arm
// has no venue in this project at all. Taking the two paths as parameters that
// default to exactly client-go's own values makes the positive arm its own
// known-positive at no cost to the production path.
//
// EVERY ENVIRONMENT READ GOES THROUGH credentialSources.lookupEnv, not
// os.Getenv. Two reasons: absent and empty must be told apart — this
// collector's whole credential story turns on an ABSENT HOME succeeding where a
// HOME pointing at a scratch directory fails — and a test must be able to
// present an environment the process is not actually running in.

// inClusterContextName is the context name reported for an in-cluster
// credential, which has no kubeconfig context to be named after.
const inClusterContextName = "in-cluster"

// client-go mounts these two paths into every pod with a service account. They
// are the DEFAULTS of the two parameters below, not constants, so a test can
// build the positive in-cluster arm.
const (
	defaultInClusterTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	defaultInClusterCAFile    = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
)

// credentialSources is where a resolution reads from. Its zero value reads an
// empty environment and the client-go default file paths, which is the shape a
// table-driven test wants; production uses [defaultCredentialSources].
type credentialSources struct {
	// lookupEnv reads one environment variable, reporting whether it was SET
	// as distinct from set-to-empty. Nil means an empty environment.
	lookupEnv func(string) (string, bool)
	// inClusterTokenFile and inClusterCAFile default to the paths a pod's
	// service account is mounted at.
	inClusterTokenFile string
	inClusterCAFile    string
}

// defaultCredentialSources is the production configuration: the real process
// environment and the real service-account mount paths.
func defaultCredentialSources() credentialSources {
	return credentialSources{
		lookupEnv:          os.LookupEnv,
		inClusterTokenFile: defaultInClusterTokenFile,
		inClusterCAFile:    defaultInClusterCAFile,
	}
}

// credential is one resolved way to reach a cluster.
type credential struct {
	// Config is the REST configuration to build clients from.
	Config *rest.Config
	// ContextName is the kubeconfig context this resolution selected, or
	// [inClusterContextName]. It is reported in errors and in node metadata so
	// an operator can tell which cluster a graph describes.
	ContextName string
}

func (s credentialSources) env(name string) (string, bool) {
	if s.lookupEnv == nil {
		return "", false
	}
	return s.lookupEnv(name)
}

func (s credentialSources) tokenFile() string {
	if s.inClusterTokenFile == "" {
		return defaultInClusterTokenFile
	}
	return s.inClusterTokenFile
}

func (s credentialSources) caFile() string {
	if s.inClusterCAFile == "" {
		return defaultInClusterCAFile
	}
	return s.inClusterCAFile
}

// resolveCredential resolves the credential this walk will use.
//
// contextName names a kubeconfig context. It is OPTIONAL: empty means the
// kubeconfig's currently selected context, which is what makes a collect
// against an operator's own default cluster take no parameters at all. A
// non-empty name that the kubeconfig does not carry is an ERROR naming both the
// name asked for and the names available; it is never quietly replaced by the
// current context.
func resolveCredential(contextName string, src credentialSources) (credential, error) {
	host, _ := src.env("KUBERNETES_SERVICE_HOST")
	port, _ := src.env("KUBERNETES_SERVICE_PORT")

	// EITHER NAME SELECTS THE IN-CLUSTER ARM, and a PARTIAL environment is an
	// ERROR rather than an absent one.
	//
	// THE VERSION THAT REQUIRED BOTH WAS A SILENT DEGRADE, measured: with
	// KUBERNETES_SERVICE_HOST set and the port missing, the resolution fell
	// through and authenticated against whatever kubeconfig was reachable,
	// returning no error and a completely different cluster's context. The
	// resulting graph looks entirely healthy under the id the operator asked
	// for.
	//
	// It is reachable because the config entry's env block IS the child's whole
	// environment, so WHICH of the two names arrives is decided by an
	// operator's entry rather than by the kubelet. An entry that declares one
	// and omits the other is an ordinary typo, and this repository's invariant
	// is that bad input errors: never silent coercion, default or degrade.
	if host != "" || port != "" {
		return resolveInCluster(contextName, host, port, src)
	}
	return resolveKubeconfig(contextName, src)
}

// resolveInCluster builds a credential from the service-account files a pod
// carries.
//
// IT NEVER FALLS THROUGH. Reaching this function means the in-cluster
// environment is present, so a missing or unreadable file is a broken pod, and
// the refusal names the file.
func resolveInCluster(contextName, host, port string, src credentialSources) (credential, error) {
	// THE PARTIAL-ENVIRONMENT REFUSAL. Reaching here means at least one of the
	// two names is set; both are required to address an API server, and the
	// refusal names the one that is missing so an operator can fix their entry
	// rather than debug a graph built against the wrong cluster.
	//
	// IT DELIBERATELY NAMES NEITHER THE KUBECONFIG NOR ANY CONTEXT. Mentioning
	// a reachable kubeconfig here would suggest the fallback this arm exists to
	// refuse.
	switch {
	case host == "" && port != "":
		return credential{}, fmt.Errorf(
			"the in-cluster environment is only half declared: KUBERNETES_SERVICE_PORT is set to %q "+
				"and KUBERNETES_SERVICE_HOST is missing or empty. Both are required to address the "+
				"API server. Declare both in this collector's env block, or neither", port)
	case host != "" && port == "":
		return credential{}, fmt.Errorf(
			"the in-cluster environment is only half declared: KUBERNETES_SERVICE_HOST is set to %q "+
				"and KUBERNETES_SERVICE_PORT is missing or empty. Both are required to address the "+
				"API server. Declare both in this collector's env block, or neither", host)
	}

	if contextName != "" {
		return credential{}, fmt.Errorf(
			"a kubeconfig context (%q) was named, but this process is running inside a cluster "+
				"(KUBERNETES_SERVICE_HOST is set) and has one mounted identity; "+
				"omit the context parameter, or unset the in-cluster environment", contextName)
	}

	tokenFile := src.tokenFile()
	token, err := os.ReadFile(tokenFile) //nolint:gosec // a mount path this module owns
	if err != nil {
		return credential{}, fmt.Errorf(
			"the in-cluster environment is set (KUBERNETES_SERVICE_HOST=%s) but the service-account "+
				"token at %s could not be read: %w", host, tokenFile, err)
	}
	caFile := src.caFile()
	if _, err := os.Stat(caFile); err != nil {
		return credential{}, fmt.Errorf(
			"the in-cluster environment is set (KUBERNETES_SERVICE_HOST=%s) but the cluster CA bundle "+
				"at %s could not be read: %w", host, caFile, err)
	}

	return credential{
		ContextName: inClusterContextName,
		Config: &rest.Config{
			Host:            "https://" + hostPort(host, port),
			BearerToken:     strings.TrimSpace(string(token)),
			BearerTokenFile: tokenFile,
			TLSClientConfig: rest.TLSClientConfig{CAFile: caFile},
		},
	}, nil
}

// hostPort joins a host and a port, bracketing a literal IPv6 host.
func hostPort(host, port string) string {
	if strings.Contains(host, ":") {
		return "[" + host + "]:" + port
	}
	return host + ":" + port
}

// resolveKubeconfig performs the standard kubeconfig resolution: KUBECONFIG if
// it is set, else HOME's .kube/config.
//
// IT SYNTHESIZES NOTHING. When neither variable is set the refusal names every
// input it looked for. That matters more than it looks: on a real GKE target an
// ABSENT HOME authenticates fine, while a HOME pointing at an empty scratch
// directory breaks the credential plugin — so a resolver that invented a home
// directory for a process whose HOME was deliberately omitted would convert a
// working configuration into a failing one.
func resolveKubeconfig(contextName string, src credentialSources) (credential, error) {
	path, from, ok := kubeconfigPath(src)
	if !ok {
		return credential{}, fmt.Errorf(
			"no Kubernetes credential could be resolved: KUBECONFIG is not set, HOME is not set " +
				"(so there is no ~/.kube/config to find), and KUBERNETES_SERVICE_HOST is not set " +
				"(so this process is not running inside a cluster). Declare the variables this " +
				"collector needs in its config entry's env block")
	}

	loaded, err := clientcmd.LoadFromFile(path)
	if err != nil {
		return credential{}, fmt.Errorf("reading the kubeconfig at %s (from %s): %w", path, from, err)
	}
	if err := checkContextExists(loaded, path, contextName); err != nil {
		return credential{}, err
	}

	overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}
	cfg := clientcmd.NewDefaultClientConfig(*loaded, overrides)
	restCfg, err := cfg.ClientConfig()
	if err != nil {
		return credential{}, fmt.Errorf(
			"building a client from the kubeconfig at %s (context %q): %w", path, resolvedContext(loaded, contextName), err)
	}
	return credential{Config: restCfg, ContextName: resolvedContext(loaded, contextName)}, nil
}

// kubeconfigPath returns the kubeconfig this resolution will read, and which
// variable it came from.
//
// KUBECONFIG may carry a LIST of paths separated by the platform's path list
// separator; the first entry that exists is taken, which is what the standard
// loader does with a list whose later entries are optional overlays.
func kubeconfigPath(src credentialSources) (path, from string, ok bool) {
	if v, set := src.env("KUBECONFIG"); set && v != "" {
		for _, candidate := range filepath.SplitList(v) {
			if candidate == "" {
				continue
			}
			if _, err := os.Stat(candidate); err == nil {
				return candidate, "KUBECONFIG", true
			}
		}
		// Every entry named and none of them present: report the first, so the
		// error names a path the operator wrote rather than a path we invented.
		if entries := filepath.SplitList(v); len(entries) > 0 && entries[0] != "" {
			return entries[0], "KUBECONFIG", true
		}
	}
	if home, set := src.env("HOME"); set && home != "" {
		return filepath.Join(home, ".kube", "config"), "HOME", true
	}
	return "", "", false
}

// checkContextExists refuses a named context the kubeconfig does not carry,
// naming the contexts it does. A collect that quietly fell back to the current
// context would collect the WRONG CLUSTER under the id the operator asked for,
// and the resulting graph would look entirely healthy.
func checkContextExists(cfg *clientcmdapi.Config, path, contextName string) error {
	if contextName == "" {
		if cfg.CurrentContext == "" {
			return fmt.Errorf(
				"the kubeconfig at %s names no current context and this collect named none; "+
					"pass the context parameter, or select one with kubectl config use-context", path)
		}
		return nil
	}
	if _, ok := cfg.Contexts[contextName]; ok {
		return nil
	}
	available := make([]string, 0, len(cfg.Contexts))
	for name := range cfg.Contexts {
		available = append(available, name)
	}
	sort.Strings(available)
	return fmt.Errorf(
		"the kubeconfig at %s carries no context named %q; it carries: %s",
		path, contextName, strings.Join(available, ", "))
}

// resolvedContext reports which context a resolution actually selected.
func resolvedContext(cfg *clientcmdapi.Config, contextName string) string {
	if contextName != "" {
		return contextName
	}
	return cfg.CurrentContext
}
