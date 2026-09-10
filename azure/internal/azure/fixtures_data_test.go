// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cosmos/armcosmos/v3"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/redis/armredis/v3"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/search/armsearch"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/sql/armsql"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/synapse/armsynapse"
)

// fixtures_data_test.go — hand-built data-service responses.

const (
	sqlServerID  = rgID + "/providers/Microsoft.Sql/servers/sql1"
	sqlDBID      = sqlServerID + "/databases/db1"
	cosmosID     = rgID + "/providers/Microsoft.DocumentDB/databaseAccounts/cosmos1"
	redisID      = rgID + "/providers/Microsoft.Cache/redis/cache1"
	searchID     = rgID + "/providers/Microsoft.Search/searchServices/search1"
	fileShareID  = storageID + "/fileServices/default/shares/share1"
	synapseWsID  = rgID + "/providers/Microsoft.Synapse/workspaces/syn1"
	sqlPoolID    = synapseWsID + "/sqlPools/pool1"
	sparkPoolID  = synapseWsID + "/bigDataPools/spark1"
	vaultKeyURI  = "https://vault1.vault.azure.net/keys/cmk"
	vaultBaseURI = "https://vault1.vault.azure.net/"
)

func dataFixtures() []fixture {
	return []fixture{
		{name: "sql server and database", build: func(t *testing.T) subResult {
			server := sqlServer()
			db := sqlDatabase()
			return subResult{
				resources: []resource{
					fx{t}.res(sqlServerResource(server)),
					fx{t}.res(sqlDatabaseResource(db)),
				},
				edges: append(sqlServerEdges(server), containsEdges(sqlServerID, sqlDBID)...),
			}
		}},
		{name: "cosmos account", build: func(t *testing.T) subResult {
			account := cosmosAccount()
			return subResult{
				resources: []resource{fx{t}.res(cosmosResource(account))},
				edges:     cosmosEdges(account),
			}
		}},
		{name: "redis cache", build: func(t *testing.T) subResult {
			cache := redisCache()
			return subResult{
				resources: []resource{fx{t}.res(redisResource(cache))},
				edges:     redisEdges(cache),
			}
		}},
		{name: "search service", build: func(t *testing.T) subResult {
			svc := searchService()
			return subResult{
				resources: []resource{fx{t}.res(searchResource(svc))},
				edges:     searchEdges(svc),
			}
		}},
		{name: "storage account and file share", build: func(t *testing.T) subResult {
			account := storageAccount()
			share := fileShare()
			return subResult{
				resources: []resource{
					fx{t}.res(storageAccountResource(account)),
					fx{t}.res(fileShareResource(share, account)),
				},
				edges: append(storageAccountEdges(account), containsEdges(storageID, fileShareID)...),
			}
		}},
		{name: "synapse workspace and its pools", build: func(t *testing.T) subResult {
			ws := synapseWorkspace()
			return subResult{
				resources: []resource{
					fx{t}.res(synapseWorkspaceResource(ws)),
					fx{t}.res(synapseSQLPoolResource(synapseSQLPool())),
					fx{t}.res(synapseSparkPoolResource(synapseSparkPool())),
				},
				edges: append(synapseWorkspaceEdges(ws), containsEdges(synapseWsID, sqlPoolID, sparkPoolID)...),
			}
		}},
	}
}

func sqlServer() *armsql.Server {
	return &armsql.Server{
		ID:       new(sqlServerID),
		Name:     new("sql1"),
		Location: new("westeurope"),
		Identity: &armsql.ResourceIdentity{
			UserAssignedIdentities: map[string]*armsql.UserIdentity{identityA: {}},
		},
		Properties: &armsql.ServerProperties{
			FullyQualifiedDomainName: new("sql1.database.windows.net"),
			State:                    new("Ready"),
			Version:                  new("12.0"),
			KeyID:                    new(vaultKeyURI),
		},
	}
}

func sqlDatabase() *armsql.Database {
	return &armsql.Database{
		ID:         new(sqlDBID),
		Name:       new("db1"),
		Location:   new("westeurope"),
		SKU:        &armsql.SKU{Name: new("S0")},
		Properties: &armsql.DatabaseProperties{Status: to.Ptr(armsql.DatabaseStatusOnline)},
	}
}

func cosmosAccount() *armcosmos.DatabaseAccountGetResults {
	return &armcosmos.DatabaseAccountGetResults{
		ID:       new(cosmosID),
		Name:     new("cosmos1"),
		Location: new("westeurope"),
		Kind:     to.Ptr(armcosmos.DatabaseAccountKindGlobalDocumentDB),
		Identity: &armcosmos.ManagedServiceIdentity{
			UserAssignedIdentities: map[string]*armcosmos.Components1Jq1T4ISchemasManagedserviceidentityPropertiesUserassignedidentitiesAdditionalproperties{
				identityA: {},
			},
		},
		Properties: &armcosmos.DatabaseAccountGetProperties{
			DatabaseAccountOfferType: new("Standard"),
			KeyVaultKeyURI:           new(vaultKeyURI),
			VirtualNetworkRules:      []*armcosmos.VirtualNetworkRule{{ID: new(subnetID)}},
		},
	}
}

func redisCache() *armredis.ResourceInfo {
	return &armredis.ResourceInfo{
		ID:       new(redisID),
		Name:     new("cache1"),
		Location: new("westeurope"),
		Identity: &armredis.ManagedServiceIdentity{
			UserAssignedIdentities: map[string]*armredis.UserAssignedIdentity{identityA: {}},
		},
		Properties: &armredis.Properties{
			SKU:          &armredis.SKU{Name: to.Ptr(armredis.SKUNamePremium), Family: to.Ptr(armredis.SKUFamilyP)},
			RedisVersion: new("6.0"),
			SubnetID:     new(subnetID),
		},
	}
}

func searchService() *armsearch.Service {
	return &armsearch.Service{
		ID:       new(searchID),
		Name:     new("search1"),
		Location: new("westeurope"),
		SKU:      &armsearch.SKU{Name: to.Ptr(armsearch.SKUNameStandard)},
		Identity: &armsearch.Identity{
			UserAssignedIdentities: map[string]*armsearch.UserAssignedIdentity{identityA: {}},
		},
		Properties: &armsearch.ServiceProperties{
			ReplicaCount:   to.Ptr[int32](2),
			PartitionCount: to.Ptr[int32](1),
			PrivateEndpointConnections: []*armsearch.PrivateEndpointConnection{{
				Properties: &armsearch.PrivateEndpointConnectionProperties{
					PrivateEndpoint: &armsearch.PrivateEndpointConnectionPropertiesPrivateEndpoint{ID: new(peID)},
				},
			}},
		},
	}
}

func storageAccount() *armstorage.Account {
	return &armstorage.Account{
		ID:       new(storageID),
		Name:     new("store1"),
		Location: new("westeurope"),
		Kind:     to.Ptr(armstorage.KindStorageV2),
		SKU:      &armstorage.SKU{Name: to.Ptr(armstorage.SKUNameStandardLRS)},
		Properties: &armstorage.AccountProperties{
			AccessTier:             to.Ptr(armstorage.AccessTierHot),
			EnableHTTPSTrafficOnly: new(true),
			Encryption: &armstorage.Encryption{
				KeyVaultProperties: &armstorage.KeyVaultProperties{
					KeyVaultURI: new(vaultBaseURI),
					KeyName:     new("cmk"),
					KeyVersion:  new("v1"),
				},
			},
			NetworkRuleSet: &armstorage.NetworkRuleSet{
				VirtualNetworkRules: []*armstorage.VirtualNetworkRule{{VirtualNetworkResourceID: new(subnetID)}},
			},
		},
	}
}

func fileShare() *armstorage.FileShareItem {
	return &armstorage.FileShareItem{
		ID:   new(fileShareID),
		Name: new("share1"),
		Properties: &armstorage.FileShareProperties{
			ShareQuota: to.Ptr[int32](100),
			AccessTier: to.Ptr(armstorage.ShareAccessTierHot),
		},
	}
}

func synapseWorkspace() *armsynapse.Workspace {
	return &armsynapse.Workspace{
		ID:       new(synapseWsID),
		Name:     new("syn1"),
		Location: new("westeurope"),
		Identity: &armsynapse.ManagedIdentity{
			UserAssignedIdentities: map[string]*armsynapse.UserAssignedManagedIdentity{identityA: {}},
		},
		Properties: &armsynapse.WorkspaceProperties{
			ProvisioningState:     new("Succeeded"),
			SQLAdministratorLogin: new("sqladmin"),
			VirtualNetworkProfile: &armsynapse.VirtualNetworkProfile{ComputeSubnetID: new(subnetID)},
			Encryption: &armsynapse.EncryptionDetails{
				Cmk: &armsynapse.CustomerManagedKeyDetails{
					Key: &armsynapse.WorkspaceKeyDetails{KeyVaultURL: new(vaultKeyURI)},
				},
			},
		},
	}
}

func synapseSQLPool() *armsynapse.SQLPool {
	return &armsynapse.SQLPool{
		ID:         new(sqlPoolID),
		Name:       new("pool1"),
		Location:   new("westeurope"),
		SKU:        &armsynapse.SKU{Name: new("DW100c")},
		Properties: &armsynapse.SQLPoolResourceProperties{Status: new("Online")},
	}
}

func synapseSparkPool() *armsynapse.BigDataPoolResourceInfo {
	return &armsynapse.BigDataPoolResourceInfo{
		ID:       new(sparkPoolID),
		Name:     new("spark1"),
		Location: new("westeurope"),
		Properties: &armsynapse.BigDataPoolResourceProperties{
			SparkVersion: new("3.4"),
			NodeCount:    to.Ptr[int32](3),
			NodeSize:     to.Ptr(armsynapse.NodeSizeMedium),
		},
	}
}

// TestKeyVaultKeyEdges_TargetTheKeysDataPlaneURI. A resource configured with a
// customer-managed key names the vault by hostname and the key by name; the
// vault's resource id is not derivable from either, so the data-plane URI is
// the only identity the source data carries.
func TestKeyVaultKeyEdges_TargetTheKeysDataPlaneURI(t *testing.T) {
	edges := storageAccountEdges(storageAccount())
	want := "https://vault1.vault.azure.net/keys/cmk/v1"
	if _, ok := edgeBetween(edges, storageID, want, edgeEncryptsWith); !ok {
		t.Errorf("no encryption edge to %q: %v", want, edges)
	}

	// The negative arms, through the same path: a key with no name, and an
	// account with no customer key at all, draw nothing.
	noName := storageAccount()
	noName.Properties.Encryption.KeyVaultProperties.KeyName = nil
	if relationsOf(storageAccountEdges(noName))[edgeEncryptsWith] != 0 {
		t.Error("an incomplete key reference drew an encryption edge")
	}
	platformKey := storageAccount()
	platformKey.Properties.Encryption = nil
	if relationsOf(storageAccountEdges(platformKey))[edgeEncryptsWith] != 0 {
		t.Error("an account on the platform key drew an encryption edge")
	}
}

// TestRedisEdges_DeriveTheNetworkFromTheSubnet. A subnet id contains its
// network's id as a prefix, so the parent is knowable without a second call —
// and a subnet id that does not carry one yields only the subnet edge.
func TestRedisEdges_DeriveTheNetworkFromTheSubnet(t *testing.T) {
	edges := redisEdges(redisCache())
	if _, ok := edgeBetween(edges, redisID, subnetID, edgeUsesSubnet); !ok {
		t.Error("no subnet edge from the cache")
	}
	if _, ok := edgeBetween(edges, redisID, vnetID, edgeUsesNetwork); !ok {
		t.Error("the network was not derived from the subnet id")
	}

	odd := redisCache()
	odd.Properties.SubnetID = new("/subscriptions/0000/resourceGroups/rg/providers/Something/else/x")
	if relationsOf(redisEdges(odd))[edgeUsesNetwork] != 0 {
		t.Error("a subnet id that names no network still produced a network edge")
	}
}

// TestSQLServerEdges_EncryptWithTheirTransparentDataEncryptionKey, and the
// database is contained by its server rather than the reverse.
func TestSQLServerEdges_EncryptWithTheirTransparentDataEncryptionKey(t *testing.T) {
	edges := sqlServerEdges(sqlServer())
	if _, ok := edgeBetween(edges, sqlServerID, vaultKeyURI, edgeEncryptsWith); !ok {
		t.Error("no encryption edge from the server to its key")
	}
	if _, ok := edgeBetween(edges, sqlServerID, identityA, edgeAssumesRole); !ok {
		t.Error("no assignment edge from the server to its attached identity")
	}
	contained := containsEdges(sqlServerID, sqlDBID)
	if _, ok := edgeBetween(contained, sqlServerID, sqlDBID, edgeContains); !ok {
		t.Error("the server does not contain its database")
	}
}

// TestSynapseWorkspaceEdges_CoverAllThreeRelationships in one response, which
// is the shape a real workspace has.
func TestSynapseWorkspaceEdges_CoverAllThreeRelationships(t *testing.T) {
	edges := synapseWorkspaceEdges(synapseWorkspace())
	for _, want := range []struct {
		to       string
		relation string
	}{
		{identityA, edgeAssumesRole},
		{subnetID, edgeUsesSubnet},
		{vnetID, edgeUsesNetwork},
		{vaultKeyURI, edgeEncryptsWith},
	} {
		if _, ok := edgeBetween(edges, synapseWsID, want.to, want.relation); !ok {
			t.Errorf("no %s edge to %s", want.relation, want.to)
		}
	}
}
