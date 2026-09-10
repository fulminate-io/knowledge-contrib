// SPDX-License-Identifier: Apache-2.0

// Package walk is this collector's implementation of the framework's collector
// interface: it validates the call's parameters, fans out over the enumerations,
// and hands back what they found together with this walk's completeness
// assertion.
//
// WHAT IT IS NOT is an MCP server. The framework advertises the tool, validates
// the arguments against the schema it advertised, encodes the envelope and
// serves the transport; nothing about any of that is visible here. This package
// answers exactly one question: what CI/CD does this organization have, and how
// is it connected.
package walk

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/collect"
	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/ghgraph"
)

// defaultConcurrency bounds how many enumerations run at once.
//
// The work is a FAN-OUT OVER API CALLS rather than a data-volume problem: seven
// enumerations, six of which make one round trip per repository, against one
// organization's rate limit. Run serially that is seven chains of round trips end
// to end; run unbounded it is seven simultaneous conversations with one host that
// counts them. Ten is the value this family of collectors already runs at, and a
// different one needs a reason — with seven enumerations it is not currently the
// binding constraint, and it becomes one only if this list grows.
const defaultConcurrency = 10

// Collector is the walk the framework serves.
type Collector struct {
	// API builds the provider clients for one walk and returns a function that
	// releases whatever they hold. It is a field rather than a call inside Walk
	// so a test can drive the whole walk over recorded responses, with no
	// credential and no network.
	API APIBuilder
	// Concurrency bounds the fan-out. Zero means [defaultConcurrency].
	Concurrency int
	// ToolName overrides the served tool's name. Empty means the framework's
	// default, which is what the config-file entry's own example names.
	ToolName string
}

// APIBuilder builds the provider clients one walk enumerates through.
type APIBuilder func(ctx context.Context) (collect.API, func(), error)

// Tool names the MCP tool this collector serves.
func (c Collector) Tool() framework.ToolSpec {
	return framework.ToolSpec{
		Name: c.ToolName,
		Description: "Enumerate a GitHub organization's Actions CI/CD resources and their " +
			"relationships: repositories, workflows, recent runs, self-hosted runners, " +
			"environments, recent deployments and secret names.",
	}
}

// Walk enumerates one organization.
//
// THE ID IS THE ORGANIZATION AND ALSO THE GRAPH INSTANCE. There is no second
// place an organization could come from: this collector reads no environment
// variable naming one, so a caller that sends a malformed id gets a refusal
// rather than a walk of something else.
//
// THE COMPLETENESS ASSERTION IS THE LOAD-BEARING RETURN. Anything this walk did
// not see makes it INCOMPLETE and the reason names which enumeration and which
// scope: a repository whose secrets the token may not read, an environment the
// provider did not return, a workflow definition that could not be fetched. A
// walk that asserted completeness after not seeing part of an organization would
// let the server treat everything it could not see as deleted — so a partial read
// costs a mark on the collect rather than the operator's graph.
//
// THE FOREIGN-CONTEXT BLOCK IS NAMED `_`, AND THAT IS A STATEMENT RATHER THAN AN
// OMISSION. The block carries what this collector's registration entry DECLARED
// it needs from the operator's other graphs, and this entry declares nothing — so
// the block is always the zero value and reading it could tell this walk nothing.
//
// WHY THIS COLLECTOR DECLARES NOTHING. Every relationship it emits is one the
// provider itself states between two of its own resources. An edge from a
// workflow to the source repository it builds, or from a deployment to a cloud
// identity, would be a NEW assertion about the operator's other graphs rather
// than an inventory of this one, and inventing one is not this collector's to do.
// A test pins the consequence: a non-empty block changes no byte of the output.
func (c Collector) Walk(
	ctx context.Context, id string, params Params, _ framework.ForeignContext,
) (framework.Result, error) {
	org, err := validOrganization(id)
	if err != nil {
		return framework.Result{}, err
	}
	caps, err := params.caps()
	if err != nil {
		return framework.Result{}, err
	}
	if c.API == nil {
		return framework.Result{}, fmt.Errorf("github-actions walk: no API was configured")
	}

	api, release, err := c.API(ctx)
	if err != nil {
		return framework.Result{}, err
	}
	if release != nil {
		defer release()
	}

	subs := collect.All(api, caps)
	enumerated, incomplete, failures := c.fanOut(ctx, org, subs)

	// EVERY ENUMERATION FAILING IS NOT A PARTIAL READ: it is a walk that learned
	// nothing, and reporting it as an incomplete success would land an empty
	// generation the server then reconciles against. Every enumeration being
	// REFUSED or partially read is a different state and is NOT an error — the
	// provider answered every time, and what it answered is a real fact about
	// this organization and this credential.
	if len(failures) > 0 && len(enumerated.Resources) == 0 {
		return framework.Result{}, fmt.Errorf(
			"github-actions walk: every enumeration of the organization %q failed: %s",
			org, strings.Join(failures, "; "))
	}

	nodes, edges, err := ghgraph.Build(enumerated)
	if err != nil {
		return framework.Result{}, err
	}
	return framework.Result{
		Nodes: nodes, Edges: edges,
		Complete: completeness(org, len(subs), incomplete, failures),
	}, nil
}

// completeness turns what the fan-out reported into the walk's own assertion.
//
// TWO OUTCOMES COUNT AGAINST COMPLETENESS AND THEY COUNT EQUALLY: an enumeration
// that FAILED outright, and one that came back having not seen part of what it
// asked for. In both cases this walk did not see all of the organization, and
// asserting otherwise is what lets the receiving server delete the part it did not
// see. They are kept apart in the REASON rather than in the verdict, because an
// operator fixes them differently: a refusal by granting a scope or a membership,
// a failure by investigating.
func completeness(org string, total int, incomplete, failures []string) framework.Completeness {
	if len(incomplete) == 0 && len(failures) == 0 {
		return framework.Complete()
	}
	var parts []string
	if len(failures) > 0 {
		parts = append(parts, fmt.Sprintf("%d failed: %s", len(failures), strings.Join(failures, "; ")))
	}
	if len(incomplete) > 0 {
		parts = append(parts, fmt.Sprintf(
			"%d did not return their whole scope, which usually means the token lacks a scope or a "+
				"membership, or the provider did not answer for one repository: %s",
			len(incomplete), strings.Join(incomplete, "; ")))
	}
	return framework.Incomplete(fmt.Sprintf(
		"%d of %d enumerations of the organization %q did not complete — %s",
		len(incomplete)+len(failures), total, org, strings.Join(parts, "; and ")))
}

// fanOut runs the enumerations with a bounded number in flight and merges what
// they found, keeping the enumerations that did not see their whole scope apart
// from the ones that FAILED. Everything that succeeded is returned either way,
// because an organization where one repository is unreadable still has the rest
// of its topology — and so does the partial answer an incomplete enumeration
// came back with.
func (c Collector) fanOut(
	ctx context.Context, org string, subs []collect.Subcollector,
) (result ghgraph.Result, incomplete, failures []string) {
	limit := c.Concurrency
	if limit <= 0 {
		limit = defaultConcurrency
	}
	if limit > len(subs) {
		limit = len(subs)
	}

	var (
		mu     sync.Mutex
		merged ghgraph.Result
		wg     sync.WaitGroup
	)
	work := make(chan collect.Subcollector)
	for range limit {
		wg.Go(func() {
			for sub := range work {
				got, err := sub.Run(ctx, org)
				mu.Lock()
				switch {
				case err == nil:
					merged.Add(got)
				case errors.Is(err, collect.ErrDenied), errors.Is(err, collect.ErrPartial):
					// Both keep whatever the enumeration had already read: an
					// organization commonly refuses ONE repository of many, and
					// discarding the rest would be the larger loss.
					merged.Add(got)
					incomplete = append(incomplete, err.Error())
				default:
					failures = append(failures, fmt.Sprintf("%s: %v", sub.Name, err))
				}
				mu.Unlock()
			}
		})
	}
	for _, sub := range subs {
		select {
		case work <- sub:
		case <-ctx.Done():
			// A cancelled context stops handing out work. The workers drain and
			// exit, so the wait below returns rather than leaking them, and the
			// enumerations that did run are still reported.
			close(work)
			wg.Wait()
			mu.Lock()
			failures = append(failures, fmt.Sprintf("walk cancelled: %v", ctx.Err()))
			out, i, f := merged, incomplete, failures
			mu.Unlock()
			return out, i, f
		}
	}
	close(work)
	wg.Wait()
	return merged, incomplete, failures
}
