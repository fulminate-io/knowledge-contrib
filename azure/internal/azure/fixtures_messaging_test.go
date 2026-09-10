// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/eventgrid/armeventgrid/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/eventhub/armeventhub"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/servicebus/armservicebus/v2"
)

// fixtures_messaging_test.go — hand-built messaging responses.

const (
	sbNamespaceID = rgID + "/providers/Microsoft.ServiceBus/namespaces/sb1"
	sbQueueID     = sbNamespaceID + "/queues/orders"
	sbDLQTarget   = sbNamespaceID + "/queues/orders-dlq"
	sbTopicID     = sbNamespaceID + "/topics/events"
	sbSubID       = sbTopicID + "/subscriptions/audit"
	ehNamespaceID = rgID + "/providers/Microsoft.EventHub/namespaces/eh1"
	ehID          = ehNamespaceID + "/eventhubs/telemetry"
	ehGroupID     = ehID + "/consumergroups/readers"
	egTopicID     = rgID + "/providers/Microsoft.EventGrid/topics/eg1"
	egSubID       = egTopicID + "/providers/Microsoft.EventGrid/eventSubscriptions/sub1"
	functionAppID = rgID + "/providers/Microsoft.Web/sites/fn1"
)

func messagingFixtures() []fixture {
	return []fixture{
		{name: "service bus namespace and its entities", build: func(t *testing.T) subResult {
			ns := serviceBusNamespace()
			queue := serviceBusQueue()
			topic := serviceBusTopic()
			sub := serviceBusSubscription()
			edges := serviceBusNamespaceEdges(ns)
			edges = append(edges, containsEdges(sbNamespaceID, sbQueueID, sbTopicID, sbSubID)...)
			edges = append(edges, queueDeadLetterEdges(queue)...)
			edges = append(edges, subscriptionEdges(sub, topic)...)
			return subResult{
				resources: []resource{
					fx{t}.res(serviceBusNamespaceResource(ns)),
					fx{t}.res(queueResource(queue, ns)),
					fx{t}.res(topicResource(topic, ns)),
					fx{t}.res(subscriptionResource(sub, ns)),
				},
				edges: edges,
			}
		}},
		{name: "event hubs namespace and its hubs", build: func(t *testing.T) subResult {
			ns := eventHubNamespace()
			hub := eventHub()
			group := consumerGroup()
			edges := eventHubNamespaceEdges(ns)
			edges = append(edges, containsEdges(ehNamespaceID, ehID)...)
			edges = append(edges, captureEdges(hub)...)
			edges = append(edges, consumerGroupEdges(group, hub)...)
			return subResult{
				resources: []resource{
					fx{t}.res(eventHubNamespaceResource(ns)),
					fx{t}.res(eventHubResource(hub, ns)),
					fx{t}.res(consumerGroupResource(group, ns)),
				},
				edges: edges,
			}
		}},
		{name: "event grid topic and subscription", build: func(t *testing.T) subResult {
			topic := eventGridTopic()
			sub := eventGridSubscription()
			return subResult{
				resources: []resource{
					fx{t}.res(eventGridTopicResource(topic)),
					fx{t}.res(eventSubscriptionResource(sub, topic)),
				},
				edges: []edge{{from: egSubID, to: eventSubscriptionTarget(sub), relation: edgeTargets}},
			}
		}},
	}
}

func serviceBusNamespace() *armservicebus.SBNamespace {
	return &armservicebus.SBNamespace{
		ID:       new(sbNamespaceID),
		Name:     new("sb1"),
		Location: new("westeurope"),
		SKU: &armservicebus.SBSKU{
			Name: to.Ptr(armservicebus.SKUNamePremium),
			Tier: to.Ptr(armservicebus.SKUTierPremium),
		},
		Properties: &armservicebus.SBNamespaceProperties{
			Encryption: &armservicebus.Encryption{
				KeyVaultProperties: []*armservicebus.KeyVaultProperties{{
					KeyVaultURI: new(vaultBaseURI),
					KeyName:     new("cmk"),
				}},
			},
		},
	}
}

// serviceBusQueue forwards its undeliverable messages to a named sibling, which
// is one of the two dead-letter shapes.
func serviceBusQueue() *armservicebus.SBQueue {
	return &armservicebus.SBQueue{
		ID:   new(sbQueueID),
		Name: new("orders"),
		Properties: &armservicebus.SBQueueProperties{
			ForwardDeadLetteredMessagesTo: new("orders-dlq"),
		},
	}
}

func serviceBusTopic() *armservicebus.SBTopic {
	return &armservicebus.SBTopic{ID: new(sbTopicID), Name: new("events")}
}

// serviceBusSubscription uses the OTHER dead-letter shape: no forwarding, but
// expiry-based dead-lettering enabled, which sends them to its own sub-queue.
func serviceBusSubscription() *armservicebus.SBSubscription {
	return &armservicebus.SBSubscription{
		ID:   new(sbSubID),
		Name: new("audit"),
		Properties: &armservicebus.SBSubscriptionProperties{
			DeadLetteringOnMessageExpiration: new(true),
		},
	}
}

func eventHubNamespace() *armeventhub.EHNamespace {
	return &armeventhub.EHNamespace{
		ID:       new(ehNamespaceID),
		Name:     new("eh1"),
		Location: new("westeurope"),
		SKU: &armeventhub.SKU{
			Name: to.Ptr(armeventhub.SKUNameStandard),
			Tier: to.Ptr(armeventhub.SKUTierStandard),
		},
		Properties: &armeventhub.EHNamespaceProperties{
			Encryption: &armeventhub.Encryption{
				KeyVaultProperties: []*armeventhub.KeyVaultProperties{{
					KeyVaultURI: new(vaultBaseURI),
					KeyName:     new("cmk"),
				}},
			},
		},
	}
}

func eventHub() *armeventhub.Eventhub {
	return &armeventhub.Eventhub{
		ID:   new(ehID),
		Name: new("telemetry"),
		Properties: &armeventhub.Properties{
			CaptureDescription: &armeventhub.CaptureDescription{
				Enabled: new(true),
				Destination: &armeventhub.Destination{
					Properties: &armeventhub.DestinationProperties{
						StorageAccountResourceID: new(storageID),
					},
				},
			},
		},
	}
}

func consumerGroup() *armeventhub.ConsumerGroup {
	return &armeventhub.ConsumerGroup{ID: new(ehGroupID), Name: new("readers")}
}

func eventGridTopic() *armeventgrid.Topic {
	return &armeventgrid.Topic{
		ID:       new(egTopicID),
		Name:     new("eg1"),
		Location: new("westeurope"),
		Properties: &armeventgrid.TopicProperties{
			Endpoint: new("https://eg1.westeurope-1.eventgrid.azure.net/api/events"),
		},
	}
}

func eventGridSubscription() *armeventgrid.EventSubscription {
	return &armeventgrid.EventSubscription{
		ID:   new(egSubID),
		Name: new("sub1"),
		Properties: &armeventgrid.EventSubscriptionProperties{
			Destination: &armeventgrid.AzureFunctionEventSubscriptionDestination{
				Properties: &armeventgrid.AzureFunctionEventSubscriptionDestinationProperties{
					ResourceID: new(functionAppID),
				},
			},
		},
	}
}

// TestDeadLetterEdges_BothShapes. A queue that forwards names a sibling by BARE
// NAME, which has to be resolved against its own id; one that only enables
// expiry-based dead-lettering sends to a sub-queue that is not a resource at
// all, named by convention so the relationship stays legible.
func TestDeadLetterEdges_BothShapes(t *testing.T) {
	forwarding := queueDeadLetterEdges(serviceBusQueue())
	if _, ok := edgeBetween(forwarding, sbQueueID, sbDLQTarget, edgeDeadLettersTo); !ok {
		t.Errorf("a forwarded queue's bare destination name was not resolved to a sibling id: %v", forwarding)
	}

	implicit := subscriptionDeadLetterEdges(serviceBusSubscription())
	if _, ok := edgeBetween(implicit, sbSubID, sbSubID+"/$deadletterqueue", edgeDeadLettersTo); !ok {
		t.Errorf("an expiry-based subscription drew no edge to its own dead-letter sub-queue: %v", implicit)
	}

	// The negative: neither configured draws nothing.
	plain := &armservicebus.SBQueue{ID: new(sbQueueID), Properties: &armservicebus.SBQueueProperties{}}
	if got := queueDeadLetterEdges(plain); len(got) != 0 {
		t.Errorf("a queue with no dead-lettering drew %d edges", len(got))
	}
}

// TestDeadLetterEdges_AnAbsolutePathIsNotRewritten. A forwarding destination
// that is already a full resource id names an entity in another namespace, and
// rewriting it against the source's id would point it at the wrong namespace.
func TestDeadLetterEdges_AnAbsolutePathIsNotRewritten(t *testing.T) {
	elsewhere := rgID + "/providers/Microsoft.ServiceBus/namespaces/other/queues/dlq"
	q := &armservicebus.SBQueue{
		ID: new(sbQueueID),
		Properties: &armservicebus.SBQueueProperties{
			ForwardDeadLetteredMessagesTo: new(elsewhere),
		},
	}
	edges := queueDeadLetterEdges(q)
	if _, ok := edgeBetween(edges, sbQueueID, elsewhere, edgeDeadLettersTo); !ok {
		t.Errorf("an absolute forwarding destination was rewritten: %v", edges)
	}
}

// TestCaptureEdges_AreASinkAndNeedTheirEnableFlag. Capture is a continuous
// archive rather than an undeliverable destination, and a capture description
// that is present but DISABLED describes an archive that is not happening.
func TestCaptureEdges_AreASinkAndNeedTheirEnableFlag(t *testing.T) {
	if _, ok := edgeBetween(captureEdges(eventHub()), ehID, storageID, edgeSinksTo); !ok {
		t.Error("an enabled capture drew no sink edge")
	}
	disabled := eventHub()
	disabled.Properties.CaptureDescription.Enabled = new(false)
	if got := captureEdges(disabled); len(got) != 0 {
		t.Errorf("a disabled capture drew %d edges", len(got))
	}
}

// TestEventSubscriptionTarget_ReadsEveryDestinationArm. The destination is a
// sum type: each Azure arm names a resource id and the webhook arm names a URL,
// which is emitted too because a delivery out of Azure is what a reader most
// wants to see.
func TestEventSubscriptionTarget_ReadsEveryDestinationArm(t *testing.T) {
	for _, tc := range []struct {
		name string
		dest armeventgrid.EventSubscriptionDestinationClassification
		want string
	}{
		{"azure function", &armeventgrid.AzureFunctionEventSubscriptionDestination{
			Properties: &armeventgrid.AzureFunctionEventSubscriptionDestinationProperties{ResourceID: new(functionAppID)},
		}, functionAppID},
		{"event hub", &armeventgrid.EventHubEventSubscriptionDestination{
			Properties: &armeventgrid.EventHubEventSubscriptionDestinationProperties{ResourceID: new(ehID)},
		}, ehID},
		{"service bus queue", &armeventgrid.ServiceBusQueueEventSubscriptionDestination{
			Properties: &armeventgrid.ServiceBusQueueEventSubscriptionDestinationProperties{ResourceID: new(sbQueueID)},
		}, sbQueueID},
		{"service bus topic", &armeventgrid.ServiceBusTopicEventSubscriptionDestination{
			Properties: &armeventgrid.ServiceBusTopicEventSubscriptionDestinationProperties{ResourceID: new(sbTopicID)},
		}, sbTopicID},
		{"webhook", &armeventgrid.WebHookEventSubscriptionDestination{
			Properties: &armeventgrid.WebHookEventSubscriptionDestinationProperties{EndpointURL: new("https://hooks.example.com/x")},
		}, "https://hooks.example.com/x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sub := eventGridSubscription()
			sub.Properties.Destination = tc.dest
			if got := eventSubscriptionTarget(sub); got != tc.want {
				t.Errorf("target is %q, expected %q", got, tc.want)
			}
		})
	}

	// The negative: a subscription with no destination names nothing.
	none := &armeventgrid.EventSubscription{Properties: &armeventgrid.EventSubscriptionProperties{}}
	if got := eventSubscriptionTarget(none); got != "" {
		t.Errorf("a destinationless subscription named %q", got)
	}
}

// TestServiceBusEntities_InheritTheNamespaceRegion. An entity has no location
// of its own, and a node with no region is the one kind a region filter can
// never find.
func TestServiceBusEntities_InheritTheNamespaceRegion(t *testing.T) {
	ns := serviceBusNamespace()
	for _, r := range []resource{
		fx{t}.res(queueResource(serviceBusQueue(), ns)),
		fx{t}.res(topicResource(serviceBusTopic(), ns)),
		fx{t}.res(subscriptionResource(serviceBusSubscription(), ns)),
	} {
		if r.region != "westeurope" {
			t.Errorf("%s has region %q, not its namespace's", r.id, r.region)
		}
	}
}
