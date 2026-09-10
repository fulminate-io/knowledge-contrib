// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/apimanagement/armapimanagement/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/appservice/armappservice/v4"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/logic/armlogic"
)

// fixtures_web_test.go — hand-built web, integration and edge responses.

const (
	webSiteID     = rgID + "/providers/Microsoft.Web/sites/web1"
	certID        = rgID + "/providers/Microsoft.Web/certificates/cert1"
	apimID        = rgID + "/providers/Microsoft.ApiManagement/service/apim1"
	apimAPIID     = apimID + "/apis/orders"
	workflowID    = rgID + "/providers/Microsoft.Logic/workflows/wf1"
	profileID     = rgID + "/providers/Microsoft.Cdn/profiles/fd1"
	afdEndpointID = profileID + "/afdEndpoints/edge1"
	containerRef  = registryHost + "/service:1.2.3"
)

func webFixtures() []fixture {
	return []fixture{
		{name: "app service site", build: func(t *testing.T) subResult {
			site := webSite()
			edges, proxies := siteEdges(site, "0000", map[string]bool{})
			return subResult{
				resources: append([]resource{fx{t}.res(siteResource(site, false))}, proxies...),
				edges:     edges,
			}
		}},
		{name: "function app with triggers", build: func(t *testing.T) subResult {
			site := functionApp()
			seen := map[string]bool{}
			edges, proxies := siteEdges(site, "0000", seen)
			triggerEdges, triggerProxies, err := triggerBindings(functionAppID, "0000", functionBindings(), seen)
			if err != nil {
				t.Fatalf("triggerBindings: %v", err)
			}
			return subResult{
				resources: append(append([]resource{fx{t}.res(siteResource(site, true))}, proxies...), triggerProxies...),
				edges:     append(edges, triggerEdges...),
			}
		}},
		{name: "app service certificate", build: func(t *testing.T) subResult {
			cert := appCertificate()
			edges, authorities := certificateEdges(cert, "0000", map[string]bool{})
			return subResult{
				resources: append([]resource{fx{t}.res(certificateResource(cert))}, authorities...),
				edges:     edges,
			}
		}},
		{name: "api management service and api", build: func(t *testing.T) subResult {
			svc := apimService()
			api := apimAPI()
			e, proxies, ok := backendRoute(apimID, ptr(api.Properties.ServiceURL), "0000", map[string]bool{})
			if !ok {
				t.Fatal("the api's backend url did not resolve to a route")
			}
			return subResult{
				resources: append([]resource{
					fx{t}.res(apimResource(svc)),
					fx{t}.res(apimAPIResource(api, svc)),
				}, proxies...),
				edges: append(apimEdges(svc), e),
			}
		}},
		{name: "logic app workflow", build: func(t *testing.T) subResult {
			wf := logicWorkflow()
			return subResult{
				resources: []resource{fx{t}.res(workflowResource(wf))},
				edges:     managedIdentityEdges(workflowID, keysOf(wf.Identity.UserAssignedIdentities)),
			}
		}},
		{name: "front door profile", build: func(t *testing.T) subResult {
			profile := frontDoorProfile()
			edges := originEdges(profile, frontDoorOrigin(), "origins")
			edges = append(edges, securityPolicyEdges(profile, frontDoorSecurityPolicy())...)
			edges = append(edges, customDomainEdges(profile, frontDoorDomain())...)
			edges = append(edges, containsEdges(profileID, afdEndpointID)...)
			return subResult{
				resources: []resource{
					fx{t}.res(profileResource(profile)),
					fx{t}.res(frontDoorEndpointResource(frontDoorEndpoint())),
				},
				edges: edges,
			}
		}},
	}
}

func webSite() *armappservice.Site {
	return &armappservice.Site{
		ID:       new(webSiteID),
		Name:     new("web1"),
		Location: new("westeurope"),
		Kind:     new("app,linux,container"),
		Identity: &armappservice.ManagedServiceIdentity{
			UserAssignedIdentities: map[string]*armappservice.UserAssignedIdentity{identityA: {}},
		},
		Properties: &armappservice.SiteProperties{
			State:                  new("Running"),
			DefaultHostName:        new("web1.azurewebsites.net"),
			HTTPSOnly:              new(true),
			VirtualNetworkSubnetID: new(subnetID),
			SiteConfig: &armappservice.SiteConfig{
				LinuxFxVersion: new("DOCKER|" + containerRef),
				AppSettings: []*armappservice.NameValuePair{
					{Name: new("SECRET"), Value: new("@Microsoft.KeyVault(SecretUri=https://vault1.vault.azure.net/secrets/api-key/)")},
					// A second reference to the SAME vault, which must draw one
					// edge rather than two.
					{Name: new("OTHER"), Value: new("@Microsoft.KeyVault(VaultName=vault1;SecretName=other)")},
					{Name: new("PLAIN"), Value: new("not-a-reference")},
				},
			},
		},
	}
}

func functionApp() *armappservice.Site {
	site := webSite()
	site.ID = new(functionAppID)
	site.Name = new("fn1")
	site.Kind = new("functionapp,linux")
	return site
}

// functionBindings is one function's binding configuration: a Service Bus
// trigger, a blob trigger, and an OUTPUT binding that must draw nothing.
func functionBindings() any {
	return map[string]any{
		"bindings": []any{
			map[string]any{"type": "serviceBusTrigger", "direction": "in", "queueName": "orders"},
			map[string]any{"type": "blobTrigger", "direction": "in", "path": "uploads/{name}"},
			map[string]any{"type": "eventHubTrigger", "direction": "in", "eventHubName": "telemetry"},
			map[string]any{"type": "queueTrigger", "direction": "in", "queueName": "jobs"},
			map[string]any{"type": "serviceBusTrigger", "direction": "in", "topicName": "events"},
			map[string]any{"type": "blob", "direction": "out", "path": "results/{name}"},
		},
	}
}

func appCertificate() *armappservice.AppCertificate {
	return &armappservice.AppCertificate{
		ID:       new(certID),
		Name:     new("cert1"),
		Location: new("westeurope"),
		Properties: &armappservice.AppCertificateProperties{
			Thumbprint:         new("ABCDEF"),
			SubjectName:        new("example.com"),
			Issuer:             new("Example CA"),
			Valid:              new(true),
			KeyVaultID:         new(vaultID),
			KeyVaultSecretName: new("tls"),
		},
	}
}

func apimService() *armapimanagement.ServiceResource {
	return &armapimanagement.ServiceResource{
		ID:       new(apimID),
		Name:     new("apim1"),
		Location: new("westeurope"),
		SKU:      &armapimanagement.ServiceSKUProperties{Name: to.Ptr(armapimanagement.SKUTypeDeveloper)},
		Identity: &armapimanagement.ServiceIdentity{
			UserAssignedIdentities: map[string]*armapimanagement.UserIdentityProperties{identityA: {}},
		},
		Properties: &armapimanagement.ServiceProperties{
			PublisherEmail: new("api@example.com"),
			GatewayURL:     new("https://apim1.azure-api.net"),
			VirtualNetworkConfiguration: &armapimanagement.VirtualNetworkConfiguration{
				SubnetResourceID: new(subnetID),
			},
		},
	}
}

func apimAPI() *armapimanagement.APIContract {
	return &armapimanagement.APIContract{
		ID:   new(apimAPIID),
		Name: new("orders"),
		Properties: &armapimanagement.APIContractProperties{
			ServiceURL:  new("https://backend.example.com/orders/v1"),
			Path:        new("orders"),
			DisplayName: new("Orders API"),
		},
	}
}

func logicWorkflow() *armlogic.Workflow {
	return &armlogic.Workflow{
		ID:       new(workflowID),
		Name:     new("wf1"),
		Location: new("westeurope"),
		Identity: &armlogic.ManagedServiceIdentity{
			UserAssignedIdentities: map[string]*armlogic.UserAssignedIdentity{identityA: {}},
		},
		Properties: &armlogic.WorkflowProperties{
			State: to.Ptr(armlogic.WorkflowStateEnabled),
		},
	}
}

func frontDoorProfile() *armcdn.Profile {
	return &armcdn.Profile{
		ID:       new(profileID),
		Name:     new("fd1"),
		Location: new("global"),
		SKU:      &armcdn.SKU{Name: to.Ptr(armcdn.SKUNamePremiumAzureFrontDoor)},
	}
}

func frontDoorEndpoint() *armcdn.AFDEndpoint {
	return &armcdn.AFDEndpoint{
		ID:       new(afdEndpointID),
		Name:     new("edge1"),
		Location: new("global"),
		Properties: &armcdn.AFDEndpointProperties{
			HostName: new("edge1.z01.azurefd.net"),
		},
	}
}

func frontDoorOrigin() *armcdn.AFDOrigin {
	return &armcdn.AFDOrigin{
		Name: new("origin1"),
		Properties: &armcdn.AFDOriginProperties{
			AzureOrigin: &armcdn.ResourceReference{ID: new(webSiteID)},
		},
	}
}

func frontDoorSecurityPolicy() *armcdn.SecurityPolicy {
	return &armcdn.SecurityPolicy{
		Name: new("waf"),
		Properties: &armcdn.SecurityPolicyProperties{
			Parameters: &armcdn.SecurityPolicyWebApplicationFirewallParameters{
				WafPolicy: &armcdn.ResourceReference{ID: new(wafPolicyID)},
			},
		},
	}
}

func frontDoorDomain() *armcdn.AFDDomain {
	return &armcdn.AFDDomain{
		Name: new("www-example-com"),
		Properties: &armcdn.AFDDomainProperties{
			TLSSettings: &armcdn.AFDDomainHTTPSParameters{
				Secret: &armcdn.ResourceReference{ID: new(profileID + "/secrets/tls")},
			},
		},
	}
}
