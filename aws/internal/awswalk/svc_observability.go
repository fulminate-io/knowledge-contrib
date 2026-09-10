// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
)

// svc_observability.go — CloudWatch alarms and log groups, CloudTrail trails, and
// SES identities and receipt rules.
//
// THE LOG GROUPS HERE ARE CLOUD RESOURCES, NOT LOGS. This collector emits the log
// group as a resource an alarm or a flow log can point at; the log ENTRIES inside
// it are a different graph produced by a different collector, and nothing here
// reads one.

type cloudWatchAPI interface {
	DescribeAlarms(ctx context.Context, in *cloudwatch.DescribeAlarmsInput, optFns ...func(*cloudwatch.Options)) (*cloudwatch.DescribeAlarmsOutput, error)
}

type cloudWatchLogsAPI interface {
	DescribeLogGroups(ctx context.Context, in *cloudwatchlogs.DescribeLogGroupsInput, optFns ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.DescribeLogGroupsOutput, error)
}

type cloudTrailAPI interface {
	DescribeTrails(ctx context.Context, in *cloudtrail.DescribeTrailsInput, optFns ...func(*cloudtrail.Options)) (*cloudtrail.DescribeTrailsOutput, error)
}

// sesAPI is IDENTITIES ONLY. It carried ListEmailTemplates while a walk was
// emitting templates as receipt rules; with that walk gone the operation is not
// called, and a narrow interface that declares an operation nothing calls is an
// invitation to call it.
type sesAPI interface {
	ListEmailIdentities(ctx context.Context, in *sesv2.ListEmailIdentitiesInput, optFns ...func(*sesv2.Options)) (*sesv2.ListEmailIdentitiesOutput, error)
}

// logGroupARN composes a CloudWatch log group's ARN from its name.
func logGroupARN(region, account, name string) string {
	return fmt.Sprintf("arn:aws:logs:%s:%s:log-group:%s", region, account, name)
}

func walkCloudWatch(ctx context.Context, w *walkContext) error {
	if err := w.walkAlarms(ctx); err != nil {
		return err
	}
	return w.walkLogGroups(ctx)
}

func (w *walkContext) walkAlarms(ctx context.Context) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*cloudwatch.DescribeAlarmsOutput, error) {
			return w.clients.CloudWatch.DescribeAlarms(ctx, &cloudwatch.DescribeAlarmsInput{NextToken: token})
		},
		func(p *cloudwatch.DescribeAlarmsOutput) *string { return p.NextToken },
		func(p *cloudwatch.DescribeAlarmsOutput) error {
			for _, a := range p.MetricAlarms {
				w.addAlarm(a)
			}
			return nil
		})
}

func (w *walkContext) addAlarm(a cwtypes.MetricAlarm) {
	arn := deref(a.AlarmArn)
	if arn == "" {
		return
	}
	detail := map[string]string{}
	put(detail, "metric_name", deref(a.MetricName))
	put(detail, "namespace", deref(a.Namespace))
	put(detail, "comparison_operator", string(a.ComparisonOperator))
	// THE ALARM'S CURRENT STATE IS DELIBERATELY NOT CARRIED. It flips with the
	// metric it watches, so a node holding it would differ between two collects
	// of an unchanged account and the carry-forward diff would write a row for
	// every alarm on every collect.
	w.sink.addNode(newNode(resource{
		id:           arn,
		resourceType: ResourceTypeCloudWatchAlarm,
		name:         deref(a.AlarmName),
		summary:      fmt.Sprintf("CloudWatch alarm %s in %s", deref(a.AlarmName), w.region),
		detail:       detail,
		region:       w.region,
	}, w.account))

	// AN ALARM ACTION IS AN ARN — usually an SNS topic — so NOTIFIES_VIA lands on
	// that topic's node without this walk classifying the target.
	for _, action := range a.AlarmActions {
		w.sink.addEdge(arn, action, EdgeNotifiesVia, map[string]string{"kind": "alarm"})
	}
	// WHAT THE ALARM WATCHES comes from its dimensions, and only a dimension this
	// collector can map onto a resource id yields an edge: an alarm on a custom
	// metric names no AWS resource, and an edge composed from a dimension value
	// of unknown kind would point at an id that does not exist.
	for _, d := range a.Dimensions {
		if target := w.resourceForDimension(deref(d.Name), deref(d.Value)); target != "" {
			w.sink.addEdge(arn, target, EdgeMonitors,
				map[string]string{"dimension": deref(d.Name), "value": deref(d.Value)})
		}
	}
}

// resourceForDimension maps one CloudWatch dimension onto the resource id it
// names, for the dimensions whose meaning is unambiguous.
//
// THE LIST IS SHORT ON PURPOSE. A dimension name is only a convention, and the
// four below are the ones whose value is documented to be a resource identifier
// this collector also emits. Adding a guess for a fifth would emit edges to
// composed ids that resolve against nothing.
func (w *walkContext) resourceForDimension(name, value string) string {
	if value == "" {
		return ""
	}
	switch name {
	case "InstanceId":
		return w.ec2ResourceARN("instance", value)
	case "VolumeId":
		return w.ec2ResourceARN("volume", value)
	case "DBInstanceIdentifier":
		return rdsInstanceARN(w.region, w.account, value)
	case "FunctionName":
		return fmt.Sprintf("arn:aws:lambda:%s:%s:function:%s", w.region, w.account, value)
	default:
		return ""
	}
}

func (w *walkContext) walkLogGroups(ctx context.Context) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*cloudwatchlogs.DescribeLogGroupsOutput, error) {
			return w.clients.CloudWatchLogs.DescribeLogGroups(ctx,
				&cloudwatchlogs.DescribeLogGroupsInput{NextToken: token})
		},
		func(p *cloudwatchlogs.DescribeLogGroupsOutput) *string { return p.NextToken },
		func(p *cloudwatchlogs.DescribeLogGroupsOutput) error {
			for _, g := range p.LogGroups {
				name := deref(g.LogGroupName)
				if name == "" {
					continue
				}
				// THE API'S OWN ARN ENDS IN :* and every other service names a log
				// group without it, so the composed form is used as the node id and
				// the API's is kept in the detail. Using the :* form would leave
				// every flow log's SINKS_TO edge pointing at a node that is not
				// there.
				arn := logGroupARN(w.region, w.account, name)
				detail := map[string]string{}
				put(detail, "api_arn", deref(g.Arn))
				if g.RetentionInDays != nil {
					detail["retention_days"] = fmt.Sprintf("%d", *g.RetentionInDays)
				}
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeCloudWatchLogGroup,
					name:         name,
					summary:      fmt.Sprintf("CloudWatch log group %s in %s", name, w.region),
					detail:       detail,
					region:       w.region,
				}, w.account))
				if key := deref(g.KmsKeyId); key != "" {
					w.sink.addEdge(arn, key, EdgeEncryptsWith, map[string]string{"via": "log_group.kms_key_id"})
				}
			}
			return nil
		})
}

// walkCloudTrail emits each trail and where it delivers.
//
// DescribeTrails IS NOT PAGINATED — it returns every trail in one response, which
// is why this walk does not go through paginate and does not pretend to.
func walkCloudTrail(ctx context.Context, w *walkContext) error {
	out, err := w.clients.CloudTrail.DescribeTrails(ctx, &cloudtrail.DescribeTrailsInput{})
	if err != nil {
		return fmt.Errorf("describe trails: %w", err)
	}
	for _, t := range out.TrailList {
		arn := deref(t.TrailARN)
		if arn == "" {
			continue
		}
		detail := map[string]string{}
		put(detail, "home_region", deref(t.HomeRegion))
		put(detail, "is_multi_region", fmt.Sprintf("%t", deref(t.IsMultiRegionTrail)))
		w.sink.addNode(newNode(resource{
			id:           arn,
			resourceType: ResourceTypeCloudTrailTrail,
			name:         deref(t.Name),
			summary:      fmt.Sprintf("CloudTrail trail %s in %s", deref(t.Name), w.region),
			detail:       detail,
			region:       w.region,
		}, w.account))

		if bucket := deref(t.S3BucketName); bucket != "" {
			w.sink.addEdge(arn, "arn:aws:s3:::"+bucket, EdgeSinksTo, map[string]string{"kind": "s3"})
		}
		if group := deref(t.CloudWatchLogsLogGroupArn); group != "" {
			w.sink.addEdge(arn, group, EdgeSinksTo, map[string]string{"kind": "cloudwatch-logs"})
		}
		if key := deref(t.KmsKeyId); key != "" {
			w.sink.addEdge(arn, key, EdgeEncryptsWith, map[string]string{"via": "trail.kms_key_id"})
		}
	}
	return nil
}
