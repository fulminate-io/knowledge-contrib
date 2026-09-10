// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/aws/aws-sdk-go-v2/service/sfn"
)

// svc_serverless.go — Lambda functions and Step Functions state machines.

type lambdaAPI interface {
	ListFunctions(ctx context.Context, in *lambda.ListFunctionsInput, optFns ...func(*lambda.Options)) (*lambda.ListFunctionsOutput, error)
}

type sfnAPI interface {
	ListStateMachines(ctx context.Context, in *sfn.ListStateMachinesInput, optFns ...func(*sfn.Options)) (*sfn.ListStateMachinesOutput, error)
}

// walkLambda emits every function and its four relationships.
//
// LAMBDA'S PAGING IS Marker/NextMarker rather than NextToken, which is why the
// adapter below exists rather than the field being read directly.
func walkLambda(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*lambda.ListFunctionsOutput, error) {
			return w.clients.Lambda.ListFunctions(ctx, &lambda.ListFunctionsInput{Marker: token})
		},
		func(p *lambda.ListFunctionsOutput) *string { return p.NextMarker },
		func(p *lambda.ListFunctionsOutput) error {
			for _, f := range p.Functions {
				w.addLambdaFunction(f)
			}
			return nil
		})
}

// addLambdaFunction emits one function and its six relationships.
//
// IT IS SPLIT FROM THE PAGE LOOP for the same reason the RDS instance body is: a
// function carries a VPC configuration block holding two id lists, a dead-letter
// block, a role and an encryption key, and every one is a nil check. Folded into
// the page visitor they put the walk over the complexity cap.
func (w *walkContext) addLambdaFunction(f lambdatypes.FunctionConfiguration) {
	arn := deref(f.FunctionArn)
	if arn == "" {
		return
	}
	detail := map[string]string{}
	put(detail, "runtime", string(f.Runtime))
	put(detail, "handler", deref(f.Handler))
	put(detail, "package_type", string(f.PackageType))
	if f.MemorySize != nil {
		detail["memory_mb"] = fmt.Sprintf("%d", *f.MemorySize)
	}
	w.sink.addNode(newNode(resource{
		id:           arn,
		resourceType: ResourceTypeLambda,
		name:         deref(f.FunctionName),
		summary:      fmt.Sprintf("Lambda function %s (%s) in %s", deref(f.FunctionName), string(f.Runtime), w.region),
		detail:       detail,
		region:       w.region,
	}, w.account))

	if role := deref(f.Role); role != "" {
		w.sink.addEdge(arn, role, EdgeAssumesRole, map[string]string{"via": "function.role"})
	}
	if f.VpcConfig != nil {
		if vpc := deref(f.VpcConfig.VpcId); vpc != "" {
			w.sink.addEdge(arn, w.ec2ResourceARN("vpc", vpc), EdgeUsesNetwork, nil)
		}
		for _, sub := range f.VpcConfig.SubnetIds {
			w.sink.addEdge(arn, w.ec2ResourceARN("subnet", sub), EdgeUsesSubnet, nil)
		}
		for _, sg := range f.VpcConfig.SecurityGroupIds {
			w.sink.addEdge(arn, w.ec2ResourceARN("security-group", sg), EdgeUsesSecurityGroup, nil)
		}
	}
	// THE DEAD-LETTER TARGET IS AN SQS QUEUE OR AN SNS TOPIC, and the API returns
	// its ARN, so the edge lands on whichever the account has without this walk
	// having to tell them apart.
	if f.DeadLetterConfig != nil {
		if target := deref(f.DeadLetterConfig.TargetArn); target != "" {
			w.sink.addEdge(arn, target, EdgeDeadLettersTo,
				map[string]string{"via": "function.dead_letter_config"})
		}
	}
	if key := deref(f.KMSKeyArn); key != "" {
		w.sink.addEdge(arn, key, EdgeEncryptsWith, map[string]string{"via": "function.kms_key_arn"})
	}
}

func walkStepFunctions(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*sfn.ListStateMachinesOutput, error) {
			return w.clients.SFN.ListStateMachines(ctx, &sfn.ListStateMachinesInput{NextToken: token})
		},
		func(p *sfn.ListStateMachinesOutput) *string { return p.NextToken },
		func(p *sfn.ListStateMachinesOutput) error {
			for _, m := range p.StateMachines {
				arn := deref(m.StateMachineArn)
				if arn == "" {
					continue
				}
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeStepFunctionsStateMachine,
					name:         deref(m.Name),
					summary:      fmt.Sprintf("Step Functions state machine %s in %s", deref(m.Name), w.region),
					detail:       map[string]string{"type": string(m.Type)},
					region:       w.region,
				}, w.account))
			}
			return nil
		})
}
