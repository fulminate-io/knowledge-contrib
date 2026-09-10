// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"fmt"
	"strconv"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/synapse/armsynapse"
)

// sub_synapse.go — Synapse workspaces and both kinds of pool inside them.

// synapseSub walks the subscription's Synapse workspaces.
//
// BOTH POOL KINDS ARE ENUMERATED. A workspace's dedicated SQL pools and its
// Spark pools are separate resource types with separate list calls, and a
// workspace commonly has both; walking one would leave half a workspace's
// compute out of the graph.
type synapseSub struct{ subBase }

func (s *synapseSub) Collect(ctx context.Context) (subResult, error) {
	workspaces, err := armsynapse.NewWorkspacesClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("synapse workspaces client: %w", err)
	}
	sqlPools, err := armsynapse.NewSQLPoolsClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("synapse sql pools client: %w", err)
	}
	sparkPools, err := armsynapse.NewBigDataPoolsClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("synapse spark pools client: %w", err)
	}

	var out subResult
	err = drain(ctx, workspaces.NewListPager(nil), func(page armsynapse.WorkspacesClientListResponse) error {
		for _, ws := range page.Value {
			if ws == nil || ws.ID == nil {
				continue
			}
			r, err := synapseWorkspaceResource(ws)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			out.edges = append(out.edges, synapseWorkspaceEdges(ws)...)
			if err := s.collectSQLPools(ctx, sqlPools, ws, &out); err != nil {
				return err
			}
			if err := s.collectSparkPools(ctx, sparkPools, ws, &out); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing synapse workspaces: %w", err)
	}
	return out, nil
}

func synapseWorkspaceResource(ws *armsynapse.Workspace) (resource, error) {
	content, err := marshalContent(ws)
	if err != nil {
		return resource{}, fmt.Errorf("projecting synapse workspace %s: %w", ptr(ws.ID), err)
	}
	r := resource{
		id:           ptr(ws.ID),
		name:         ptr(ws.Name),
		resourceType: rtSynapseWorkspace,
		region:       ptr(ws.Location),
		content:      content,
		metadata:     map[string]string{},
	}
	if p := ws.Properties; p != nil {
		setIfNotEmpty(r.metadata, "provisioningState", ptr(p.ProvisioningState))
		setIfNotEmpty(r.metadata, "sqlAdministratorLogin", ptr(p.SQLAdministratorLogin))
		setIfNotEmpty(r.metadata, "managedVirtualNetwork", ptr(p.ManagedVirtualNetwork))
		if p.PublicNetworkAccess != nil {
			r.metadata["publicNetworkAccess"] = string(*p.PublicNetworkAccess)
		}
	}
	return r, nil
}

// synapseWorkspaceEdges draws the workspace's identities, its compute subnet
// and its customer-managed key.
func synapseWorkspaceEdges(ws *armsynapse.Workspace) []edge {
	id := ptr(ws.ID)
	var out []edge
	if ws.Identity != nil {
		out = append(out, managedIdentityEdges(id, keysOf(ws.Identity.UserAssignedIdentities))...)
	}
	if ws.Properties == nil {
		return out
	}
	if vnp := ws.Properties.VirtualNetworkProfile; vnp != nil {
		out = append(out, subnetEdges(id, ptr(vnp.ComputeSubnetID))...)
	}
	if enc := ws.Properties.Encryption; enc != nil && enc.Cmk != nil && enc.Cmk.Key != nil {
		if kvURL := ptr(enc.Cmk.Key.KeyVaultURL); kvURL != "" {
			out = append(out, edge{from: id, to: kvURL, relation: edgeEncryptsWith})
		}
	}
	return out
}

func (s *synapseSub) collectSQLPools(
	ctx context.Context, client *armsynapse.SQLPoolsClient, ws *armsynapse.Workspace, out *subResult,
) error {
	rg, name := armResourceGroup(ptr(ws.ID)), ptr(ws.Name)
	if rg == "" || name == "" {
		return nil
	}
	err := drain(ctx, client.NewListByWorkspacePager(rg, name, nil),
		func(page armsynapse.SQLPoolsClientListByWorkspaceResponse) error {
			for _, pool := range page.Value {
				if pool == nil || pool.ID == nil {
					continue
				}
				r, err := synapseSQLPoolResource(pool)
				if err != nil {
					return err
				}
				out.resources = append(out.resources, r)
				out.edges = append(out.edges, containsEdges(ptr(ws.ID), ptr(pool.ID))...)
			}
			return nil
		})
	if err != nil {
		return fmt.Errorf("listing sql pools of workspace %s: %w", name, err)
	}
	return nil
}

func (s *synapseSub) collectSparkPools(
	ctx context.Context, client *armsynapse.BigDataPoolsClient, ws *armsynapse.Workspace, out *subResult,
) error {
	rg, name := armResourceGroup(ptr(ws.ID)), ptr(ws.Name)
	if rg == "" || name == "" {
		return nil
	}
	err := drain(ctx, client.NewListByWorkspacePager(rg, name, nil),
		func(page armsynapse.BigDataPoolsClientListByWorkspaceResponse) error {
			for _, pool := range page.Value {
				if pool == nil || pool.ID == nil {
					continue
				}
				r, err := synapseSparkPoolResource(pool)
				if err != nil {
					return err
				}
				out.resources = append(out.resources, r)
				out.edges = append(out.edges, containsEdges(ptr(ws.ID), ptr(pool.ID))...)
			}
			return nil
		})
	if err != nil {
		return fmt.Errorf("listing spark pools of workspace %s: %w", name, err)
	}
	return nil
}

// synapseSQLPoolResource and synapseSparkPoolResource are the two pool
// conversions. A pool DOES carry its own location, unlike most child entities,
// so neither inherits the workspace's.
func synapseSQLPoolResource(pool *armsynapse.SQLPool) (resource, error) {
	r, err := entityResource(ptr(pool.ID), ptr(pool.Name), rtSQLPool, ptr(pool.Location), pool)
	if err != nil {
		return resource{}, err
	}
	if pool.SKU != nil {
		setIfNotEmpty(r.metadata, "skuName", ptr(pool.SKU.Name))
	}
	if p := pool.Properties; p != nil {
		setIfNotEmpty(r.metadata, "status", ptr(p.Status))
		setIfNotEmpty(r.metadata, "provisioningState", ptr(p.ProvisioningState))
	}
	return r, nil
}

func synapseSparkPoolResource(pool *armsynapse.BigDataPoolResourceInfo) (resource, error) {
	r, err := entityResource(ptr(pool.ID), ptr(pool.Name), rtSparkPool, ptr(pool.Location), pool)
	if err != nil {
		return resource{}, err
	}
	if p := pool.Properties; p != nil {
		setIfNotEmpty(r.metadata, "sparkVersion", ptr(p.SparkVersion))
		setIfNotEmpty(r.metadata, "provisioningState", ptr(p.ProvisioningState))
		if p.NodeCount != nil {
			r.metadata["nodeCount"] = strconv.Itoa(int(*p.NodeCount))
		}
		if p.NodeSize != nil {
			r.metadata["nodeSize"] = string(*p.NodeSize)
		}
	}
	return r, nil
}
