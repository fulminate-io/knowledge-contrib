// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"fmt"
	"strconv"
	"strings"

	filestorepb "cloud.google.com/go/filestore/apiv1/filestorepb"
	kmspb "cloud.google.com/go/kms/apiv1/kmspb"
	redispb "cloud.google.com/go/redis/apiv1/redispb"
	secretmanagerpb "cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	gcs "cloud.google.com/go/storage"
	sqladmin "google.golang.org/api/sqladmin/v1beta4"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpcontent"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
)

// data.go — where data lives: object storage, managed databases, caches, file
// shares, secrets and the keys that encrypt them.
//
// THE ENCRYPTION EDGE IS THE THREAD THROUGH ALL OF THEM. A customer-managed key
// is the one relationship every storage service in this family expresses the
// same way, and it is the question a reader most often has about a data store,
// so every converter here emits it from the same field with the same edge type.

// StorageBuckets enumerates the project's object storage buckets.
func StorageBuckets(list Lister[*gcs.BucketAttrs]) Subcollector {
	return New("gcp-storage-buckets", list, convertBucket)
}

func convertBucket(_ string, bucket *gcs.BucketAttrs) (gcpgraph.Result, error) {
	if bucket == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil bucket")
	}
	if bucket.Name == "" {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a bucket with no name")
	}
	// The scheme-qualified name is the id, because that is how every consumer
	// and every other Google surface addresses a bucket.
	id := "gs://" + bucket.Name

	kmsKey := ""
	if bucket.Encryption != nil {
		kmsKey = bucket.Encryption.DefaultKMSKeyName
	}
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: bucket.Name, SelfLink: id, Location: bucket.Location,
		Labels: bucket.Labels,
		Fields: nonEmptyFields(map[string]string{
			"storageClass":  bucket.StorageClass,
			"locationType":  bucket.LocationType,
			"defaultKmsKey": kmsKey,
		}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	metadata := map[string]string{
		"versioning": strconv.FormatBool(bucket.VersioningEnabled),
		"uniform_bucket_level_access": strconv.FormatBool(
			bucket.UniformBucketLevelAccess.Enabled),
	}
	setIfNotEmpty(metadata, "location", bucket.Location)
	setIfNotEmpty(metadata, "storage_class", bucket.StorageClass)
	setIfNotEmpty(metadata, "public_access_prevention", bucket.PublicAccessPrevention.String())
	for k, v := range bucket.Labels {
		metadata["label/"+k] = v
	}

	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: id, Name: bucket.Name, ResourceType: gcpgraph.ResourceTypeStorageBucket,
		Region: bucket.Location, Content: raw, Metadata: metadata,
	}}}
	if kmsKey != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: kmsKey, Type: gcpgraph.EdgeEncryptsWith,
		})
	}
	if bucket.Logging != nil && bucket.Logging.LogBucket != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: "gs://" + bucket.Logging.LogBucket, Type: gcpgraph.EdgeSinksTo,
			Metadata: map[string]string{"log_object_prefix": bucket.Logging.LogObjectPrefix},
		})
	}
	return out, nil
}

// PubSubTopicInfo is the topic shape this collector needs. It is this package's
// own type rather than the SDK's because the SDK's topic handle carries its
// configuration behind a second network call, and a converter that made one
// would not be a pure function of what it was handed.
type PubSubTopicInfo struct {
	ID         string
	Name       string
	Labels     map[string]string
	KMSKeyName string
	State      string
}

// PubSubTopics enumerates the project's message topics.
func PubSubTopics(list Lister[PubSubTopicInfo]) Subcollector {
	return New("gcp-pubsub-topics", list, convertPubSubTopic)
}

func convertPubSubTopic(_ string, topic PubSubTopicInfo) (gcpgraph.Result, error) {
	if topic.ID == "" {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a topic with no resource name")
	}
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: topic.Name, SelfLink: topic.ID, State: topic.State, Labels: topic.Labels,
		Fields: nonEmptyFields(map[string]string{"kmsKeyName": topic.KMSKeyName}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	metadata := map[string]string{}
	setIfNotEmpty(metadata, "state", topic.State)
	for k, v := range topic.Labels {
		metadata["label/"+k] = v
	}
	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: topic.ID, Name: topic.Name, ResourceType: gcpgraph.ResourceTypePubSubTopic,
		Content: raw, Metadata: metadata,
	}}}
	if topic.KMSKeyName != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: topic.ID, To: topic.KMSKeyName, Type: gcpgraph.EdgeEncryptsWith,
		})
	}
	return out, nil
}

// PubSubSubscriptionInfo is the subscription shape this collector needs, for the
// same reason as [PubSubTopicInfo].
type PubSubSubscriptionInfo struct {
	ID                string
	Name              string
	TopicID           string
	DeadLetterTopicID string
	PushEndpoint      string
	Filter            string
	Labels            map[string]string
	Detached          bool
}

// PubSubSubscriptions enumerates the project's message subscriptions.
func PubSubSubscriptions(list Lister[PubSubSubscriptionInfo]) Subcollector {
	return New("gcp-pubsub-subscriptions", list, convertPubSubSubscription)
}

func convertPubSubSubscription(_ string, sub PubSubSubscriptionInfo) (gcpgraph.Result, error) {
	if sub.ID == "" {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a subscription with no resource name")
	}
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: sub.Name, SelfLink: sub.ID, Labels: sub.Labels,
		Fields: nonEmptyFields(map[string]string{
			"topic":           sub.TopicID,
			"deadLetterTopic": sub.DeadLetterTopicID,
			"pushEndpoint":    sub.PushEndpoint,
			"filter":          sub.Filter,
		}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	metadata := map[string]string{"detached": strconv.FormatBool(sub.Detached)}
	setIfNotEmpty(metadata, "filter", sub.Filter)
	// The push endpoint is a URL an operator configured. It is recorded because
	// "where does this subscription deliver to" is the question a subscription
	// exists to answer.
	setIfNotEmpty(metadata, "push_endpoint", sub.PushEndpoint)
	for k, v := range sub.Labels {
		metadata["label/"+k] = v
	}

	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: sub.ID, Name: sub.Name, ResourceType: gcpgraph.ResourceTypePubSubSubscription,
		Content: raw, Metadata: metadata,
	}}}
	if sub.TopicID != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: sub.ID, To: sub.TopicID, Type: gcpgraph.EdgeSubscribesTo,
		})
	}
	if sub.DeadLetterTopicID != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: sub.ID, To: sub.DeadLetterTopicID, Type: gcpgraph.EdgeDeadLettersTo,
		})
	}
	return out, nil
}

// Secrets enumerates the project's stored secrets. IT STORES NO SECRET VALUE:
// this enumeration reads the secret's METADATA and never a version's payload,
// which is the difference between a graph of where secrets are used and a copy
// of the secrets themselves.
func Secrets(list Lister[*secretmanagerpb.Secret]) Subcollector {
	return New("gcp-secrets", list, convertSecret)
}

func convertSecret(_ string, secret *secretmanagerpb.Secret) (gcpgraph.Result, error) {
	if secret == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil secret")
	}
	id := secret.GetName()
	if id == "" {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a secret with no resource name")
	}
	kmsKey := secret.GetCustomerManagedEncryption().GetKmsKeyName()
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: gcpgraph.LastSegment(id), SelfLink: id, Labels: secret.GetLabels(),
		Fields: nonEmptyFields(map[string]string{"kmsKeyName": kmsKey}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	metadata := map[string]string{}
	for k, v := range secret.GetLabels() {
		metadata["label/"+k] = v
	}
	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: id, Name: gcpgraph.LastSegment(id), ResourceType: gcpgraph.ResourceTypeSecret,
		Content: raw, Metadata: metadata,
	}}}
	if kmsKey != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: kmsKey, Type: gcpgraph.EdgeEncryptsWith,
		})
	}
	// A secret can announce its own rotation onto a topic, which is a real
	// dependency between the secret and the messaging layer.
	for _, topic := range secret.GetTopics() {
		if name := topic.GetName(); name != "" {
			out.Relations = append(out.Relations, gcpgraph.Relation{
				From: id, To: name, Type: gcpgraph.EdgeTriggers,
			})
		}
	}
	return out, nil
}

// KMSKeyRings enumerates the project's key rings.
func KMSKeyRings(list Lister[*kmspb.KeyRing]) Subcollector {
	return New("gcp-kms-key-rings", list, convertKeyRing)
}

func convertKeyRing(_ string, ring *kmspb.KeyRing) (gcpgraph.Result, error) {
	if ring == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil key ring")
	}
	id := ring.GetName()
	if id == "" {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a key ring with no resource name")
	}
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: gcpgraph.LastSegment(id), SelfLink: id, Location: locationOfResourceName(id),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	return gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: id, Name: gcpgraph.LastSegment(id), ResourceType: gcpgraph.ResourceTypeKMSKeyRing,
		Region: locationOfResourceName(id), Content: raw,
	}}}, nil
}

// KMSCryptoKeys enumerates the project's encryption keys.
func KMSCryptoKeys(list Lister[*kmspb.CryptoKey]) Subcollector {
	return New("gcp-kms-crypto-keys", list, convertCryptoKey)
}

func convertCryptoKey(_ string, key *kmspb.CryptoKey) (gcpgraph.Result, error) {
	if key == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil crypto key")
	}
	id := key.GetName()
	if id == "" {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a crypto key with no resource name")
	}
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: gcpgraph.LastSegment(id), SelfLink: id, Location: locationOfResourceName(id),
		Labels: key.GetLabels(),
		Fields: nonEmptyFields(map[string]string{
			"purpose":   key.GetPurpose().String(),
			"algorithm": key.GetVersionTemplate().GetAlgorithm().String(),
		}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	metadata := map[string]string{}
	setIfNotEmpty(metadata, "purpose", key.GetPurpose().String())
	setIfNotEmpty(metadata, "primary_state", key.GetPrimary().GetState().String())
	for k, v := range key.GetLabels() {
		metadata["label/"+k] = v
	}
	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: id, Name: gcpgraph.LastSegment(id), ResourceType: gcpgraph.ResourceTypeKMSCryptoKey,
		Region: locationOfResourceName(id), Content: raw, Metadata: metadata,
	}}}
	// The ring is the key's own parent, read off its resource name rather than
	// from a separate lookup: projects/{p}/locations/{l}/keyRings/{r}/cryptoKeys/{k}.
	if ring, _, ok := strings.Cut(id, "/cryptoKeys/"); ok {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: ring, To: id, Type: gcpgraph.EdgeContains,
		})
	}
	return out, nil
}

// RedisInstances enumerates the project's managed caches.
func RedisInstances(list Lister[*redispb.Instance]) Subcollector {
	return New("gcp-redis-instances", list, convertRedisInstance)
}

func convertRedisInstance(_ string, inst *redispb.Instance) (gcpgraph.Result, error) {
	if inst == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil cache instance")
	}
	id := inst.GetName()
	if id == "" {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a cache instance with no resource name")
	}
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: gcpgraph.LastSegment(id), SelfLink: id, State: inst.GetState().String(),
		Location: inst.GetLocationId(), Labels: inst.GetLabels(),
		Fields: nonEmptyFields(map[string]string{
			"redisVersion":      inst.GetRedisVersion(),
			"authorizedNetwork": inst.GetAuthorizedNetwork(),
			"host":              inst.GetHost(),
		}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	metadata := map[string]string{
		"memory_size_gb": strconv.Itoa(int(inst.GetMemorySizeGb())),
		"auth_enabled":   strconv.FormatBool(inst.GetAuthEnabled()),
	}
	setIfNotEmpty(metadata, "state", inst.GetState().String())
	setIfNotEmpty(metadata, "tier", inst.GetTier().String())
	setIfNotEmpty(metadata, "redis_version", inst.GetRedisVersion())
	for k, v := range inst.GetLabels() {
		metadata["label/"+k] = v
	}
	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: id, Name: gcpgraph.LastSegment(id), ResourceType: gcpgraph.ResourceTypeRedisInstance,
		Region: inst.GetLocationId(), Content: raw, Metadata: metadata,
	}}}
	if network := inst.GetAuthorizedNetwork(); network != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: network, Type: gcpgraph.EdgeUsesNetwork,
		})
	}
	return out, nil
}

// FilestoreInstances enumerates the project's managed file shares.
func FilestoreInstances(list Lister[*filestorepb.Instance]) Subcollector {
	return New("gcp-filestore-instances", list, convertFilestoreInstance)
}

func convertFilestoreInstance(_ string, inst *filestorepb.Instance) (gcpgraph.Result, error) {
	if inst == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil file share")
	}
	id := inst.GetName()
	if id == "" {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a file share with no resource name")
	}
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: gcpgraph.LastSegment(id), SelfLink: id, Description: inst.GetDescription(),
		State: inst.GetState().String(), Location: locationOfResourceName(id),
		Labels: inst.GetLabels(),
		Fields: nonEmptyFields(map[string]string{"kmsKeyName": inst.GetKmsKeyName()}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	metadata := map[string]string{"share_count": strconv.Itoa(len(inst.GetFileShares()))}
	setIfNotEmpty(metadata, "state", inst.GetState().String())
	setIfNotEmpty(metadata, "tier", inst.GetTier().String())
	for k, v := range inst.GetLabels() {
		metadata["label/"+k] = v
	}
	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: id, Name: gcpgraph.LastSegment(id), ResourceType: gcpgraph.ResourceTypeFilestoreInstance,
		Region: locationOfResourceName(id), Content: raw, Metadata: metadata,
	}}}
	if key := inst.GetKmsKeyName(); key != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: key, Type: gcpgraph.EdgeEncryptsWith,
		})
	}
	for _, network := range inst.GetNetworks() {
		if name := network.GetNetwork(); name != "" {
			out.Relations = append(out.Relations, gcpgraph.Relation{
				From: id, To: name, Type: gcpgraph.EdgeUsesNetwork,
			})
		}
	}
	return out, nil
}

// SQLInstances enumerates the project's managed relational databases.
func SQLInstances(list Lister[*sqladmin.DatabaseInstance]) Subcollector {
	return New("gcp-sql-instances", list, convertSQLInstance)
}

func convertSQLInstance(projectID string, inst *sqladmin.DatabaseInstance) (gcpgraph.Result, error) {
	if inst == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil database instance")
	}
	if inst.Name == "" {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a database instance with no name")
	}
	id := "projects/" + projectID + "/instances/" + inst.Name

	content := gcpcontent.SQLInstance{
		Name:            inst.Name,
		DatabaseVersion: inst.DatabaseVersion,
		Region:          inst.Region,
		State:           inst.State,
	}
	if inst.Settings != nil {
		content.Tier = inst.Settings.Tier
	}
	for _, mapping := range inst.IpAddresses {
		if mapping != nil && mapping.IpAddress != "" {
			content.IPAddresses = append(content.IPAddresses, mapping.IpAddress)
		}
	}
	raw, err := gcpcontent.Marshal(content)
	if err != nil {
		return gcpgraph.Result{}, err
	}
	metadata := map[string]string{"address_count": strconv.Itoa(len(content.IPAddresses))}
	setIfNotEmpty(metadata, "state", inst.State)
	setIfNotEmpty(metadata, "database_version", inst.DatabaseVersion)
	setIfNotEmpty(metadata, "tier", content.Tier)
	setIfNotEmpty(metadata, "connection_name", inst.ConnectionName)

	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: id, Name: inst.Name, ResourceType: gcpgraph.ResourceTypeSQLInstance,
		Region: inst.Region, Content: raw, Metadata: metadata,
	}}}
	if inst.Settings != nil && inst.Settings.IpConfiguration != nil {
		if network := inst.Settings.IpConfiguration.PrivateNetwork; network != "" {
			out.Relations = append(out.Relations, gcpgraph.Relation{
				From: id, To: network, Type: gcpgraph.EdgeUsesNetwork,
			})
		}
	}
	if inst.DiskEncryptionConfiguration != nil && inst.DiskEncryptionConfiguration.KmsKeyName != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: inst.DiskEncryptionConfiguration.KmsKeyName,
			Type: gcpgraph.EdgeEncryptsWith,
		})
	}
	return out, nil
}
