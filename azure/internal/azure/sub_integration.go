// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/apimanagement/armapimanagement/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/logic/armlogic"
)

// sub_integration.go — API Management and Logic Apps.

// apimBackendID names a backend an API routes to, keyed by hostname.
//
// THE HOST IS THE KEY, not the whole URL: the same backend is reached at
// several paths by several APIs, and keying on the URL would mint a node per
// route rather than per backend.
func apimBackendID(host string) string { return "azure:apim:backend:" + host }

// apimSub walks the subscription's API Management services and their APIs.
type apimSub struct{ subBase }

func (s *apimSub) Collect(ctx context.Context) (subResult, error) {
	services, err := armapimanagement.NewServiceClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("api management services client: %w", err)
	}
	apis, err := armapimanagement.NewAPIClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("api management apis client: %w", err)
	}

	var out subResult
	seen := map[string]bool{}

	err = drain(ctx, services.NewListPager(nil), func(page armapimanagement.ServiceClientListResponse) error {
		for _, svc := range page.Value {
			if svc == nil || svc.ID == nil {
				continue
			}
			r, err := apimResource(svc)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			out.edges = append(out.edges, apimEdges(svc)...)
			if err := s.collectAPIs(ctx, apis, svc, seen, &out); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing api management services: %w", err)
	}
	return out, nil
}

func apimResource(svc *armapimanagement.ServiceResource) (resource, error) {
	content, err := marshalContent(svc)
	if err != nil {
		return resource{}, fmt.Errorf("projecting api management service %s: %w", ptr(svc.ID), err)
	}
	r := resource{
		id:           ptr(svc.ID),
		name:         ptr(svc.Name),
		resourceType: rtAPIMService,
		region:       ptr(svc.Location),
		content:      content,
		metadata:     map[string]string{},
	}
	if svc.SKU != nil && svc.SKU.Name != nil {
		r.metadata["skuName"] = string(*svc.SKU.Name)
	}
	if p := svc.Properties; p != nil {
		setIfNotEmpty(r.metadata, "publisherEmail", ptr(p.PublisherEmail))
		setIfNotEmpty(r.metadata, "gatewayUrl", ptr(p.GatewayURL))
	}
	return r, nil
}

func apimEdges(svc *armapimanagement.ServiceResource) []edge {
	id := ptr(svc.ID)
	var out []edge
	if svc.Properties != nil && svc.Properties.VirtualNetworkConfiguration != nil {
		if subnetID := ptr(svc.Properties.VirtualNetworkConfiguration.SubnetResourceID); subnetID != "" {
			out = append(out, edge{from: id, to: subnetID, relation: edgeUsesSubnet})
		}
	}
	if svc.Identity != nil {
		out = append(out, managedIdentityEdges(id, keysOf(svc.Identity.UserAssignedIdentities))...)
	}
	return out
}

// collectAPIs enumerates one service's APIs and draws where each routes.
func (s *apimSub) collectAPIs(
	ctx context.Context,
	client *armapimanagement.APIClient,
	svc *armapimanagement.ServiceResource,
	seen map[string]bool,
	out *subResult,
) error {
	rg, name := armResourceGroup(ptr(svc.ID)), ptr(svc.Name)
	if rg == "" || name == "" {
		return nil
	}
	// Two APIs on one service routing to one backend draw one edge.
	routed := map[string]bool{}

	err := drain(ctx, client.NewListByServicePager(rg, name, nil),
		func(page armapimanagement.APIClientListByServiceResponse) error {
			for _, api := range page.Value {
				if api == nil || api.ID == nil {
					continue
				}
				r, err := apimAPIResource(api, svc)
				if err != nil {
					return err
				}
				out.resources = append(out.resources, r)

				if api.Properties == nil {
					continue
				}
				serviceURL := ptr(api.Properties.ServiceURL)
				if serviceURL == "" {
					continue
				}
				e, p, ok := backendRoute(ptr(svc.ID), serviceURL, s.subscriptionID, seen)
				if !ok || routed[e.to] {
					continue
				}
				routed[e.to] = true
				out.edges = append(out.edges, e)
				out.resources = append(out.resources, p...)
			}
			return nil
		})
	if err != nil {
		return fmt.Errorf("listing the apis of service %s: %w", name, err)
	}
	return nil
}

func apimAPIResource(api *armapimanagement.APIContract, svc *armapimanagement.ServiceResource) (resource, error) {
	content, err := marshalContent(api)
	if err != nil {
		return resource{}, fmt.Errorf("projecting api %s: %w", ptr(api.ID), err)
	}
	r := resource{
		id:           ptr(api.ID),
		name:         ptr(api.Name),
		resourceType: rtAPIMAPI,
		region:       ptr(svc.Location),
		content:      content,
		metadata:     map[string]string{},
	}
	if api.Properties != nil {
		setIfNotEmpty(r.metadata, "serviceUrl", ptr(api.Properties.ServiceURL))
		setIfNotEmpty(r.metadata, "path", ptr(api.Properties.Path))
		setIfNotEmpty(r.metadata, "displayName", ptr(api.Properties.DisplayName))
	}
	return r, nil
}

// backendRoute draws where an API routes, and mints the proxy for it.
//
// THE ROUTE IS THE SERVICE'S, not the API's, and that is deliberate: an
// operator asking what a gateway talks to wants the gateway's backends, and an
// edge per API would repeat the same backend once per route.
func backendRoute(serviceID, serviceURL, subscriptionID string, seen map[string]bool) (edge, []resource, bool) {
	parsed, err := url.Parse(serviceURL)
	if err != nil {
		return edge{}, nil, false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return edge{}, nil, false
	}
	id := apimBackendID(host)
	e := edge{
		from:     serviceID,
		to:       id,
		relation: edgeRoutesTo,
		metadata: map[string]string{"serviceUrl": serviceURL},
	}
	if seen[id] {
		return e, nil, true
	}
	seen[id] = true
	p := proxy(id, host, rtAPIMBackend,
		"an api names its backend by url, which carries no resource id",
		"api management backend url", subscriptionID)
	p.metadata["host"] = host
	return e, []resource{p}, true
}

// logicAppSub walks the subscription's Logic Apps workflows.
type logicAppSub struct{ subBase }

func (s *logicAppSub) Collect(ctx context.Context) (subResult, error) {
	client, err := armlogic.NewWorkflowsClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("logic apps client: %w", err)
	}
	var out subResult
	err = drain(ctx, client.NewListBySubscriptionPager(nil), func(page armlogic.WorkflowsClientListBySubscriptionResponse) error {
		for _, wf := range page.Value {
			if wf == nil || wf.ID == nil {
				continue
			}
			r, err := workflowResource(wf)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			if wf.Identity != nil {
				out.edges = append(out.edges, managedIdentityEdges(ptr(wf.ID), keysOf(wf.Identity.UserAssignedIdentities))...)
			}
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing logic apps: %w", err)
	}
	return out, nil
}

// workflowContent is the curated projection stored on a workflow node.
//
// THE WORKFLOW DEFINITION IS DELIBERATELY ABSENT. It is an arbitrarily large
// document describing every step of the workflow, it commonly embeds
// connection parameters, and nothing in this graph reads it.
type workflowContent struct {
	ID         string             `json:"id"`
	Name       string             `json:"name"`
	Location   string             `json:"location,omitempty"`
	Properties workflowProperties `json:"properties"`
}

type workflowProperties struct {
	State             string `json:"state,omitempty"`
	ProvisioningState string `json:"provisioningState,omitempty"`
	SKU               string `json:"sku,omitempty"`
}

func workflowResource(wf *armlogic.Workflow) (resource, error) {
	proj := workflowContent{ID: ptr(wf.ID), Name: ptr(wf.Name), Location: ptr(wf.Location)}
	if p := wf.Properties; p != nil {
		if p.State != nil {
			proj.Properties.State = string(*p.State)
		}
		if p.ProvisioningState != nil {
			proj.Properties.ProvisioningState = string(*p.ProvisioningState)
		}
		if p.SKU != nil && p.SKU.Name != nil {
			proj.Properties.SKU = string(*p.SKU.Name)
		}
	}
	content, err := marshalContent(proj)
	if err != nil {
		return resource{}, fmt.Errorf("projecting workflow %s: %w", ptr(wf.ID), err)
	}
	r := resource{
		id:           ptr(wf.ID),
		name:         ptr(wf.Name),
		resourceType: rtWorkflow,
		region:       ptr(wf.Location),
		content:      content,
		metadata:     map[string]string{},
	}
	setIfNotEmpty(r.metadata, "state", proj.Properties.State)
	setIfNotEmpty(r.metadata, "provisioningState", proj.Properties.ProvisioningState)
	setIfNotEmpty(r.metadata, "skuName", proj.Properties.SKU)
	return r, nil
}
