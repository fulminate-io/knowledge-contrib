// SPDX-License-Identifier: Apache-2.0

package k8slogs

import (
	"fmt"
	"os"
	"strings"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// kubeconfig.go — where this collector's credentials come from, and nowhere
// else.
//
// TWO SOURCES, IN ORDER: the kubeconfig resolved by client-go's own default
// loading rules, then the in-cluster service account. The client that spawns
// this collector passes NO credential of any kind; everything below is resolved
// from the environment variables the collector's config entry lists and the
// files they point at.
//
// THE IN-CLUSTER LOADER IS A FIELD RATHER THAN A DIRECT CALL, and that is the
// one structural difference from the in-tree helper this mirrors. client-go's
// rest.InClusterConfig reads its token and CA from paths that are FUNCTION-LOCAL
// CONSTANTS — not variables, not options — so there is no seam anywhere in
// client-go to make the in-cluster arm observable. Writing a token into a
// temporary directory does not help: the function still opens
// /var/run/secrets/kubernetes.io/serviceaccount/token. Since this module owns
// its resolver anyway, the loader is its own dependency and the arm becomes a
// known positive a test can supply.

// InClusterLoader returns the in-cluster configuration, or the reason it could
// not. It defaults to rest.InClusterConfig.
type InClusterLoader func() (*rest.Config, error)

// Resolver resolves a kubecontext name to a client-go configuration.
type Resolver struct {
	// InCluster is the second source. Nil means rest.InClusterConfig.
	InCluster InClusterLoader
}

// Config returns the configuration for contextName, or an error naming the
// failure of BOTH sources.
//
// A NAMED CONTEXT THAT IS NOT IN THE KUBECONFIG FAILS LOUDLY rather than
// falling through to the current context. Silently collecting the wrong
// cluster's logs under the name the operator asked for is the failure this
// refusal exists to prevent, and client-go's own override does exactly that
// fall-through when asked for a context it cannot find — so the membership
// check below is made HERE, before the override is applied, rather than left to
// the loader.
//
// AN EMPTY contextName MEANS THE KUBECONFIG'S CURRENT CONTEXT. That is not a
// fall-through: it is the operator declining to name one.
func (r Resolver) Config(contextName string) (*rest.Config, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if contextName != "" {
		if err := r.assertContextExists(rules, contextName); err != nil {
			return nil, err
		}
	}
	overrides := &clientcmd.ConfigOverrides{}
	if contextName != "" {
		overrides.CurrentContext = contextName
	}

	cfg, kubeErr := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	if kubeErr == nil {
		return cfg, nil
	}

	cfg, inClusterErr := r.inCluster()
	if inClusterErr == nil {
		return cfg, nil
	}

	// THE PROCESS IS IN A POD AND ITS OWN CREDENTIALS FAILED, so the combined
	// message is the wrong one to give: a deployed pod with a broken
	// service-account mount would otherwise read exactly like a laptop with no
	// kubeconfig, and an operator reaches for the wrong lever. The two variables
	// the pod's own environment carries are how this process knows which of the
	// two situations it is in.
	if _, inPod := os.LookupEnv(EnvKubernetesServiceHost); inPod {
		return nil, inClusterFailure(inClusterErr)
	}
	return nil, fmt.Errorf(
		"k8s-logs: no credentials: the kubeconfig did not resolve (%v) and this process is not in-cluster (%v); "+
			"list KUBECONFIG or HOME in this collector's config entry, or run it inside a cluster",
		kubeErr, inClusterErr)
}

// InClusterConfig returns the in-cluster configuration alone, naming the
// in-cluster failure SPECIFICALLY rather than as half of a combined message.
//
// It exists because the combined message above is unreadable in the case that
// matters most: a pod whose service-account mount is broken produces the SAME
// text as a laptop with no kubeconfig, and an operator reading it reaches for
// the wrong lever. A caller that knows it is in-cluster calls this.
func (r Resolver) InClusterConfig() (*rest.Config, error) {
	cfg, err := r.inCluster()
	if err != nil {
		return nil, inClusterFailure(err)
	}
	return cfg, nil
}

// inClusterFailure is the in-cluster-specific refusal, spelled once because
// [Resolver.Config] returns it too when this process is running in a pod.
func inClusterFailure(err error) error {
	return fmt.Errorf(
		"k8s-logs: the in-cluster credentials did not resolve (%v); "+
			"a pod reaches the apiserver through its mounted service account, so check the mount and "+
			"that "+EnvKubernetesServiceHost+" and "+EnvKubernetesServicePort+
			" are listed in this collector's config entry",
		err)
}

// inCluster calls the injected loader, defaulting to client-go's.
func (r Resolver) inCluster() (*rest.Config, error) {
	if r.InCluster != nil {
		return r.InCluster()
	}
	return rest.InClusterConfig()
}

// assertContextExists refuses a named context the kubeconfig does not hold.
func (r Resolver) assertContextExists(rules clientcmd.ClientConfigLoader, contextName string) error {
	raw, err := rules.Load()
	if err != nil {
		return fmt.Errorf("k8s-logs: the kubeconfig could not be read while looking for context %q: %w", contextName, err)
	}
	if _, ok := raw.Contexts[contextName]; ok {
		return nil
	}
	known := make([]string, 0, len(raw.Contexts))
	for name := range raw.Contexts {
		known = append(known, name)
	}
	return fmt.Errorf(
		"k8s-logs: the kubeconfig holds no context named %q; it holds %s",
		contextName, describeKnownContexts(known))
}

// describeKnownContexts renders the available context names for a refusal, and
// says so plainly when there are none.
func describeKnownContexts(known []string) string {
	if len(known) == 0 {
		return "no contexts at all"
	}
	sortStrings(known)
	return strings.Join(known, ", ")
}

// Clientset builds a typed client from a configuration.
func Clientset(cfg *rest.Config) (kubernetes.Interface, error) {
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("k8s-logs: building a Kubernetes client: %w", err)
	}
	return cs, nil
}

// ParseKubeContext pulls the cloud project and cluster name out of a GKE
// context name, which is spelled `gke_<project>_<location>_<cluster>`.
//
// A context from any other provider yields two empty strings, and that is the
// answer rather than a failure: EKS, AKS and on-prem contexts encode nothing,
// so the cluster labels are simply absent from those streams.
func ParseKubeContext(name string) (project, cluster string) {
	const prefix = "gke_"
	if !strings.HasPrefix(name, prefix) {
		return "", ""
	}
	parts := strings.Split(name[len(prefix):], "_")
	// Project, location and cluster at least. A location may itself hold
	// underscores, so the cluster is taken from the END rather than by index.
	if len(parts) < 3 {
		return "", ""
	}
	return parts[0], parts[len(parts)-1]
}
