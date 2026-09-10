// SPDX-License-Identifier: Apache-2.0

package gcpclients

import (
	"context"
	"fmt"

	artifactregistry "cloud.google.com/go/artifactregistry/apiv1"
	compute "cloud.google.com/go/compute/apiv1"
	container "cloud.google.com/go/container/apiv1"
	eventarc "cloud.google.com/go/eventarc/apiv1"
	filestore "cloud.google.com/go/filestore/apiv1"
	functions "cloud.google.com/go/functions/apiv2"
	iam "cloud.google.com/go/iam/admin/apiv1"
	kms "cloud.google.com/go/kms/apiv1"
	logging "cloud.google.com/go/logging/apiv2"
	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	"cloud.google.com/go/pubsub"
	redis "cloud.google.com/go/redis/apiv1"
	resourcemanager "cloud.google.com/go/resourcemanager/apiv3"
	run "cloud.google.com/go/run/apiv2"
	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	gcs "cloud.google.com/go/storage"
	workflows "cloud.google.com/go/workflows/apiv1"
	bq "google.golang.org/api/bigquery/v2"
	cloudidentity "google.golang.org/api/cloudidentity/v1"
	cloudscheduler "google.golang.org/api/cloudscheduler/v1"
	cloudtasks "google.golang.org/api/cloudtasks/v2"
	dataflow "google.golang.org/api/dataflow/v1b3"
	dns "google.golang.org/api/dns/v1"
	firestore "google.golang.org/api/firestore/v1"
	"google.golang.org/api/option"
	sqladmin "google.golang.org/api/sqladmin/v1beta4"
	vpcaccess "google.golang.org/api/vpcaccess/v1"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/collect"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpauth"
)

// clients holds every open handle one walk uses, so a single release closes
// them all whatever went wrong.
type clients struct {
	closers []func() error

	instances          *compute.InstancesClient
	networks           *compute.NetworksClient
	subnetworks        *compute.SubnetworksClient
	firewalls          *compute.FirewallsClient
	disks              *compute.DisksClient
	forwardingRules    *compute.GlobalForwardingRulesClient
	targetHTTPProxies  *compute.TargetHttpProxiesClient
	targetHTTPSProxies *compute.TargetHttpsProxiesClient
	urlMaps            *compute.UrlMapsClient
	backendServices    *compute.BackendServicesClient
	securityPolicies   *compute.SecurityPoliciesClient
	sslCertificates    *compute.SslCertificatesClient
	routers            *compute.RoutersClient
	instanceGroups     *compute.InstanceGroupsClient

	container        *container.ClusterManagerClient
	run              *run.ServicesClient
	functions        *functions.FunctionClient
	storage          *gcs.Client
	pubsub           *pubsub.Client
	secrets          *secretmanager.Client
	kms              *kms.KeyManagementClient
	redis            *redis.CloudRedisClient
	filestore        *filestore.CloudFilestoreManagerClient
	iam              *iam.IamClient
	resourceManager  *resourcemanager.ProjectsClient
	artifactRegistry *artifactregistry.Client
	logging          *logging.ConfigClient
	alertPolicies    *monitoring.AlertPolicyClient
	channels         *monitoring.NotificationChannelClient
	eventarc         *eventarc.Client
	workflows        *workflows.Client

	sqladmin      *sqladmin.Service
	dns           *dns.Service
	tasks         *cloudtasks.Service
	scheduler     *cloudscheduler.Service
	bigquery      *bq.Service
	firestore     *firestore.Service
	dataflow      *dataflow.Service
	vpcaccess     *vpcaccess.Service
	cloudIdentity *cloudidentity.Service
}

// Enumerations is the production wiring: it resolves the credential, opens every
// client, and returns the walk's enumerations beside the release that closes
// them.
//
// A FAILURE TO OPEN ANY CLIENT FAILS THE WHOLE WALK, and it closes what it had
// already opened first. Constructing a client does not call the API, so a
// failure here is a configuration or credential problem rather than a permission
// one — and continuing with a nil handle would surface as a panic inside an
// enumeration instead.
func Enumerations(ctx context.Context, projectID string) ([]collect.Subcollector, func(), error) {
	creds, err := gcpauth.Find(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	opts := []option.ClientOption{option.WithCredentials(creds)}

	c := &clients{}
	release := func() {
		// A close failure is reported and not acted on: the walk is over, and
		// there is nothing left to do differently.
		if err := closeAll(c.closers); err != nil {
			logCloseFailure(err)
		}
	}
	if err := c.open(ctx, projectID, opts); err != nil {
		release()
		return nil, nil, err
	}
	return c.subcollectors(), release, nil
}

// open constructs every client. Each step names the service it could not reach,
// because "creating clients failed" tells an operator nothing about which API is
// not enabled.
func (c *clients) open(ctx context.Context, projectID string, opts []option.ClientOption) error {
	if err := c.openCompute(ctx, opts); err != nil {
		return err
	}
	if err := c.openPlatform(ctx, projectID, opts); err != nil {
		return err
	}
	return c.openREST(ctx, opts)
}

func (c *clients) openCompute(ctx context.Context, opts []option.ClientOption) error {
	var err error
	if c.instances, err = compute.NewInstancesRESTClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the compute instances client: %w", err)
	}
	c.closers = append(c.closers, c.instances.Close)
	if c.networks, err = compute.NewNetworksRESTClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the compute networks client: %w", err)
	}
	c.closers = append(c.closers, c.networks.Close)
	if c.subnetworks, err = compute.NewSubnetworksRESTClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the compute subnetworks client: %w", err)
	}
	c.closers = append(c.closers, c.subnetworks.Close)
	if c.firewalls, err = compute.NewFirewallsRESTClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the compute firewalls client: %w", err)
	}
	c.closers = append(c.closers, c.firewalls.Close)
	if c.disks, err = compute.NewDisksRESTClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the compute disks client: %w", err)
	}
	c.closers = append(c.closers, c.disks.Close)
	if c.forwardingRules, err = compute.NewGlobalForwardingRulesRESTClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the compute forwarding rules client: %w", err)
	}
	c.closers = append(c.closers, c.forwardingRules.Close)
	if c.targetHTTPProxies, err = compute.NewTargetHttpProxiesRESTClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the compute target HTTP proxies client: %w", err)
	}
	c.closers = append(c.closers, c.targetHTTPProxies.Close)
	if c.targetHTTPSProxies, err = compute.NewTargetHttpsProxiesRESTClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the compute target HTTPS proxies client: %w", err)
	}
	c.closers = append(c.closers, c.targetHTTPSProxies.Close)
	if c.urlMaps, err = compute.NewUrlMapsRESTClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the compute URL maps client: %w", err)
	}
	c.closers = append(c.closers, c.urlMaps.Close)
	if c.backendServices, err = compute.NewBackendServicesRESTClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the compute backend services client: %w", err)
	}
	c.closers = append(c.closers, c.backendServices.Close)
	if c.securityPolicies, err = compute.NewSecurityPoliciesRESTClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the compute security policies client: %w", err)
	}
	c.closers = append(c.closers, c.securityPolicies.Close)
	if c.sslCertificates, err = compute.NewSslCertificatesRESTClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the compute SSL certificates client: %w", err)
	}
	c.closers = append(c.closers, c.sslCertificates.Close)
	if c.routers, err = compute.NewRoutersRESTClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the compute routers client: %w", err)
	}
	c.closers = append(c.closers, c.routers.Close)
	if c.instanceGroups, err = compute.NewInstanceGroupsRESTClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the compute instance groups client: %w", err)
	}
	c.closers = append(c.closers, c.instanceGroups.Close)
	return nil
}

// openPlatform opens the clients for the services that RUN things and the ones
// that STORE things. It is split from [clients.openIdentity] only for length:
// one sequential list of constructors is one unit of work whatever it is called,
// and the split point is the boundary between the two families rather than an
// arbitrary line.
func (c *clients) openPlatform(ctx context.Context, projectID string, opts []option.ClientOption) error {
	var err error
	if c.container, err = container.NewClusterManagerClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the kubernetes engine client: %w", err)
	}
	c.closers = append(c.closers, c.container.Close)
	if c.run, err = run.NewServicesClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the cloud run client: %w", err)
	}
	c.closers = append(c.closers, c.run.Close)
	if c.functions, err = functions.NewFunctionClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the cloud functions client: %w", err)
	}
	c.closers = append(c.closers, c.functions.Close)
	if c.storage, err = gcs.NewClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the cloud storage client: %w", err)
	}
	c.closers = append(c.closers, c.storage.Close)
	if c.pubsub, err = pubsub.NewClient(ctx, projectID, opts...); err != nil {
		return fmt.Errorf("opening the pub/sub client: %w", err)
	}
	c.closers = append(c.closers, c.pubsub.Close)
	if c.secrets, err = secretmanager.NewClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the secret manager client: %w", err)
	}
	c.closers = append(c.closers, c.secrets.Close)
	if c.kms, err = kms.NewKeyManagementClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the key management client: %w", err)
	}
	c.closers = append(c.closers, c.kms.Close)
	if c.redis, err = redis.NewCloudRedisClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the memorystore client: %w", err)
	}
	c.closers = append(c.closers, c.redis.Close)
	if c.filestore, err = filestore.NewCloudFilestoreManagerClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the filestore client: %w", err)
	}
	c.closers = append(c.closers, c.filestore.Close)
	return c.openIdentity(ctx, opts)
}

// openIdentity opens the clients for identity, artifacts and observability.
func (c *clients) openIdentity(ctx context.Context, opts []option.ClientOption) error {
	var err error
	if c.iam, err = iam.NewIamClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the iam client: %w", err)
	}
	c.closers = append(c.closers, c.iam.Close)
	if c.resourceManager, err = resourcemanager.NewProjectsClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the resource manager client: %w", err)
	}
	c.closers = append(c.closers, c.resourceManager.Close)
	if c.artifactRegistry, err = artifactregistry.NewClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the artifact registry client: %w", err)
	}
	c.closers = append(c.closers, c.artifactRegistry.Close)
	if c.logging, err = logging.NewConfigClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the logging config client: %w", err)
	}
	c.closers = append(c.closers, c.logging.Close)
	if c.alertPolicies, err = monitoring.NewAlertPolicyClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the monitoring alert policy client: %w", err)
	}
	c.closers = append(c.closers, c.alertPolicies.Close)
	if c.channels, err = monitoring.NewNotificationChannelClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the monitoring notification channel client: %w", err)
	}
	c.closers = append(c.closers, c.channels.Close)
	if c.eventarc, err = eventarc.NewClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the eventarc client: %w", err)
	}
	c.closers = append(c.closers, c.eventarc.Close)
	if c.workflows, err = workflows.NewClient(ctx, opts...); err != nil {
		return fmt.Errorf("opening the workflows client: %w", err)
	}
	c.closers = append(c.closers, c.workflows.Close)
	return nil
}

// openREST opens the services that speak HTTP. None of them holds a connection,
// so none registers a closer.
func (c *clients) openREST(ctx context.Context, opts []option.ClientOption) error {
	var err error
	if c.sqladmin, err = sqladmin.NewService(ctx, opts...); err != nil {
		return fmt.Errorf("opening the cloud sql client: %w", err)
	}
	if c.dns, err = dns.NewService(ctx, opts...); err != nil {
		return fmt.Errorf("opening the cloud dns client: %w", err)
	}
	if c.tasks, err = cloudtasks.NewService(ctx, opts...); err != nil {
		return fmt.Errorf("opening the cloud tasks client: %w", err)
	}
	if c.scheduler, err = cloudscheduler.NewService(ctx, opts...); err != nil {
		return fmt.Errorf("opening the cloud scheduler client: %w", err)
	}
	if c.bigquery, err = bq.NewService(ctx, opts...); err != nil {
		return fmt.Errorf("opening the bigquery client: %w", err)
	}
	if c.firestore, err = firestore.NewService(ctx, opts...); err != nil {
		return fmt.Errorf("opening the firestore client: %w", err)
	}
	if c.dataflow, err = dataflow.NewService(ctx, opts...); err != nil {
		return fmt.Errorf("opening the dataflow client: %w", err)
	}
	if c.vpcaccess, err = vpcaccess.NewService(ctx, opts...); err != nil {
		return fmt.Errorf("opening the serverless vpc access client: %w", err)
	}
	if c.cloudIdentity, err = cloudidentity.NewService(ctx, opts...); err != nil {
		return fmt.Errorf("opening the cloud identity client: %w", err)
	}
	return nil
}
