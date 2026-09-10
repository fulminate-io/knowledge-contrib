// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
)

// sub_frontdoor.go — Front Door profiles, their endpoints, their origins, the
// policies guarding them and the certificates they serve.
//
// FRONT DOOR AND CDN SHARE ONE RESOURCE TYPE, told apart by the profile's SKU.
// This walk keeps only the Front Door ones: a classic CDN profile has none of
// the endpoint, origin or security structure below, so walking it would make
// four list calls per profile that each return nothing.

// frontDoorSub walks the subscription's Front Door profiles.
type frontDoorSub struct{ subBase }

func (s *frontDoorSub) Collect(ctx context.Context) (subResult, error) {
	profiles, err := armcdn.NewProfilesClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("front door profiles client: %w", err)
	}
	clients, err := s.buildClients()
	if err != nil {
		return subResult{}, err
	}

	var out subResult
	err = drain(ctx, profiles.NewListPager(nil), func(page armcdn.ProfilesClientListResponse) error {
		for _, profile := range page.Value {
			if profile == nil || profile.ID == nil || !isFrontDoorProfile(profile) {
				continue
			}
			r, err := profileResource(profile)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			if err := clients.collectProfile(ctx, profile, &out); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing front door profiles: %w", err)
	}
	return out, nil
}

// frontDoorClients is the set of per-profile clients the walk needs. They are
// built once for the whole subcollector rather than per profile.
type frontDoorClients struct {
	endpoints    *armcdn.AFDEndpointsClient
	originGroups *armcdn.AFDOriginGroupsClient
	origins      *armcdn.AFDOriginsClient
	policies     *armcdn.SecurityPoliciesClient
	domains      *armcdn.AFDCustomDomainsClient
}

func (s *frontDoorSub) buildClients() (*frontDoorClients, error) {
	var c frontDoorClients
	var err error
	if c.endpoints, err = armcdn.NewAFDEndpointsClient(s.subscriptionID, s.cred, s.clientOptions()); err != nil {
		return nil, fmt.Errorf("front door endpoints client: %w", err)
	}
	if c.originGroups, err = armcdn.NewAFDOriginGroupsClient(s.subscriptionID, s.cred, s.clientOptions()); err != nil {
		return nil, fmt.Errorf("front door origin groups client: %w", err)
	}
	if c.origins, err = armcdn.NewAFDOriginsClient(s.subscriptionID, s.cred, s.clientOptions()); err != nil {
		return nil, fmt.Errorf("front door origins client: %w", err)
	}
	if c.policies, err = armcdn.NewSecurityPoliciesClient(s.subscriptionID, s.cred, s.clientOptions()); err != nil {
		return nil, fmt.Errorf("front door security policies client: %w", err)
	}
	if c.domains, err = armcdn.NewAFDCustomDomainsClient(s.subscriptionID, s.cred, s.clientOptions()); err != nil {
		return nil, fmt.Errorf("front door custom domains client: %w", err)
	}
	return &c, nil
}

// isFrontDoorProfile reports whether a CDN profile is a Front Door profile.
func isFrontDoorProfile(profile *armcdn.Profile) bool {
	if profile.SKU == nil || profile.SKU.Name == nil {
		return false
	}
	switch *profile.SKU.Name {
	case armcdn.SKUNameStandardAzureFrontDoor, armcdn.SKUNamePremiumAzureFrontDoor:
		return true
	default:
		return false
	}
}

func profileResource(profile *armcdn.Profile) (resource, error) {
	content, err := marshalContent(profile)
	if err != nil {
		return resource{}, fmt.Errorf("projecting front door profile %s: %w", ptr(profile.ID), err)
	}
	r := resource{
		id:           ptr(profile.ID),
		name:         ptr(profile.Name),
		resourceType: rtCDNProfile,
		region:       ptr(profile.Location),
		content:      content,
		metadata:     map[string]string{},
	}
	if profile.SKU != nil && profile.SKU.Name != nil {
		r.metadata["skuName"] = string(*profile.SKU.Name)
	}
	return r, nil
}

func (c *frontDoorClients) collectProfile(ctx context.Context, profile *armcdn.Profile, out *subResult) error {
	rg, name := armResourceGroup(ptr(profile.ID)), ptr(profile.Name)
	if rg == "" || name == "" {
		return nil
	}
	if err := c.collectEndpoints(ctx, rg, profile, out); err != nil {
		return err
	}
	if err := c.collectOrigins(ctx, rg, profile, out); err != nil {
		return err
	}
	if err := c.collectPolicies(ctx, rg, profile, out); err != nil {
		return err
	}
	return c.collectDomains(ctx, rg, profile, out)
}

func (c *frontDoorClients) collectEndpoints(ctx context.Context, rg string, profile *armcdn.Profile, out *subResult) error {
	err := drain(ctx, c.endpoints.NewListByProfilePager(rg, ptr(profile.Name), nil),
		func(page armcdn.AFDEndpointsClientListByProfileResponse) error {
			for _, ep := range page.Value {
				if ep == nil || ep.ID == nil {
					continue
				}
				r, err := frontDoorEndpointResource(ep)
				if err != nil {
					return err
				}
				out.resources = append(out.resources, r)
				out.edges = append(out.edges, containsEdges(ptr(profile.ID), ptr(ep.ID))...)
			}
			return nil
		})
	if err != nil {
		return fmt.Errorf("listing endpoints of front door profile %s: %w", ptr(profile.Name), err)
	}
	return nil
}

// collectOrigins draws where a profile routes.
//
// THE EDGE RUNS FROM THE PROFILE, not from the origin group, and origin groups
// are not emitted as nodes: a group is a load-balancing policy over origins
// rather than a thing traffic reaches, so the relationship a reader wants is
// profile to backend.
func (c *frontDoorClients) collectOrigins(ctx context.Context, rg string, profile *armcdn.Profile, out *subResult) error {
	err := drain(ctx, c.originGroups.NewListByProfilePager(rg, ptr(profile.Name), nil),
		func(page armcdn.AFDOriginGroupsClientListByProfileResponse) error {
			for _, group := range page.Value {
				if group == nil || group.Name == nil {
					continue
				}
				if err := c.collectOriginsInGroup(ctx, rg, profile, *group.Name, out); err != nil {
					return err
				}
			}
			return nil
		})
	if err != nil {
		return fmt.Errorf("listing origin groups of front door profile %s: %w", ptr(profile.Name), err)
	}
	return nil
}

func (c *frontDoorClients) collectOriginsInGroup(
	ctx context.Context, rg string, profile *armcdn.Profile, groupName string, out *subResult,
) error {
	err := drain(ctx, c.origins.NewListByOriginGroupPager(rg, ptr(profile.Name), groupName, nil),
		func(page armcdn.AFDOriginsClientListByOriginGroupResponse) error {
			for _, origin := range page.Value {
				out.edges = append(out.edges, originEdges(profile, origin, groupName)...)
			}
			return nil
		})
	if err != nil {
		return fmt.Errorf("listing origins of group %s: %w", groupName, err)
	}
	return nil
}

// collectPolicies draws the web application firewall policies guarding the
// profile. The edge runs policy to profile, as every guarding edge does.
func (c *frontDoorClients) collectPolicies(ctx context.Context, rg string, profile *armcdn.Profile, out *subResult) error {
	err := drain(ctx, c.policies.NewListByProfilePager(rg, ptr(profile.Name), nil),
		func(page armcdn.SecurityPoliciesClientListByProfileResponse) error {
			for _, policy := range page.Value {
				out.edges = append(out.edges, securityPolicyEdges(profile, policy)...)
			}
			return nil
		})
	if err != nil {
		return fmt.Errorf("listing security policies of front door profile %s: %w", ptr(profile.Name), err)
	}
	return nil
}

// firewallPolicyID reads the policy a security policy applies. The parameters
// field is a sum type and only the firewall arm names one.
func firewallPolicyID(policy *armcdn.SecurityPolicy) string {
	if policy == nil || policy.Properties == nil || policy.Properties.Parameters == nil {
		return ""
	}
	params, ok := policy.Properties.Parameters.(*armcdn.SecurityPolicyWebApplicationFirewallParameters)
	if !ok || params.WafPolicy == nil {
		return ""
	}
	return ptr(params.WafPolicy.ID)
}

// collectDomains draws the certificates the profile serves for its custom
// domains.
func (c *frontDoorClients) collectDomains(ctx context.Context, rg string, profile *armcdn.Profile, out *subResult) error {
	err := drain(ctx, c.domains.NewListByProfilePager(rg, ptr(profile.Name), nil),
		func(page armcdn.AFDCustomDomainsClientListByProfileResponse) error {
			for _, domain := range page.Value {
				out.edges = append(out.edges, customDomainEdges(profile, domain)...)
			}
			return nil
		})
	if err != nil {
		return fmt.Errorf("listing custom domains of front door profile %s: %w", ptr(profile.Name), err)
	}
	return nil
}

// frontDoorEndpointResource converts one endpoint. Its hostname is promoted to
// metadata because that is what a DNS record points at, which is how a reader
// joins the two.
func frontDoorEndpointResource(ep *armcdn.AFDEndpoint) (resource, error) {
	r, err := entityResource(ptr(ep.ID), ptr(ep.Name), rtAFDEndpoint, ptr(ep.Location), ep)
	if err != nil {
		return resource{}, err
	}
	if ep.Properties != nil {
		setIfNotEmpty(r.metadata, "hostName", ptr(ep.Properties.HostName))
	}
	return r, nil
}

// originEdges draws where a profile routes through one origin.
//
// AN ORIGIN OUTSIDE AZURE DRAWS NOTHING: it is named by hostname, has no
// resource id, and an edge to a hostname would be an endpoint nothing resolves.
func originEdges(profile *armcdn.Profile, origin *armcdn.AFDOrigin, groupName string) []edge {
	if origin == nil || origin.Properties == nil || origin.Properties.AzureOrigin == nil {
		return nil
	}
	target := ptr(origin.Properties.AzureOrigin.ID)
	if !strings.HasPrefix(target, "/") {
		return nil
	}
	return []edge{{
		from: ptr(profile.ID), to: target, relation: edgeRoutesTo,
		metadata: map[string]string{"origin_group": groupName},
	}}
}

// securityPolicyEdges draws the firewall policy guarding a profile.
func securityPolicyEdges(profile *armcdn.Profile, policy *armcdn.SecurityPolicy) []edge {
	policyID := firewallPolicyID(policy)
	if policyID == "" {
		return nil
	}
	return []edge{{from: policyID, to: ptr(profile.ID), relation: edgeProtects}}
}

// customDomainEdges draws the certificate a profile serves for one domain.
func customDomainEdges(profile *armcdn.Profile, domain *armcdn.AFDDomain) []edge {
	if domain == nil || domain.Properties == nil || domain.Properties.TLSSettings == nil {
		return nil
	}
	secret := domain.Properties.TLSSettings.Secret
	if secret == nil || ptr(secret.ID) == "" {
		return nil
	}
	return []edge{{
		from: ptr(profile.ID), to: ptr(secret.ID), relation: edgeUsesCert,
		metadata: map[string]string{"domain": ptr(domain.Name)},
	}}
}
