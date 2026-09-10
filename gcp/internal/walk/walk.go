// SPDX-License-Identifier: Apache-2.0

// Package walk is this collector's implementation of the framework's collector
// interface: it validates the call's parameters, fans out over the enumerations,
// runs the derivations over what they found, and hands back the result.
//
// WHAT IT IS NOT is an MCP server. The framework advertises the tool, validates
// the arguments against the schema it advertised, encodes the envelope and
// serves the transport; nothing about any of that is visible here. This package
// answers exactly one question: what is in this project, and how is it connected.
package walk

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/collect"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/resolve"
)

// defaultConcurrency bounds how many enumerations run at once.
//
// The work is a FAN-OUT OVER API CALLS rather than a data-volume problem: around
// forty enumerations, each one or more paginated round trips, producing a few
// hundred nodes. Run serially that is forty round-trip chains end to end; run
// unbounded it is forty simultaneous connections against one project's quota.
// Ten is the value this family of collectors already runs GCP at, and a
// different one needs a reason.
const defaultConcurrency = 10

// Params is the collect tool's own parameter object. The framework infers its
// schema, advertises it inside the contract's input schema, and validates a
// call's arguments against it before [Collector.Walk] is reached.
type Params struct {
	// Project is the project to enumerate. It is REQUIRED and it is the only
	// place a project comes from: this collector reads no environment variable
	// to resolve one, so a caller that omits it gets a refusal rather than a
	// walk of whatever project the host happened to be configured for.
	Project string `json:"project" jsonschema:"the Google Cloud project id to enumerate"`
}

// Enumerations builds the enumerations for one project and returns them with a
// function that releases whatever they hold. It is a field on [Collector] rather
// than a call inside Walk so a test can drive the whole walk over canned API
// responses, with no credential and no network.
type Enumerations func(ctx context.Context, projectID string) ([]collect.Subcollector, func(), error)

// Collector is the walk the framework serves.
type Collector struct {
	// Enumerations builds this walk's enumerations. Required.
	Enumerations Enumerations
	// Concurrency bounds the fan-out. Zero means [defaultConcurrency].
	Concurrency int
	// ToolName overrides the served tool's name. Empty means the framework's
	// default, which is what the config-file entry's own example names.
	ToolName string
}

// Tool names the MCP tool this collector serves.
func (c Collector) Tool() framework.ToolSpec {
	return framework.ToolSpec{
		Name:        c.ToolName,
		Description: "Enumerate a Google Cloud project's resources and their relationships.",
	}
}

// Walk enumerates one project.
//
// THE COMPLETENESS ASSERTION IS THE LOAD-BEARING RETURN. Three outcomes make
// this walk INCOMPLETE and the reason names which enumerations and which kind:
// one that FAILED, one the provider REFUSED, and one the provider answered only
// IN PART. A walk that asserted completeness after not seeing half a project
// would let the server treat everything it could not see as deleted — so a
// partial read is reported as partial, and the operator gets a graph that is
// short rather than one that is wrong.
// THE FOREIGN-CONTEXT BLOCK IS NAMED `_`, AND THAT IS A STATEMENT RATHER THAN
// AN OMISSION. The block carries what this collector's registration entry
// DECLARED it needs from the operator's other graphs, and this entry declares
// nothing — so the block is always the zero value and reading it could tell this
// walk nothing about the operator's graphs.
//
// WHY THIS COLLECTOR DECLARES NOTHING. The cross-graph edges the client's
// retired post-collect pass produced for this provider family were all
// Kubernetes-shaped at their SOURCE; this provider appears only as the resolved
// TARGET of one, and a target needs no context to be pointed at. Declaring a
// slice this walk would not read would move the operator's cloud or code nodes
// across a process boundary for nothing.
//
// A test pins the consequence: a non-empty block changes no byte of the output,
// so wiring the block in without also declaring it in the entry is a red rather
// than a graph that silently depends on data the entry never asked for.
func (c Collector) Walk(
	ctx context.Context, id string, params Params, _ framework.ForeignContext,
) (framework.Result, error) {
	project, err := validProject(params.Project)
	if err != nil {
		return framework.Result{}, err
	}
	if c.Enumerations == nil {
		return framework.Result{}, fmt.Errorf("gcp walk: no enumerations were configured")
	}

	subs, release, err := c.Enumerations(ctx, project)
	if err != nil {
		return framework.Result{}, err
	}
	if release != nil {
		defer release()
	}

	enumerated, incomplete, failures := c.fanOut(ctx, project, subs)

	// Every enumeration FAILING is not a partial read: it is a walk that learned
	// nothing, and reporting it as an incomplete success would land an empty
	// generation the server then reconciles against.
	//
	// Every enumeration being REFUSED or PARTIALLY READ is a different state and
	// is NOT an error. The provider answered every time; it answered "not with
	// this credential" or "not all of it". Both are real, reportable facts about
	// the project, and the walk reports them as incomplete rather than as broken.
	if len(failures) > 0 && len(enumerated.Resources) == 0 {
		return framework.Result{}, fmt.Errorf(
			"gcp walk: every enumeration of project %q failed: %s", project, strings.Join(failures, "; "))
	}

	// The derivations are NOT best-effort. A resolver that cannot read its input
	// has produced a graph missing edges nothing else will ever produce, and the
	// walk says so rather than logging it.
	derived, err := resolve.All(project, enumerated)
	if err != nil {
		return framework.Result{}, err
	}

	nodes, edges, err := gcpgraph.Build(derived)
	if err != nil {
		return framework.Result{}, err
	}

	_ = id // the collect id names the graph instance; the walk itself does not use it.
	return framework.Result{
		Nodes: nodes, Edges: edges,
		Complete: completeness(project, len(subs), incomplete, failures),
	}, nil
}

// The provider's own rules for a project id: 6 to 30 characters, lowercase
// letters, digits and hyphens, starting with a letter and not ending with one.
const (
	projectMinLen = 6
	projectMaxLen = 30
)

// validProject trims and validates the one value a caller supplies.
//
// IT IS VALIDATED HERE RATHER THAN PASSED THROUGH because this id is
// interpolated into every request path the walk builds. A malformed one passed
// through reaches the caller as forty per-service errors, or worse as an
// enumeration that matched nothing and looked empty. Bad input errors once,
// naming what is wrong and quoting what was sent.
func validProject(raw string) (string, error) {
	project := strings.TrimSpace(raw)
	if project == "" {
		return "", fmt.Errorf(
			"no project was named: pass the project id in the collect params as " +
				"{\"project\": \"<project-id>\"}. This collector reads no environment variable to " +
				"resolve one, so there is nothing to fall back to")
	}
	if len(project) < projectMinLen || len(project) > projectMaxLen {
		return "", fmt.Errorf(
			"the project id %q is %d characters; a project id is between %d and %d",
			project, len(project), projectMinLen, projectMaxLen)
	}
	if first := project[0]; first < 'a' || first > 'z' {
		return "", fmt.Errorf(
			"the project id %q does not start with a letter; a project id must start with a "+
				"lowercase letter", project)
	}
	if project[len(project)-1] == '-' {
		return "", fmt.Errorf(
			"the project id %q ends with a hyphen; a project id may not end with a hyphen", project)
	}
	for _, r := range project {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			return "", fmt.Errorf(
				"the project id %q contains %q; a project id is lowercase letters, digits and "+
					"hyphens only. If you meant a resource name such as projects/<id>, pass the id "+
					"alone", project, r)
		}
	}
	return project, nil
}

// completeness turns what the fan-out reported into the walk's own assertion.
//
// THREE OUTCOMES COUNT AGAINST COMPLETENESS AND THEY COUNT EQUALLY: an
// enumeration that FAILED, one the provider REFUSED, and one the provider
// answered only IN PART. In every case this walk did not see all of the project,
// and asserting otherwise is what lets the receiving server delete the part it
// did not see. They are kept apart in the REASON rather than in the verdict,
// because an operator fixes them differently: a refusal by granting a role or
// enabling an API, a partial read usually by waiting for the provider, a failure
// by investigating.
func completeness(project string, total int, incomplete, failures []string) framework.Completeness {
	if len(incomplete) == 0 && len(failures) == 0 {
		return framework.Complete()
	}
	var parts []string
	if len(failures) > 0 {
		parts = append(parts, fmt.Sprintf("%d failed: %s", len(failures), strings.Join(failures, "; ")))
	}
	if len(incomplete) > 0 {
		parts = append(parts, fmt.Sprintf(
			"%d did not return the whole scope, which usually means an API is not enabled, a role "+
				"is not granted, or a location was unreachable: %s",
			len(incomplete), strings.Join(incomplete, "; ")))
	}
	return framework.Incomplete(fmt.Sprintf(
		"%d of %d enumerations of project %q did not complete — %s",
		len(incomplete)+len(failures), total, project, strings.Join(parts, "; and ")))
}

// fanOut runs the enumerations with a bounded number in flight and merges what
// they found, keeping the enumerations that did not see their whole scope apart
// from the ones that FAILED. Everything that succeeded is returned either way,
// because a project where one API is unreachable still has the rest of its
// topology — and so is the partial page an incomplete enumeration came with.
func (c Collector) fanOut(
	ctx context.Context, project string, subs []collect.Subcollector,
) (result gcpgraph.Result, incomplete, failures []string) {
	limit := c.Concurrency
	if limit <= 0 {
		limit = defaultConcurrency
	}
	if limit > len(subs) {
		limit = len(subs)
	}

	var (
		mu     sync.Mutex
		merged gcpgraph.Result
		wg     sync.WaitGroup
	)
	work := make(chan collect.Subcollector)
	for range limit {
		wg.Go(func() {
			for sub := range work {
				got, err := sub.Run(ctx, project)
				mu.Lock()
				switch {
				case err == nil:
					merged.Add(got)
				case errors.Is(err, collect.ErrDenied), errors.Is(err, collect.ErrPartial):
					// Both keep whatever the enumeration had already read: an
					// aggregated list commonly refuses or loses ONE scope of many,
					// and discarding the rest would be the larger loss.
					merged.Add(got)
					incomplete = append(incomplete, fmt.Sprintf("%s: %v", sub.Name, err))
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
