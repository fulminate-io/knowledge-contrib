// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/appservice/armappservice/v4"
)

// sub_web_bindings.go — the two things a site references by NAME rather than by
// id: the key vaults its settings read secrets from, and the messaging entities
// its functions trigger on.
//
// BOTH PRODUCE PROXY NODES, and the reason is the same in each case: the
// reference does not carry enough identity to reconstruct an ARM id. A vault
// reference names the vault's hostname or bare name, with no resource group; a
// trigger binding names a queue or hub by name, with no namespace. Emitting the
// edge to a namespaced proxy keeps the relationship legible and keeps the
// missing identity explicit, which an edge to a guessed ARM id would not.

// vaultReferencePattern recognizes a Key Vault reference in an app setting.
// Azure spells them two ways and both begin the same.
var vaultReferencePattern = regexp.MustCompile(`@Microsoft\.KeyVault\(`)

// The two spellings, each naming the vault differently: by secret URI, whose
// host's first label is the vault name, or by bare vault name.
var (
	vaultSecretURIPattern = regexp.MustCompile(`SecretUri=https://([^.]+)\.vault\.azure\.net`)
	vaultNamePattern      = regexp.MustCompile(`VaultName=([^;)]+)`)
)

// vaultProxyID names a vault referenced without a resource group.
func vaultProxyID(vaultName string) string { return "azure:keyvault:" + vaultName }

// vaultReferenceEdges draws the vaults a site's settings read secrets from.
func vaultReferenceEdges(
	siteID, subscriptionID string, settings []*armappservice.NameValuePair, seen map[string]bool,
) ([]edge, []resource) {
	var edges []edge
	var proxies []resource
	// A site referencing one vault from five settings gets one edge.
	perSite := map[string]bool{}

	for _, setting := range settings {
		if setting == nil {
			continue
		}
		value := ptr(setting.Value)
		if !vaultReferencePattern.MatchString(value) {
			continue
		}
		vaultName := referencedVaultName(value)
		if vaultName == "" || perSite[vaultName] {
			continue
		}
		perSite[vaultName] = true

		id := vaultProxyID(vaultName)
		edges = append(edges, edge{from: siteID, to: id, relation: edgeMountsSecret})
		if seen[id] {
			continue
		}
		seen[id] = true
		proxies = append(proxies, proxy(id, vaultName, rtVaultProxy,
			"an app setting names a key vault without a resource group, so its ARM id cannot be reconstructed",
			"app service key vault reference", subscriptionID))
	}
	return edges, proxies
}

// referencedVaultName reads the vault name out of a Key Vault reference.
func referencedVaultName(value string) string {
	if m := vaultSecretURIPattern.FindStringSubmatch(value); len(m) > 1 {
		return m[1]
	}
	if m := vaultNamePattern.FindStringSubmatch(value); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// collectTriggers reads the individual functions in a function app and draws
// what each one triggers on.
func collectTriggers(
	ctx context.Context,
	client *armappservice.WebAppsClient,
	site *armappservice.Site,
	subscriptionID string,
	seen map[string]bool,
	out *subResult,
) error {
	rg, name := armResourceGroup(ptr(site.ID)), ptr(site.Name)
	if rg == "" || name == "" {
		return nil
	}
	err := drain(ctx, client.NewListFunctionsPager(rg, name, nil),
		func(page armappservice.WebAppsClientListFunctionsResponse) error {
			for _, fn := range page.Value {
				if fn == nil || fn.Properties == nil || fn.Properties.Config == nil {
					continue
				}
				edges, proxies, err := triggerBindings(ptr(site.ID), subscriptionID, fn.Properties.Config, seen)
				if err != nil {
					return err
				}
				out.edges = append(out.edges, edges...)
				out.resources = append(out.resources, proxies...)
			}
			return nil
		})
	if err != nil {
		return fmt.Errorf("listing the functions of app %s: %w", name, err)
	}
	return nil
}

// functionConfig is the shape of a function's binding configuration. The SDK
// models it as an untyped value, so it is round-tripped through JSON to read.
type functionConfig struct {
	Bindings []functionBinding `json:"bindings"`
}

type functionBinding struct {
	Type         string `json:"type"`
	Direction    string `json:"direction"`
	Connection   string `json:"connection"`
	QueueName    string `json:"queueName"`
	TopicName    string `json:"topicName"`
	EventHubName string `json:"eventHubName"`
	Path         string `json:"path"`
}

// triggerBindings draws what a function triggers on.
//
// ONLY TRIGGER BINDINGS PRODUCE EDGES. A function's bindings include its
// outputs and its inputs as well, and an output binding is a write rather than
// a cause; drawing it as a trigger would invert the direction of the
// relationship.
func triggerBindings(
	siteID, subscriptionID string, config any, seen map[string]bool,
) ([]edge, []resource, error) {
	raw, err := json.Marshal(config)
	if err != nil {
		return nil, nil, fmt.Errorf("re-encoding a function's binding configuration: %w", err)
	}
	var cfg functionConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, nil, fmt.Errorf("decoding a function's binding configuration: %w", err)
	}

	var edges []edge
	var proxies []resource
	for _, b := range cfg.Bindings {
		if !strings.HasSuffix(strings.ToLower(b.Type), "trigger") {
			continue
		}
		id, resourceType, name := triggerTarget(b)
		if id == "" {
			continue
		}
		edges = append(edges, edge{
			from:     siteID,
			to:       id,
			relation: edgeTriggers,
			metadata: map[string]string{"triggerType": b.Type},
		})
		if seen[id] {
			continue
		}
		seen[id] = true
		proxies = append(proxies, proxy(id, name, resourceType,
			"a function trigger names its source entity without a namespace or account, so its ARM id cannot be reconstructed",
			"function app trigger binding", subscriptionID))
	}
	return edges, proxies, nil
}

// triggerTarget names what a trigger binding fires on: its proxy id, the
// resource type of the node to emit for it, and its display name.
//
// THE CONNECTION SETTING IS NOT READ. It names an app setting holding a
// connection string, which is where the namespace or account would come from —
// and reading it would mean reading a secret out of the settings to identify a
// resource. The name is enough to draw the relationship.
func triggerTarget(b functionBinding) (id, resourceType, name string) {
	kind := strings.ToLower(b.Type)
	switch {
	case strings.Contains(kind, "servicebus"):
		if b.QueueName != "" {
			return "azure:servicebus:queue:" + b.QueueName, rtSBQueueProxy, b.QueueName
		}
		if b.TopicName != "" {
			return "azure:servicebus:topic:" + b.TopicName, rtSBTopicProxy, b.TopicName
		}
	case strings.Contains(kind, "eventhub"):
		if b.EventHubName != "" {
			return "azure:eventhub:hub:" + b.EventHubName, rtEventHubProxy, b.EventHubName
		}
	case strings.Contains(kind, "queue"):
		if b.QueueName != "" {
			return "azure:storage:queue:" + b.QueueName, rtStorageQueue, b.QueueName
		}
	case strings.Contains(kind, "blob"):
		if b.Path != "" {
			return "azure:storage:blob:" + b.Path, rtStorageBlob, b.Path
		}
	}
	return "", "", ""
}
