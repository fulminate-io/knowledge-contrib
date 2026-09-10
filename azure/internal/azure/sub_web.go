// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/appservice/armappservice/v4"
)

// sub_web.go — App Service sites and function apps.
//
// ONE LIST CALL SERVES BOTH, and the two subcollectors partition its results:
// Azure returns web apps and function apps from the same endpoint, told apart
// only by a `kind` field. Each subcollector makes the call and keeps its half,
// which costs one extra list and keeps the two independent, rather than one
// subcollector emitting two resource types and owning both failure modes.

// appServiceSub walks the subscription's App Service sites, excluding function
// apps.
type appServiceSub struct{ subBase }

func (s *appServiceSub) Collect(ctx context.Context) (subResult, error) {
	return collectSites(ctx, s.subBase, false)
}

// functionsSub walks the subscription's function apps, including the trigger
// bindings of the individual functions inside them.
type functionsSub struct{ subBase }

func (s *functionsSub) Collect(ctx context.Context) (subResult, error) {
	return collectSites(ctx, s.subBase, true)
}

// collectSites walks the sites of one kind.
func collectSites(ctx context.Context, base subBase, functionApps bool) (subResult, error) {
	client, err := armappservice.NewWebAppsClient(base.subscriptionID, base.cred, base.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("web apps client: %w", err)
	}

	var out subResult
	// The proxy nodes a site's settings and triggers reference are deduped
	// across the whole subcollector: two sites naming one vault discover one
	// vault, not two.
	seen := map[string]bool{}

	err = drain(ctx, client.NewListPager(nil), func(page armappservice.WebAppsClientListResponse) error {
		for _, site := range page.Value {
			if site == nil || site.ID == nil {
				continue
			}
			if isFunctionApp(site.Kind) != functionApps {
				continue
			}
			r, err := siteResource(site, functionApps)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			edges, proxies := siteEdges(site, base.subscriptionID, seen)
			out.edges = append(out.edges, edges...)
			out.resources = append(out.resources, proxies...)
			if !functionApps {
				continue
			}
			if err := collectTriggers(ctx, client, site, base.subscriptionID, seen, &out); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing sites: %w", err)
	}
	return out, nil
}

// isFunctionApp reports whether a site is a function app.
//
// The kind field is a comma-separated list of tags ("functionapp,linux"), so
// this reads the tags rather than the whole string, which is why a Linux
// function app is not mistaken for a web app.
func isFunctionApp(kind *string) bool {
	for part := range strings.SplitSeq(strings.ToLower(ptr(kind)), ",") {
		if strings.TrimSpace(part) == "functionapp" {
			return true
		}
	}
	return false
}

// siteContent is the curated projection stored on a site node.
//
// THE APP SETTINGS ARE DELIBERATELY ABSENT. They are read for edges before this
// projection is built, and they are the single most likely place in an Azure
// subscription for a connection string or a key to be sitting in plain text;
// storing them on a node would copy every one of them into the graph.
type siteContent struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Location   string         `json:"location,omitempty"`
	Kind       string         `json:"kind,omitempty"`
	Properties siteProperties `json:"properties"`
}

type siteProperties struct {
	State           string     `json:"state,omitempty"`
	DefaultHostName string     `json:"defaultHostName,omitempty"`
	HTTPSOnly       *bool      `json:"httpsOnly,omitempty"`
	SiteConfig      siteConfig `json:"siteConfig"`
}

type siteConfig struct {
	LinuxFxVersion   string `json:"linuxFxVersion,omitempty"`
	WindowsFxVersion string `json:"windowsFxVersion,omitempty"`
}

func siteResource(site *armappservice.Site, functionApp bool) (resource, error) {
	proj := siteContent{
		ID:       ptr(site.ID),
		Name:     ptr(site.Name),
		Location: ptr(site.Location),
		Kind:     ptr(site.Kind),
	}
	if p := site.Properties; p != nil {
		proj.Properties.State = ptr(p.State)
		proj.Properties.DefaultHostName = ptr(p.DefaultHostName)
		if p.HTTPSOnly != nil {
			v := *p.HTTPSOnly
			proj.Properties.HTTPSOnly = &v
		}
		if p.SiteConfig != nil {
			proj.Properties.SiteConfig.LinuxFxVersion = ptr(p.SiteConfig.LinuxFxVersion)
			proj.Properties.SiteConfig.WindowsFxVersion = ptr(p.SiteConfig.WindowsFxVersion)
		}
	}
	content, err := marshalContent(proj)
	if err != nil {
		return resource{}, fmt.Errorf("projecting site %s: %w", ptr(site.ID), err)
	}

	resourceType := rtWebSite
	if functionApp {
		resourceType = rtFunctionApp
	}
	r := resource{
		id:           ptr(site.ID),
		name:         ptr(site.Name),
		resourceType: resourceType,
		region:       ptr(site.Location),
		content:      content,
		metadata:     map[string]string{},
		// The walk fact resolver 3 reads: whichever of the two runtime fields
		// carries a container image, if either does.
		containerImage: siteContainerImage(proj.Properties.SiteConfig),
	}
	setIfNotEmpty(r.metadata, "kind", proj.Kind)
	setIfNotEmpty(r.metadata, "state", proj.Properties.State)
	setIfNotEmpty(r.metadata, "defaultHostName", proj.Properties.DefaultHostName)
	if proj.Properties.HTTPSOnly != nil {
		r.metadata["httpsOnly"] = strconv.FormatBool(*proj.Properties.HTTPSOnly)
	}
	setIfNotEmpty(r.metadata, "containerImage", r.containerImage)
	return r, nil
}

// siteContainerImage reads the container image a site runs, from whichever of
// the two runtime fields carries one.
func siteContainerImage(cfg siteConfig) string {
	if img := parseDockerFxVersion(cfg.LinuxFxVersion); img != "" {
		return img
	}
	return parseDockerFxVersion(cfg.WindowsFxVersion)
}

// siteEdges draws a site's identities, its network integration and the vaults
// its settings reference, together with the proxy nodes those references need.
func siteEdges(site *armappservice.Site, subscriptionID string, seen map[string]bool) ([]edge, []resource) {
	id := ptr(site.ID)
	var out []edge
	if site.Identity != nil {
		out = append(out, managedIdentityEdges(id, keysOf(site.Identity.UserAssignedIdentities))...)
	}
	if site.Properties == nil {
		return out, nil
	}
	if subnetID := ptr(site.Properties.VirtualNetworkSubnetID); subnetID != "" {
		out = append(out, edge{from: id, to: subnetID, relation: edgeUsesSubnet})
	}
	if site.Properties.SiteConfig == nil {
		return out, nil
	}
	vaultEdges, proxies := vaultReferenceEdges(id, subscriptionID, site.Properties.SiteConfig.AppSettings, seen)
	return append(out, vaultEdges...), proxies
}
