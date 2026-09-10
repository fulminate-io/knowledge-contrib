// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/eventgrid/armeventgrid/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/eventhub/armeventhub"
)

// sub_eventing.go — Event Hubs namespaces with their hubs and consumer groups,
// and Event Grid topics with their subscriptions.

// eventHubSub walks the subscription's Event Hubs namespaces.
type eventHubSub struct{ subBase }

func (s *eventHubSub) Collect(ctx context.Context) (subResult, error) {
	namespaces, err := armeventhub.NewNamespacesClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("event hubs namespaces client: %w", err)
	}
	hubs, err := armeventhub.NewEventHubsClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("event hubs client: %w", err)
	}
	groups, err := armeventhub.NewConsumerGroupsClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("event hubs consumer groups client: %w", err)
	}

	var out subResult
	err = drain(ctx, namespaces.NewListPager(nil), func(page armeventhub.NamespacesClientListResponse) error {
		for _, ns := range page.Value {
			if ns == nil || ns.ID == nil {
				continue
			}
			r, err := eventHubNamespaceResource(ns)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			out.edges = append(out.edges, eventHubNamespaceEdges(ns)...)
			if err := s.collectNetworkRules(ctx, namespaces, ns, &out); err != nil {
				return err
			}
			if err := s.collectHubs(ctx, hubs, groups, ns, &out); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing event hubs namespaces: %w", err)
	}
	return out, nil
}

func eventHubNamespaceResource(ns *armeventhub.EHNamespace) (resource, error) {
	content, err := marshalContent(ns)
	if err != nil {
		return resource{}, fmt.Errorf("projecting event hubs namespace %s: %w", ptr(ns.ID), err)
	}
	r := resource{
		id:           ptr(ns.ID),
		name:         ptr(ns.Name),
		resourceType: rtEventHubNS,
		region:       ptr(ns.Location),
		content:      content,
		metadata:     map[string]string{},
	}
	if ns.SKU != nil {
		if ns.SKU.Name != nil {
			r.metadata["skuName"] = string(*ns.SKU.Name)
		}
		if ns.SKU.Tier != nil {
			r.metadata["skuTier"] = string(*ns.SKU.Tier)
		}
	}
	return r, nil
}

func eventHubNamespaceEdges(ns *armeventhub.EHNamespace) []edge {
	if ns.Properties == nil || ns.Properties.Encryption == nil {
		return nil
	}
	var out []edge
	for _, kvp := range ns.Properties.Encryption.KeyVaultProperties {
		if kvp == nil {
			continue
		}
		out = append(out, keyVaultKeyEdges(ptr(ns.ID), ptr(kvp.KeyVaultURI), ptr(kvp.KeyName), ptr(kvp.KeyVersion))...)
	}
	return out
}

func (s *eventHubSub) collectNetworkRules(
	ctx context.Context, client *armeventhub.NamespacesClient, ns *armeventhub.EHNamespace, out *subResult,
) error {
	rg, name := armResourceGroup(ptr(ns.ID)), ptr(ns.Name)
	if rg == "" || name == "" {
		return nil
	}
	resp, err := client.GetNetworkRuleSet(ctx, rg, name, nil)
	if err != nil {
		return fmt.Errorf("reading the network rule set of event hubs namespace %s: %w", name, err)
	}
	if resp.Properties == nil {
		return nil
	}
	for _, rule := range resp.Properties.VirtualNetworkRules {
		if rule == nil || rule.Subnet == nil {
			continue
		}
		if subnetID := ptr(rule.Subnet.ID); subnetID != "" {
			out.edges = append(out.edges, edge{from: ptr(ns.ID), to: subnetID, relation: edgeUsesSubnet})
		}
	}
	return nil
}

func (s *eventHubSub) collectHubs(
	ctx context.Context,
	hubs *armeventhub.EventHubsClient,
	groups *armeventhub.ConsumerGroupsClient,
	ns *armeventhub.EHNamespace,
	out *subResult,
) error {
	rg, name := armResourceGroup(ptr(ns.ID)), ptr(ns.Name)
	if rg == "" || name == "" {
		return nil
	}
	err := drain(ctx, hubs.NewListByNamespacePager(rg, name, nil),
		func(page armeventhub.EventHubsClientListByNamespaceResponse) error {
			for _, hub := range page.Value {
				if hub == nil || hub.ID == nil || hub.Name == nil {
					continue
				}
				r, err := eventHubResource(hub, ns)
				if err != nil {
					return err
				}
				out.resources = append(out.resources, r)
				out.edges = append(out.edges, containsEdges(ptr(ns.ID), ptr(hub.ID))...)
				out.edges = append(out.edges, captureEdges(hub)...)
				if err := s.collectConsumerGroups(ctx, groups, rg, ns, hub, out); err != nil {
					return err
				}
			}
			return nil
		})
	if err != nil {
		return fmt.Errorf("listing hubs of namespace %s: %w", name, err)
	}
	return nil
}

func (s *eventHubSub) collectConsumerGroups(
	ctx context.Context,
	client *armeventhub.ConsumerGroupsClient,
	resourceGroup string,
	ns *armeventhub.EHNamespace,
	hub *armeventhub.Eventhub,
	out *subResult,
) error {
	err := drain(ctx, client.NewListByEventHubPager(resourceGroup, ptr(ns.Name), ptr(hub.Name), nil),
		func(page armeventhub.ConsumerGroupsClientListByEventHubResponse) error {
			for _, cg := range page.Value {
				if cg == nil || cg.ID == nil {
					continue
				}
				r, err := consumerGroupResource(cg, ns)
				if err != nil {
					return err
				}
				out.resources = append(out.resources, r)
				out.edges = append(out.edges, consumerGroupEdges(cg, hub)...)
			}
			return nil
		})
	if err != nil {
		return fmt.Errorf("listing consumer groups of hub %s: %w", ptr(hub.Name), err)
	}
	return nil
}

// consumerGroupEdges draws what a consumer group reads from. Like
// [subscriptionEdges] it is a function rather than an inline literal, for the
// same reason: an emission inside a lister is unreachable from a converter test.
func consumerGroupEdges(cg *armeventhub.ConsumerGroup, hub *armeventhub.Eventhub) []edge {
	if cg == nil || cg.ID == nil || hub == nil || hub.ID == nil {
		return nil
	}
	return []edge{{from: *cg.ID, to: *hub.ID, relation: edgeSubscribesTo}}
}

// captureEdges draws where a hub archives its events.
//
// IT IS A SINK RATHER THAN A DEAD-LETTER RELATIONSHIP: capture is a continuous
// archive of everything the hub receives, not a destination for what could not
// be delivered.
func captureEdges(hub *armeventhub.Eventhub) []edge {
	if hub.Properties == nil {
		return nil
	}
	capture := hub.Properties.CaptureDescription
	if capture == nil || capture.Enabled == nil || !*capture.Enabled {
		return nil
	}
	if capture.Destination == nil || capture.Destination.Properties == nil {
		return nil
	}
	storageID := ptr(capture.Destination.Properties.StorageAccountResourceID)
	if storageID == "" {
		return nil
	}
	return []edge{{from: ptr(hub.ID), to: storageID, relation: edgeSinksTo}}
}

// eventGridSub walks the subscription's Event Grid topics and the subscriptions
// on them.
type eventGridSub struct{ subBase }

func (s *eventGridSub) Collect(ctx context.Context) (subResult, error) {
	topics, err := armeventgrid.NewTopicsClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("event grid topics client: %w", err)
	}
	subs, err := armeventgrid.NewEventSubscriptionsClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("event grid subscriptions client: %w", err)
	}

	var out subResult
	err = drain(ctx, topics.NewListBySubscriptionPager(nil), func(page armeventgrid.TopicsClientListBySubscriptionResponse) error {
		for _, topic := range page.Value {
			if topic == nil || topic.ID == nil || topic.Name == nil {
				continue
			}
			r, err := eventGridTopicResource(topic)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			if err := s.collectEventSubscriptions(ctx, subs, topic, &out); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing event grid topics: %w", err)
	}
	return out, nil
}

func (s *eventGridSub) collectEventSubscriptions(
	ctx context.Context, client *armeventgrid.EventSubscriptionsClient, topic *armeventgrid.Topic, out *subResult,
) error {
	rg := armResourceGroup(ptr(topic.ID))
	if rg == "" {
		return nil
	}
	pager := client.NewListByResourcePager(rg, "Microsoft.EventGrid", "topics", ptr(topic.Name), nil)
	err := drain(ctx, pager, func(page armeventgrid.EventSubscriptionsClientListByResourceResponse) error {
		for _, sub := range page.Value {
			if sub == nil || sub.ID == nil {
				continue
			}
			r, err := eventSubscriptionResource(sub, topic)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			if target := eventSubscriptionTarget(sub); target != "" {
				out.edges = append(out.edges, edge{from: ptr(sub.ID), to: target, relation: edgeTargets})
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("listing subscriptions of event grid topic %s: %w", ptr(topic.Name), err)
	}
	return nil
}

// eventSubscriptionTarget reads where an event subscription delivers.
//
// THE DESTINATION IS A SUM TYPE and each arm carries a different identity: the
// Azure destinations name a resource id, and the webhook destination names a
// URL. The URL is emitted as the target anyway, because an endpoint outside
// Azure is exactly the delivery a reader most wants to see.
func eventSubscriptionTarget(sub *armeventgrid.EventSubscription) string {
	if sub.Properties == nil || sub.Properties.Destination == nil {
		return ""
	}
	switch d := sub.Properties.Destination.(type) {
	case *armeventgrid.AzureFunctionEventSubscriptionDestination:
		if d.Properties != nil {
			return ptr(d.Properties.ResourceID)
		}
	case *armeventgrid.EventHubEventSubscriptionDestination:
		if d.Properties != nil {
			return ptr(d.Properties.ResourceID)
		}
	case *armeventgrid.ServiceBusQueueEventSubscriptionDestination:
		if d.Properties != nil {
			return ptr(d.Properties.ResourceID)
		}
	case *armeventgrid.ServiceBusTopicEventSubscriptionDestination:
		if d.Properties != nil {
			return ptr(d.Properties.ResourceID)
		}
	case *armeventgrid.StorageQueueEventSubscriptionDestination:
		if d.Properties != nil {
			return ptr(d.Properties.ResourceID)
		}
	case *armeventgrid.WebHookEventSubscriptionDestination:
		if d.Properties != nil {
			return ptr(d.Properties.EndpointURL)
		}
	}
	return ""
}

// eventHubResource and consumerGroupResource are the two child conversions of
// an Event Hubs namespace.
func eventHubResource(hub *armeventhub.Eventhub, ns *armeventhub.EHNamespace) (resource, error) {
	return entityResource(ptr(hub.ID), ptr(hub.Name), rtEventHub, ptr(ns.Location), hub)
}

func consumerGroupResource(cg *armeventhub.ConsumerGroup, ns *armeventhub.EHNamespace) (resource, error) {
	return entityResource(ptr(cg.ID), ptr(cg.Name), rtConsumerGroup, ptr(ns.Location), cg)
}

// eventGridTopicResource converts one Event Grid topic. Its endpoint is
// promoted to metadata because it is what a publisher is configured with, and
// is therefore what an operator searches for.
func eventGridTopicResource(topic *armeventgrid.Topic) (resource, error) {
	r, err := entityResource(ptr(topic.ID), ptr(topic.Name), rtEventGridTopic, ptr(topic.Location), topic)
	if err != nil {
		return resource{}, err
	}
	if p := topic.Properties; p != nil {
		setIfNotEmpty(r.metadata, "endpoint", ptr(p.Endpoint))
		if p.ProvisioningState != nil {
			r.metadata["provisioningState"] = string(*p.ProvisioningState)
		}
	}
	return r, nil
}

// eventSubscriptionResource converts one event subscription, inheriting the
// topic's region.
func eventSubscriptionResource(sub *armeventgrid.EventSubscription, topic *armeventgrid.Topic) (resource, error) {
	return entityResource(ptr(sub.ID), ptr(sub.Name), rtEventGridSub, ptr(topic.Location), sub)
}
