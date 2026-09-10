// SPDX-License-Identifier: Apache-2.0

package gcpclients

import (
	"context"

	compute "cloud.google.com/go/compute/apiv1"
	computepb "cloud.google.com/go/compute/apiv1/computepb"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/collect"
)

// compute_listers.go — the compute enumerations.
//
// COMPUTE SPLITS ITS LISTS TWO WAYS and both are represented. A GLOBAL resource
// lists directly; a ZONAL or REGIONAL one lists AGGREGATED, one entry per scope.
//
// BOTH FORMS REPORT A PARTIAL READ THE SAME WAY: a warning whose code is
// UNREACHABLE or PARTIAL_SUCCESS, on the scope's entry in the aggregated form
// and on the page in the flat one. Neither is an error, so a drain that did not
// read the warning would return a clean nil having not seen part of the project.
// Every list below therefore carries a signal, and the flat ones read it off the
// iterator's own page response.

// scopedItems builds the projection drainScoped takes: the scope's items, and
// the scope's key when the provider said it could not read that scope.
func scopedItems[T any](key string, items []T, warning *computepb.Warning) ([]T, string) {
	if len(computeWarningScopes(warning)) == 0 {
		return items, ""
	}
	return items, key
}

func (c *clients) computeSubcollectors() []collect.Subcollector {
	return []collect.Subcollector{
		collect.ComputeInstances(func(ctx context.Context, project string) ([]*computepb.Instance, error) {
			it := c.instances.AggregatedList(ctx, &computepb.AggregatedListInstancesRequest{Project: project})
			return drainScoped(it.Next, func(pair compute.InstancesScopedListPair) ([]*computepb.Instance, string) {
				return scopedItems(pair.Key, pair.Value.GetInstances(), pair.Value.GetWarning())
			})
		}),
		collect.Networks(func(ctx context.Context, project string) ([]*computepb.Network, error) {
			it := c.networks.List(ctx, &computepb.ListNetworksRequest{Project: project})
			return drain(it.Next, computeWarningSignal[*computepb.NetworkList](func() any { return it.Response }))
		}),
		collect.Subnetworks(func(ctx context.Context, project string) ([]*computepb.Subnetwork, error) {
			it := c.subnetworks.AggregatedList(ctx, &computepb.AggregatedListSubnetworksRequest{Project: project})
			return drainScoped(it.Next, func(pair compute.SubnetworksScopedListPair) ([]*computepb.Subnetwork, string) {
				return scopedItems(pair.Key, pair.Value.GetSubnetworks(), pair.Value.GetWarning())
			})
		}),
		collect.Firewalls(func(ctx context.Context, project string) ([]*computepb.Firewall, error) {
			it := c.firewalls.List(ctx, &computepb.ListFirewallsRequest{Project: project})
			return drain(it.Next, computeWarningSignal[*computepb.FirewallList](func() any { return it.Response }))
		}),
		collect.Disks(func(ctx context.Context, project string) ([]*computepb.Disk, error) {
			it := c.disks.AggregatedList(ctx, &computepb.AggregatedListDisksRequest{Project: project})
			return drainScoped(it.Next, func(pair compute.DisksScopedListPair) ([]*computepb.Disk, string) {
				return scopedItems(pair.Key, pair.Value.GetDisks(), pair.Value.GetWarning())
			})
		}),
		collect.ForwardingRules(func(ctx context.Context, project string) ([]*computepb.ForwardingRule, error) {
			it := c.forwardingRules.List(ctx, &computepb.ListGlobalForwardingRulesRequest{Project: project})
			return drain(it.Next,
				computeWarningSignal[*computepb.ForwardingRuleList](func() any { return it.Response }))
		}),
		collect.TargetHTTPProxies(func(ctx context.Context, project string) ([]*computepb.TargetHttpProxy, error) {
			it := c.targetHTTPProxies.List(ctx, &computepb.ListTargetHttpProxiesRequest{Project: project})
			return drain(it.Next,
				computeWarningSignal[*computepb.TargetHttpProxyList](func() any { return it.Response }))
		}),
		collect.TargetHTTPSProxies(func(ctx context.Context, project string) ([]*computepb.TargetHttpsProxy, error) {
			it := c.targetHTTPSProxies.List(ctx, &computepb.ListTargetHttpsProxiesRequest{Project: project})
			return drain(it.Next,
				computeWarningSignal[*computepb.TargetHttpsProxyList](func() any { return it.Response }))
		}),
		collect.URLMaps(func(ctx context.Context, project string) ([]*computepb.UrlMap, error) {
			it := c.urlMaps.List(ctx, &computepb.ListUrlMapsRequest{Project: project})
			return drain(it.Next, computeWarningSignal[*computepb.UrlMapList](func() any { return it.Response }))
		}),
		collect.BackendServices(func(ctx context.Context, project string) ([]*computepb.BackendService, error) {
			it := c.backendServices.List(ctx, &computepb.ListBackendServicesRequest{Project: project})
			return drain(it.Next,
				computeWarningSignal[*computepb.BackendServiceList](func() any { return it.Response }))
		}),
		collect.SecurityPolicies(func(ctx context.Context, project string) ([]*computepb.SecurityPolicy, error) {
			it := c.securityPolicies.List(ctx, &computepb.ListSecurityPoliciesRequest{Project: project})
			return drain(it.Next,
				computeWarningSignal[*computepb.SecurityPolicyList](func() any { return it.Response }))
		}),
		collect.SSLCertificates(func(ctx context.Context, project string) ([]*computepb.SslCertificate, error) {
			it := c.sslCertificates.List(ctx, &computepb.ListSslCertificatesRequest{Project: project})
			return drain(it.Next,
				computeWarningSignal[*computepb.SslCertificateList](func() any { return it.Response }))
		}),
		collect.Routers(func(ctx context.Context, project string) ([]*computepb.Router, error) {
			it := c.routers.AggregatedList(ctx, &computepb.AggregatedListRoutersRequest{Project: project})
			return drainScoped(it.Next, func(pair compute.RoutersScopedListPair) ([]*computepb.Router, string) {
				return scopedItems(pair.Key, pair.Value.GetRouters(), pair.Value.GetWarning())
			})
		}),
		collect.InstanceGroups(func(ctx context.Context, project string) ([]*computepb.InstanceGroup, error) {
			it := c.instanceGroups.AggregatedList(ctx,
				&computepb.AggregatedListInstanceGroupsRequest{Project: project})
			return drainScoped(it.Next,
				func(pair compute.InstanceGroupsScopedListPair) ([]*computepb.InstanceGroup, string) {
					return scopedItems(pair.Key, pair.Value.GetInstanceGroups(), pair.Value.GetWarning())
				})
		}),
	}
}
