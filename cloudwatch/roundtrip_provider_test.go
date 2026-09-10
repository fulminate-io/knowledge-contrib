// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
)

// roundtrip_provider_test.go — the child process the round-trip test drives: the
// SHIPPED collector with its CloudWatch client substituted for recorded pages,
// and nothing else changed.

// roundTripEvents are the recorded pages the round-trip child serves: one event
// in a log group whose last path segment is the service label the declared
// block below resolves.
func roundTripEvents() []recordedGroup {
	ts := int64(1772366774000)
	return []recordedGroup{{
		LogGroup: "/ecs/prod/api-server",
		Pages: []recordedPage{{Events: []recordedEvent{{
			EventID: "e-1", Timestamp: &ts, LogStreamName: "ecs-task-1", Message: "upstream refused",
		}}}},
	}}
}

// roundTripCollector is the SHIPPED collector with its client substituted for
// recorded pages.
//
// NOTHING ELSE IS SUBSTITUTED. The walk, the resolution and the emission are the
// production ones, and the foreign context reaches them the way it reaches them
// in production — decoded from the call's envelope by the framework and handed
// over as an argument. That is what makes this a round trip rather than a unit
// test with extra steps.
func roundTripCollector() *Collector { return recordedCollector(roundTripEvents()) }

// recordedCollector is the SHIPPED collector with its client substituted for the
// given recorded pages.
func recordedCollector(groups []recordedGroup) *Collector {
	pages := make(map[string][]*cloudwatchlogs.FilterLogEventsOutput)
	for _, g := range groups {
		for _, p := range g.Pages {
			pages[g.LogGroup] = append(pages[g.LogGroup], &cloudwatchlogs.FilterLogEventsOutput{
				Events: buildEvents(p.Events),
			})
		}
	}
	served := make(map[string]int)
	c := New()
	c.newClient = func(context.Context, string) (filterLogEventsClient, error) {
		return filterLogEventsFunc(func(
			_ context.Context, in *cloudwatchlogs.FilterLogEventsInput, _ ...func(*cloudwatchlogs.Options),
		) (*cloudwatchlogs.FilterLogEventsOutput, error) {
			name := aws.ToString(in.LogGroupName)
			i := served[name]
			if i >= len(pages[name]) {
				return &cloudwatchlogs.FilterLogEventsOutput{}, nil
			}
			served[name]++
			return pages[name][i], nil
		}), nil
	}
	return c
}

// filterLogEventsFunc adapts a function to the one-method client interface.
type filterLogEventsFunc func(
	context.Context, *cloudwatchlogs.FilterLogEventsInput, ...func(*cloudwatchlogs.Options),
) (*cloudwatchlogs.FilterLogEventsOutput, error)

func (f filterLogEventsFunc) FilterLogEvents(
	ctx context.Context, in *cloudwatchlogs.FilterLogEventsInput, opts ...func(*cloudwatchlogs.Options),
) (*cloudwatchlogs.FilterLogEventsOutput, error) {
	return f(ctx, in, opts...)
}

// The two log groups the correlation child serves, and the services their names
// derive. Two DIFFERENT services are what a correlation needs: the detector pairs
// error templates across services and never within one.
const (
	correlationGroupA   = "/aws/lambda/checkout"
	correlationGroupB   = "/ecs/prod/payments"
	correlationServiceA = "checkout"
	correlationServiceB = "payments"

	correlationResourceA = "arn:aws:lambda:us-east-1:111122223333:function:checkout"
	correlationResourceB = "arn:aws:ecs:us-east-1:111122223333:service/prod/payments"
	correlationAccount   = "acct-1"
)

// correlationEvents are error bursts in two services, close enough in time to
// overlap. Each group carries two entries of one shape so its template has a
// real range rather than a single instant.
func correlationEvents() []recordedGroup {
	const base = int64(1772366774000)
	at := func(offsetMillis int64) *int64 { ts := base + offsetMillis; return &ts }
	return []recordedGroup{
		{LogGroup: correlationGroupA, Pages: []recordedPage{{Events: []recordedEvent{
			{EventID: "a1", Timestamp: at(0), LogStreamName: "s-a", Message: "ERROR checkout upstream refused"},
			{EventID: "a2", Timestamp: at(4000), LogStreamName: "s-a", Message: "ERROR checkout upstream refused"},
		}}}},
		{LogGroup: correlationGroupB, Pages: []recordedPage{{Events: []recordedEvent{
			{EventID: "b1", Timestamp: at(1000), LogStreamName: "s-b", Message: "ERROR payments ledger write failed"},
			{EventID: "b2", Timestamp: at(5000), LogStreamName: "s-b", Message: "ERROR payments ledger write failed"},
		}}}},
	}
}

// correlationBlock is the declared cloud context those two services resolve
// against. withEdge decides whether the dependency between the two resources is
// declared, which is the only difference between the confirmed arm and its
// control.
func correlationBlock(withEdge bool) map[string]any {
	return correlationBlockDirected(withEdge, false)
}

// correlationBlockDirected is the same block with control over which WAY the
// declared edge runs.
//
// THE DIRECTION IS A REAL AXIS. A declared edge is directed, and the detector
// asks its dependency question in whichever order it paired the two templates,
// which is not the operator's to predict. The context records each declared edge
// in both directions for that reason, and only a fixture that declares it the
// OTHER way round can observe it.
func correlationBlockDirected(withEdge, reversed bool) map[string]any {
	edges := []any{}
	if withEdge {
		from, to := correlationResourceA, correlationResourceB
		if reversed {
			from, to = to, from
		}
		edges = append(edges, map[string]any{"from_id": from, "to_id": to})
	}
	return map[string]any{"cloud": []any{map[string]any{
		"graph_name": correlationAccount,
		"nodes": []any{
			map[string]any{
				"id": correlationResourceA, "symbol_name": correlationServiceA,
				"metadata": map[string]any{"resource_type": "lambda:function"},
			},
			map[string]any{
				"id": correlationResourceB, "symbol_name": correlationServiceB,
				"metadata": map[string]any{"resource_type": "ecs:service"},
			},
		},
		"edges": edges,
	}}}
}
