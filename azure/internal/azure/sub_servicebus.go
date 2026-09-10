// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/servicebus/armservicebus/v2"
)

// sub_servicebus.go — Service Bus namespaces and the entities inside them.

// serviceBusSub walks the subscription's Service Bus namespaces, their queues,
// their topics and each topic's subscriptions.
type serviceBusSub struct{ subBase }

func (s *serviceBusSub) Collect(ctx context.Context) (subResult, error) {
	namespaces, err := armservicebus.NewNamespacesClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("service bus namespaces client: %w", err)
	}
	queues, err := armservicebus.NewQueuesClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("service bus queues client: %w", err)
	}
	topics, err := armservicebus.NewTopicsClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("service bus topics client: %w", err)
	}
	subscriptions, err := armservicebus.NewSubscriptionsClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("service bus subscriptions client: %w", err)
	}

	var out subResult
	err = drain(ctx, namespaces.NewListPager(nil), func(page armservicebus.NamespacesClientListResponse) error {
		for _, ns := range page.Value {
			if ns == nil || ns.ID == nil {
				continue
			}
			r, err := serviceBusNamespaceResource(ns)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			out.edges = append(out.edges, serviceBusNamespaceEdges(ns)...)
			if err := s.collectNetworkRules(ctx, namespaces, ns, &out); err != nil {
				return err
			}
			if err := s.collectQueues(ctx, queues, ns, &out); err != nil {
				return err
			}
			if err := s.collectTopics(ctx, topics, subscriptions, ns, &out); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing service bus namespaces: %w", err)
	}
	return out, nil
}

func serviceBusNamespaceResource(ns *armservicebus.SBNamespace) (resource, error) {
	content, err := marshalContent(ns)
	if err != nil {
		return resource{}, fmt.Errorf("projecting service bus namespace %s: %w", ptr(ns.ID), err)
	}
	r := resource{
		id:           ptr(ns.ID),
		name:         ptr(ns.Name),
		resourceType: rtServiceBusNS,
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

// serviceBusNamespaceEdges draws the namespace's customer-managed keys. A
// namespace can carry several, one per encrypted store.
func serviceBusNamespaceEdges(ns *armservicebus.SBNamespace) []edge {
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

// collectNetworkRules reads the namespace's network rule set, which is a
// SEPARATE GET rather than a field on the namespace, and draws the subnets it
// admits.
func (s *serviceBusSub) collectNetworkRules(
	ctx context.Context, client *armservicebus.NamespacesClient, ns *armservicebus.SBNamespace, out *subResult,
) error {
	rg, name := armResourceGroup(ptr(ns.ID)), ptr(ns.Name)
	if rg == "" || name == "" {
		return nil
	}
	resp, err := client.GetNetworkRuleSet(ctx, rg, name, nil)
	if err != nil {
		return fmt.Errorf("reading the network rule set of service bus namespace %s: %w", name, err)
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

func (s *serviceBusSub) collectQueues(
	ctx context.Context, client *armservicebus.QueuesClient, ns *armservicebus.SBNamespace, out *subResult,
) error {
	rg, name := armResourceGroup(ptr(ns.ID)), ptr(ns.Name)
	if rg == "" || name == "" {
		return nil
	}
	err := drain(ctx, client.NewListByNamespacePager(rg, name, nil),
		func(page armservicebus.QueuesClientListByNamespaceResponse) error {
			for _, q := range page.Value {
				if q == nil || q.ID == nil {
					continue
				}
				r, err := queueResource(q, ns)
				if err != nil {
					return err
				}
				out.resources = append(out.resources, r)
				out.edges = append(out.edges, containsEdges(ptr(ns.ID), ptr(q.ID))...)
				out.edges = append(out.edges, queueDeadLetterEdges(q)...)
			}
			return nil
		})
	if err != nil {
		return fmt.Errorf("listing queues of service bus namespace %s: %w", name, err)
	}
	return nil
}

func (s *serviceBusSub) collectTopics(
	ctx context.Context,
	topics *armservicebus.TopicsClient,
	subscriptions *armservicebus.SubscriptionsClient,
	ns *armservicebus.SBNamespace,
	out *subResult,
) error {
	rg, name := armResourceGroup(ptr(ns.ID)), ptr(ns.Name)
	if rg == "" || name == "" {
		return nil
	}
	err := drain(ctx, topics.NewListByNamespacePager(rg, name, nil),
		func(page armservicebus.TopicsClientListByNamespaceResponse) error {
			for _, t := range page.Value {
				if t == nil || t.ID == nil || t.Name == nil {
					continue
				}
				r, err := topicResource(t, ns)
				if err != nil {
					return err
				}
				out.resources = append(out.resources, r)
				out.edges = append(out.edges, containsEdges(ptr(ns.ID), ptr(t.ID))...)
				if err := s.collectSubscriptions(ctx, subscriptions, rg, ns, t, out); err != nil {
					return err
				}
			}
			return nil
		})
	if err != nil {
		return fmt.Errorf("listing topics of service bus namespace %s: %w", name, err)
	}
	return nil
}

// collectSubscriptions enumerates one topic's subscriptions.
//
// A SUBSCRIPTION IS CONTAINED BY THE NAMESPACE AND SUBSCRIBES TO THE TOPIC:
// containment follows the ARM hierarchy, which nests it under the namespace,
// while the subscription relationship is the one an operator cares about, and
// they run in opposite directions.
func (s *serviceBusSub) collectSubscriptions(
	ctx context.Context,
	client *armservicebus.SubscriptionsClient,
	resourceGroup string,
	ns *armservicebus.SBNamespace,
	topic *armservicebus.SBTopic,
	out *subResult,
) error {
	err := drain(ctx, client.NewListByTopicPager(resourceGroup, ptr(ns.Name), ptr(topic.Name), nil),
		func(page armservicebus.SubscriptionsClientListByTopicResponse) error {
			for _, sub := range page.Value {
				if sub == nil || sub.ID == nil {
					continue
				}
				r, err := subscriptionResource(sub, ns)
				if err != nil {
					return err
				}
				out.resources = append(out.resources, r)
				out.edges = append(out.edges, containsEdges(ptr(ns.ID), ptr(sub.ID))...)
				out.edges = append(out.edges, subscriptionEdges(sub, topic)...)
			}
			return nil
		})
	if err != nil {
		return fmt.Errorf("listing subscriptions of topic %s: %w", ptr(topic.Name), err)
	}
	return nil
}

// subscriptionEdges draws what a topic subscription is a subscription TO, and
// where its undeliverable messages go.
//
// IT IS A FUNCTION RATHER THAN TWO LINES INSIDE THE LISTER because a
// relationship emitted only inside a lister is one no test can reach without
// standing the whole listing layer up: both of this collector's SUBSCRIBES_TO
// emissions were once inline, and both could be deleted with a green suite
// because the fixtures that claimed to cover them built the edge themselves.
func subscriptionEdges(sub *armservicebus.SBSubscription, topic *armservicebus.SBTopic) []edge {
	if sub == nil || sub.ID == nil || topic == nil || topic.ID == nil {
		return nil
	}
	out := []edge{{from: *sub.ID, to: *topic.ID, relation: edgeSubscribesTo}}
	return append(out, subscriptionDeadLetterEdges(sub)...)
}

// queueDeadLetterEdges draws where a queue's undeliverable messages go.
//
// TWO SHAPES, AND THE SECOND IS IMPLICIT. A queue configured to forward its
// dead letters names the destination entity, which is a sibling under the same
// namespace and is named by bare name rather than by id. A queue with no
// forwarding but with expiry-based dead-lettering enabled sends them to its own
// dead-letter sub-queue, which has no resource of its own; the edge names it by
// the conventional suffix, so the relationship is legible even though the
// target is not a resource ARM knows.
func queueDeadLetterEdges(q *armservicebus.SBQueue) []edge {
	if q == nil || q.ID == nil || q.Properties == nil {
		return nil
	}
	return deadLetterEdges(*q.ID, ptr(q.Properties.ForwardDeadLetteredMessagesTo), q.Properties.DeadLetteringOnMessageExpiration)
}

func subscriptionDeadLetterEdges(sub *armservicebus.SBSubscription) []edge {
	if sub == nil || sub.ID == nil || sub.Properties == nil {
		return nil
	}
	return deadLetterEdges(*sub.ID, ptr(sub.Properties.ForwardDeadLetteredMessagesTo), sub.Properties.DeadLetteringOnMessageExpiration)
}

func deadLetterEdges(sourceID, forwardTo string, onExpiration *bool) []edge {
	if forwardTo != "" {
		return []edge{{from: sourceID, to: siblingID(sourceID, forwardTo), relation: edgeDeadLettersTo}}
	}
	if onExpiration != nil && *onExpiration {
		return []edge{{from: sourceID, to: sourceID + "/$deadletterqueue", relation: edgeDeadLettersTo}}
	}
	return nil
}

// queueResource, topicResource and subscriptionResource are the three entity
// conversions, each inheriting the namespace's region because an entity has no
// location of its own.
func queueResource(q *armservicebus.SBQueue, ns *armservicebus.SBNamespace) (resource, error) {
	return entityResource(ptr(q.ID), ptr(q.Name), rtServiceBusQueue, ptr(ns.Location), q)
}

func topicResource(t *armservicebus.SBTopic, ns *armservicebus.SBNamespace) (resource, error) {
	return entityResource(ptr(t.ID), ptr(t.Name), rtServiceBusTopic, ptr(ns.Location), t)
}

func subscriptionResource(sub *armservicebus.SBSubscription, ns *armservicebus.SBNamespace) (resource, error) {
	return entityResource(ptr(sub.ID), ptr(sub.Name), rtServiceBusSub, ptr(ns.Location), sub)
}
