// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
)

// awsclient.go — GETTING A CREDENTIAL, AND FAILING LOUDLY WHEN THERE IS NONE.
//
// THE DEFAULT CHAIN IS THE ONLY ROUTE. There is no key parameter, no profile
// parameter and no static-credentials provider: a credential value never rides
// the collect, and the operator supplies one the way every other AWS tool on
// the machine gets one — through the environment this collector's config entry
// declares. That is why the module does not require aws-sdk-go-v2/credentials
// at all; adding it back would add a second route.

// newDefaultChainClient builds a CloudWatch Logs client for one region from the
// AWS default credential chain.
//
// IT RESOLVES THE CREDENTIAL EAGERLY rather than leaving it to the first API
// call. A chain that resolves nothing otherwise surfaces as a failed
// FilterLogEvents, which reads as a permissions or a network problem; resolving
// here lets the refusal say that no credential was found and name the chain
// that looked.
//
// A REGION IS NOT REQUIRED HERE and its absence is not this function's to
// refuse: an operator with a profile or an instance role has a region in the
// environment, and config.LoadDefaultConfig finds it. A region that resolves
// nowhere is refused below, once, naming what the caller can do about it.
func newDefaultChainClient(ctx context.Context, region string) (*cloudwatchlogs.Client, error) {
	opts := []func(*config.LoadOptions) error{}
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf(
			"cloudwatch: loading AWS configuration from the default credential chain failed: %w", err)
	}

	if _, err := cfg.Credentials.Retrieve(ctx); err != nil {
		return nil, fmt.Errorf(
			"cloudwatch: the AWS default credential chain resolved no credential: %w; "+
				"the chain reads, in order, the environment variables, the shared credentials and config files "+
				"under $HOME/.aws, a web-identity or SSO session, the container credential endpoint and the EC2 "+
				"instance metadata service — this collector supplies none of them itself, so the names it needs "+
				"must be listed with their values in its entry in the collector configuration file", err)
	}

	if cfg.Region == "" {
		return nil, fmt.Errorf(
			"cloudwatch: no AWS region resolved; set params.region on the collect, or declare AWS_REGION " +
				"or AWS_DEFAULT_REGION in this collector's configuration entry")
	}

	return cloudwatchlogs.NewFromConfig(cfg), nil
}
