// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// svc_storage.go — S3 buckets, Secrets Manager secrets and KMS keys: what an
// account stores and what encrypts it.

type s3API interface {
	ListBuckets(ctx context.Context, in *s3.ListBucketsInput, optFns ...func(*s3.Options)) (*s3.ListBucketsOutput, error)
}

type secretsManagerAPI interface {
	ListSecrets(ctx context.Context, in *secretsmanager.ListSecretsInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.ListSecretsOutput, error)
}

type kmsAPI interface {
	ListKeys(ctx context.Context, in *kms.ListKeysInput, optFns ...func(*kms.Options)) (*kms.ListKeysOutput, error)
	DescribeKey(ctx context.Context, in *kms.DescribeKeyInput, optFns ...func(*kms.Options)) (*kms.DescribeKeyOutput, error)
}

// walkS3 emits every bucket in the account.
//
// A BUCKET IS GLOBAL AND CARRIES NO REGION HERE. ListBuckets returns every bucket
// in the account whatever region this collect walks, and its name is unique
// across all of AWS — so stamping a bucket with the region of the collect that
// happened to find it would be a fact about the collect. The bucket's own
// location is a second API call per bucket, which is not paid: a thousand-bucket
// account would spend a thousand round trips on a field no edge here uses.
func walkS3(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*s3.ListBucketsOutput, error) {
			return w.clients.S3.ListBuckets(ctx, &s3.ListBucketsInput{ContinuationToken: token})
		},
		func(p *s3.ListBucketsOutput) *string { return p.ContinuationToken },
		func(p *s3.ListBucketsOutput) error {
			for _, b := range p.Buckets {
				name := deref(b.Name)
				if name == "" {
					continue
				}
				w.sink.addNode(newNode(resource{
					id:           "arn:aws:s3:::" + name,
					resourceType: ResourceTypeS3Bucket,
					name:         name,
					summary:      fmt.Sprintf("S3 bucket %s", name),
				}, w.account))
			}
			return nil
		})
}

func walkSecretsManager(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*secretsmanager.ListSecretsOutput, error) {
			return w.clients.SecretsManager.ListSecrets(ctx, &secretsmanager.ListSecretsInput{NextToken: token})
		},
		func(p *secretsmanager.ListSecretsOutput) *string { return p.NextToken },
		func(p *secretsmanager.ListSecretsOutput) error {
			for _, s := range p.SecretList {
				arn := deref(s.ARN)
				if arn == "" {
					continue
				}
				// THE SECRET'S VALUE IS NEVER READ. ListSecrets returns metadata
				// only, and this collector calls no operation that returns a
				// secret value — a graph node carrying one would put the
				// account's credentials into a searchable, summarized,
				// synchronized document.
				detail := map[string]string{}
				put(detail, "description", deref(s.Description))
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeSecretsManagerItem,
					name:         deref(s.Name),
					summary:      fmt.Sprintf("Secrets Manager secret %s in %s", deref(s.Name), w.region),
					detail:       detail,
					region:       w.region,
				}, w.account))
				if key := deref(s.KmsKeyId); key != "" {
					w.sink.addEdge(arn, key, EdgeEncryptsWith, map[string]string{"via": "secret.kms_key_id"})
				}
			}
			return nil
		})
}

// walkKMS lists key ids and describes each.
//
// THE LIST RETURNS IDS AND ARNS ONLY, so the describe is what supplies the alias
// a human recognizes, the key's manager and whether it is enabled — and an
// AWS-MANAGED key is skipped there rather than here, because only the describe
// says which it is.
func walkKMS(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*kms.ListKeysOutput, error) {
			return w.clients.KMS.ListKeys(ctx, &kms.ListKeysInput{Marker: token})
		},
		func(p *kms.ListKeysOutput) *string { return iamMarker(p.Truncated, p.NextMarker) },
		func(p *kms.ListKeysOutput) error {
			for _, k := range p.Keys {
				id := deref(k.KeyId)
				if id == "" {
					continue
				}
				if err := w.describeKMSKey(ctx, id, deref(k.KeyArn)); err != nil {
					return err
				}
			}
			return nil
		})
}

func (w *walkContext) describeKMSKey(ctx context.Context, keyID, keyARN string) error {
	out, err := w.clients.KMS.DescribeKey(ctx, &kms.DescribeKeyInput{KeyId: &keyID})
	if err != nil {
		return fmt.Errorf("describe key %s: %w", keyID, err)
	}
	m := out.KeyMetadata
	if m == nil {
		return nil
	}
	arn := firstNonEmpty(deref(m.Arn), keyARN)
	if arn == "" {
		return nil
	}
	detail := map[string]string{}
	put(detail, "key_manager", string(m.KeyManager))
	put(detail, "key_state", string(m.KeyState))
	put(detail, "key_usage", string(m.KeyUsage))
	put(detail, "description", deref(m.Description))
	w.sink.addNode(newNode(resource{
		id:           arn,
		resourceType: ResourceTypeKMSKey,
		name:         firstNonEmpty(deref(m.Description), keyID),
		summary:      fmt.Sprintf("KMS key %s in %s", keyID, w.region),
		detail:       detail,
		region:       w.region,
	}, w.account))
	return nil
}
