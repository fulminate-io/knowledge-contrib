// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// walk_network.go — Services, EndpointSlices, Ingresses and NetworkPolicies.
//
// THREE OF THESE FOUR CARRY A GATE A NAIVE FIXTURE WOULD NEVER CONSTRUCT, and
// each is commented at the gate rather than in a table somewhere else:
// HAS_ENDPOINT_SLICE needs the service-name label, BACKS needs a targetRef that
// names a Pod, and the two NetworkPolicy derivations need a policyTypes value
// whose ABSENCE is not the same as its being empty.

func walkServices(ctx context.Context, c clientBundle, w *walkResult) error {
	list, err := c.typed.CoreV1().Services("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return listErr(w, "Service", err)
	}
	noteContinue(w, "Service", list.Continue)
	for i := range list.Items {
		s := &list.Items[i]
		extra := map[string]string{
			"service_type": string(s.Spec.Type),
			"cluster_ip":   s.Spec.ClusterIP,
		}
		// The selector is carried as its own metadata key so the SELECTS
		// derivation reads a parsed value rather than re-deriving one from the
		// object body.
		if len(s.Spec.Selector) > 0 {
			encoded, err := json.Marshal(s.Spec.Selector)
			if err != nil {
				return fmt.Errorf("service %s selector: %w", resourceID(s.Namespace, "Service", s.Name), err)
			}
			extra["selector"] = string(encoded)
		}
		// The load-balancer ingress addresses feed the EXPOSED_BY family.
		if addrs := serviceLoadBalancerAddresses(s); len(addrs) > 0 {
			extra["lb_ingress"] = strings.Join(addrs, ",")
		}
		if err := add(w, metaOf(s.ObjectMeta), "Service", extra, s); err != nil {
			return err
		}
	}
	return nil
}

// serviceLoadBalancerAddresses renders each load-balancer ingress entry as a
// labeled address. The label is kept because the EXPOSED_BY family records
// WHICH lookup key matched, which is what lets an operator audit a mis-linkage
// without re-running the resolution.
func serviceLoadBalancerAddresses(s *corev1.Service) []string {
	var out []string
	for _, ing := range s.Status.LoadBalancer.Ingress {
		if ing.IP != "" {
			out = append(out, "ip="+ing.IP)
		}
		if ing.Hostname != "" {
			out = append(out, "hostname="+ing.Hostname)
		}
	}
	return out
}

func walkEndpointSlices(ctx context.Context, c clientBundle, w *walkResult) error {
	list, err := c.typed.DiscoveryV1().EndpointSlices("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return listErr(w, "EndpointSlice", err)
	}
	noteContinue(w, "EndpointSlice", list.Continue)
	for i := range list.Items {
		es := &list.Items[i]
		if err := add(w, metaOf(es.ObjectMeta), "EndpointSlice", map[string]string{
			"address_type": string(es.AddressType),
		}, es); err != nil {
			return err
		}
		id := resourceID(es.Namespace, "EndpointSlice", es.Name)

		// HAS_ENDPOINT_SLICE: a slice belongs to the Service its
		// kubernetes.io/service-name label names. A slice WITHOUT that label
		// belongs to no Service and gets no edge; that is the gate, and a
		// fixture omitting the label must produce nothing.
		if svc := es.Labels[discoveryv1.LabelServiceName]; svc != "" {
			w.addEdges(Edge{
				FromID: resourceID(es.Namespace, "Service", svc),
				ToID:   id,
				Type:   edgeHasEndpointSlice,
			})
		}

		// BACKS: an endpoint backs the Pod its targetRef names. An endpoint
		// with a nil targetRef, or one naming anything other than a Pod, backs
		// nothing this graph holds and gets no edge.
		for j := range es.Endpoints {
			ep := &es.Endpoints[j]
			if ep.TargetRef == nil || ep.TargetRef.Kind != "Pod" || ep.TargetRef.Name == "" {
				continue
			}
			ns := ep.TargetRef.Namespace
			if ns == "" {
				ns = es.Namespace
			}
			w.addEdges(Edge{
				FromID: id,
				ToID:   resourceID(ns, "Pod", ep.TargetRef.Name),
				Type:   edgeBacks,
				Method: endpointCondition(ep),
			})
		}
	}
	return nil
}

// endpointCondition names the endpoint's readiness on the edge, so a consumer
// can tell a serving backend from one that is draining without re-reading the
// slice body.
func endpointCondition(ep *discoveryv1.Endpoint) string {
	switch {
	case ep.Conditions.Terminating != nil && *ep.Conditions.Terminating:
		return "terminating"
	case ep.Conditions.Ready != nil && !*ep.Conditions.Ready:
		return "not-ready"
	case ep.Conditions.Serving != nil && *ep.Conditions.Serving:
		return "serving"
	default:
		return "ready"
	}
}

func walkIngresses(ctx context.Context, c clientBundle, w *walkResult) error {
	list, err := c.typed.NetworkingV1().Ingresses("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return listErr(w, "Ingress", err)
	}
	noteContinue(w, "Ingress", list.Continue)
	for i := range list.Items {
		ing := &list.Items[i]
		extra := map[string]string{}
		if ing.Spec.IngressClassName != nil {
			extra["ingress_class"] = *ing.Spec.IngressClassName
		}
		if addrs := ingressLoadBalancerAddresses(ing); len(addrs) > 0 {
			extra["lb_ingress"] = strings.Join(addrs, ",")
		}
		if err := add(w, metaOf(ing.ObjectMeta), "Ingress", extra, ing); err != nil {
			return err
		}
		id := resourceID(ing.Namespace, "Ingress", ing.Name)
		for _, backend := range ingressBackends(ing) {
			w.addEdges(Edge{
				FromID: id,
				ToID:   resourceID(ing.Namespace, "Service", backend),
				Type:   edgeRoutesTo,
			})
		}
	}
	return nil
}

func ingressLoadBalancerAddresses(ing *networkingv1.Ingress) []string {
	var out []string
	for _, lb := range ing.Status.LoadBalancer.Ingress {
		if lb.IP != "" {
			out = append(out, "ip="+lb.IP)
		}
		if lb.Hostname != "" {
			out = append(out, "hostname="+lb.Hostname)
		}
	}
	return out
}

// ingressBackends collects the distinct Service names an Ingress routes to,
// from its default backend and from every path of every rule.
func ingressBackends(ing *networkingv1.Ingress) []string {
	seen := map[string]bool{}
	var out []string
	appendService := func(b *networkingv1.IngressBackend) {
		if b == nil || b.Service == nil || b.Service.Name == "" || seen[b.Service.Name] {
			return
		}
		seen[b.Service.Name] = true
		out = append(out, b.Service.Name)
	}
	appendService(ing.Spec.DefaultBackend)
	for i := range ing.Spec.Rules {
		http := ing.Spec.Rules[i].HTTP
		if http == nil {
			continue
		}
		for j := range http.Paths {
			appendService(&http.Paths[j].Backend)
		}
	}
	return out
}

func walkNetworkPolicies(ctx context.Context, c clientBundle, w *walkResult) error {
	list, err := c.typed.NetworkingV1().NetworkPolicies("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return listErr(w, "NetworkPolicy", err)
	}
	noteContinue(w, "NetworkPolicy", list.Continue)
	for i := range list.Items {
		np := &list.Items[i]
		extra := map[string]string{}
		if len(np.Spec.PodSelector.MatchLabels) > 0 {
			encoded, err := json.Marshal(np.Spec.PodSelector.MatchLabels)
			if err != nil {
				return fmt.Errorf("network policy %s selector: %w",
					resourceID(np.Namespace, "NetworkPolicy", np.Name), err)
			}
			extra["pod_selector"] = string(encoded)
		}
		// policy_types is carried ONLY WHEN THE OBJECT DECLARES IT, because an
		// ABSENT policyTypes is not an empty one: Kubernetes reads an absent
		// value as "ingress, and egress too if there are egress rules", and
		// writing an empty string here would let the derivation read it as "no
		// directions at all".
		if len(np.Spec.PolicyTypes) > 0 {
			types := make([]string, 0, len(np.Spec.PolicyTypes))
			for _, t := range np.Spec.PolicyTypes {
				types = append(types, string(t))
			}
			extra["policy_types"] = strings.Join(types, ",")
		}
		// The implicit-egress rule the derivation needs when policy_types is
		// absent: egress is in effect exactly when the spec carries egress
		// rules.
		if len(np.Spec.Egress) > 0 {
			extra["has_egress_rules"] = "true"
		}
		if err := add(w, metaOf(np.ObjectMeta), "NetworkPolicy", extra, np); err != nil {
			return err
		}
	}
	return nil
}
