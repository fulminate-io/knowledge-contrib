// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// collector.go — THE COLLECTOR THE FRAMEWORK SERVES.
//
// It implements framework.Collector and nothing else: the framework advertises
// the contract schemas with this collector's Params spliced in, validates a
// call's params before Walk runs, and encodes the result as the contract
// envelope. Nothing about MCP, JSON Schema or the envelope appears in this
// package.

// ToolName is the MCP tool this collector serves. It is what a config entry's
// `tool` field must name.
const ToolName = "collect_aws"

// Collector walks one AWS account.
//
// IT HOLDS TWO SEAMS AND NO STATE. loadConfig resolves the credential chain and
// newClients builds the service clients; both are fields rather than direct calls
// so a test drives the whole walk — params, fan-out, derivation, envelope — with
// in-memory clients and no credential, no endpoint and no network.
type Collector struct {
	// loadConfig resolves an aws.Config for a region. Nil means the AWS default
	// credential chain, which is what a real collect uses.
	loadConfig func(ctx context.Context, region string) (aws.Config, error)
	// newClients builds the service clients from a resolved config. Nil means
	// the real SDK clients.
	newClients func(cfg aws.Config) *Clients
	// identity resolves the account id the credential belongs to. Nil means STS.
	identity func(ctx context.Context, cfg aws.Config) (string, error)
}

// New returns the collector a real deployment serves.
func New() *Collector { return &Collector{} }

// Tool names the MCP tool this collector serves.
func (c *Collector) Tool() framework.ToolSpec {
	return framework.ToolSpec{
		Name: ToolName,
		Description: "Enumerate an AWS account's resources and the relationships among them, " +
			"as cloud-resource nodes carrying a resource_type. Credentials come from the AWS " +
			"default chain; the collect id names the graph instance.",
	}
}

// Walk enumerates the account and returns what it found.
//
// THE ORDER IS: validate, resolve credentials, learn the account, fan out over
// the service walks, derive, encode. Each step's failure is DIFFERENT in kind and
// they are not merged:
//
//   - a bad param is a caller error, refused before any network call;
//   - an unresolvable credential is an operator error, and the message names the
//     chain that was tried rather than passing the SDK's own error up alone;
//   - a failed SERVICE walk is neither: the account was partly enumerated, so the
//     result SHIPS with walk_complete false rather than failing the collect.
//
// THE LAST DISTINCTION IS THE LOAD-BEARING ONE. A collect that failed outright
// writes nothing and changes nothing. A collect that succeeded with
// walk_complete TRUE tells the server every record this walk did not name is
// gone. Asserting the second over a partial walk is how a throttled service
// deletes half a graph.
//
// THE FOREIGN-CONTEXT BLOCK IS NAMED `_` BECAUSE THIS COLLECTOR DECLARES NONE.
// The block carries the foreign-graph slices a collector's config entry asked
// for, and this one asks for nothing: an AWS account walk reads AWS, and every
// relationship it emits has both endpoints in its own result or in a form the
// contract already admits as legitimately external. Taking the parameter and
// ignoring it is the framework's own instruction for that case, and it is what
// keeps the compile-time interface assertion honest rather than making this
// collector look like it consults a graph it never reads.
func (c *Collector) Walk(ctx context.Context, id string, params Params, _ framework.ForeignContext) (framework.Result, error) {
	if err := params.validate(); err != nil {
		return framework.Result{}, err
	}

	cfg, err := c.resolveConfig(ctx, params.Region)
	if err != nil {
		return framework.Result{}, err
	}
	// THE STAMPED REGION IS THE CONFIG'S, NOT THE PARAMS'. They are the same
	// value on every ordinary path — resolveConfig passes params.Region into the
	// config loader — and taking the params' would be a defensive line that
	// creates the very inconsistency it looks like it prevents: the SERVICE
	// CLIENTS are built from cfg, so a config that resolved a different region
	// than it was asked for would produce nodes stamped with a region the walk did
	// not visit. One source of truth is what keeps the stamp and the walk agreeing.
	region := cfg.Region
	if region == "" {
		return framework.Result{}, fmt.Errorf(
			"no AWS region is set: pass params.region, or set AWS_REGION or AWS_DEFAULT_REGION in this " +
				"collector's environment, or give the active profile a region")
	}

	account, err := c.resolveAccount(ctx, cfg)
	if err != nil {
		return framework.Result{}, err
	}
	// THE ACCOUNT SELECTOR IS A GUARD, checked here because this is the first
	// point at which both the requested and the actual account are known. A
	// collect that walked the wrong account would produce a graph that looks
	// entirely correct.
	if params.Account != "" && params.Account != account {
		return framework.Result{}, fmt.Errorf(
			"params.account is %s but this collector's credentials resolve to account %s; "+
				"this collector assumes no role, so point it at credentials for %s instead",
			params.Account, account, params.Account)
	}

	w := &walkContext{
		clients: c.buildClients(cfg),
		account: account,
		region:  region,
		sink:    newSink(),
	}
	errs := runServiceWalks(ctx, w, allServiceWalks())
	w.runDerivedPasses()

	nodes, edges := w.sink.result()
	complete := framework.Complete()
	if len(errs) > 0 {
		complete = framework.Incomplete(incompleteReason(errs))
	}
	return framework.Result{Nodes: nodes, Edges: edges, Complete: complete}, nil
}

// resolveConfig resolves the credential chain for one region.
//
// THE FAILURE NAMES THE CHAIN. config.LoadDefaultConfig resolves lazily, so its
// own error rarely says what an operator should do; the wrapper below names every
// source it would have used, which is the difference between an actionable
// failure and a stack trace.
func (c *Collector) resolveConfig(ctx context.Context, region string) (aws.Config, error) {
	load := c.loadConfig
	if load == nil {
		load = func(ctx context.Context, region string) (aws.Config, error) {
			var opts []func(*config.LoadOptions) error
			if region != "" {
				opts = append(opts, config.WithRegion(region))
			}
			return config.LoadDefaultConfig(ctx, opts...)
		}
	}
	cfg, err := load(ctx, region)
	if err != nil {
		return aws.Config{}, fmt.Errorf(
			"could not resolve AWS credentials from the default chain "+
				"(AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY, AWS_PROFILE and the shared config and credentials "+
				"files under HOME, AWS_WEB_IDENTITY_TOKEN_FILE, the container credential endpoint, and the "+
				"instance metadata service): %w", err)
	}
	return cfg, nil
}

// resolveAccount learns which account the credential belongs to.
//
// IT IS NOT OPTIONAL AND IT IS NOT GUESSABLE. Every composed EC2-family ARN
// carries the account id, so a walk that skipped this would emit ids that
// resolve against nothing. It is also the first call that actually EXERCISES the
// credential, which is why an expired or absent one surfaces here with a message
// naming the chain rather than at some arbitrary service walk.
func (c *Collector) resolveAccount(ctx context.Context, cfg aws.Config) (string, error) {
	resolve := c.identity
	if resolve == nil {
		resolve = func(ctx context.Context, cfg aws.Config) (string, error) {
			out, err := sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
			if err != nil {
				return "", err
			}
			return deref(out.Account), nil
		}
	}
	account, err := resolve(ctx, cfg)
	if err != nil {
		return "", fmt.Errorf(
			"could not read the caller identity, so the credential the AWS default chain resolved is not "+
				"usable (chain tried: AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY, AWS_PROFILE and the shared "+
				"config and credentials files under HOME, AWS_WEB_IDENTITY_TOKEN_FILE, the container "+
				"credential endpoint, and the instance metadata service): %w", err)
	}
	if account == "" {
		return "", fmt.Errorf(
			"the caller identity carries no account id, so every composed resource ARN would be malformed")
	}
	return account, nil
}

func (c *Collector) buildClients(cfg aws.Config) *Clients {
	if c.newClients != nil {
		return c.newClients(cfg)
	}
	return NewClients(cfg)
}
