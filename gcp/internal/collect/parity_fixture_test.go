// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"testing"
	"time"

	artifactregistrypb "cloud.google.com/go/artifactregistry/apiv1/artifactregistrypb"
	computepb "cloud.google.com/go/compute/apiv1/computepb"
	containerpb "cloud.google.com/go/container/apiv1/containerpb"
	eventarcpb "cloud.google.com/go/eventarc/apiv1/eventarcpb"
	filestorepb "cloud.google.com/go/filestore/apiv1/filestorepb"
	functionspb "cloud.google.com/go/functions/apiv2/functionspb"
	adminpb "cloud.google.com/go/iam/admin/apiv1/adminpb"
	kmspb "cloud.google.com/go/kms/apiv1/kmspb"
	loggingpb "cloud.google.com/go/logging/apiv2/loggingpb"
	monitoringpb "cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"
	redispb "cloud.google.com/go/redis/apiv1/redispb"
	resourcemanagerpb "cloud.google.com/go/resourcemanager/apiv3/resourcemanagerpb"
	runpb "cloud.google.com/go/run/apiv2/runpb"
	secretmanagerpb "cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	gcs "cloud.google.com/go/storage"
	workflowspb "cloud.google.com/go/workflows/apiv1/workflowspb"
	bq "google.golang.org/api/bigquery/v2"
	cloudidentity "google.golang.org/api/cloudidentity/v1"
	cloudscheduler "google.golang.org/api/cloudscheduler/v1"
	cloudtasks "google.golang.org/api/cloudtasks/v2"
	dataflow "google.golang.org/api/dataflow/v1b3"
	dnsapi "google.golang.org/api/dns/v1"
	firestoreapi "google.golang.org/api/firestore/v1"
	sqladmin "google.golang.org/api/sqladmin/v1beta4"
	vpcaccess "google.golang.org/api/vpcaccess/v1"
	"google.golang.org/protobuf/proto"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/collect"
)

// parity_fixture_test.go — ONE recorded response per enumeration, assembled into
// a project that exercises every converter this collector has.
//
// IT IS DELIBERATELY A SINGLE PROJECT rather than a fixture per test. The
// question the coverage assertion answers is whether the WHOLE collector reaches
// its declared vocabulary, and a per-test fixture set can pass every test while
// leaving a resource type nothing emits.
//
// EVERY FIXTURE IS SHAPED TO REACH ITS CONVERTER'S EDGES, not merely to produce
// a node: a disk carries both a source image and a source snapshot, a firewall
// names a network, a router carries both a gateway and a peer. A fixture that
// produced only nodes would let the edge half of the floor pass vacuously.

const (
	fixtureProject = "proj-a"
	selfLinkBase   = "https://www.googleapis.com/compute/v1/projects/proj-a"
	fxNetwork      = selfLinkBase + "/global/networks/vpc"
	fxSubnet       = selfLinkBase + "/regions/us-central1/subnetworks/sn"
	fxInstance     = selfLinkBase + "/zones/us-central1-a/instances/web"
	fxDisk         = selfLinkBase + "/zones/us-central1-a/disks/data"
	fxKMSKey       = "projects/proj-a/locations/us-central1/keyRings/ring/cryptoKeys/key"
	fxTopic        = "projects/proj-a/topics/events"
	fxRunService   = "projects/proj-a/locations/us-central1/services/api"
	fxARRepo       = "projects/proj-a/locations/us-central1/repositories/images"
)

// fixtureEnumerations is every enumeration this collector has, each over its own
// recorded response.
func fixtureEnumerations(t *testing.T) []collect.Subcollector {
	t.Helper()
	subs := computeFixtures()
	subs = append(subs, platformFixtures()...)
	subs = append(subs, dataFixtures()...)
	subs = append(subs, serviceFixtures()...)
	return subs
}

func computeFixtures() []collect.Subcollector {
	return []collect.Subcollector{
		collect.ComputeInstances(staticLister(&computepb.Instance{
			Name: new("web"), SelfLink: new(fxInstance),
			Zone:   new(selfLinkBase + "/zones/us-central1-a"),
			Status: new("RUNNING"),
			Tags:   &computepb.Tags{Items: []string{"web"}},
			ServiceAccounts: []*computepb.ServiceAccount{
				{Email: new("svc@proj-a.iam.gserviceaccount.com")},
			},
			NetworkInterfaces: []*computepb.NetworkInterface{{
				Network: new(fxNetwork), Subnetwork: new(fxSubnet),
				NetworkIP:     new("10.0.0.5"),
				AccessConfigs: []*computepb.AccessConfig{{NatIP: new("34.1.2.3")}},
			}},
			Disks: []*computepb.AttachedDisk{{Source: new(fxDisk)}},
		})),
		collect.Networks(staticLister(&computepb.Network{
			Name: new("vpc"), SelfLink: new(fxNetwork),
			Subnetworks: []string{fxSubnet},
			Peerings: []*computepb.NetworkPeering{{
				Name:    new("peer-1"),
				Network: new("https://www.googleapis.com/compute/v1/projects/other/global/networks/theirs"),
				State:   new("ACTIVE"),
			}},
		})),
		// The subnetwork's parent network is in ANOTHER project, which is what
		// makes the shared-VPC derivation reachable from this one fixture set.
		collect.Subnetworks(staticLister(&computepb.Subnetwork{
			Name: new("sn"), SelfLink: new(fxSubnet),
			Network:     new("https://www.googleapis.com/compute/v1/projects/host-proj/global/networks/shared"),
			Region:      new(selfLinkBase + "/regions/us-central1"),
			IpCidrRange: new("10.0.0.0/24"),
		})),
		// Two rules, one per direction: a rule's direction picks both the arm
		// and the edge type, so one rule can never reach both derived types.
		collect.Firewalls(staticLister(
			&computepb.Firewall{
				Name: new("allow-in"), SelfLink: new(selfLinkBase + "/global/firewalls/allow-in"),
				Network: new(fxNetwork), Direction: new("INGRESS"),
				SourceRanges: []string{"0.0.0.0/0"},
				Allowed:      []*computepb.Allowed{{IPProtocol: new("tcp"), Ports: []string{"443"}}},
			},
			&computepb.Firewall{
				Name: new("allow-out"), SelfLink: new(selfLinkBase + "/global/firewalls/allow-out"),
				Network: new(fxNetwork), Direction: new("EGRESS"),
				DestinationRanges: []string{"10.0.0.0/8"},
				Allowed:           []*computepb.Allowed{{IPProtocol: new("tcp")}},
			},
		)),
		collect.Disks(staticLister(&computepb.Disk{
			Name: new("data"), SelfLink: new(fxDisk),
			Zone:           new(selfLinkBase + "/zones/us-central1-a"),
			SizeGb:         proto.Int64(100),
			SourceImage:    new(selfLinkBase + "/global/images/base"),
			SourceSnapshot: new(selfLinkBase + "/global/snapshots/snap"),
			DiskEncryptionKey: &computepb.CustomerEncryptionKey{
				KmsKeyName: new(fxKMSKey),
			},
		})),
		collect.ForwardingRules(staticLister(&computepb.ForwardingRule{
			Name: new("fr"), SelfLink: new(selfLinkBase + "/global/forwardingRules/fr"),
			IPAddress: new("34.9.9.9"),
			Target:    new(selfLinkBase + "/global/targetHttpsProxies/tp"),
		})),
		collect.TargetHTTPProxies(staticLister(&computepb.TargetHttpProxy{
			Name: new("tp-http"), SelfLink: new(selfLinkBase + "/global/targetHttpProxies/tp-http"),
			UrlMap: new(selfLinkBase + "/global/urlMaps/um"),
		})),
		collect.TargetHTTPSProxies(staticLister(&computepb.TargetHttpsProxy{
			Name: new("tp"), SelfLink: new(selfLinkBase + "/global/targetHttpsProxies/tp"),
			UrlMap:          new(selfLinkBase + "/global/urlMaps/um"),
			SslCertificates: []string{selfLinkBase + "/global/sslCertificates/cert"},
		})),
		collect.URLMaps(staticLister(&computepb.UrlMap{
			Name: new("um"), SelfLink: new(selfLinkBase + "/global/urlMaps/um"),
			DefaultService: new(selfLinkBase + "/global/backendServices/bs"),
		})),
		collect.BackendServices(staticLister(&computepb.BackendService{
			Name: new("bs"), SelfLink: new(selfLinkBase + "/global/backendServices/bs"),
			Backends:       []*computepb.Backend{{Group: new(selfLinkBase + "/zones/us-central1-a/instanceGroups/ig")}},
			SecurityPolicy: new(selfLinkBase + "/global/securityPolicies/armor"),
		})),
		collect.SecurityPolicies(staticLister(&computepb.SecurityPolicy{
			Name: new("armor"), SelfLink: new(selfLinkBase + "/global/securityPolicies/armor"),
			Associations: []*computepb.SecurityPolicyAssociation{{
				AttachmentId: new(selfLinkBase + "/global/backendServices/bs"),
			}},
		})),
		collect.SSLCertificates(staticLister(&computepb.SslCertificate{
			Name: new("cert"), SelfLink: new(selfLinkBase + "/global/sslCertificates/cert"),
			Type: new("MANAGED"),
		})),
		collect.Routers(staticLister(&computepb.Router{
			Name: new("rtr"), SelfLink: new(selfLinkBase + "/regions/us-central1/routers/rtr"),
			Region: new(selfLinkBase + "/regions/us-central1"), Network: new(fxNetwork),
			Nats: []*computepb.RouterNat{{
				Name:        new("nat"),
				Subnetworks: []*computepb.RouterNatSubnetworkToNat{{Name: new(fxSubnet)}},
			}},
			BgpPeers: []*computepb.RouterBgpPeer{{
				Name: new("peer"), PeerIpAddress: new("169.254.0.1"),
				PeerAsn: proto.Uint32(64512),
			}},
		})),
		collect.InstanceGroups(staticLister(&computepb.InstanceGroup{
			Name: new("ig"), SelfLink: new(selfLinkBase + "/zones/us-central1-a/instanceGroups/ig"),
			Zone:    new(selfLinkBase + "/zones/us-central1-a"),
			Network: new(fxNetwork), Subnetwork: new(fxSubnet), Size: proto.Int32(2),
		})),
	}
}

func platformFixtures() []collect.Subcollector {
	return []collect.Subcollector{
		collect.GKEClusters(staticLister(&containerpb.Cluster{
			Name: "cluster-1", Location: "us-central1", Status: containerpb.Cluster_RUNNING,
			Network: "vpc", Subnetwork: "sn",
			WorkloadIdentityConfig: &containerpb.WorkloadIdentityConfig{
				WorkloadPool: "proj-a.svc.id.goog",
			},
			NodePools: []*containerpb.NodePool{{
				Name:   "default",
				Config: &containerpb.NodeConfig{ServiceAccount: "nodes@proj-a.iam.gserviceaccount.com"},
			}},
		})),
		collect.RunServices(staticLister(&runpb.Service{
			Name: fxRunService, Uri: "https://api.example.com",
			Template: &runpb.RevisionTemplate{
				ServiceAccount: "run@proj-a.iam.gserviceaccount.com",
				Containers: []*runpb.Container{{
					Image: "us-central1-docker.pkg.dev/proj-a/images/api:v1",
					Env: []*runpb.EnvVar{{
						Name: "TOKEN",
						Values: &runpb.EnvVar_ValueSource{ValueSource: &runpb.EnvVarSource{
							SecretKeyRef: &runpb.SecretKeySelector{Secret: "api-token"},
						}},
					}},
				}},
			},
		})),
		collect.CloudFunctions(staticLister(&functionspb.Function{
			Name:        "projects/proj-a/locations/us-central1/functions/fn",
			State:       functionspb.Function_ACTIVE,
			BuildConfig: &functionspb.BuildConfig{Runtime: "go122", EntryPoint: "Handle"},
			ServiceConfig: &functionspb.ServiceConfig{
				Uri: "https://fn.example.com", ServiceAccountEmail: "fn@proj-a.iam.gserviceaccount.com",
				SecretEnvironmentVariables: []*functionspb.SecretEnvVar{{Secret: "api-token"}},
			},
			EventTrigger: &functionspb.EventTrigger{
				EventType: "google.cloud.pubsub.topic.v1.messagePublished", PubsubTopic: fxTopic,
			},
		})),
		collect.ServiceAccounts(staticLister(&adminpb.ServiceAccount{
			Name:  "projects/proj-a/serviceAccounts/svc@proj-a.iam.gserviceaccount.com",
			Email: "svc@proj-a.iam.gserviceaccount.com", DisplayName: "service",
		}, &adminpb.ServiceAccount{
			// A FOREIGN account, so the cross-project trust derivation is
			// reachable from this fixture set.
			Name:  "projects/proj-a/serviceAccounts/foreign@proj-b.iam.gserviceaccount.com",
			Email: "foreign@proj-b.iam.gserviceaccount.com",
		})),
		collect.Projects(staticLister(&resourcemanagerpb.Project{
			Name: "projects/123456", ProjectId: fixtureProject, DisplayName: "Project A",
			State: resourcemanagerpb.Project_ACTIVE,
		})),
		collect.PolicyBindings(staticLister(
			collect.PolicyBinding{
				ResourceID: "projects/proj-a", Role: "roles/iam.serviceAccountTokenCreator",
				Principal: "serviceAccount:foreign@proj-b.iam.gserviceaccount.com",
			},
			collect.PolicyBinding{
				ResourceID: "projects/proj-a", Role: "roles/viewer",
				Principal: "group:team@example.com",
			},
		)),
		collect.IdentityGroups(staticLister(collect.IdentityGroup{
			Group: &cloudidentity.Group{
				Name: "groups/01abcdef", DisplayName: "Team",
				GroupKey: &cloudidentity.EntityKey{Id: "team@example.com"},
			},
			Members: []*cloudidentity.Membership{{
				Name:               "groups/01abcdef/memberships/1",
				PreferredMemberKey: &cloudidentity.EntityKey{Id: "person@example.com"},
				Type:               "USER",
			}},
		})),
		collect.ArtifactRepositories(staticLister(
			&artifactregistrypb.Repository{
				Name: fxARRepo, Format: artifactregistrypb.Repository_DOCKER,
				KmsKeyName: fxKMSKey,
			},
			&artifactregistrypb.Repository{
				Name: "projects/proj-a/locations/us-central1/repositories/mirror",
				Mode: artifactregistrypb.Repository_REMOTE_REPOSITORY,
				ModeConfig: &artifactregistrypb.Repository_RemoteRepositoryConfig{
					RemoteRepositoryConfig: &artifactregistrypb.RemoteRepositoryConfig{
						RemoteSource: &artifactregistrypb.RemoteRepositoryConfig_DockerRepository_{
							DockerRepository: &artifactregistrypb.RemoteRepositoryConfig_DockerRepository{
								Upstream: &artifactregistrypb.RemoteRepositoryConfig_DockerRepository_PublicRepository_{
									PublicRepository: artifactregistrypb.
										RemoteRepositoryConfig_DockerRepository_DOCKER_HUB,
								},
							},
						},
					},
				},
			},
		)),
	}
}

func dataFixtures() []collect.Subcollector {
	return []collect.Subcollector{
		collect.StorageBuckets(staticLister(&gcs.BucketAttrs{
			Name: "assets", Location: "US", StorageClass: "STANDARD",
			Encryption: &gcs.BucketEncryption{DefaultKMSKeyName: fxKMSKey},
			Logging:    &gcs.BucketLogging{LogBucket: "audit"},
		})),
		collect.PubSubTopics(staticLister(collect.PubSubTopicInfo{
			ID: fxTopic, Name: "events", KMSKeyName: fxKMSKey, State: "ACTIVE",
		})),
		collect.PubSubSubscriptions(staticLister(collect.PubSubSubscriptionInfo{
			ID: "projects/proj-a/subscriptions/events-sub", Name: "events-sub",
			TopicID: fxTopic, DeadLetterTopicID: "projects/proj-a/topics/dead",
		})),
		collect.Secrets(staticLister(&secretmanagerpb.Secret{
			Name: "projects/proj-a/secrets/api-token",
			CustomerManagedEncryption: &secretmanagerpb.CustomerManagedEncryption{
				KmsKeyName: fxKMSKey,
			},
			Topics: []*secretmanagerpb.Topic{{Name: fxTopic}},
		})),
		collect.KMSKeyRings(staticLister(&kmspb.KeyRing{
			Name: "projects/proj-a/locations/us-central1/keyRings/ring",
		})),
		collect.KMSCryptoKeys(staticLister(&kmspb.CryptoKey{
			Name: fxKMSKey, Purpose: kmspb.CryptoKey_ENCRYPT_DECRYPT,
		})),
		collect.RedisInstances(staticLister(&redispb.Instance{
			Name:       "projects/proj-a/locations/us-central1/instances/cache",
			LocationId: "us-central1", AuthorizedNetwork: fxNetwork, MemorySizeGb: 1,
			State: redispb.Instance_READY,
		})),
		collect.FilestoreInstances(staticLister(&filestorepb.Instance{
			Name:  "projects/proj-a/locations/us-central1-a/instances/share",
			State: filestorepb.Instance_READY, KmsKeyName: fxKMSKey,
			Networks: []*filestorepb.NetworkConfig{{Network: fxNetwork}},
		})),
		collect.LogSinks(staticLister(&loggingpb.LogSink{
			Name: "audit-sink", Destination: "storage.googleapis.com/projects/proj-a/buckets/audit",
			Filter: "severity>=ERROR",
		})),
		collect.AlertPolicies(staticLister(&monitoringpb.AlertPolicy{
			Name: "projects/proj-a/alertPolicies/1", DisplayName: "High error rate",
			NotificationChannels: []string{"projects/proj-a/notificationChannels/1"},
		})),
		collect.NotificationChannels(staticLister(&monitoringpb.NotificationChannel{
			Name: "projects/proj-a/notificationChannels/1", DisplayName: "On call", Type: "email",
		})),
		collect.EventarcTriggers(staticLister(&eventarcpb.Trigger{
			Name:           "projects/proj-a/locations/us-central1/triggers/t",
			ServiceAccount: "trigger@proj-a.iam.gserviceaccount.com",
			EventFilters: []*eventarcpb.EventFilter{{
				Attribute: "type", Value: "google.cloud.storage.object.v1.finalized",
			}},
			Destination: &eventarcpb.Destination{
				Descriptor_: &eventarcpb.Destination_CloudRun{
					CloudRun: &eventarcpb.CloudRun{Service: fxRunService},
				},
			},
			Transport: &eventarcpb.Transport{
				Intermediary: &eventarcpb.Transport_Pubsub{
					Pubsub: &eventarcpb.Pubsub{Topic: fxTopic},
				},
			},
		})),
		collect.Workflows(staticLister(&workflowspb.Workflow{
			Name:  "projects/proj-a/locations/us-central1/workflows/wf",
			State: workflowspb.Workflow_ACTIVE, CryptoKeyName: fxKMSKey,
			ServiceAccount: "projects/proj-a/serviceAccounts/wf@proj-a.iam.gserviceaccount.com",
		})),
		collect.DataflowJobs(staticLister(&dataflow.Job{
			Id: "job-1", Name: "pipeline", Location: "us-central1", CurrentState: "JOB_STATE_RUNNING",
			Environment: &dataflow.Environment{ServiceAccountEmail: "df@proj-a.iam.gserviceaccount.com"},
		})),
	}
}

func serviceFixtures() []collect.Subcollector {
	return []collect.Subcollector{
		collect.SQLInstances(staticLister(&sqladmin.DatabaseInstance{
			Name: "db", Region: "us-central1", DatabaseVersion: "POSTGRES_15", State: "RUNNABLE",
			IpAddresses: []*sqladmin.IpMapping{{IpAddress: "35.1.2.3", Type: "PRIMARY"}},
			Settings: &sqladmin.Settings{
				Tier:            "db-f1-micro",
				IpConfiguration: &sqladmin.IpConfiguration{PrivateNetwork: fxNetwork},
			},
			DiskEncryptionConfiguration: &sqladmin.DiskEncryptionConfiguration{KmsKeyName: fxKMSKey},
		})),
		collect.VPCConnectors(staticLister(&vpcaccess.Connector{
			Name:    "projects/proj-a/locations/us-central1/connectors/conn",
			Network: "vpc", IpCidrRange: "10.8.0.0/28", State: "READY",
			Subnet: &vpcaccess.Subnet{Name: "sn"},
		})),
		collect.TaskQueues(staticLister(&cloudtasks.Queue{
			Name: "projects/proj-a/locations/us-central1/queues/q", State: "RUNNING",
		})),
		collect.ScheduledJobs(staticLister(&cloudscheduler.Job{
			Name:     "projects/proj-a/locations/us-central1/jobs/nightly",
			Schedule: "0 2 * * *", State: "ENABLED",
			HttpTarget: &cloudscheduler.HttpTarget{
				Uri:       "https://api.example.com/cron",
				OidcToken: &cloudscheduler.OidcToken{ServiceAccountEmail: "cron@proj-a.iam.gserviceaccount.com"},
			},
		})),
		collect.DNSZones(staticLister(collect.DNSZone{
			Zone: &dnsapi.ManagedZone{
				Name: "example", DnsName: "example.com.", Visibility: "private",
				CreationTime: time.Now().UTC().Format(time.RFC3339),
				PrivateVisibilityConfig: &dnsapi.ManagedZonePrivateVisibilityConfig{
					Networks: []*dnsapi.ManagedZonePrivateVisibilityConfigNetwork{{NetworkUrl: fxNetwork}},
				},
			},
			Records: []*dnsapi.ResourceRecordSet{
				{Name: "api.example.com.", Type: "A", Ttl: 300, Rrdatas: []string{"34.1.2.3"}},
			},
		})),
		collect.BigQueryDatasets(staticLister(collect.BigQueryDataset{
			ProjectID: fixtureProject, DatasetID: "analytics", Location: "US",
			KMSKeyName: fxKMSKey,
			Tables: []*bq.TableListTables{{
				TableReference: &bq.TableReference{
					ProjectId: fixtureProject, DatasetId: "analytics", TableId: "events",
				},
				Type: "TABLE",
			}},
		})),
		collect.FirestoreDatabases(staticLister(collect.FirestoreDatabase{
			Database: &firestoreapi.GoogleFirestoreAdminV1Database{
				Name: "projects/proj-a/databases/(default)", LocationId: "nam5",
				Type:       "FIRESTORE_NATIVE",
				CmekConfig: &firestoreapi.GoogleFirestoreAdminV1CmekConfig{KmsKeyName: fxKMSKey},
			},
			Schedules: []*firestoreapi.GoogleFirestoreAdminV1BackupSchedule{{
				Name: "projects/proj-a/databases/(default)/backupSchedules/1", Retention: "604800s",
			}},
		})),
	}
}
