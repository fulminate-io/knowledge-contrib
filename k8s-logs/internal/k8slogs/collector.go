// SPDX-License-Identifier: Apache-2.0

package k8slogs

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"k8s.io/client-go/kubernetes"

	"github.com/fulminate-io/knowledge-contrib/framework"
	"github.com/fulminate-io/knowledge-contrib/k8s-logs/internal/logpipe"
)

// collector.go — the walk: pods to containers to lines to a graph.
//
// WHAT walk_complete MEANS HERE, because no schema can say it and the honest
// answer is not "the whole log". A pod log has no end: the container is still
// writing. So completeness is bounded by the collect's own PARAMETERS — true
// when every container the parameters named was read to the end of the
// requested window with no bound engaging, and FALSE the moment a bound cut a
// stream short or a pod or container the selector named could not be read.
//
// THE COST IS ASYMMETRIC, which is why the arms lean the way they do. The
// server treats a COMPLETE collect's absent rows as deleted. So an
// optimistically-true assertion after a partial walk reports a silently emptied
// graph as a successful collect, while a conservatively-false one costs a
// deletion pass and nothing else.

// GraphFamily is the registration name this collector is installed under, which
// is also the graph family its results land in.
//
// IT CANNOT BE `k8s`. That name belongs to the Kubernetes CLOUD collector,
// which models the cluster's objects rather than its logs, and a registration
// name IS the family — so two collectors under one name would write two
// unrelated vocabularies into one graph.
const GraphFamily = "k8s-logs"

// ToolName is the MCP tool this collector serves.
const ToolName = "collect_k8s_logs"

// ClientBuilder returns the Kubernetes client for a named context. It is a
// field on the collector rather than a direct call so a test drives the walk
// against a fake clientset without a cluster, a kubeconfig or a network.
type ClientBuilder func(contextName string) (kubernetes.Interface, error)

// Collector reads pod and container logs and returns the log graph.
type Collector struct {
	// NewClient builds the Kubernetes client. Nil means the standard
	// kubeconfig-then-in-cluster resolution.
	NewClient ClientBuilder
}

// Tool names the MCP tool this collector serves.
func (c *Collector) Tool() framework.ToolSpec {
	return framework.ToolSpec{
		Name: ToolName,
		Description: "Collect Kubernetes pod and container logs into a log graph of streams, " +
			"Drain-clustered templates and compressed chunks. Reads through the standard kubeconfig " +
			"or in-cluster credentials; the caller passes none.",
	}
}

// Walk reads every named namespace's pods, every selected container of each,
// and returns the graph those lines build.
func (c *Collector) Walk(
	ctx context.Context,
	id string,
	params Params,
	foreign framework.ForeignContext,
) (framework.Result, error) {
	resolved, err := params.Validate()
	if err != nil {
		return framework.Result{}, err
	}

	client, err := c.client(resolved.Context)
	if err != nil {
		return framework.Result{}, err
	}

	project, cluster := ParseKubeContext(resolved.Context)
	entries, incomplete, err := c.readAll(ctx, client, resolved, project, cluster)
	if err != nil {
		return framework.Result{}, err
	}

	result, err := c.build(entries, resolved, CloudContextFrom(foreign))
	if err != nil {
		return framework.Result{}, err
	}

	return framework.Result{
		Nodes:    result.Nodes,
		Edges:    result.Edges,
		Complete: completeness(incomplete),
	}, nil
}

// client builds the Kubernetes client, through the injected builder when there
// is one.
func (c *Collector) client(contextName string) (kubernetes.Interface, error) {
	if c.NewClient != nil {
		return c.NewClient(contextName)
	}
	cfg, err := Resolver{}.Config(contextName)
	if err != nil {
		return nil, err
	}
	return Clientset(cfg)
}

// readAll walks every namespace, pod and container the parameters name and
// returns the entries with the reasons the walk was incomplete.
//
// A CONTAINER THAT CANNOT BE READ DOES NOT FAIL THE COLLECT, and it does not
// pass silently either: it is recorded as a reason the walk is incomplete, so
// the result carries what WAS read and the server declines to treat the rest as
// gone. A NAMESPACE that cannot be LISTED is the same. The one thing that does
// fail the collect is a failure that makes the whole walk meaningless — no
// client, or a cancelled context.
func (c *Collector) readAll(
	ctx context.Context,
	client kubernetes.Interface,
	params Resolved,
	project, cluster string,
) ([]logpipe.Entry, []string, error) {
	var entries []logpipe.Entry
	var incomplete []string

	for _, ns := range params.Namespaces {
		// NO SEPARATE TOP-OF-LOOP CANCELLATION CHECK, and its absence is a
		// finding rather than an omission. One stood here and nothing could
		// discriminate it: the first thing this loop does is list, and a list on
		// a cancelled context fails IMMEDIATELY and LOCALLY — the client refuses
		// it before it dials — so the check below catches every cancellation
		// that reaches a namespace boundary, and it catches one that lands
		// mid-request too. A guard whose removal no test can notice is a
		// liability, so it was removed rather than left with a test that only
		// appeared to observe it.
		pods, err := ListPods(ctx, client, ns, params.LabelSelector)
		if err != nil {
			// A CANCELLED LIST IS NOT A REFUSED NAMESPACE. The cancellation may
			// land on the request rather than between iterations, and filing it
			// as incompleteness would report a collect the CALLER abandoned as a
			// partial view of the cluster. Checked here rather than at the top of
			// the loop, because a list on a cancelled context fails locally
			// before it dials and this branch is where that failure arrives.
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, nil, fmt.Errorf(
					"k8s-logs: the collect was cancelled while listing namespace %q: %w", ns, ctxErr)
			}
			incomplete = append(incomplete, fmt.Sprintf("namespace %s could not be listed: %v", ns, err))
			continue
		}
		for i := range pods {
			pod := &pods[i]
			for _, src := range SourcesForPod(pod, params.Containers) {
				read, err := ReadContainerLog(ctx, client, src, StreamLabels(
					src.Namespace, src.Pod, src.Container, cluster, project,
				), ReadOptions{
					Since:      params.Since,
					Until:      params.Until,
					TailLines:  params.TailLines,
					LimitBytes: params.LimitBytes,
					Stream:     params.Stream,
				})
				if err != nil {
					if ctxErr := ctx.Err(); ctxErr != nil {
						return nil, nil, err
					}
					incomplete = append(incomplete, fmt.Sprintf(
						"container %s of pod %s/%s could not be read: %v", src.Container, src.Namespace, src.Pod, err))
					continue
				}
				if read.Truncated {
					incomplete = append(incomplete, fmt.Sprintf(
						"container %s of pod %s/%s was cut short by a bound this collect set",
						src.Container, src.Namespace, src.Pod))
				}
				if read.SkippedLines > 0 {
					incomplete = append(incomplete, fmt.Sprintf(
						"container %s of pod %s/%s returned %d line(s) with no parseable timestamp",
						src.Container, src.Namespace, src.Pod, read.SkippedLines))
				}
				entries = append(entries, read.Entries...)
			}
		}
	}
	return entries, incomplete, nil
}

// build runs the pipeline over the entries with whatever cloud context this
// collect was given.
func (c *Collector) build(entries []logpipe.Entry, params Resolved, cloud CloudContext) (logpipe.Result, error) {
	// The streams have to exist before the cloud slice can be matched against
	// them, and the pipeline builds the streams — so the match runs on a
	// preliminary stream set and the result is handed back in. Building the
	// streams twice is cheap; guessing which namespaces the walk found is not.
	streams, _ := logpipe.BuildStreams(entries, 0)

	opts := logpipe.Options{ChunkWindow: params.ChunkWindow}
	if !cloud.IsEmpty() {
		opts.Resolutions = cloud.Resolutions(streams)
		opts.ProxyMap = cloud.ProxyMap(streams)
		// UNTYPED NIL WHEN THERE IS NOTHING TO RESOLVE FROM. Assigning a typed
		// nil pointer into the two interfaces would make them non-nil, and the
		// detector would call through them instead of leaving every candidate
		// unconfirmed.
		if resolver := cloud.Correlation(streams); resolver != nil {
			opts.Resolver = resolver
			opts.Oracle = resolver
		}
	}
	return logpipe.Build(entries, opts)
}

// completeness turns the collected reasons into the walk's assertion. The
// reasons are sorted and joined so two runs that hit the same problems report
// them identically.
func completeness(reasons []string) framework.Completeness {
	if len(reasons) == 0 {
		return framework.Complete()
	}
	sort.Strings(reasons)
	return framework.Incomplete(strings.Join(reasons, "; "))
}
