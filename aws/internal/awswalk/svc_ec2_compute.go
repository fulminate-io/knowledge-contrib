// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// svc_ec2_compute.go — instances and volumes: the EC2 resources that RUN
// something rather than describe the network around it.

func walkEC2Instances(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*ec2.DescribeInstancesOutput, error) {
			return w.clients.EC2.DescribeInstances(ctx, &ec2.DescribeInstancesInput{NextToken: token})
		},
		func(p *ec2.DescribeInstancesOutput) *string { return p.NextToken },
		func(p *ec2.DescribeInstancesOutput) error {
			// THE RESPONSE IS RESERVATIONS OF INSTANCES, not instances: a walk
			// that read p.Instances would not compile, and one that read only the
			// first reservation would silently report a fraction of the account.
			for _, r := range p.Reservations {
				for _, inst := range r.Instances {
					w.addInstance(inst)
				}
			}
			return nil
		})
}

// addInstance is split out because the nesting above already costs two loops and
// the per-instance work is where every edge is.
func (w *walkContext) addInstance(inst ec2types.Instance) {
	id := deref(inst.InstanceId)
	if id == "" {
		return
	}
	arn := w.ec2ResourceARN("instance", id)
	detail := map[string]string{}
	put(detail, "instance_type", string(inst.InstanceType))
	put(detail, "private_ip", deref(inst.PrivateIpAddress))
	put(detail, "public_ip", deref(inst.PublicIpAddress))
	put(detail, "image_id", deref(inst.ImageId))
	put(detail, "vpc_id", deref(inst.VpcId))
	put(detail, "subnet_id", deref(inst.SubnetId))
	if inst.State != nil {
		put(detail, "state", string(inst.State.Name))
	}
	w.sink.addNode(newNode(resource{
		id:           arn,
		resourceType: ResourceTypeEC2Instance,
		name:         firstNonEmpty(nameTag(inst.Tags), id),
		summary:      fmt.Sprintf("EC2 instance %s (%s) in %s", id, string(inst.InstanceType), w.region),
		detail:       detail,
		region:       w.region,
	}, w.account))

	if vpc := deref(inst.VpcId); vpc != "" {
		w.sink.addEdge(arn, w.ec2ResourceARN("vpc", vpc), EdgeUsesNetwork, nil)
	}
	if sub := deref(inst.SubnetId); sub != "" {
		w.sink.addEdge(arn, w.ec2ResourceARN("subnet", sub), EdgeUsesSubnet, nil)
	}
	for _, g := range inst.SecurityGroups {
		if gid := deref(g.GroupId); gid != "" {
			w.sink.addEdge(arn, w.ec2ResourceARN("security-group", gid), EdgeUsesSecurityGroup, nil)
		}
	}
	// AN INSTANCE PROFILE IS NOT A ROLE, and the difference matters to whoever
	// asks "what can this instance do": the profile's ARN names the profile, and
	// the role it carries has its own ARN under iam. The edge names the PROFILE
	// arn as the API returned it, which resolves against the IAM walk's roles
	// when the profile and role share a name and dangles otherwise — the honest
	// answer, since resolving it would take a call this walk does not make.
	if inst.IamInstanceProfile != nil {
		if p := deref(inst.IamInstanceProfile.Arn); p != "" {
			w.sink.addEdge(arn, p, EdgeAssumesRole,
				map[string]string{"via": "instance.iam_instance_profile"})
		}
	}
}

func walkEBSVolumes(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*ec2.DescribeVolumesOutput, error) {
			return w.clients.EC2.DescribeVolumes(ctx, &ec2.DescribeVolumesInput{NextToken: token})
		},
		func(p *ec2.DescribeVolumesOutput) *string { return p.NextToken },
		func(p *ec2.DescribeVolumesOutput) error {
			for _, v := range p.Volumes {
				id := deref(v.VolumeId)
				if id == "" {
					continue
				}
				arn := w.ec2ResourceARN("volume", id)
				detail := map[string]string{}
				put(detail, "state", string(v.State))
				put(detail, "volume_type", string(v.VolumeType))
				put(detail, "size_gib", fmt.Sprintf("%d", deref(v.Size)))
				put(detail, "encrypted", fmt.Sprintf("%t", deref(v.Encrypted)))
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeEBSVolume,
					name:         firstNonEmpty(nameTag(v.Tags), id),
					summary:      fmt.Sprintf("EBS volume %s (%s) in %s", id, string(v.VolumeType), w.region),
					detail:       detail,
					region:       w.region,
				}, w.account))
				if key := deref(v.KmsKeyId); key != "" {
					w.sink.addEdge(arn, key, EdgeEncryptsWith,
						map[string]string{"via": "volume.kms_key_id"})
				}
				for _, att := range v.Attachments {
					if inst := deref(att.InstanceId); inst != "" {
						w.sink.addEdge(w.ec2ResourceARN("instance", inst), arn, EdgeContains,
							map[string]string{"device": deref(att.Device)})
					}
				}
			}
			return nil
		})
}
