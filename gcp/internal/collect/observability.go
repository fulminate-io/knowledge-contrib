// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"fmt"
	"strconv"

	eventarcpb "cloud.google.com/go/eventarc/apiv1/eventarcpb"
	loggingpb "cloud.google.com/go/logging/apiv2/loggingpb"
	monitoringpb "cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"
	workflowspb "cloud.google.com/go/workflows/apiv1/workflowspb"
	dataflow "google.golang.org/api/dataflow/v1b3"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpcontent"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
)

// observability.go — what watches the project and what reacts to it: log sinks,
// alert policies and their notification channels, event triggers, workflows and
// data pipelines.
//
// A NOTIFICATION CHANNEL IS ENUMERATED IN ITS OWN RIGHT, and an alert policy
// also REFERENCES one. Both emit a node for it and the id is the same resource
// name, so the walk's own deduplication collapses them: a channel a policy names
// but the channel enumeration could not read is still a node, and a channel both
// see is one node with the enumeration's own detail.

// LogSinks enumerates the project's log routing sinks.
func LogSinks(list Lister[*loggingpb.LogSink]) Subcollector {
	return New("gcp-logging-sinks", list, convertLogSink)
}

func convertLogSink(projectID string, sink *loggingpb.LogSink) (gcpgraph.Result, error) {
	if sink == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil log sink")
	}
	if sink.GetName() == "" {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a log sink with no name")
	}
	id := "projects/" + projectID + "/sinks/" + sink.GetName()
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: sink.GetName(), SelfLink: id, Description: sink.GetDescription(),
		Fields: nonEmptyFields(map[string]string{
			"destination":    sink.GetDestination(),
			"filter":         sink.GetFilter(),
			"writerIdentity": sink.GetWriterIdentity(),
		}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	metadata := map[string]string{
		"disabled":         strconv.FormatBool(sink.GetDisabled()),
		"include_children": strconv.FormatBool(sink.GetIncludeChildren()),
	}
	setIfNotEmpty(metadata, "destination", sink.GetDestination())

	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: id, Name: sink.GetName(), ResourceType: gcpgraph.ResourceTypeLoggingSink,
		Content: raw, Metadata: metadata,
	}}}
	// The destination is a full resource URI of whatever service receives the
	// logs — a bucket, a dataset, a topic. It is emitted verbatim: normalizing
	// it per service would be three guesses where the API gave one answer.
	if dest := sink.GetDestination(); dest != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: dest, Type: gcpgraph.EdgeSinksTo,
		})
	}
	return out, nil
}

// AlertPolicies enumerates the project's alerting policies.
func AlertPolicies(list Lister[*monitoringpb.AlertPolicy]) Subcollector {
	return New("gcp-alert-policies", list, convertAlertPolicy)
}

func convertAlertPolicy(projectID string, policy *monitoringpb.AlertPolicy) (gcpgraph.Result, error) {
	if policy == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil alert policy")
	}
	id := policy.GetName()
	if id == "" {
		return gcpgraph.Result{}, fmt.Errorf("the API returned an alert policy with no resource name")
	}
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: policy.GetDisplayName(), SelfLink: id,
		Description: policy.GetDocumentation().GetContent(),
		Fields:      nonEmptyFields(map[string]string{"combiner": policy.GetCombiner().String()}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	metadata := map[string]string{
		"condition_count":    strconv.Itoa(len(policy.GetConditions())),
		"notification_count": strconv.Itoa(len(policy.GetNotificationChannels())),
		"enabled":            strconv.FormatBool(policy.GetEnabled().GetValue()),
	}
	setIfNotEmpty(metadata, "severity", policy.GetSeverity().String())
	for k, v := range policy.GetUserLabels() {
		metadata["label/"+k] = v
	}

	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: id, Name: policy.GetDisplayName(), ResourceType: gcpgraph.ResourceTypeAlertPolicy,
		Content: raw, Metadata: metadata,
	}}}
	// What the policy watches is the project: the conditions are filter
	// expressions over its metrics rather than references to a resource this
	// walk can name, so the honest edge is the one that is true.
	out.Relations = append(out.Relations, gcpgraph.Relation{
		From: id, To: gcpgraph.ProjectResourceName(projectID), Type: gcpgraph.EdgeMonitors,
	})
	for _, channel := range policy.GetNotificationChannels() {
		if channel == "" {
			continue
		}
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: channel, Type: gcpgraph.EdgeNotifiesVia,
		})
		// A referenced channel gets a node here too. The channel enumeration
		// emits the same id with more detail, and the walk keeps the first —
		// which is why this one carries the flag saying it was referenced rather
		// than read, and the enumerated one does not.
		out.Resources = append(out.Resources, gcpgraph.Resource{
			ID: channel, Name: gcpgraph.LastSegment(channel),
			ResourceType: gcpgraph.ResourceTypeNotificationChannel,
			Summary:      gcpgraph.ResourceTypeNotificationChannel + " " + gcpgraph.LastSegment(channel),
			Metadata: map[string]string{
				"collected":        "false",
				"collected_reason": "referenced by an alert policy and not read directly",
			},
		})
	}
	return out, nil
}

// NotificationChannels enumerates the project's alert delivery channels.
func NotificationChannels(list Lister[*monitoringpb.NotificationChannel]) Subcollector {
	return New("gcp-notification-channels", list, convertNotificationChannel)
}

func convertNotificationChannel(_ string, channel *monitoringpb.NotificationChannel) (gcpgraph.Result, error) {
	if channel == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil notification channel")
	}
	id := channel.GetName()
	if id == "" {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a notification channel with no resource name")
	}
	// The channel's own Labels hold the DESTINATION — an address, a number, a
	// webhook URL — which is operator contact detail rather than infrastructure.
	// It is deliberately not copied into the graph; the channel's type and
	// display name are what a reader needs to identify it.
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: channel.GetDisplayName(), SelfLink: id, Description: channel.GetDescription(),
		Fields: nonEmptyFields(map[string]string{"type": channel.GetType()}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	metadata := map[string]string{"enabled": strconv.FormatBool(channel.GetEnabled().GetValue())}
	setIfNotEmpty(metadata, "channel_type", channel.GetType())
	setIfNotEmpty(metadata, "verification_status", channel.GetVerificationStatus().String())

	return gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: id, Name: channel.GetDisplayName(),
		ResourceType: gcpgraph.ResourceTypeNotificationChannel,
		Content:      raw, Metadata: metadata,
	}}}, nil
}

// EventarcTriggers enumerates the project's event triggers.
func EventarcTriggers(list Lister[*eventarcpb.Trigger]) Subcollector {
	return New("gcp-eventarc-triggers", list, convertEventarcTrigger)
}

func convertEventarcTrigger(projectID string, trigger *eventarcpb.Trigger) (gcpgraph.Result, error) {
	if trigger == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil trigger")
	}
	id := trigger.GetName()
	if id == "" {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a trigger with no resource name")
	}
	eventType := ""
	for _, filter := range trigger.GetEventFilters() {
		if filter.GetAttribute() == "type" {
			eventType = filter.GetValue()
		}
	}
	destination := trigger.GetDestination().GetCloudRun().GetService()
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: gcpgraph.LastSegment(id), SelfLink: id, Location: locationOfResourceName(id),
		Labels: trigger.GetLabels(),
		Fields: nonEmptyFields(map[string]string{
			"eventType":      eventType,
			"serviceAccount": trigger.GetServiceAccount(),
			"destination":    destination,
			"transportTopic": trigger.GetTransport().GetPubsub().GetTopic(),
		}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	metadata := map[string]string{"filter_count": strconv.Itoa(len(trigger.GetEventFilters()))}
	setIfNotEmpty(metadata, "event_type", eventType)
	setIfNotEmpty(metadata, "location", locationOfResourceName(id))
	for k, v := range trigger.GetLabels() {
		metadata["label/"+k] = v
	}

	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: id, Name: gcpgraph.LastSegment(id), ResourceType: gcpgraph.ResourceTypeEventarcTrigger,
		Region: locationOfResourceName(id), Content: raw, Metadata: metadata,
	}}}
	if email := trigger.GetServiceAccount(); email != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: gcpgraph.ServiceAccountResourceName(projectID, email),
			Type: gcpgraph.EdgeUsesSA,
		})
	}
	if destination != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: destination, Type: gcpgraph.EdgeTriggers,
			Metadata: map[string]string{"event_type": eventType},
		})
	}
	if topic := trigger.GetTransport().GetPubsub().GetTopic(); topic != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: topic, Type: gcpgraph.EdgeSubscribesTo,
		})
	}
	return out, nil
}

// Workflows enumerates the project's orchestration workflows.
func Workflows(list Lister[*workflowspb.Workflow]) Subcollector {
	return New("gcp-workflows", list, convertWorkflow)
}

func convertWorkflow(projectID string, workflow *workflowspb.Workflow) (gcpgraph.Result, error) {
	if workflow == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil workflow")
	}
	id := workflow.GetName()
	if id == "" {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a workflow with no resource name")
	}
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: gcpgraph.LastSegment(id), SelfLink: id, Description: workflow.GetDescription(),
		State: workflow.GetState().String(), Location: locationOfResourceName(id),
		Labels: workflow.GetLabels(),
		Fields: nonEmptyFields(map[string]string{
			"serviceAccount": workflow.GetServiceAccount(),
			"revisionId":     workflow.GetRevisionId(),
			"cryptoKeyName":  workflow.GetCryptoKeyName(),
		}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	metadata := map[string]string{}
	setIfNotEmpty(metadata, "state", workflow.GetState().String())
	setIfNotEmpty(metadata, "revision_id", workflow.GetRevisionId())
	setIfNotEmpty(metadata, "location", locationOfResourceName(id))
	for k, v := range workflow.GetLabels() {
		metadata["label/"+k] = v
	}

	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: id, Name: gcpgraph.LastSegment(id), ResourceType: gcpgraph.ResourceTypeWorkflow,
		Region: locationOfResourceName(id), Content: raw, Metadata: metadata,
	}}}
	// A workflow's service account is given as a full resource name already.
	if account := workflow.GetServiceAccount(); account != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: normalizeServiceAccountRef(projectID, account), Type: gcpgraph.EdgeUsesSA,
		})
	}
	if key := workflow.GetCryptoKeyName(); key != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: key, Type: gcpgraph.EdgeEncryptsWith,
		})
	}
	return out, nil
}

// DataflowJobs enumerates the project's data pipeline jobs.
func DataflowJobs(list Lister[*dataflow.Job]) Subcollector {
	return New("gcp-dataflow-jobs", list, convertDataflowJob)
}

func convertDataflowJob(projectID string, job *dataflow.Job) (gcpgraph.Result, error) {
	if job == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil dataflow job")
	}
	if job.Id == "" {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a dataflow job with no id")
	}
	// A job's id is unique per project but carries no project itself, so the id
	// is qualified here rather than left to collide across a multi-project read.
	id := fmt.Sprintf("projects/%s/locations/%s/jobs/%s", projectID, job.Location, job.Id)

	var serviceAccount, network, subnetwork string
	if job.Environment != nil {
		serviceAccount = job.Environment.ServiceAccountEmail
	}
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: job.Name, SelfLink: id, State: job.CurrentState, Location: job.Location,
		CreateTime: job.CreateTime, Labels: job.Labels,
		Fields: nonEmptyFields(map[string]string{
			"type":           job.Type,
			"serviceAccount": serviceAccount,
			"network":        network,
			"subnetwork":     subnetwork,
		}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	metadata := map[string]string{}
	setIfNotEmpty(metadata, "state", job.CurrentState)
	setIfNotEmpty(metadata, "job_type", job.Type)
	setIfNotEmpty(metadata, "location", job.Location)
	for k, v := range job.Labels {
		metadata["label/"+k] = v
	}

	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: id, Name: job.Name, ResourceType: gcpgraph.ResourceTypeDataflowJob,
		Region: job.Location, Content: raw, Metadata: metadata,
	}}}
	if serviceAccount != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: gcpgraph.ServiceAccountResourceName(projectID, serviceAccount),
			Type: gcpgraph.EdgeUsesSA,
		})
	}
	return out, nil
}

// normalizeServiceAccountRef accepts either a bare email or a full resource name
// and returns the resource name the account enumeration emits as its node id.
func normalizeServiceAccountRef(projectID, ref string) string {
	if len(ref) >= len("projects/") && ref[:len("projects/")] == "projects/" {
		return ref
	}
	return gcpgraph.ServiceAccountResourceName(projectID, ref)
}
