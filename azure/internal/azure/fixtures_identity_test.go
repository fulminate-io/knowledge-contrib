// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization/v3"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerregistry/armcontainerregistry"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v6"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/keyvault/armkeyvault"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/msi/armmsi"
)

// fixtures_identity_test.go — identities, directory groups, vaults, clusters
// and registries: everything the trust and grant resolvers read.

const (
	tenantOne     = "11111111-1111-1111-1111-111111111111"
	tenantTwo     = "22222222-2222-2222-2222-222222222222"
	principalOne  = "33333333-3333-3333-3333-333333333333"
	groupObjectID = "44444444-4444-4444-4444-444444444444"
	memberUserID  = "55555555-5555-5555-5555-555555555555"
	vaultID       = rgID + "/providers/Microsoft.KeyVault/vaults/vault1"
	clusterID     = rgID + "/providers/Microsoft.ContainerService/managedClusters/aks1"
	registryID    = rgID + "/providers/Microsoft.ContainerRegistry/registries/reg1"
	registryHost  = "reg1.azurecr.io"
	scopeID       = rgID + "/providers/Microsoft.Storage/storageAccounts/store1"
	aksIssuer     = "https://westeurope.oic.prod-aks.azure.com/tenant/cluster/"
	githubIssuer  = "https://token.actions.githubusercontent.com"
	foreignIssuer = "https://accounts.example.com"
	entraIssuer   = "https://login.microsoftonline.com/" + tenantTwo + "/v2.0"
)

func identityFixtures() []fixture {
	return []fixture{
		{name: "managed identity with a role assignment", build: func(t *testing.T) subResult {
			identity := managedIdentity()
			return subResult{
				resources: []resource{fx{t}.res(identityResource(identity))},
				edges:     roleAssignmentEdges(identityA, roleAssignments()),
			}
		}},
		{name: "federated credentials", build: func(t *testing.T) subResult {
			var out subResult
			for _, cred := range []*armmsi.FederatedIdentityCredential{
				federatedCredentialFor(githubIssuer, "repo:acme/service:ref:refs/heads/main"),
				federatedCredentialFor(foreignIssuer, "workload-one"),
				federatedCredentialFor(aksIssuer, "system:serviceaccount:apps:api"),
			} {
				resources, edges := federatedCredential(identityA, cred)
				out.resources = append(out.resources, resources...)
				out.edges = append(out.edges, edges...)
			}
			return out
		}},
		{name: "directory group", build: func(t *testing.T) subResult {
			g := directoryGroup()
			return subResult{
				resources: []resource{fx{t}.res(groupResource(g))},
				edges:     membershipEdges(g.ID, groupMembers()),
			}
		}},
		{name: "key vault with both grant models", build: func(t *testing.T) subResult {
			vault := keyVault()
			return subResult{
				resources: []resource{fx{t}.res(vaultResource(vault))},
				edges:     append(fx{t}.edges(vaultEdges(vault)), vaultRoleAssignmentEdges(vaultID, vaultRoleAssignments())...),
			}
		}},
		{name: "kubernetes cluster", build: func(t *testing.T) subResult {
			cluster := managedCluster()
			return subResult{
				resources: []resource{fx{t}.res(clusterResource(cluster))},
				edges:     clusterEdges(cluster),
			}
		}},
		{name: "container registry", build: func(t *testing.T) subResult {
			reg := containerRegistry()
			return subResult{
				resources: []resource{fx{t}.res(registryResource(reg))},
				edges:     registryEdges(reg),
			}
		}},
	}
}

func managedIdentity() *armmsi.Identity {
	return &armmsi.Identity{
		ID:       new(identityA),
		Name:     new("id-a"),
		Location: new("westeurope"),
		Properties: &armmsi.UserAssignedIdentityProperties{
			ClientID:    new("66666666-6666-6666-6666-666666666666"),
			PrincipalID: new(principalOne),
			TenantID:    new(tenantOne),
		},
	}
}

// roleAssignments is what the per-identity list returns: assignments whose
// principal is the walked identity, so their principal type is ServicePrincipal.
func roleAssignments() []*armauthorization.RoleAssignment {
	return []*armauthorization.RoleAssignment{{
		Properties: &armauthorization.RoleAssignmentProperties{
			Scope:            new(scopeID),
			RoleDefinitionID: new("/subscriptions/0000/providers/Microsoft.Authorization/roleDefinitions/reader"),
			PrincipalType:    to.Ptr(armauthorization.PrincipalTypeServicePrincipal),
		},
	}}
}

func federatedCredentialFor(issuer, subject string) *armmsi.FederatedIdentityCredential {
	return &armmsi.FederatedIdentityCredential{
		ID:   new(identityA + "/federatedIdentityCredentials/fic"),
		Name: new("fic"),
		Properties: &armmsi.FederatedIdentityCredentialProperties{
			Issuer:    new(issuer),
			Subject:   new(subject),
			Audiences: []*string{new("api://AzureADTokenExchange")},
		},
	}
}

func directoryGroup() graphGroup {
	return graphGroup{
		ID:              groupObjectID,
		DisplayName:     "platform-admins",
		Mail:            "platform-admins@example.com",
		GroupTypes:      []string{"Unified"},
		SecurityEnabled: true,
	}
}

func groupMembers() []graphMember {
	return []graphMember{
		{ODataType: "#microsoft.graph.user", ID: memberUserID, DisplayName: "A User"},
		{ODataType: "#microsoft.graph.group", ID: "77777777-7777-7777-7777-777777777777", DisplayName: "Nested"},
	}
}

// keyVault carries an access policy AND network rules; its role assignments
// come from the separate list below, because a vault uses one grant model or
// the other and this fixture exercises both arms of the resolver that reads
// them.
func keyVault() *armkeyvault.Vault {
	return &armkeyvault.Vault{
		ID:       new(vaultID),
		Name:     new("vault1"),
		Location: new("westeurope"),
		Properties: &armkeyvault.VaultProperties{
			TenantID:                new(tenantOne),
			SKU:                     &armkeyvault.SKU{Name: to.Ptr(armkeyvault.SKUNameStandard)},
			EnableSoftDelete:        new(true),
			EnablePurgeProtection:   new(true),
			EnableRbacAuthorization: new(true),
			AccessPolicies: []*armkeyvault.AccessPolicyEntry{{
				TenantID: new(tenantOne),
				ObjectID: new(groupObjectID),
				Permissions: &armkeyvault.Permissions{
					Secrets: []*armkeyvault.SecretPermissions{to.Ptr(armkeyvault.SecretPermissionsGet)},
				},
			}},
			NetworkACLs: &armkeyvault.NetworkRuleSet{
				VirtualNetworkRules: []*armkeyvault.VirtualNetworkRule{{ID: new(subnetID)}},
			},
		},
	}
}

func vaultRoleAssignments() []*armauthorization.RoleAssignment {
	return []*armauthorization.RoleAssignment{{
		Properties: &armauthorization.RoleAssignmentProperties{
			PrincipalID:      new(groupObjectID),
			Scope:            new(vaultID),
			RoleDefinitionID: new("/subscriptions/0000/providers/Microsoft.Authorization/roleDefinitions/kv-secrets-user"),
			PrincipalType:    to.Ptr(armauthorization.PrincipalTypeGroup),
		},
	}}
}

func managedCluster() *armcontainerservice.ManagedCluster {
	return &armcontainerservice.ManagedCluster{
		ID:       new(clusterID),
		Name:     new("aks1"),
		Location: new("westeurope"),
		Identity: &armcontainerservice.ManagedClusterIdentity{
			UserAssignedIdentities: map[string]*armcontainerservice.ManagedServiceIdentityUserAssignedIdentitiesValue{
				identityA: {},
			},
		},
		Properties: &armcontainerservice.ManagedClusterProperties{
			KubernetesVersion: new("1.30.2"),
			NodeResourceGroup: new("MC_rg_aks1_westeurope"),
			Fqdn:              new("aks1.hcp.westeurope.azmk8s.io"),
			AgentPoolProfiles: []*armcontainerservice.ManagedClusterAgentPoolProfile{{
				Name:         new("system"),
				VnetSubnetID: new(subnetID),
			}},
			OidcIssuerProfile: &armcontainerservice.ManagedClusterOIDCIssuerProfile{
				IssuerURL: new(aksIssuer),
			},
		},
	}
}

func containerRegistry() *armcontainerregistry.Registry {
	return &armcontainerregistry.Registry{
		ID:       new(registryID),
		Name:     new("reg1"),
		Location: new("westeurope"),
		SKU:      &armcontainerregistry.SKU{Name: to.Ptr(armcontainerregistry.SKUNameStandard)},
		Properties: &armcontainerregistry.RegistryProperties{
			LoginServer: new(registryHost),
			PrivateEndpointConnections: []*armcontainerregistry.PrivateEndpointConnection{{
				Properties: &armcontainerregistry.PrivateEndpointConnectionProperties{
					PrivateEndpoint: &armcontainerregistry.PrivateEndpoint{ID: new(peID)},
				},
			}},
		},
	}
}

// TestIdentityResource_StampsTheKeysTheResolversGateOn. Both keys are read
// BEFORE any edge is walked by the resolver that needs them, so an identity
// missing either produces nothing from that resolver while every count of edge
// types still passes.
func TestIdentityResource_StampsTheKeysTheResolversGateOn(t *testing.T) {
	r := fx{t}.res(identityResource(managedIdentity()))
	if r.tenantID != tenantOne {
		t.Errorf("the identity carries tenant %q forward, expected %q", r.tenantID, tenantOne)
	}
	if r.principalID != principalOne {
		t.Errorf("the identity carries principal %q forward, expected %q", r.principalID, principalOne)
	}
	md := r.node().Metadata
	if md[mdNodeTenantID] != tenantOne || md[mdNodePrincipalID] != principalOne {
		t.Errorf("the node does not carry both keys in metadata: %v", md)
	}
}

// TestRoleAssignmentEdges_CarryThePrincipalType is what distinguishes a role
// assignment from an attached identity: only the assignment knows what kind of
// principal it granted to.
func TestRoleAssignmentEdges_CarryThePrincipalType(t *testing.T) {
	edges := roleAssignmentEdges(identityA, roleAssignments())
	e, ok := edgeBetween(edges, identityA, scopeID, edgeAssumesRole)
	if !ok {
		t.Fatal("no assignment edge from the identity to the scope")
	}
	if e.metadata[mdPrincipalType] != "ServicePrincipal" {
		t.Errorf("the assignment does not carry its principal type: %v", e.metadata)
	}
	if e.metadata[mdSource] != "rbac" {
		t.Errorf("the assignment does not record that it came from a role assignment: %v", e.metadata)
	}

	// The negative: an assignment with no scope names nothing and draws nothing.
	scopeless := []*armauthorization.RoleAssignment{{Properties: &armauthorization.RoleAssignmentProperties{}}}
	if got := roleAssignmentEdges(identityA, scopeless); len(got) != 0 {
		t.Errorf("an assignment with no scope drew %d edges", len(got))
	}
}

// TestFederatedCredential_MintsANodeForEveryWorkloadEXCEPTKubernetes. The
// Kubernetes case is the one a Kubernetes collector owns, and minting a node
// for it here would put a foreign family's node in this graph.
func TestFederatedCredential_MintsANodeForEveryWorkloadEXCEPTKubernetes(t *testing.T) {
	for _, tc := range []struct {
		name       string
		issuer     string
		subject    string
		wantSource string
		wantNode   string
	}{
		{"github", githubIssuer, "repo:acme/service:ref:refs/heads/main", "github:acme/service", rtGitHubIdentity},
		{"generic oidc", foreignIssuer, "workload-one", "oidc:" + foreignIssuer + "/workload-one", rtOIDCIdentity},
		{"kubernetes", aksIssuer, "system:serviceaccount:apps:api", "apps/ServiceAccount/api", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resources, edges := federatedCredential(identityA, federatedCredentialFor(tc.issuer, tc.subject))
			e, ok := edgeBetween(edges, tc.wantSource, identityA, edgeWorkloadIdentity)
			if !ok {
				t.Fatalf("no workload edge from %q: %v", tc.wantSource, edges)
			}
			if e.metadata[mdIssuer] != tc.issuer {
				t.Errorf("the edge does not carry the issuer the trust resolver gates on: %v", e.metadata)
			}
			switch tc.wantNode {
			case "":
				if len(resources) != 0 {
					t.Errorf("a kubernetes workload minted %d nodes, which belong to a kubernetes collector", len(resources))
				}
			default:
				if len(resources) != 1 || resources[0].resourceType != tc.wantNode {
					t.Errorf("expected one %s node, got %v", tc.wantNode, resources)
				}
			}
		})
	}

	// The negative: a credential with no subject names no workload.
	resources, edges := federatedCredential(identityA, federatedCredentialFor(githubIssuer, ""))
	if len(resources) != 0 || len(edges) != 0 {
		t.Error("a credential with no subject produced a workload anyway")
	}
}

// TestVaultEdges_BothGrantModelsProduceTheSameRelationship, distinguished by
// their evidence. A subscription on role assignments exercises only the second,
// so a collector built to the first alone loses every grant on such a vault.
func TestVaultEdges_BothGrantModelsProduceTheSameRelationship(t *testing.T) {
	policy := fx{t}.edges(vaultEdges(keyVault()))
	e, ok := edgeBetween(policy, vaultID, groupObjectID, edgeAccessedBy)
	if !ok {
		t.Fatal("the access policy drew no grant edge")
	}
	if e.metadata[mdSource] != vaultGrantSourcePolicy {
		t.Errorf("the access-policy grant is not marked as one: %v", e.metadata)
	}
	if _, hasType := e.metadata[mdPrincipalType]; hasType {
		t.Error("an access policy carries a principal type, which an access policy does not record")
	}

	rbac := vaultRoleAssignmentEdges(vaultID, vaultRoleAssignments())
	e2, ok := edgeBetween(rbac, vaultID, groupObjectID, edgeAccessedBy)
	if !ok {
		t.Fatal("the role assignment drew no grant edge")
	}
	if e2.metadata[mdSource] != vaultGrantSourceRBAC {
		t.Errorf("the role-assignment grant is not marked as one: %v", e2.metadata)
	}
	if e2.metadata[mdPrincipalType] != "Group" {
		t.Errorf("the role assignment does not carry its principal type: %v", e2.metadata)
	}
}

// TestVaultUsesRBAC_DefaultsToTheOlderModel. A vault that does not say is on
// access policies, which is Azure's own default; treating silence as RBAC
// would make this walk list role assignments for every vault that has none.
func TestVaultUsesRBAC_DefaultsToTheOlderModel(t *testing.T) {
	silent := keyVault()
	silent.Properties.EnableRbacAuthorization = nil
	if vaultUsesRBAC(silent) {
		t.Error("a vault that does not declare its grant model was read as using role assignments")
	}
	if !vaultUsesRBAC(keyVault()) {
		t.Error("a vault that declares role assignments was read as not using them")
	}
}

// TestMembershipEdges_RunBothWays, so a query can start from the group or the
// member, and a nested group converges with its own node.
func TestMembershipEdges_RunBothWays(t *testing.T) {
	edges := membershipEdges(groupObjectID, groupMembers())
	groupNode := aadGroupIDPrefix + groupObjectID
	userNode := aadPrincipalIDPrefix + memberUserID
	if _, ok := edgeBetween(edges, groupNode, userNode, edgeHasMember); !ok {
		t.Error("no membership edge from the group to its member")
	}
	if _, ok := edgeBetween(edges, userNode, groupNode, edgeMemberOf); !ok {
		t.Error("no membership edge from the member back to its group")
	}
	nested := aadGroupIDPrefix + "77777777-7777-7777-7777-777777777777"
	if _, ok := edgeBetween(edges, groupNode, nested, edgeHasMember); !ok {
		t.Error("a nested group is not addressed as a group, so it will never converge with its own node")
	}
}

// TestRegistryResource_CarriesItsLoginServerForward is the walk fact resolver 3
// matches an image against.
func TestRegistryResource_CarriesItsLoginServerForward(t *testing.T) {
	r := fx{t}.res(registryResource(containerRegistry()))
	if r.acrLoginServer != registryHost {
		t.Errorf("the registry carried %q forward, expected %q", r.acrLoginServer, registryHost)
	}
	// Its private endpoint connections are read for edges and kept OUT of the
	// stored body, where a nested approval record would dominate the node.
	if contains([]string{string(r.content)}, "privateEndpointConnections") {
		t.Error("the registry's private endpoint connections reached the stored body")
	}
}
