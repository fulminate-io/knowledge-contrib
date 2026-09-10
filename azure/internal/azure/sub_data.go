// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"fmt"
	"strconv"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cosmos/armcosmos/v3"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/redis/armredis/v3"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/search/armsearch"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/sql/armsql"
)

// sub_data.go — the data services: SQL, Cosmos DB, Redis and AI Search.

// sqlSub walks the subscription's SQL servers and their databases.
type sqlSub struct{ subBase }

func (s *sqlSub) Collect(ctx context.Context) (subResult, error) {
	servers, err := armsql.NewServersClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("sql servers client: %w", err)
	}
	databases, err := armsql.NewDatabasesClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("sql databases client: %w", err)
	}
	endpoints, err := armsql.NewPrivateEndpointConnectionsClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("sql private endpoint connections client: %w", err)
	}

	var out subResult
	err = drain(ctx, servers.NewListPager(nil), func(page armsql.ServersClientListResponse) error {
		for _, server := range page.Value {
			if server == nil || server.ID == nil {
				continue
			}
			r, err := sqlServerResource(server)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			out.edges = append(out.edges, sqlServerEdges(server)...)
			if err := s.collectDatabases(ctx, databases, server, &out); err != nil {
				return err
			}
			if err := s.collectPrivateEndpoints(ctx, endpoints, server, &out); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing sql servers: %w", err)
	}
	return out, nil
}

func sqlServerResource(server *armsql.Server) (resource, error) {
	content, err := marshalContent(server)
	if err != nil {
		return resource{}, fmt.Errorf("projecting sql server %s: %w", ptr(server.ID), err)
	}
	r := resource{
		id:           ptr(server.ID),
		name:         ptr(server.Name),
		resourceType: rtSQLServer,
		region:       ptr(server.Location),
		content:      content,
		metadata:     map[string]string{},
	}
	if p := server.Properties; p != nil {
		setIfNotEmpty(r.metadata, "fqdn", ptr(p.FullyQualifiedDomainName))
		setIfNotEmpty(r.metadata, "state", ptr(p.State))
		setIfNotEmpty(r.metadata, "version", ptr(p.Version))
	}
	return r, nil
}

// sqlServerEdges draws the server's identities and its transparent-data
// encryption key.
func sqlServerEdges(server *armsql.Server) []edge {
	id := ptr(server.ID)
	var out []edge
	if server.Identity != nil {
		out = append(out, managedIdentityEdges(id, keysOf(server.Identity.UserAssignedIdentities))...)
	}
	if server.Properties != nil {
		if key := ptr(server.Properties.KeyID); key != "" {
			out = append(out, edge{from: id, to: key, relation: edgeEncryptsWith})
		}
	}
	return out
}

func (s *sqlSub) collectDatabases(
	ctx context.Context, client *armsql.DatabasesClient, server *armsql.Server, out *subResult,
) error {
	rg, name := armResourceGroup(ptr(server.ID)), ptr(server.Name)
	if rg == "" || name == "" {
		return nil
	}
	err := drain(ctx, client.NewListByServerPager(rg, name, nil), func(page armsql.DatabasesClientListByServerResponse) error {
		for _, db := range page.Value {
			if db == nil || db.ID == nil {
				continue
			}
			r, err := sqlDatabaseResource(db)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			out.edges = append(out.edges, containsEdges(ptr(server.ID), ptr(db.ID))...)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("listing databases of sql server %s: %w", name, err)
	}
	return nil
}

// collectPrivateEndpoints draws the server's private endpoints.
//
// THE EDGE POINTS AT THE ENDPOINT, not at the subnet beneath it: the endpoint
// is a resource this walk enumerates on its own, and it carries its own subnet
// edge, so pointing here at the subnet would draw the same fact twice by two
// different routes.
func (s *sqlSub) collectPrivateEndpoints(
	ctx context.Context, client *armsql.PrivateEndpointConnectionsClient, server *armsql.Server, out *subResult,
) error {
	rg, name := armResourceGroup(ptr(server.ID)), ptr(server.Name)
	if rg == "" || name == "" {
		return nil
	}
	err := drain(ctx, client.NewListByServerPager(rg, name, nil),
		func(page armsql.PrivateEndpointConnectionsClientListByServerResponse) error {
			for _, conn := range page.Value {
				if conn == nil || conn.Properties == nil || conn.Properties.PrivateEndpoint == nil {
					continue
				}
				if peID := ptr(conn.Properties.PrivateEndpoint.ID); peID != "" {
					out.edges = append(out.edges, edge{from: ptr(server.ID), to: peID, relation: edgeUsesSubnet})
				}
			}
			return nil
		})
	if err != nil {
		return fmt.Errorf("listing private endpoint connections of sql server %s: %w", name, err)
	}
	return nil
}

// cosmosSub walks the subscription's Cosmos DB accounts.
type cosmosSub struct{ subBase }

func (s *cosmosSub) Collect(ctx context.Context) (subResult, error) {
	client, err := armcosmos.NewDatabaseAccountsClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("cosmos accounts client: %w", err)
	}
	var out subResult
	err = drain(ctx, client.NewListPager(nil), func(page armcosmos.DatabaseAccountsClientListResponse) error {
		for _, account := range page.Value {
			if account == nil || account.ID == nil {
				continue
			}
			r, err := cosmosResource(account)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			out.edges = append(out.edges, cosmosEdges(account)...)
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing cosmos accounts: %w", err)
	}
	return out, nil
}

func cosmosResource(account *armcosmos.DatabaseAccountGetResults) (resource, error) {
	content, err := marshalContent(account)
	if err != nil {
		return resource{}, fmt.Errorf("projecting cosmos account %s: %w", ptr(account.ID), err)
	}
	r := resource{
		id:           ptr(account.ID),
		name:         ptr(account.Name),
		resourceType: rtCosmosAccount,
		region:       ptr(account.Location),
		content:      content,
		metadata:     map[string]string{},
	}
	if account.Kind != nil {
		r.metadata["kind"] = string(*account.Kind)
	}
	if p := account.Properties; p != nil {
		setIfNotEmpty(r.metadata, "offerType", ptr(p.DatabaseAccountOfferType))
		if p.ConsistencyPolicy != nil && p.ConsistencyPolicy.DefaultConsistencyLevel != nil {
			r.metadata["consistencyLevel"] = string(*p.ConsistencyPolicy.DefaultConsistencyLevel)
		}
		if p.EnableMultipleWriteLocations != nil {
			r.metadata["enableMultipleWriteLocations"] = strconv.FormatBool(*p.EnableMultipleWriteLocations)
		}
	}
	return r, nil
}

func cosmosEdges(account *armcosmos.DatabaseAccountGetResults) []edge {
	id := ptr(account.ID)
	var out []edge
	if p := account.Properties; p != nil {
		// The key is named by its data-plane URI, which is the only identity
		// the account carries for it.
		if key := ptr(p.KeyVaultKeyURI); key != "" {
			out = append(out, edge{from: id, to: key, relation: edgeEncryptsWith})
		}
		for _, rule := range p.VirtualNetworkRules {
			if rule == nil {
				continue
			}
			if subnetID := ptr(rule.ID); subnetID != "" {
				out = append(out, edge{from: id, to: subnetID, relation: edgeUsesSubnet})
			}
		}
	}
	if account.Identity != nil {
		out = append(out, managedIdentityEdges(id, keysOf(account.Identity.UserAssignedIdentities))...)
	}
	return out
}

// redisSub walks the subscription's Redis caches.
type redisSub struct{ subBase }

func (s *redisSub) Collect(ctx context.Context) (subResult, error) {
	client, err := armredis.NewClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("redis client: %w", err)
	}
	var out subResult
	err = drain(ctx, client.NewListBySubscriptionPager(nil), func(page armredis.ClientListBySubscriptionResponse) error {
		for _, cache := range page.Value {
			if cache == nil || cache.ID == nil {
				continue
			}
			r, err := redisResource(cache)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			out.edges = append(out.edges, redisEdges(cache)...)
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing redis caches: %w", err)
	}
	return out, nil
}

func redisResource(cache *armredis.ResourceInfo) (resource, error) {
	content, err := marshalContent(cache)
	if err != nil {
		return resource{}, fmt.Errorf("projecting redis cache %s: %w", ptr(cache.ID), err)
	}
	r := resource{
		id:           ptr(cache.ID),
		name:         ptr(cache.Name),
		resourceType: rtRedis,
		region:       ptr(cache.Location),
		content:      content,
		metadata:     map[string]string{},
	}
	redisMetadata(cache.Properties, r.metadata)
	return r, nil
}

// redisEdges draws the cache's subnet and its identities.
//
// NO ENCRYPTION EDGE IS DRAWN, and the absence is a property of the resource
// rather than a gap here: customer-managed keys exist only on the Enterprise
// tier, which is a different resource type served by a different SDK, and every
// tier this type covers is encrypted with a platform key that has no node.
// redisMetadata promotes the cache's settings an operator filters on. It is a
// function of its own because the SKU is nested one level deeper than the rest
// and reading it inline nests three deep.
func redisMetadata(p *armredis.Properties, md map[string]string) {
	if p == nil {
		return
	}
	if p.SKU != nil {
		if p.SKU.Name != nil {
			md["skuName"] = string(*p.SKU.Name)
		}
		if p.SKU.Family != nil {
			md["skuFamily"] = string(*p.SKU.Family)
		}
	}
	setIfNotEmpty(md, "redisVersion", ptr(p.RedisVersion))
	if p.MinimumTLSVersion != nil {
		md["minimumTlsVersion"] = string(*p.MinimumTLSVersion)
	}
	if p.PublicNetworkAccess != nil {
		md["publicNetworkAccess"] = string(*p.PublicNetworkAccess)
	}
	if p.EnableNonSSLPort != nil {
		md["enableNonSslPort"] = strconv.FormatBool(*p.EnableNonSSLPort)
	}
}

func redisEdges(cache *armredis.ResourceInfo) []edge {
	id := ptr(cache.ID)
	var out []edge
	if cache.Properties != nil {
		out = append(out, subnetEdges(id, ptr(cache.Properties.SubnetID))...)
	}
	if cache.Identity != nil {
		out = append(out, managedIdentityEdges(id, keysOf(cache.Identity.UserAssignedIdentities))...)
	}
	return out
}

// searchSub walks the subscription's AI Search services.
type searchSub struct{ subBase }

func (s *searchSub) Collect(ctx context.Context) (subResult, error) {
	client, err := armsearch.NewServicesClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("search services client: %w", err)
	}
	var out subResult
	err = drain(ctx, client.NewListBySubscriptionPager(nil, nil), func(page armsearch.ServicesClientListBySubscriptionResponse) error {
		for _, svc := range page.Value {
			if svc == nil || svc.ID == nil {
				continue
			}
			r, err := searchResource(svc)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			out.edges = append(out.edges, searchEdges(svc)...)
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing search services: %w", err)
	}
	return out, nil
}

func searchResource(svc *armsearch.Service) (resource, error) {
	content, err := marshalContent(svc)
	if err != nil {
		return resource{}, fmt.Errorf("projecting search service %s: %w", ptr(svc.ID), err)
	}
	r := resource{
		id:           ptr(svc.ID),
		name:         ptr(svc.Name),
		resourceType: rtSearchService,
		region:       ptr(svc.Location),
		content:      content,
		metadata:     map[string]string{},
	}
	if svc.SKU != nil && svc.SKU.Name != nil {
		r.metadata["skuName"] = string(*svc.SKU.Name)
	}
	if p := svc.Properties; p != nil {
		if p.ReplicaCount != nil {
			r.metadata["replicaCount"] = strconv.Itoa(int(*p.ReplicaCount))
		}
		if p.PartitionCount != nil {
			r.metadata["partitionCount"] = strconv.Itoa(int(*p.PartitionCount))
		}
		if p.PublicNetworkAccess != nil {
			r.metadata["publicNetworkAccess"] = string(*p.PublicNetworkAccess)
		}
	}
	return r, nil
}

func searchEdges(svc *armsearch.Service) []edge {
	id := ptr(svc.ID)
	var out []edge
	if svc.Identity != nil {
		out = append(out, managedIdentityEdges(id, keysOf(svc.Identity.UserAssignedIdentities))...)
	}
	if svc.Properties != nil {
		for _, conn := range svc.Properties.PrivateEndpointConnections {
			if conn == nil || conn.Properties == nil || conn.Properties.PrivateEndpoint == nil {
				continue
			}
			if peID := ptr(conn.Properties.PrivateEndpoint.ID); peID != "" {
				out = append(out, edge{from: id, to: peID, relation: edgeUsesSubnet})
			}
		}
	}
	return out
}

// sqlDatabaseResource converts one database. A database carries its own
// location, which can differ from its server's on a geo-replicated secondary.
func sqlDatabaseResource(db *armsql.Database) (resource, error) {
	r, err := entityResource(ptr(db.ID), ptr(db.Name), rtSQLDatabase, ptr(db.Location), db)
	if err != nil {
		return resource{}, err
	}
	if db.SKU != nil {
		setIfNotEmpty(r.metadata, "skuName", ptr(db.SKU.Name))
	}
	if db.Properties != nil && db.Properties.Status != nil {
		r.metadata["status"] = string(*db.Properties.Status)
	}
	return r, nil
}
