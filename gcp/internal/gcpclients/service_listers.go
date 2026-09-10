// SPDX-License-Identifier: Apache-2.0

package gcpclients

import (
	"context"
	"fmt"
	"log/slog"

	artifactregistrypb "cloud.google.com/go/artifactregistry/apiv1/artifactregistrypb"
	containerpb "cloud.google.com/go/container/apiv1/containerpb"
	eventarcpb "cloud.google.com/go/eventarc/apiv1/eventarcpb"
	filestorepb "cloud.google.com/go/filestore/apiv1/filestorepb"
	functionspb "cloud.google.com/go/functions/apiv2/functionspb"
	adminpb "cloud.google.com/go/iam/admin/apiv1/adminpb"
	iampb "cloud.google.com/go/iam/apiv1/iampb"
	kmspb "cloud.google.com/go/kms/apiv1/kmspb"
	loggingpb "cloud.google.com/go/logging/apiv2/loggingpb"
	monitoringpb "cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"
	"cloud.google.com/go/pubsub"
	redispb "cloud.google.com/go/redis/apiv1/redispb"
	resourcemanagerpb "cloud.google.com/go/resourcemanager/apiv3/resourcemanagerpb"
	runpb "cloud.google.com/go/run/apiv2/runpb"
	secretmanagerpb "cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	gcs "cloud.google.com/go/storage"
	workflowspb "cloud.google.com/go/workflows/apiv1/workflowspb"
	"google.golang.org/api/iterator"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/collect"
)

// service_listers.go — the enumerations for everything that is not compute.
//
// A LOCATION-SCOPED LIST USES THE WILDCARD. Most of these APIs list per
// location, and asking for every location by name would need a second call to
// discover the locations. The provider accepts "-" as "every location", which is
// one call and cannot go stale against a new region.
const allLocations = "-"

// logCloseFailure reports a client that would not close. It goes to STDERR
// through slog and never to stdout: on the stdio transport stdout carries the
// protocol, and a stray line there is an opaque handshake failure rather than a
// log message.
func logCloseFailure(err error) {
	slog.Warn("a gcp client did not close cleanly", "error", err)
}

// subcollectors is the whole walk: every enumeration, in one list.
func (c *clients) subcollectors() []collect.Subcollector {
	subs := c.computeSubcollectors()
	subs = append(subs, c.platformSubcollectors()...)
	subs = append(subs, c.dataSubcollectors()...)
	subs = append(subs, c.restSubcollectors()...)
	return subs
}

func (c *clients) platformSubcollectors() []collect.Subcollector {
	return []collect.Subcollector{
		collect.GKEClusters(func(ctx context.Context, project string) ([]*containerpb.Cluster, error) {
			resp, err := c.container.ListClusters(ctx, &containerpb.ListClustersRequest{
				Parent: fmt.Sprintf("projects/%s/locations/%s", project, allLocations),
			})
			if err != nil {
				return nil, err
			}
			// A cluster list names the zones it could not reach on its own
			// field rather than as an error, so a caller that read only the
			// clusters would see a short list as a complete one.
			return resp.GetClusters(), partialRead(resp.GetMissingZones())
		}),
		collect.RunServices(func(ctx context.Context, project string) ([]*runpb.Service, error) {
			it := c.run.ListServices(ctx, &runpb.ListServicesRequest{
				Parent: fmt.Sprintf("projects/%s/locations/%s", project, allLocations),
			})
			return drain(it.Next, unreachableSignal[*runpb.ListServicesResponse](func() any { return it.Response }))
		}),
		collect.CloudFunctions(func(ctx context.Context, project string) ([]*functionspb.Function, error) {
			it := c.functions.ListFunctions(ctx, &functionspb.ListFunctionsRequest{
				Parent: fmt.Sprintf("projects/%s/locations/%s", project, allLocations),
			})
			return drain(it.Next, unreachableSignal[*functionspb.ListFunctionsResponse](func() any { return it.Response }))
		}),
		collect.ServiceAccounts(func(ctx context.Context, project string) ([]*adminpb.ServiceAccount, error) {
			it := c.iam.ListServiceAccounts(ctx, &adminpb.ListServiceAccountsRequest{
				Name: "projects/" + project,
			})
			return drain(it.Next, nil)
		}),
		collect.Projects(func(ctx context.Context, project string) ([]*resourcemanagerpb.Project, error) {
			got, err := c.resourceManager.GetProject(ctx, &resourcemanagerpb.GetProjectRequest{
				Name: "projects/" + project,
			})
			if err != nil {
				return nil, err
			}
			return []*resourcemanagerpb.Project{got}, nil
		}),
		collect.PolicyBindings(c.listPolicyBindings),
		collect.IdentityGroups(c.listIdentityGroups),
		collect.ArtifactRepositories(func(ctx context.Context, project string) ([]*artifactregistrypb.Repository, error) {
			it := c.artifactRegistry.ListRepositories(ctx, &artifactregistrypb.ListRepositoriesRequest{
				Parent: fmt.Sprintf("projects/%s/locations/%s", project, allLocations),
			})
			return drain(it.Next, nil)
		}),
	}
}

func (c *clients) dataSubcollectors() []collect.Subcollector {
	return []collect.Subcollector{
		collect.StorageBuckets(func(ctx context.Context, project string) ([]*gcs.BucketAttrs, error) {
			it := c.storage.Buckets(ctx, project)
			return drain(it.Next, nil)
		}),
		collect.PubSubTopics(c.listPubSubTopics),
		collect.PubSubSubscriptions(c.listPubSubSubscriptions),
		collect.Secrets(func(ctx context.Context, project string) ([]*secretmanagerpb.Secret, error) {
			it := c.secrets.ListSecrets(ctx, &secretmanagerpb.ListSecretsRequest{
				Parent: "projects/" + project,
			})
			return drain(it.Next, nil)
		}),
		collect.KMSKeyRings(func(ctx context.Context, project string) ([]*kmspb.KeyRing, error) {
			it := c.kms.ListKeyRings(ctx, &kmspb.ListKeyRingsRequest{
				Parent: fmt.Sprintf("projects/%s/locations/%s", project, allLocations),
			})
			return drain(it.Next, nil)
		}),
		collect.KMSCryptoKeys(c.listCryptoKeys),
		collect.RedisInstances(func(ctx context.Context, project string) ([]*redispb.Instance, error) {
			it := c.redis.ListInstances(ctx, &redispb.ListInstancesRequest{
				Parent: fmt.Sprintf("projects/%s/locations/%s", project, allLocations),
			})
			return drain(it.Next, unreachableSignal[*redispb.ListInstancesResponse](func() any { return it.Response }))
		}),
		collect.FilestoreInstances(func(ctx context.Context, project string) ([]*filestorepb.Instance, error) {
			it := c.filestore.ListInstances(ctx, &filestorepb.ListInstancesRequest{
				Parent: fmt.Sprintf("projects/%s/locations/%s", project, allLocations),
			})
			return drain(it.Next, unreachableSignal[*filestorepb.ListInstancesResponse](func() any { return it.Response }))
		}),
		collect.LogSinks(func(ctx context.Context, project string) ([]*loggingpb.LogSink, error) {
			it := c.logging.ListSinks(ctx, &loggingpb.ListSinksRequest{Parent: "projects/" + project})
			return drain(it.Next, nil)
		}),
		collect.AlertPolicies(func(ctx context.Context, project string) ([]*monitoringpb.AlertPolicy, error) {
			it := c.alertPolicies.ListAlertPolicies(ctx, &monitoringpb.ListAlertPoliciesRequest{
				Name: "projects/" + project,
			})
			return drain(it.Next, nil)
		}),
		collect.NotificationChannels(func(ctx context.Context, project string) ([]*monitoringpb.NotificationChannel, error) {
			it := c.channels.ListNotificationChannels(ctx, &monitoringpb.ListNotificationChannelsRequest{
				Name: "projects/" + project,
			})
			return drain(it.Next, nil)
		}),
		collect.EventarcTriggers(func(ctx context.Context, project string) ([]*eventarcpb.Trigger, error) {
			it := c.eventarc.ListTriggers(ctx, &eventarcpb.ListTriggersRequest{
				Parent: fmt.Sprintf("projects/%s/locations/%s", project, allLocations),
			})
			return drain(it.Next, unreachableSignal[*eventarcpb.ListTriggersResponse](func() any { return it.Response }))
		}),
		collect.Workflows(func(ctx context.Context, project string) ([]*workflowspb.Workflow, error) {
			it := c.workflows.ListWorkflows(ctx, &workflowspb.ListWorkflowsRequest{
				Parent: fmt.Sprintf("projects/%s/locations/%s", project, allLocations),
			})
			return drain(it.Next, unreachableSignal[*workflowspb.ListWorkflowsResponse](func() any { return it.Response }))
		}),
	}
}

// listCryptoKeys walks every key ring first, because keys are listed per ring
// and the wildcard location does not extend to the ring segment.
func (c *clients) listCryptoKeys(ctx context.Context, project string) ([]*kmspb.CryptoKey, error) {
	ringIt := c.kms.ListKeyRings(ctx, &kmspb.ListKeyRingsRequest{
		Parent: fmt.Sprintf("projects/%s/locations/%s", project, allLocations),
	})
	rings, err := drain(ringIt.Next, nil)
	if err != nil {
		return nil, err
	}
	var out []*kmspb.CryptoKey
	for _, ring := range rings {
		keyIt := c.kms.ListCryptoKeys(ctx, &kmspb.ListCryptoKeysRequest{Parent: ring.GetName()})
		keys, err := drain(keyIt.Next, nil)
		// The partial result is kept: one ring the caller may not read should not
		// cost every other ring's keys.
		out = append(out, keys...)
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

// listPolicyBindings flattens the project's own IAM policy into one row per
// (role, principal) pair.
func (c *clients) listPolicyBindings(ctx context.Context, project string) ([]collect.PolicyBinding, error) {
	policy, err := c.resourceManager.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{
		Resource: "projects/" + project,
	})
	if err != nil {
		return nil, err
	}
	resourceID := "projects/" + project
	var out []collect.PolicyBinding
	for _, binding := range policy.GetBindings() {
		for _, member := range binding.GetMembers() {
			out = append(out, collect.PolicyBinding{
				ResourceID: resourceID, Role: binding.GetRole(), Principal: member,
			})
		}
	}
	return out, nil
}

// listPubSubTopics reads each topic's configuration, which is a second call per
// topic: the iterator yields a handle rather than a value.
func (c *clients) listPubSubTopics(ctx context.Context, _ string) ([]collect.PubSubTopicInfo, error) {
	it := c.pubsub.Topics(ctx)
	var out []collect.PubSubTopicInfo
	for {
		topic, err := it.Next()
		if err == iterator.Done {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		info := collect.PubSubTopicInfo{ID: topic.String(), Name: topic.ID()}
		// A configuration this run could not read is RETURNED, not logged and
		// skipped. What it carries is the encryption relationship, and a topic
		// emitted without one is indistinguishable from a topic that has none —
		// so the walk reports itself incomplete rather than quietly asserting
		// that this topic is unencrypted.
		cfg, err := topic.Config(ctx)
		if err != nil {
			return out, fmt.Errorf("reading the configuration of topic %s: %w", info.ID, err)
		}
		info.Labels = cfg.Labels
		info.KMSKeyName = cfg.KMSKeyName
		info.State = topicStateName(cfg.State)
		out = append(out, info)
	}
}

// listPubSubSubscriptions reads each subscription's configuration, on the same
// terms as the topics above.
func (c *clients) listPubSubSubscriptions(ctx context.Context, _ string) ([]collect.PubSubSubscriptionInfo, error) {
	it := c.pubsub.Subscriptions(ctx)
	var out []collect.PubSubSubscriptionInfo
	for {
		sub, err := it.Next()
		if err == iterator.Done {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		info := collect.PubSubSubscriptionInfo{ID: sub.String(), Name: sub.ID()}
		// Returned rather than logged, for the reason the topics above give: the
		// configuration carries this subscription's whole relationship set, and a
		// subscription emitted without one asserts that it subscribes to nothing.
		cfg, err := sub.Config(ctx)
		if err != nil {
			return out, fmt.Errorf("reading the configuration of subscription %s: %w", info.ID, err)
		}
		if cfg.Topic != nil {
			info.TopicID = cfg.Topic.String()
		}
		if cfg.DeadLetterPolicy != nil {
			info.DeadLetterTopicID = cfg.DeadLetterPolicy.DeadLetterTopic
		}
		info.PushEndpoint = cfg.PushConfig.Endpoint
		info.Filter = cfg.Filter
		info.Labels = cfg.Labels
		info.Detached = cfg.Detached
		out = append(out, info)
	}
}

// topicStateName renders a topic's state. The SDK models it as an untyped
// integer constant with no name of its own, so the mapping is written out here
// rather than stringified into a number a reader cannot interpret.
func topicStateName(state pubsub.TopicState) string {
	switch state {
	case pubsub.TopicStateActive:
		return "ACTIVE"
	case pubsub.TopicStateIngestionResourceError:
		return "INGESTION_RESOURCE_ERROR"
	default:
		return ""
	}
}
