// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/acm"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	"github.com/aws/aws-sdk-go-v2/service/route53"
)

// svc_edge.go — CloudFront, ACM and Route 53: the three global services that
// decide how the internet reaches an account.

type cloudFrontAPI interface {
	ListDistributions(ctx context.Context, in *cloudfront.ListDistributionsInput, optFns ...func(*cloudfront.Options)) (*cloudfront.ListDistributionsOutput, error)
}

type acmAPI interface {
	ListCertificates(ctx context.Context, in *acm.ListCertificatesInput, optFns ...func(*acm.Options)) (*acm.ListCertificatesOutput, error)
	DescribeCertificate(ctx context.Context, in *acm.DescribeCertificateInput, optFns ...func(*acm.Options)) (*acm.DescribeCertificateOutput, error)
}

type route53API interface {
	ListHostedZones(ctx context.Context, in *route53.ListHostedZonesInput, optFns ...func(*route53.Options)) (*route53.ListHostedZonesOutput, error)
}

func walkCloudFront(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*cloudfront.ListDistributionsOutput, error) {
			return w.clients.CloudFront.ListDistributions(ctx, &cloudfront.ListDistributionsInput{Marker: token})
		},
		func(p *cloudfront.ListDistributionsOutput) *string {
			if p.DistributionList == nil {
				return nil
			}
			return iamMarker(deref(p.DistributionList.IsTruncated), p.DistributionList.NextMarker)
		},
		func(p *cloudfront.ListDistributionsOutput) error {
			if p.DistributionList == nil {
				return nil
			}
			for _, d := range p.DistributionList.Items {
				arn := deref(d.ARN)
				if arn == "" {
					continue
				}
				detail := map[string]string{}
				put(detail, "domain_name", deref(d.DomainName))
				put(detail, "status", deref(d.Status))
				put(detail, "enabled", fmt.Sprintf("%t", deref(d.Enabled)))
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeCloudFrontDistribution,
					name:         firstNonEmpty(deref(d.Comment), deref(d.Id)),
					summary:      fmt.Sprintf("CloudFront distribution %s (%s)", deref(d.Id), deref(d.DomainName)),
					detail:       detail,
				}, w.account))

				// THE CERTIFICATE IS AN ACM ARN only when the distribution uses
				// one: a distribution on the default CloudFront certificate names
				// no ACM resource, and an edge for it would point at nothing.
				if d.ViewerCertificate != nil {
					if cert := deref(d.ViewerCertificate.ACMCertificateArn); cert != "" {
						w.sink.addEdge(arn, cert, EdgeUsesCert,
							map[string]string{"via": "distribution.viewer_certificate"})
					}
				}
				if d.Origins != nil {
					for _, o := range d.Origins.Items {
						// AN ORIGIN'S DOMAIN IS A HOSTNAME, not an ARN, and only
						// an S3 origin maps onto a resource this account holds.
						if bucket, ok := s3BucketFromOriginDomain(deref(o.DomainName)); ok {
							w.sink.addEdge(arn, "arn:aws:s3:::"+bucket, EdgeExposedVia,
								map[string]string{"origin_id": deref(o.Id)})
						}
					}
				}
			}
			return nil
		})
}

// s3BucketFromOriginDomain maps a CloudFront origin domain onto the S3 bucket it
// names, when it names one.
//
// The two forms AWS emits are <bucket>.s3.amazonaws.com and
// <bucket>.s3.<region>.amazonaws.com; anything else — a custom origin, an ALB, a
// third-party host — names no bucket and yields no edge.
func s3BucketFromOriginDomain(domain string) (string, bool) {
	if !strings.Contains(domain, ".s3.") && !strings.HasSuffix(domain, ".s3.amazonaws.com") {
		return "", false
	}
	bucket, _, found := strings.Cut(domain, ".s3.")
	if !found || bucket == "" {
		return "", false
	}
	return bucket, true
}

// walkACM lists certificates and describes each.
//
// THE DESCRIBE IS WHAT CARRIES DNS VALIDATION, which is the only way to connect a
// certificate to the hosted zone that proves it. The list returns the domain and
// the ARN and nothing about validation.
func walkACM(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*acm.ListCertificatesOutput, error) {
			return w.clients.ACM.ListCertificates(ctx, &acm.ListCertificatesInput{NextToken: token})
		},
		func(p *acm.ListCertificatesOutput) *string { return p.NextToken },
		func(p *acm.ListCertificatesOutput) error {
			for _, c := range p.CertificateSummaryList {
				arn := deref(c.CertificateArn)
				if arn == "" {
					continue
				}
				detail := map[string]string{}
				put(detail, "domain_name", deref(c.DomainName))
				put(detail, "status", string(c.Status))
				put(detail, "certificate_type", string(c.Type))
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeACMCertificate,
					name:         deref(c.DomainName),
					summary:      fmt.Sprintf("ACM certificate for %s in %s", deref(c.DomainName), w.region),
					detail:       detail,
					region:       w.region,
				}, w.account))
				w.recordCertificateValidation(ctx, arn)
			}
			return nil
		})
}

// recordCertificateValidation records the DNS-validated domains of one
// certificate for the derived pass.
//
// IT CANNOT EMIT THE EDGE HERE, and that is a consequence of the fan-out rather
// than a style choice: the edge's far end is a Route 53 hosted zone, the Route 53
// walk runs concurrently with this one, and a match attempted here would see
// whichever zones happened to have arrived. derive_certs.go runs after the whole
// account is enumerated, so it sees every zone.
//
// THE FAILURE DOES NOT PROPAGATE AND IS NOT SWALLOWED: one certificate's
// validation options going unread costs that certificate's VALIDATED_BY edge, so
// the read records into the account's failure set and the account asserts
// INCOMPLETE naming this operation. Failing the whole ACM walk would cost every
// other certificate's.
func (w *walkContext) recordCertificateValidation(ctx context.Context, certARN string) {
	out, err := w.clients.ACM.DescribeCertificate(ctx, &acm.DescribeCertificateInput{CertificateArn: &certARN})
	if err != nil {
		w.recordSubreadFailure("acm.DescribeCertificate", certARN, err)
		return
	}
	if out.Certificate == nil {
		w.recordSubreadFailure("acm.DescribeCertificate", certARN,
			errors.New("the call succeeded and carried no certificate"))
		return
	}
	var domains []string
	for _, opt := range out.Certificate.DomainValidationOptions {
		// DNS VALIDATION IS THE ONLY KIND A HOSTED ZONE PROVES. An
		// email-validated certificate is validated by a person reading a mailbox,
		// which no resource in this account represents.
		if opt.ValidationMethod != "DNS" {
			continue
		}
		if d := deref(opt.DomainName); d != "" {
			domains = append(domains, d)
		}
	}
	if len(domains) > 0 {
		w.recordCertificate(certARN, domains)
	}
}

func walkRoute53(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*route53.ListHostedZonesOutput, error) {
			return w.clients.Route53.ListHostedZones(ctx, &route53.ListHostedZonesInput{Marker: token})
		},
		func(p *route53.ListHostedZonesOutput) *string { return iamMarker(p.IsTruncated, p.NextMarker) },
		func(p *route53.ListHostedZonesOutput) error {
			for _, z := range p.HostedZones {
				// THE ID ARRIVES AS /hostedzone/<id> and the ARN wants the bare
				// id, so the prefix is stripped here rather than at each use.
				bare := strings.TrimPrefix(deref(z.Id), "/hostedzone/")
				if bare == "" {
					continue
				}
				arn := "arn:aws:route53:::hostedzone/" + bare
				detail := map[string]string{}
				put(detail, "name", deref(z.Name))
				if z.Config != nil {
					detail["private_zone"] = fmt.Sprintf("%t", z.Config.PrivateZone)
				}
				if z.ResourceRecordSetCount != nil {
					detail["record_count"] = fmt.Sprintf("%d", *z.ResourceRecordSetCount)
				}
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeRoute53HostedZone,
					name:         deref(z.Name),
					summary:      fmt.Sprintf("Route 53 hosted zone %s", deref(z.Name)),
					detail:       detail,
				}, w.account))
				w.recordHostedZone(arn, deref(z.Name))
			}
			return nil
		})
}
