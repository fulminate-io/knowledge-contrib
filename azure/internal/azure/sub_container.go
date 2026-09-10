// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerregistry/armcontainerregistry"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v6"
)

// sub_container.go — Kubernetes clusters and container registries.

// aksSub walks the subscription's managed Kubernetes clusters.
//
// IT DOES NOT CASCADE INTO THE CLUSTER. A cluster's workloads are a Kubernetes
// graph, which a Kubernetes collector walks; this collector's result carries
// one graph, and a node from another family in it would be a node in the wrong
// graph rather than a link to another one.
type aksSub struct{ subBase }

func (s *aksSub) Collect(ctx context.Context) (subResult, error) {
	client, err := armcontainerservice.NewManagedClustersClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("managed clusters client: %w", err)
	}
	var out subResult
	err = drain(ctx, client.NewListPager(nil), func(page armcontainerservice.ManagedClustersClientListResponse) error {
		for _, cluster := range page.Value {
			if cluster == nil || cluster.ID == nil {
				continue
			}
			r, err := clusterResource(cluster)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			out.edges = append(out.edges, clusterEdges(cluster)...)
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing managed clusters: %w", err)
	}
	return out, nil
}

func clusterResource(cluster *armcontainerservice.ManagedCluster) (resource, error) {
	content, err := marshalContent(cluster)
	if err != nil {
		return resource{}, fmt.Errorf("projecting managed cluster %s: %w", ptr(cluster.ID), err)
	}
	r := resource{
		id:           ptr(cluster.ID),
		name:         ptr(cluster.Name),
		resourceType: rtManagedCluster,
		region:       ptr(cluster.Location),
		content:      content,
		metadata:     map[string]string{},
	}
	if p := cluster.Properties; p != nil {
		setIfNotEmpty(r.metadata, "kubernetesVersion", ptr(p.KubernetesVersion))
		setIfNotEmpty(r.metadata, "nodeResourceGroup", ptr(p.NodeResourceGroup))
		setIfNotEmpty(r.metadata, "fqdn", ptr(p.Fqdn))
		if p.PowerState != nil && p.PowerState.Code != nil {
			r.metadata["powerState"] = string(*p.PowerState.Code)
		}
		if p.OidcIssuerProfile != nil {
			// The cluster's own OIDC issuer, which is what a federated
			// credential on a managed identity names when workload identity is
			// wired up. Carrying it makes that federation legible from the
			// cluster's side too.
			setIfNotEmpty(r.metadata, "oidcIssuerUrl", ptr(p.OidcIssuerProfile.IssuerURL))
		}
	}
	return r, nil
}

// clusterEdges draws the subnets the cluster's node pools sit in and the
// identities it is attached to.
func clusterEdges(cluster *armcontainerservice.ManagedCluster) []edge {
	id := ptr(cluster.ID)
	var out []edge
	if p := cluster.Properties; p != nil {
		for _, pool := range p.AgentPoolProfiles {
			if pool == nil {
				continue
			}
			if subnetID := ptr(pool.VnetSubnetID); subnetID != "" {
				out = append(out, edge{from: id, to: subnetID, relation: edgeUsesSubnet})
			}
		}
	}
	if cluster.Identity != nil {
		out = append(out, managedIdentityEdges(id, keysOf(cluster.Identity.UserAssignedIdentities))...)
	}
	return out
}

// registrySub walks the subscription's container registries.
//
// IT CARRIES EACH REGISTRY'S LOGIN SERVER FORWARD as a walk fact, which is what
// resolver 3 matches a site's container image against.
type registrySub struct{ subBase }

func (s *registrySub) Collect(ctx context.Context) (subResult, error) {
	client, err := armcontainerregistry.NewRegistriesClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("container registries client: %w", err)
	}
	var out subResult
	err = drain(ctx, client.NewListPager(nil), func(page armcontainerregistry.RegistriesClientListResponse) error {
		for _, reg := range page.Value {
			if reg == nil || reg.ID == nil {
				continue
			}
			r, err := registryResource(reg)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			out.edges = append(out.edges, registryEdges(reg)...)
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing container registries: %w", err)
	}
	return out, nil
}

// registryContent is the curated projection stored on a registry node.
//
// THE PRIVATE ENDPOINT CONNECTIONS ARE DELIBERATELY ABSENT from it. They are
// read for edges before this projection is built, and each carries a nested
// approval record that would dominate the node's body without saying anything
// the edges do not.
type registryContent struct {
	ID         string             `json:"id"`
	Name       string             `json:"name"`
	Location   string             `json:"location,omitempty"`
	SKU        string             `json:"sku,omitempty"`
	Properties registryProperties `json:"properties"`
}

type registryProperties struct {
	LoginServer       string `json:"loginServer,omitempty"`
	ProvisioningState string `json:"provisioningState,omitempty"`
	AdminUserEnabled  *bool  `json:"adminUserEnabled,omitempty"`
}

func registryResource(reg *armcontainerregistry.Registry) (resource, error) {
	proj := registryContent{ID: ptr(reg.ID), Name: ptr(reg.Name), Location: ptr(reg.Location)}
	if reg.SKU != nil && reg.SKU.Name != nil {
		proj.SKU = string(*reg.SKU.Name)
	}
	if p := reg.Properties; p != nil {
		proj.Properties.LoginServer = ptr(p.LoginServer)
		if p.ProvisioningState != nil {
			proj.Properties.ProvisioningState = string(*p.ProvisioningState)
		}
		if p.AdminUserEnabled != nil {
			v := *p.AdminUserEnabled
			proj.Properties.AdminUserEnabled = &v
		}
	}
	content, err := marshalContent(proj)
	if err != nil {
		return resource{}, fmt.Errorf("projecting container registry %s: %w", ptr(reg.ID), err)
	}
	r := resource{
		id:             ptr(reg.ID),
		name:           ptr(reg.Name),
		resourceType:   rtRegistry,
		region:         ptr(reg.Location),
		content:        content,
		metadata:       map[string]string{},
		acrLoginServer: proj.Properties.LoginServer,
	}
	setIfNotEmpty(r.metadata, "skuName", proj.SKU)
	setIfNotEmpty(r.metadata, "loginServer", proj.Properties.LoginServer)
	setIfNotEmpty(r.metadata, "provisioningState", proj.Properties.ProvisioningState)
	return r, nil
}

// registryEdges draws the private endpoints fronting the registry.
//
// A REGISTRY HAS NO VIRTUAL-NETWORK RULES to read: unlike the data services,
// its network integration is expressed entirely as private endpoint
// connections, so this is the only network relationship it has.
func registryEdges(reg *armcontainerregistry.Registry) []edge {
	if reg.Properties == nil {
		return nil
	}
	id := ptr(reg.ID)
	var out []edge
	for _, conn := range reg.Properties.PrivateEndpointConnections {
		if conn == nil || conn.Properties == nil || conn.Properties.PrivateEndpoint == nil {
			continue
		}
		if peID := ptr(conn.Properties.PrivateEndpoint.ID); peID != "" {
			out = append(out, edge{from: id, to: peID, relation: edgeUsesSubnet})
		}
	}
	return out
}
