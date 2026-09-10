// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
)

// fixtures_web_behavior_test.go — the behavioral rows over the web fixtures in
// fixtures_web_test.go. They are split from the fixtures by file size alone.

// TestIsFunctionApp_ReadsTheKindTags. The kind field is a comma-separated list,
// so a Linux function app is "functionapp,linux" and a substring match on the
// whole string would also claim every app whose kind merely contains the word.
func TestIsFunctionApp_ReadsTheKindTags(t *testing.T) {
	for _, tc := range []struct {
		kind string
		want bool
	}{
		{"functionapp", true},
		{"functionapp,linux", true},
		{"FunctionApp,Linux", true},
		{"app,linux,container", false},
		{"app", false},
		{"", false},
	} {
		if got := isFunctionApp(new(tc.kind)); got != tc.want {
			t.Errorf("kind %q read as functionApp=%v, expected %v", tc.kind, got, tc.want)
		}
	}
	if isFunctionApp(nil) {
		t.Error("a site with no kind was read as a function app")
	}
}

// TestSiteResource_CarriesTheContainerImageAndDropsTheSettings. The image is
// the walk fact resolver 3 reads; the settings are where a connection string
// most often sits in plain text, and storing them would copy every one into the
// graph.
func TestSiteResource_CarriesTheContainerImageAndDropsTheSettings(t *testing.T) {
	r := fx{t}.res(siteResource(webSite(), false))
	if r.containerImage != containerRef {
		t.Errorf("the site carried image %q forward, expected %q", r.containerImage, containerRef)
	}
	body := string(r.content)
	for _, forbidden := range []string{"appSettings", "@Microsoft.KeyVault", "not-a-reference"} {
		if containsSubstring(body, forbidden) {
			t.Errorf("the stored body carries %q, which is app-settings material", forbidden)
		}
	}

	// A site running code rather than a container carries no image.
	code := webSite()
	code.Properties.SiteConfig.LinuxFxVersion = new("PYTHON|3.12")
	codeResource := fx{t}.res(siteResource(code, false))
	if codeResource.containerImage != "" {
		t.Errorf("a site running a runtime stack reported image %q", codeResource.containerImage)
	}
}

// TestVaultReferenceEdges_OneEdgePerVaultAndOneProxyPerWalk. Both spellings of
// a Key Vault reference name the same vault, and a site referencing it five
// times has one relationship with it.
func TestVaultReferenceEdges_OneEdgePerVaultAndOneProxyPerWalk(t *testing.T) {
	seen := map[string]bool{}
	edges, proxies := vaultReferenceEdges(webSiteID, "0000", webSite().Properties.SiteConfig.AppSettings, seen)
	if len(edges) != 1 {
		t.Errorf("two references to one vault drew %d edges: %v", len(edges), edges)
	}
	if _, ok := edgeBetween(edges, webSiteID, vaultProxyID("vault1"), edgeMountsSecret); !ok {
		t.Errorf("no secret edge to the referenced vault: %v", edges)
	}
	if len(proxies) != 1 {
		t.Fatalf("expected one vault proxy, got %d", len(proxies))
	}
	if proxies[0].metadata[metaCollected] != "false" {
		t.Error("the vault proxy does not record that it was not collected")
	}

	// A SECOND site referencing the same vault draws its own edge and NO
	// second proxy, because the walk already has the node.
	edges2, proxies2 := vaultReferenceEdges(functionAppID, "0000", webSite().Properties.SiteConfig.AppSettings, seen)
	if len(edges2) != 1 {
		t.Errorf("the second site drew %d edges", len(edges2))
	}
	if len(proxies2) != 0 {
		t.Errorf("the second site minted %d duplicate proxies", len(proxies2))
	}
}

// TestTriggerBindings_OnlyTriggersDrawEdges. A function's bindings include its
// outputs, and an output is a write rather than a cause; drawing one as a
// trigger would invert the relationship.
func TestTriggerBindings_OnlyTriggersDrawEdges(t *testing.T) {
	edges, proxies, err := triggerBindings(functionAppID, "0000", functionBindings(), map[string]bool{})
	if err != nil {
		t.Fatalf("triggerBindings: %v", err)
	}
	if len(edges) != 5 {
		t.Errorf("expected five trigger edges, got %d: %v", len(edges), edges)
	}
	for _, e := range edges {
		if e.to == "azure:storage:blob:results/{name}" {
			t.Error("an OUTPUT binding was drawn as a trigger")
		}
	}
	types := resourceTypesOf(proxies)
	for _, want := range []string{rtSBQueueProxy, rtSBTopicProxy, rtEventHubProxy, rtStorageQueue, rtStorageBlob} {
		if types[want] != 1 {
			t.Errorf("expected one %s proxy, got %d", want, types[want])
		}
	}

	// The negative: a binding configuration that decodes to nothing draws
	// nothing rather than failing.
	none, _, err := triggerBindings(functionAppID, "0000", map[string]any{}, map[string]bool{})
	if err != nil {
		t.Fatalf("an empty binding configuration errored: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("an empty binding configuration drew %d edges", len(none))
	}
}

// TestCertificateEdges_StorageAndMaterialAreDifferentTargets, and the authority
// is minted once however many certificates it issued.
func TestCertificateEdges_StorageAndMaterialAreDifferentTargets(t *testing.T) {
	seen := map[string]bool{}
	edges, authorities := certificateEdges(appCertificate(), "0000", seen)
	if _, ok := edgeBetween(edges, certID, vaultID, edgeStoredIn); !ok {
		t.Error("no storage edge from the certificate to its vault")
	}
	if _, ok := edgeBetween(edges, certID, vaultID+"/secrets/tls", edgeEncryptsWith); !ok {
		t.Error("no material edge from the certificate to the secret holding it")
	}
	if _, ok := edgeBetween(edges, certID, certificateAuthorityID("Example CA"), edgeIssuedBy); !ok {
		t.Error("no issuer edge from the certificate to its authority")
	}
	if len(authorities) != 1 {
		t.Fatalf("expected one authority node, got %d", len(authorities))
	}

	_, again := certificateEdges(appCertificate(), "0000", seen)
	if len(again) != 0 {
		t.Errorf("a second certificate from the same authority minted %d duplicate nodes", len(again))
	}
}

// TestBackendRoute_KeysOnTheHost. Several APIs route to one backend at several
// paths, and keying on the whole url would mint a node per route.
func TestBackendRoute_KeysOnTheHost(t *testing.T) {
	seen := map[string]bool{}
	first, proxies, ok := backendRoute(apimID, "https://backend.example.com/orders/v1", "0000", seen)
	if !ok {
		t.Fatal("a well-formed backend url did not resolve")
	}
	if first.to != apimBackendID("backend.example.com") {
		t.Errorf("the route targets %q rather than the backend host", first.to)
	}
	if len(proxies) != 1 {
		t.Fatalf("expected one backend proxy, got %d", len(proxies))
	}

	second, more, ok := backendRoute(apimID, "https://backend.example.com/invoices/v2", "0000", seen)
	if !ok {
		t.Fatal("the second url did not resolve")
	}
	if second.to != first.to {
		t.Errorf("two paths on one host produced two targets: %q and %q", first.to, second.to)
	}
	if len(more) != 0 {
		t.Errorf("the second route minted %d duplicate proxies", len(more))
	}

	// The negative: a url with no host resolves to nothing.
	if _, _, ok := backendRoute(apimID, "not a url at all", "0000", seen); ok {
		t.Error("a url with no host resolved to a backend")
	}
}

// TestIsFrontDoorProfile_KeepsOnlyFrontDoorSKUs. Front Door and classic CDN
// share one resource type, and a classic profile has none of the endpoint,
// origin or policy structure this walk reads.
func TestIsFrontDoorProfile_KeepsOnlyFrontDoorSKUs(t *testing.T) {
	if !isFrontDoorProfile(frontDoorProfile()) {
		t.Error("a premium Front Door profile was not recognized")
	}
	classic := frontDoorProfile()
	classic.SKU = &armcdn.SKU{Name: to.Ptr(armcdn.SKUNameStandardMicrosoft)}
	if isFrontDoorProfile(classic) {
		t.Error("a classic CDN profile was walked as a Front Door profile")
	}
	skuless := frontDoorProfile()
	skuless.SKU = nil
	if isFrontDoorProfile(skuless) {
		t.Error("a profile with no SKU was walked as a Front Door profile")
	}
}

// TestFrontDoorEdges_OriginsPolicyAndCertificate, including the negative on a
// non-Azure origin, which is named by hostname and has no node here.
func TestFrontDoorEdges_OriginsPolicyAndCertificate(t *testing.T) {
	profile := frontDoorProfile()
	if _, ok := edgeBetween(originEdges(profile, frontDoorOrigin(), "g"), profileID, webSiteID, edgeRoutesTo); !ok {
		t.Error("an Azure origin drew no route")
	}
	external := frontDoorOrigin()
	external.Properties.AzureOrigin = nil
	if got := originEdges(profile, external, "g"); len(got) != 0 {
		t.Errorf("an origin outside Azure drew %d edges", len(got))
	}

	if _, ok := edgeBetween(securityPolicyEdges(profile, frontDoorSecurityPolicy()), wafPolicyID, profileID, edgeProtects); !ok {
		t.Error("the firewall policy drew no guarding edge, or drew it in the wrong direction")
	}
	if _, ok := edgeBetween(customDomainEdges(profile, frontDoorDomain()), profileID, profileID+"/secrets/tls", edgeUsesCert); !ok {
		t.Error("a domain with TLS settings drew no certificate edge")
	}
	plain := frontDoorDomain()
	plain.Properties.TLSSettings = nil
	if got := customDomainEdges(profile, plain); len(got) != 0 {
		t.Errorf("a domain with no TLS settings drew %d edges", len(got))
	}
}

func containsSubstring(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
