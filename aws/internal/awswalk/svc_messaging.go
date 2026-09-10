// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// svc_messaging.go — SQS, SNS, EventBridge and Kinesis: how work moves between
// the account's components.

type sqsAPI interface {
	ListQueues(ctx context.Context, in *sqs.ListQueuesInput, optFns ...func(*sqs.Options)) (*sqs.ListQueuesOutput, error)
}

type snsAPI interface {
	ListTopics(ctx context.Context, in *sns.ListTopicsInput, optFns ...func(*sns.Options)) (*sns.ListTopicsOutput, error)
}

type eventBridgeAPI interface {
	ListRules(ctx context.Context, in *eventbridge.ListRulesInput, optFns ...func(*eventbridge.Options)) (*eventbridge.ListRulesOutput, error)
	ListTargetsByRule(ctx context.Context, in *eventbridge.ListTargetsByRuleInput, optFns ...func(*eventbridge.Options)) (*eventbridge.ListTargetsByRuleOutput, error)
}

type kinesisAPI interface {
	ListStreams(ctx context.Context, in *kinesis.ListStreamsInput, optFns ...func(*kinesis.Options)) (*kinesis.ListStreamsOutput, error)
}

// walkSQS emits one node per queue.
//
// THE LIST RETURNS URLS, NOT ARNS, and the ARN has to be composed from the URL's
// last two segments — the account and the queue name. A walk that used the URL as
// the node id would give every edge naming a queue by ARN, which is how every
// other service refers to one, nothing to land on.
func walkSQS(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*sqs.ListQueuesOutput, error) {
			return w.clients.SQS.ListQueues(ctx, &sqs.ListQueuesInput{NextToken: token})
		},
		func(p *sqs.ListQueuesOutput) *string { return p.NextToken },
		func(p *sqs.ListQueuesOutput) error {
			for _, url := range p.QueueUrls {
				arn, name := sqsARNFromURL(url, w.region)
				if arn == "" {
					continue
				}
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeSQSQueue,
					name:         name,
					summary:      fmt.Sprintf("SQS queue %s in %s", name, w.region),
					detail:       map[string]string{"queue_url": url},
					region:       w.region,
				}, w.account))
			}
			return nil
		})
}

// sqsARNFromURL maps a queue URL onto its ARN and its name.
//
// The URL is https://sqs.<region>.amazonaws.com/<account>/<name>; a URL with
// fewer than two trailing segments names no queue and yields nothing rather than
// a composed ARN with an empty field in it.
func sqsARNFromURL(url, region string) (arn, name string) {
	parts := strings.Split(strings.TrimSuffix(url, "/"), "/")
	if len(parts) < 2 {
		return "", ""
	}
	name = parts[len(parts)-1]
	account := parts[len(parts)-2]
	if name == "" || account == "" {
		return "", ""
	}
	return fmt.Sprintf("arn:aws:sqs:%s:%s:%s", region, account, name), name
}

func walkSNS(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*sns.ListTopicsOutput, error) {
			return w.clients.SNS.ListTopics(ctx, &sns.ListTopicsInput{NextToken: token})
		},
		func(p *sns.ListTopicsOutput) *string { return p.NextToken },
		func(p *sns.ListTopicsOutput) error {
			for _, t := range p.Topics {
				arn := deref(t.TopicArn)
				if arn == "" {
					continue
				}
				// THE NAME IS THE ARN'S LAST SEGMENT: ListTopics returns ARNs and
				// nothing else, so a topic's operator-facing name has to come from
				// there.
				name := arn
				if i := strings.LastIndex(arn, ":"); i >= 0 {
					name = arn[i+1:]
				}
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeSNSTopic,
					name:         name,
					summary:      fmt.Sprintf("SNS topic %s in %s", name, w.region),
					region:       w.region,
				}, w.account))
			}
			return nil
		})
}

// walkEventBridge emits each rule and TRIGGERS each of its targets.
//
// ONE TARGETS CALL PER RULE, and there is no batch form. It is paid because a
// rule without its targets is a schedule with no consequence: the whole reason an
// EventBridge rule is in a dependency graph is what it invokes.
func walkEventBridge(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*eventbridge.ListRulesOutput, error) {
			return w.clients.EventBridge.ListRules(ctx, &eventbridge.ListRulesInput{NextToken: token})
		},
		func(p *eventbridge.ListRulesOutput) *string { return p.NextToken },
		func(p *eventbridge.ListRulesOutput) error {
			for _, r := range p.Rules {
				arn := deref(r.Arn)
				name := deref(r.Name)
				if arn == "" || name == "" {
					continue
				}
				detail := map[string]string{}
				put(detail, "state", string(r.State))
				put(detail, "schedule_expression", deref(r.ScheduleExpression))
				put(detail, "event_bus_name", deref(r.EventBusName))
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeEventBridgeRule,
					name:         name,
					summary:      fmt.Sprintf("EventBridge rule %s in %s", name, w.region),
					detail:       detail,
					region:       w.region,
				}, w.account))
				w.addEventBridgeTargets(ctx, arn, name, deref(r.EventBusName))
			}
			return nil
		})
}

// addEventBridgeTargets emits TRIGGERS from a rule to each of its targets, and
// DEAD_LETTERS_TO where a target carries a dead-letter queue.
//
// IT PAGINATES AND IT RECORDS ITS FAILURES. ListTargetsByRuleOutput carries a
// NextToken, so a rule with many targets returns a truncated set; and a read that
// fails records into the account's failure set rather than returning silently,
// because unread targets are TRIGGERS and DEAD_LETTERS_TO edges that a COMPLETE
// assertion would invite the server to delete on the next collect.
func (w *walkContext) addEventBridgeTargets(ctx context.Context, ruleARN, ruleName, busName string) {
	err := paginate(ctx,
		func(ctx context.Context, token *string) (*eventbridge.ListTargetsByRuleOutput, error) {
			in := &eventbridge.ListTargetsByRuleInput{Rule: &ruleName, NextToken: token}
			if busName != "" {
				in.EventBusName = &busName
			}
			return w.clients.EventBridge.ListTargetsByRule(ctx, in)
		},
		func(p *eventbridge.ListTargetsByRuleOutput) *string { return p.NextToken },
		func(p *eventbridge.ListTargetsByRuleOutput) error {
			for _, t := range p.Targets {
				target := deref(t.Arn)
				if target == "" {
					continue
				}
				// THE TARGET'S ARN IS WHATEVER IT IS — a Lambda function, an SQS
				// queue, a Step Functions state machine — and it is used
				// unmodified, so the edge lands on that service's own node
				// without this walk having to classify it.
				w.sink.addEdge(ruleARN, target, EdgeTriggers, map[string]string{"target_id": deref(t.Id)})
				if t.DeadLetterConfig != nil {
					if dlq := deref(t.DeadLetterConfig.Arn); dlq != "" {
						w.sink.addEdge(ruleARN, dlq, EdgeDeadLettersTo,
							map[string]string{"target_id": deref(t.Id)})
					}
				}
			}
			return nil
		})
	w.recordSubreadFailure("eventbridge.ListTargetsByRule", ruleARN, err)
}

func walkKinesis(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*kinesis.ListStreamsOutput, error) {
			return w.clients.Kinesis.ListStreams(ctx, &kinesis.ListStreamsInput{NextToken: token})
		},
		func(p *kinesis.ListStreamsOutput) *string { return p.NextToken },
		func(p *kinesis.ListStreamsOutput) error {
			// THE SUMMARY LIST CARRIES THE ARN and the bare name list does not, so
			// the summaries are preferred and the names are the fallback for an
			// older API shape.
			for _, s := range p.StreamSummaries {
				arn := deref(s.StreamARN)
				if arn == "" {
					continue
				}
				detail := map[string]string{}
				put(detail, "status", string(s.StreamStatus))
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeKinesisStream,
					name:         deref(s.StreamName),
					summary:      fmt.Sprintf("Kinesis stream %s in %s", deref(s.StreamName), w.region),
					detail:       detail,
					region:       w.region,
				}, w.account))
			}
			for _, name := range p.StreamNames {
				arn := fmt.Sprintf("arn:aws:kinesis:%s:%s:stream/%s", w.region, w.account, name)
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeKinesisStream,
					name:         name,
					summary:      fmt.Sprintf("Kinesis stream %s in %s", name, w.region),
					region:       w.region,
				}, w.account))
			}
			return nil
		})
}
