// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"fmt"
	"strconv"

	computepb "cloud.google.com/go/compute/apiv1/computepb"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpcontent"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
)

// loadbalancer.go — the five resources one HTTP load balancer is assembled from,
// and the chain of edges that lets a reader walk it end to end:
//
//	forwardingRule --ROUTES_TO--> targetHttp(s)Proxy --PROXIES_FROM--> urlMap
//	urlMap --TARGETS--> backendService --TARGETS--> instanceGroup
//	targetHttpsProxy --USES_CERT--> sslCertificate
//	securityPolicy --PROTECTS--> backendService
//
// The forwarding rule's address is also what the DNS resolver joins a published
// record onto, which is why its content carries the address rather than only its
// metadata.

// ForwardingRules enumerates the project's forwarding rules.
func ForwardingRules(list Lister[*computepb.ForwardingRule]) Subcollector {
	return New("gcp-forwarding-rules", list, convertForwardingRule)
}

func convertForwardingRule(_ string, rule *computepb.ForwardingRule) (gcpgraph.Result, error) {
	if rule == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil forwarding rule")
	}
	selfLink := rule.GetSelfLink()
	if selfLink == "" {
		return gcpgraph.Result{}, fmt.Errorf("forwarding rule %q carries no self link", rule.GetName())
	}

	content := gcpcontent.ForwardingRule{
		Name:       rule.GetName(),
		SelfLink:   selfLink,
		IPAddress:  rule.GetIPAddress(),
		IPProtocol: rule.GetIPProtocol(),
		PortRange:  rule.GetPortRange(),
		Target:     rule.GetTarget(),
		Network:    rule.GetNetwork(),
		Subnetwork: rule.GetSubnetwork(),
		Region:     gcpgraph.LastSegment(rule.GetRegion()),
	}
	raw, err := gcpcontent.Marshal(content)
	if err != nil {
		return gcpgraph.Result{}, err
	}

	metadata := map[string]string{}
	setIfNotEmpty(metadata, "ip_address", content.IPAddress)
	setIfNotEmpty(metadata, "ip_protocol", content.IPProtocol)
	setIfNotEmpty(metadata, "port_range", content.PortRange)
	setIfNotEmpty(metadata, "load_balancing_scheme", rule.GetLoadBalancingScheme())

	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID:           selfLink,
		Name:         rule.GetName(),
		ResourceType: gcpgraph.ResourceTypeForwardingRule,
		Region:       content.Region,
		Content:      raw,
		Metadata:     metadata,
	}}}
	// A rule points at EITHER a target proxy OR a backend service directly,
	// depending on the load balancer's kind, so both are read and neither is
	// assumed.
	for _, target := range []string{rule.GetTarget(), rule.GetBackendService()} {
		if target != "" {
			out.Relations = append(out.Relations, gcpgraph.Relation{
				From: selfLink, To: target, Type: gcpgraph.EdgeRoutesTo,
			})
		}
	}
	return out, nil
}

// TargetHTTPProxies enumerates the project's HTTP target proxies.
func TargetHTTPProxies(list Lister[*computepb.TargetHttpProxy]) Subcollector {
	return New("gcp-target-http-proxies", list, convertTargetHTTPProxy)
}

func convertTargetHTTPProxy(_ string, proxy *computepb.TargetHttpProxy) (gcpgraph.Result, error) {
	if proxy == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil target HTTP proxy")
	}
	selfLink := proxy.GetSelfLink()
	if selfLink == "" {
		return gcpgraph.Result{}, fmt.Errorf("target HTTP proxy %q carries no self link", proxy.GetName())
	}
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: proxy.GetName(), SelfLink: selfLink, Description: proxy.GetDescription(),
		CreateTime: proxy.GetCreationTimestamp(),
		Fields:     nonEmptyFields(map[string]string{"urlMap": proxy.GetUrlMap()}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: selfLink, Name: proxy.GetName(), ResourceType: gcpgraph.ResourceTypeTargetHTTPProxy,
		Region: gcpgraph.LastSegment(proxy.GetRegion()), Content: raw,
	}}}
	if urlMap := proxy.GetUrlMap(); urlMap != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: selfLink, To: urlMap, Type: gcpgraph.EdgeProxiesFrom,
		})
	}
	return out, nil
}

// TargetHTTPSProxies enumerates the project's HTTPS target proxies.
func TargetHTTPSProxies(list Lister[*computepb.TargetHttpsProxy]) Subcollector {
	return New("gcp-target-https-proxies", list, convertTargetHTTPSProxy)
}

func convertTargetHTTPSProxy(_ string, proxy *computepb.TargetHttpsProxy) (gcpgraph.Result, error) {
	if proxy == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil target HTTPS proxy")
	}
	selfLink := proxy.GetSelfLink()
	if selfLink == "" {
		return gcpgraph.Result{}, fmt.Errorf("target HTTPS proxy %q carries no self link", proxy.GetName())
	}
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: proxy.GetName(), SelfLink: selfLink, Description: proxy.GetDescription(),
		CreateTime: proxy.GetCreationTimestamp(),
		Fields:     nonEmptyFields(map[string]string{"urlMap": proxy.GetUrlMap()}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: selfLink, Name: proxy.GetName(), ResourceType: gcpgraph.ResourceTypeTargetHTTPSProxy,
		Region: gcpgraph.LastSegment(proxy.GetRegion()), Content: raw,
		Metadata: map[string]string{"certificate_count": strconv.Itoa(len(proxy.GetSslCertificates()))},
	}}}
	if urlMap := proxy.GetUrlMap(); urlMap != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: selfLink, To: urlMap, Type: gcpgraph.EdgeProxiesFrom,
		})
	}
	for _, cert := range proxy.GetSslCertificates() {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: selfLink, To: cert, Type: gcpgraph.EdgeUsesCert,
		})
	}
	return out, nil
}

// URLMaps enumerates the project's URL maps.
func URLMaps(list Lister[*computepb.UrlMap]) Subcollector {
	return New("gcp-url-maps", list, convertURLMap)
}

func convertURLMap(_ string, urlMap *computepb.UrlMap) (gcpgraph.Result, error) {
	if urlMap == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil URL map")
	}
	selfLink := urlMap.GetSelfLink()
	if selfLink == "" {
		return gcpgraph.Result{}, fmt.Errorf("URL map %q carries no self link", urlMap.GetName())
	}
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: urlMap.GetName(), SelfLink: selfLink, Description: urlMap.GetDescription(),
		CreateTime: urlMap.GetCreationTimestamp(),
		Fields:     nonEmptyFields(map[string]string{"defaultService": urlMap.GetDefaultService()}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: selfLink, Name: urlMap.GetName(), ResourceType: gcpgraph.ResourceTypeURLMap,
		Region: gcpgraph.LastSegment(urlMap.GetRegion()), Content: raw,
	}}}

	// EVERY backend a map can route to, deduped: the default service, each path
	// matcher's default, and each path and route rule's service. A map that only
	// recorded its default would hide the routes that actually matter.
	seen := map[string]bool{}
	addTarget := func(service string) {
		if service == "" || seen[service] {
			return
		}
		seen[service] = true
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: selfLink, To: service, Type: gcpgraph.EdgeTargets,
		})
	}
	addTarget(urlMap.GetDefaultService())
	for _, matcher := range urlMap.GetPathMatchers() {
		addTarget(matcher.GetDefaultService())
		for _, rule := range matcher.GetPathRules() {
			addTarget(rule.GetService())
		}
		for _, rule := range matcher.GetRouteRules() {
			addTarget(rule.GetService())
		}
	}
	return out, nil
}

// BackendServices enumerates the project's backend services.
func BackendServices(list Lister[*computepb.BackendService]) Subcollector {
	return New("gcp-backend-services", list, convertBackendService)
}

func convertBackendService(_ string, svc *computepb.BackendService) (gcpgraph.Result, error) {
	if svc == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil backend service")
	}
	selfLink := svc.GetSelfLink()
	if selfLink == "" {
		return gcpgraph.Result{}, fmt.Errorf("backend service %q carries no self link", svc.GetName())
	}
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: svc.GetName(), SelfLink: selfLink, Description: svc.GetDescription(),
		CreateTime: svc.GetCreationTimestamp(),
		Fields: nonEmptyFields(map[string]string{
			"protocol":       svc.GetProtocol(),
			"securityPolicy": svc.GetSecurityPolicy(),
		}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	metadata := map[string]string{"backend_count": strconv.Itoa(len(svc.GetBackends()))}
	setIfNotEmpty(metadata, "protocol", svc.GetProtocol())
	setIfNotEmpty(metadata, "load_balancing_scheme", svc.GetLoadBalancingScheme())

	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: selfLink, Name: svc.GetName(), ResourceType: gcpgraph.ResourceTypeBackendService,
		Region: gcpgraph.LastSegment(svc.GetRegion()), Content: raw, Metadata: metadata,
	}}}
	for _, backend := range svc.GetBackends() {
		if group := backend.GetGroup(); group != "" {
			out.Relations = append(out.Relations, gcpgraph.Relation{
				From: selfLink, To: group, Type: gcpgraph.EdgeTargets,
			})
		}
	}
	// The edge runs FROM the policy: a policy protects a service, and the
	// security-policy enumeration may not have been permitted, so the service
	// carries the edge whether or not that node was collected.
	if policy := svc.GetSecurityPolicy(); policy != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: policy, To: selfLink, Type: gcpgraph.EdgeProtects,
		})
	}
	return out, nil
}

// SSLCertificates enumerates the project's SSL certificates.
func SSLCertificates(list Lister[*computepb.SslCertificate]) Subcollector {
	return New("gcp-ssl-certificates", list, convertSSLCertificate)
}

func convertSSLCertificate(_ string, cert *computepb.SslCertificate) (gcpgraph.Result, error) {
	if cert == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil SSL certificate")
	}
	selfLink := cert.GetSelfLink()
	if selfLink == "" {
		return gcpgraph.Result{}, fmt.Errorf("SSL certificate %q carries no self link", cert.GetName())
	}
	// The certificate BODY and the private key are deliberately not stored. The
	// public certificate is not a secret, but a collector that copied key
	// material into a graph would be a credential-disclosure path the moment the
	// graph is shared, and nothing downstream needs it.
	fields := nonEmptyFields(map[string]string{
		"expireTime": cert.GetExpireTime(),
		"type":       cert.GetType(),
		"status":     cert.GetManaged().GetStatus(),
	})
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: cert.GetName(), SelfLink: selfLink, Description: cert.GetDescription(),
		CreateTime: cert.GetCreationTimestamp(), Fields: fields,
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	metadata := map[string]string{
		"domain_count": strconv.Itoa(len(cert.GetSubjectAlternativeNames())),
	}
	setIfNotEmpty(metadata, "expire_time", cert.GetExpireTime())
	setIfNotEmpty(metadata, "cert_type", cert.GetType())

	return gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: selfLink, Name: cert.GetName(), ResourceType: gcpgraph.ResourceTypeSSLCertificate,
		Region: gcpgraph.LastSegment(cert.GetRegion()), Content: raw, Metadata: metadata,
	}}}, nil
}

// SecurityPolicies enumerates the project's edge security policies.
func SecurityPolicies(list Lister[*computepb.SecurityPolicy]) Subcollector {
	return New("gcp-security-policies", list, convertSecurityPolicy)
}

func convertSecurityPolicy(_ string, policy *computepb.SecurityPolicy) (gcpgraph.Result, error) {
	if policy == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil security policy")
	}
	selfLink := policy.GetSelfLink()
	if selfLink == "" {
		return gcpgraph.Result{}, fmt.Errorf("security policy %q carries no self link", policy.GetName())
	}
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: policy.GetName(), SelfLink: selfLink, Description: policy.GetDescription(),
		CreateTime: policy.GetCreationTimestamp(), Labels: policy.GetLabels(),
		Fields: nonEmptyFields(map[string]string{"type": policy.GetType()}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	metadata := map[string]string{"rule_count": strconv.Itoa(len(policy.GetRules()))}
	setIfNotEmpty(metadata, "policy_type", policy.GetType())

	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: selfLink, Name: policy.GetName(), ResourceType: gcpgraph.ResourceTypeSecurityPolicy,
		Region: gcpgraph.LastSegment(policy.GetRegion()), Content: raw, Metadata: metadata,
	}}}
	for _, assoc := range policy.GetAssociations() {
		if target := assoc.GetAttachmentId(); target != "" {
			out.Relations = append(out.Relations, gcpgraph.Relation{
				From: selfLink, To: target, Type: gcpgraph.EdgeProtects,
			})
		}
	}
	return out, nil
}

// nonEmptyFields drops empty values so a stored content document does not carry
// keys whose only meaning is "the API did not say".
func nonEmptyFields(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		if v != "" {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
