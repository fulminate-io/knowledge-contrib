// SPDX-License-Identifier: Apache-2.0

// Package walk is this collector's implementation of the framework's collector
// interface: it validates the call's parameters, reads the repository list, fans
// out over the five enumerations that need it, joins the variable references the
// pipeline definitions carried, and hands back what they found together with this
// walk's completeness assertion.
//
// WHAT IT IS NOT is an MCP server. The framework advertises the tool, validates
// the arguments against the schema it advertised, encodes the envelope and
// serves the transport; nothing about any of that is visible here. This package
// answers exactly one question: what CI/CD does this workspace have, and how is
// it connected.
package walk

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbclient"
	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbgraph"
	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/collect"
)

// DefaultConcurrency bounds how many enumerations run at once when the call
// names no bound. It is EXPORTED so this module's own documentation gate can
// compare the README's operator-facing parameter table against the number the
// walk applies, rather than against a second literal that is free to drift from
// it — measured on this tree, changing it from 10 to 7 left all seven packages
// green and the table silently false, while both of its sibling constants red.
//
// The work is a FAN-OUT OVER API CALLS rather than a data-volume problem: five
// enumerations, four of which make one round trip per repository, against one
// workspace's rate limit. Run serially that is five chains of round trips end to
// end; run unbounded it is five simultaneous conversations with one host that
// counts them and answers 429 when it stops liking the number. Ten is the value
// this family of collectors already runs at, and with five enumerations it is
// not currently the binding constraint — it becomes one only if this list grows.
const DefaultConcurrency = 10

// parallelEnumerations is how many enumerations the second phase runs. It is
// named so the refusal message for an oversized bound can state it.
const parallelEnumerations = 5

// Collector is the walk the framework serves.
type Collector struct {
	// Client builds the provider client for one walk from the credentials the
	// environment supplied. It is a field rather than a call inside Walk so a
	// test can drive the whole walk against a real HTTP server standing in for
	// the provider, with no credential and no network. Nil means [bbclient.New],
	// which is what the shipped binary uses.
	Client ClientBuilder
	// Concurrency bounds the fan-out when the call names no bound. Zero means
	// [DefaultConcurrency].
	Concurrency int
	// ToolName overrides the served tool's name. Empty means the framework's
	// default, which is what the config-file entry's own example names.
	ToolName string
}

// ClientBuilder builds the API client one walk enumerates through.
type ClientBuilder func(username, appPassword string) *bbclient.Client

// Tool names the MCP tool this collector serves.
func (c Collector) Tool() framework.ToolSpec {
	return framework.ToolSpec{
		Name: c.ToolName,
		Description: "Enumerate a Bitbucket workspace's Pipelines CI/CD resources and their " +
			"relationships: repositories, pipeline definitions, recent runs, runners and their " +
			"labels, deployment environments and their approval gates, and pipeline variable " +
			"names.",
	}
}

// Walk enumerates one workspace.
//
// THE ID IS THE WORKSPACE AND ALSO THE GRAPH INSTANCE. There is no second place
// a workspace could come from: this collector takes no parameter naming one and
// reads no environment variable naming one, so a caller that sends a malformed
// id gets a refusal rather than a walk of something else. It also means this
// collector holds no second definition of the graph's identity — it builds no
// graph name at all, because for a registered family the graph name IS the
// collect id, resolved on the client's side before this binary is called.
//
// THE COMPLETENESS ASSERTION IS THE LOAD-BEARING RETURN. Anything this walk did
// not see makes it INCOMPLETE and the reason names which enumeration and which
// scope: a repository whose variables the credential may not read, an
// environment the provider did not return, a read the provider kept rate-limiting
// after the client's whole retry budget. A walk that asserted completeness after
// not seeing part of a workspace would let the server treat everything it could
// not see as deleted — so a partial read costs a mark on the collect rather than
// the operator's graph.
//
// THE FOREIGN-CONTEXT BLOCK IS NAMED `_`, AND THAT IS A STATEMENT RATHER THAN AN
// OMISSION. The block carries what this collector's registration entry DECLARED
// it needs from the operator's other graphs, and this entry declares nothing — so
// the block is always the zero value and reading it could tell this walk nothing.
//
// WHY THIS COLLECTOR DECLARES NOTHING. Every relationship it emits is one the
// provider itself states between two of its own resources. An edge from a
// pipeline to a cloud identity it deploys with would be a NEW assertion about the
// operator's other graphs rather than an inventory of this one, and inventing one
// is not this collector's to do. A test pins the consequence: a non-empty block
// changes no byte of the output.
func (c Collector) Walk(
	ctx context.Context, id string, params Params, _ framework.ForeignContext,
) (framework.Result, error) {
	workspace, err := validWorkspace(id)
	if err != nil {
		return framework.Result{}, err
	}
	limit, err := params.concurrency()
	if err != nil {
		return framework.Result{}, err
	}
	depth, err := historyDepth()
	if err != nil {
		return framework.Result{}, err
	}
	username, appPassword, err := credentials()
	if err != nil {
		return framework.Result{}, err
	}

	build := c.Client
	if build == nil {
		build = bbclient.New
	}
	client := build(username, appPassword)

	// PHASE 1, ALONE AND SYNCHRONOUS. Every enumeration below needs the
	// repository list and four of them need each repository's main branch, so
	// this read comes first.
	//
	// A FAILURE TO LIST IS A HARD ERROR: a walk that never learned what the
	// workspace contains has nothing to enumerate, and reporting it as an
	// incomplete success would land an empty generation the server then
	// reconciles against.
	//
	// AN INCOMPLETENESS WITHIN IT IS NOT THE SAME THING. The listing answered and
	// this enumeration could not carry ONE repository — refused, rate-limited, or
	// a detail it could not render. The rest of the workspace is still walkable,
	// so what was read is kept and the walk is marked, exactly as it is for the
	// five that follow. The two are told apart by the sentinel, which the listing
	// path does not wrap.
	repositories, repoErr := collect.Repositories(client).Run(ctx, workspace)
	repoIncomplete, fatal := phaseOneOutcome(workspace, repoErr)
	if fatal != nil {
		return framework.Result{}, fatal
	}

	// PHASE 2, FIVE IN PARALLEL over the shared list.
	subs := collect.AfterRepos(client, collect.RepoInfos(repositories), depth)
	// THE PHASE-ONE MARKS ARE SEEDED INTO THE FAN-OUT rather than appended after
	// it, and that is structural rather than stylistic: a parameter cannot be
	// silently dropped, and an append can. This path is defensive — the only
	// incompleteness phase one can report today is a detail it could not render —
	// so it is exactly the wiring a later edit would remove without noticing.
	enumerated, incomplete, failures := c.fanOut(ctx, limit, workspace, subs, repoIncomplete)
	enumerated.Add(repositories)

	// THE THREE JOINS, each of which needs the whole walk and none of which any
	// single enumeration can do. Every variable reference, every deployment and
	// every runs-on label a pipeline definition carried is resolved against the
	// nodes this same walk emitted, which is what makes each USES_SECRET,
	// DEPLOYS_TO and RUNS_IN edge name a node that is really there; and the
	// workspace root is minted when a workspace-scoped enumeration found
	// something in a workspace with no repositories.
	bbgraph.ResolveSecretRefs(&enumerated, workspace)
	bbgraph.ResolveStepTargets(&enumerated, workspace)
	bbgraph.EnsureWorkspaceRoot(&enumerated, workspace)

	nodes, edges, err := bbgraph.Build(enumerated)
	if err != nil {
		return framework.Result{}, err
	}
	return framework.Result{
		Nodes: nodes, Edges: edges,
		Complete: completeness(workspace, len(subs)+1, incomplete, failures),
	}, nil
}

// phaseOneOutcome decides what the repositories enumeration's error means: a
// mark on the walk, or the end of it.
//
// IT IS A FUNCTION RATHER THAN A SWITCH INSIDE Walk because it is the whole
// decision, and the arm that keeps the walk alive is not reachable from a
// recorded fixture: the only incompleteness that enumeration can report today is
// a detail it could not render, which no provider response can provoke. A guard
// nothing can exercise is a guard nobody has read, so the decision is testable
// on its own.
func phaseOneOutcome(workspace string, err error) (incomplete []string, fatal error) {
	switch {
	case err == nil:
		return nil, nil
	case errors.Is(err, collect.ErrDenied),
		errors.Is(err, collect.ErrRateLimited),
		errors.Is(err, collect.ErrPartial):
		// THE LISTING ANSWERED and this enumeration could not carry one
		// repository. The rest of the workspace is still walkable, so what was
		// read is kept and the walk is marked.
		return []string{err.Error()}, nil
	default:
		// THE LISTING ITSELF FAILED. Five enumerations start from it, so a walk
		// that could not read it never found out what the workspace contains, and
		// reporting that as an incomplete success would land an empty generation
		// the server then reconciles against.
		return nil, fmt.Errorf(
			"bitbucket-pipelines walk: the workspace %q could not be enumerated: %w",
			workspace, err)
	}
}

// completeness turns what the fan-out reported into the walk's own assertion.
//
// TWO OUTCOMES COUNT AGAINST COMPLETENESS AND THEY COUNT EQUALLY: an enumeration
// that FAILED outright, and one that came back having not seen part of what it
// asked for. In both cases this walk did not see all of the workspace, and
// asserting otherwise is what lets the receiving server delete the part it did
// not see. They are kept apart in the REASON rather than in the verdict, because
// an operator fixes them differently: a refusal by granting the app password a
// permission, a rate limit by waiting or collecting less history, a partial read
// or a failure by investigating.
func completeness(
	workspace string, total int, incomplete, failures []string,
) framework.Completeness {
	if len(incomplete) == 0 && len(failures) == 0 {
		return framework.Complete()
	}
	var parts []string
	if len(failures) > 0 {
		parts = append(parts,
			fmt.Sprintf("%d failed: %s", len(failures), strings.Join(failures, "; ")))
	}
	if len(incomplete) > 0 {
		parts = append(parts, fmt.Sprintf(
			"%d did not return their whole scope, which usually means the app password lacks a "+
				"permission, the provider rate-limited the read, or it did not answer for one "+
				"repository: %s",
			len(incomplete), strings.Join(incomplete, "; ")))
	}
	return framework.Incomplete(fmt.Sprintf(
		"%d of %d enumerations of the workspace %q did not complete — %s",
		len(incomplete)+len(failures), total, workspace, strings.Join(parts, "; and ")))
}

// fanOut runs the second-phase enumerations with a bounded number in flight and
// merges what they found, keeping the enumerations that did not see their whole
// scope apart from the ones that FAILED. Everything that succeeded is returned
// either way, because a workspace where one repository is unreadable still has
// the rest of its topology — and so does the partial answer an incomplete
// enumeration came back with.
func (c Collector) fanOut(
	ctx context.Context, limit int, workspace string, subs []collect.Subcollector,
	seed []string,
) (result bbgraph.Result, incomplete, failures []string) {
	incomplete = append(incomplete, seed...)

	if limit <= 0 {
		limit = c.Concurrency
	}
	if limit <= 0 {
		limit = DefaultConcurrency
	}
	if limit > len(subs) {
		limit = len(subs)
	}

	var (
		mu     sync.Mutex
		merged bbgraph.Result
		wg     sync.WaitGroup
	)
	work := make(chan collect.Subcollector)
	for range limit {
		wg.Go(func() {
			for sub := range work {
				got, err := sub.Run(ctx, workspace)
				mu.Lock()
				switch {
				case err == nil:
					merged.Add(got)
				case errors.Is(err, collect.ErrDenied),
					errors.Is(err, collect.ErrRateLimited),
					errors.Is(err, collect.ErrPartial):
					// All three keep whatever the enumeration had already read: a
					// workspace commonly refuses ONE repository of many, and
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
