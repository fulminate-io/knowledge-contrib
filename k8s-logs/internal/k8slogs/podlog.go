// SPDX-License-Identifier: Apache-2.0

package k8slogs

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/fulminate-io/knowledge-contrib/k8s-logs/internal/logpipe"
)

// podlog.go — reading pod and container logs, and the four API facts that
// decide how.
//
//  1. THE POD LIST IS NAMESPACE-SCOPED. Pods(namespace) means N namespaces is N
//     list calls, not one filtered call — and the EMPTY namespace is not an
//     empty result, it is EVERY namespace in the cluster. An unvalidated empty
//     namespace parameter reads the whole cluster, so this collector refuses it
//     rather than passing it through.
//
//  2. INIT CONTAINERS ARE LOG SOURCES. A pod's log sources are its regular
//     containers AND its init containers, which on a service mesh are
//     long-running sidecars carrying most of the pod's output. Enumerating only
//     the regular containers silently omits them.
//
//  3. THE TIME RANGE IS ASYMMETRIC AT THE API. PodLogOptions carries a START
//     bound (SinceTime, SinceSeconds) and NO end bound at all. An end must be
//     applied client-side against each line's own timestamp, which is why the
//     read always asks for Timestamps.
//
//  4. THE READ IS A STREAM, NOT A PAGE. GetLogs returns a request whose Stream
//     yields an io.ReadCloser, so there is no continuation token to loop on and
//     the caller bounds itself with TailLines or LimitBytes when it wants a
//     bound at all.

// Source names one container's log within one pod.
type Source struct {
	Namespace string
	Pod       string
	Container string
	// Init records that this container is an init container. It is carried for
	// diagnostics only and is deliberately NOT a label: whether a container is
	// an init container is a property of the pod spec, and folding it into
	// stream identity would split a stream if a pod were respecified.
	Init bool
}

// StreamOpener opens one container's log stream. It is the THIRD injectable
// dependency of this module, beside the client builder and the in-cluster
// loader, and it exists for the same reason they do: the real call reaches an
// apiserver, so a test that needs to observe what this package does with the
// returned stream — that it is closed, that a mid-body failure is not
// swallowed — has to be able to supply one.
type StreamOpener func(ctx context.Context, client kubernetes.Interface, src Source, opts *corev1.PodLogOptions) (io.ReadCloser, error)

// OpenPodLogStream is the real opener: the apiserver's own log subresource.
func OpenPodLogStream(ctx context.Context, client kubernetes.Interface, src Source, opts *corev1.PodLogOptions) (io.ReadCloser, error) {
	return client.CoreV1().Pods(src.Namespace).GetLogs(src.Pod, opts).Stream(ctx)
}

// ReadOptions bounds one container read and names the seam it opens through.
type ReadOptions struct {
	// Since is the start bound, applied SERVER-side. A zero value means the
	// container's whole retained log.
	Since time.Time
	// Until is the end bound, applied CLIENT-side against each line's own
	// timestamp because the API has no end bound. A zero value means no end.
	Until time.Time
	// TailLines and LimitBytes are the operator's optional self-bounds. NIL
	// means unbounded and there is no ceiling on either: this collector imposes
	// no size limit of its own and refuses no bound for being large. A bound
	// that was MEASURED to engage is reported rather than hidden.
	TailLines  *int64
	LimitBytes *int64
	// Stream selects stdout, stderr, or both. Empty means both.
	Stream string
	// Open is the stream seam. Nil means [OpenPodLogStream], the real one.
	Open StreamOpener
}

// The stream selectors an operator may name.
const (
	StreamAll    = "all"
	StreamStdout = "stdout"
	StreamStderr = "stderr"
)

// ReadResult is one container read's outcome.
type ReadResult struct {
	Entries []logpipe.Entry
	// Truncated records that a bound the operator set cut the read short, which
	// makes the whole walk incomplete.
	Truncated bool
	// SkippedLines counts lines dropped for want of a parseable timestamp
	// prefix. It is reported rather than swallowed: a nonzero count with a
	// zero entry count means the timestamps were not requested or not returned,
	// which is a different failure from an empty container.
	SkippedLines int
}

// ListPods lists the pods of one namespace, narrowed by an optional label
// selector.
//
// AN EMPTY NAMESPACE IS REFUSED. At the API an empty namespace means every
// namespace, so passing an unvalidated one through turns a narrow collect into
// a cluster-wide read with nothing in the request saying so.
func ListPods(ctx context.Context, client kubernetes.Interface, namespace, labelSelector string) ([]corev1.Pod, error) {
	if namespace == "" {
		return nil, fmt.Errorf(
			"k8s-logs: an empty namespace reaches the Kubernetes API as EVERY namespace; name the namespaces to read")
	}
	list, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return nil, fmt.Errorf("k8s-logs: listing pods in namespace %q: %w", namespace, err)
	}
	return list.Items, nil
}

// SourcesForPod enumerates a pod's log sources: its init containers and its
// regular containers, in that order, narrowed to the named containers when the
// caller named any.
//
// The order is the order the containers RAN, which is what a human reading the
// list expects.
func SourcesForPod(pod *corev1.Pod, containers []string) []Source {
	wanted := make(map[string]struct{}, len(containers))
	for _, c := range containers {
		wanted[c] = struct{}{}
	}
	keep := func(name string) bool {
		if len(wanted) == 0 {
			return true
		}
		_, ok := wanted[name]
		return ok
	}

	out := make([]Source, 0, len(pod.Spec.InitContainers)+len(pod.Spec.Containers))
	for _, c := range pod.Spec.InitContainers {
		if keep(c.Name) {
			out = append(out, Source{Namespace: pod.Namespace, Pod: pod.Name, Container: c.Name, Init: true})
		}
	}
	for _, c := range pod.Spec.Containers {
		if keep(c.Name) {
			out = append(out, Source{Namespace: pod.Namespace, Pod: pod.Name, Container: c.Name})
		}
	}
	return out
}

// THERE IS NO MAXIMUM LINE LENGTH, and its absence is deliberate rather than an
// omission. A container writing a multi-megabyte line is ordinary — a serialized
// payload, a whole stack trace on one line — and any fixed ceiling is a size cap
// on traffic that never reaches a language model: this is one process handing
// bytes to another. An earlier revision of this file buffered lines through a
// scanner with an eight-megabyte bound, which refused a longer line with an
// error and failed the whole container's read. The reader below grows to
// whatever a line needs.
//
// ReadContainerLog reads one container's log and turns it into entries.
//
// Timestamps are ALWAYS requested, whatever the caller asked for, because the
// client-side end bound and every entry's own timestamp are parsed from the
// prefix the flag adds. The prefix is then stripped from the message, so a
// template is derived from the application's own text rather than from a
// timestamp that would be wildcarded away anyway.
func ReadContainerLog(
	ctx context.Context,
	client kubernetes.Interface,
	src Source,
	labels map[string]string,
	opts ReadOptions,
) (ReadResult, error) {
	podOpts := &corev1.PodLogOptions{
		Container:  src.Container,
		Timestamps: true,
		TailLines:  opts.TailLines,
		LimitBytes: opts.LimitBytes,
	}
	if !opts.Since.IsZero() {
		since := metav1.NewTime(opts.Since)
		podOpts.SinceTime = &since
	}
	if stream := streamSelector(opts.Stream); stream != nil {
		podOpts.Stream = stream
	}

	open := opts.Open
	if open == nil {
		open = OpenPodLogStream
	}
	body, err := open(ctx, client, src, podOpts)
	if err != nil {
		// A CANCELLATION THAT SURFACES THROUGH THE OPEN IS STILL A CANCELLATION.
		// client-go's rate limiter reports it in its own words ("client rate
		// limiter Wait returned an error: context canceled") before the stream
		// is dialed, and which call sees the cancelled context first is a race
		// the caller cannot control; the failure names the cancellation either
		// way, so an abandoned collect is never read as a container that refused.
		if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ReadResult{}, fmt.Errorf(
				"k8s-logs: the read of container %s in pod %s/%s was cancelled: %w",
				src.Container, src.Namespace, src.Pod, err)
		}
		return ReadResult{}, fmt.Errorf(
			"k8s-logs: reading the log of container %s in pod %s/%s: %w",
			src.Container, src.Namespace, src.Pod, err)
	}
	// The stream is an open connection to the apiserver, one per container per
	// collect and the highest-multiplicity resource this collector holds.
	defer func() { _ = body.Close() }()

	return scanLog(ctx, body, src, labels, opts)
}

// scanLog turns one open log stream into entries. It is separate from the
// request so the parsing is testable without a client.
func scanLog(
	ctx context.Context,
	body io.Reader,
	src Source,
	labels map[string]string,
	opts ReadOptions,
) (ReadResult, error) {
	var result ReadResult
	reader := bufio.NewReader(body)

	lines, bytesRead := 0, int64(0)
	for {
		// Checked each iteration: a busy container's log is unbounded, and a
		// cancelled collect that keeps reading burns the apiserver's time on a
		// result nobody will receive.
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf(
				"k8s-logs: the read of container %s in pod %s/%s was cancelled: %w",
				src.Container, src.Namespace, src.Pod, err)
		}
		line, readErr := reader.ReadString('\n')
		bytesRead += int64(len(line))
		if line != "" {
			lines++
			result.appendLine(strings.TrimRight(line, "\r\n"), labels, opts)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return result, fmt.Errorf(
				"k8s-logs: the log stream of container %s in pod %s/%s failed after %d lines: %w",
				src.Container, src.Namespace, src.Pod, lines, readErr)
		}
	}
	result.Truncated = boundEngaged(opts, lines, bytesRead)
	return result, nil
}

// appendLine turns one already-newline-stripped line into an entry, or records
// why it did not become one.
func (r *ReadResult) appendLine(line string, labels map[string]string, opts ReadOptions) {
	ts, msg, ok := splitTimestamp(line)
	if !ok {
		r.SkippedLines++
		return
	}
	if !opts.Until.IsZero() && ts.After(opts.Until) {
		// Past the requested end. The API applies no end bound, so this is where
		// the range actually closes.
		return
	}
	if !opts.Since.IsZero() && ts.Before(opts.Since) {
		return
	}
	r.Entries = append(r.Entries, logpipe.Entry{
		Timestamp: ts,
		Severity:  logpipe.DeriveSeverity(msg),
		Message:   msg,
		Labels:    labels,
	})
}

// boundEngaged reports whether a bound the operator set actually cut this read
// short. BOTH ARMS ARE MEASURED against what the read returned; neither infers
// truncation from the presence of a bound.
//
// The byte arm is the one that used to be inferred: an earlier revision
// reported every read carrying a byte bound as truncated, whether or not the
// bound was reached, so a bound set generously made every collect incomplete
// forever. It now compares the bytes the reader actually delivered against the
// bound. The apiserver documents returning slightly more or slightly less than
// a byte limit, which is why the comparison is "reached it" rather than
// "equalled it".
//
// The line arm is a measurement too: the lines were counted, and a read that
// returned as many as it asked for is a read whose bound was spent. Whether
// more was behind it is unknowable, and the conservative answer is the correct
// one — over-reporting incompleteness costs a deletion pass, while
// under-reporting it lets the server treat everything the walk did not carry
// as gone.
func boundEngaged(opts ReadOptions, lines int, bytesRead int64) bool {
	if opts.TailLines != nil && *opts.TailLines > 0 && int64(lines) >= *opts.TailLines {
		return true
	}
	return opts.LimitBytes != nil && *opts.LimitBytes > 0 && bytesRead >= *opts.LimitBytes
}

// streamSelector maps the collect parameter onto the API's own field, which is
// a pointer to one of three capitalized values. "all" and the empty value both
// leave it nil, which is the API's own default and means both streams
// interleaved.
func streamSelector(name string) *string {
	switch name {
	case StreamStdout:
		s := corev1.LogStreamStdout
		return &s
	case StreamStderr:
		s := corev1.LogStreamStderr
		return &s
	default:
		return nil
	}
}

// ValidateStream refuses a stream selector this collector does not know, and
// refuses the ONE COMBINATION THE API ITSELF FORBIDS: a tail-line bound
// alongside a single-stream selection.
//
// The apiserver's own documentation states that constraint, and the failure it
// produces is a request rejection mid-walk rather than anything a reader could
// diagnose from the result — so it is caught here, where the message can name
// both parameters the operator set.
func ValidateStream(name string, tailLines *int64) error {
	switch name {
	case "", StreamAll, StreamStdout, StreamStderr:
	default:
		return fmt.Errorf(
			"k8s-logs: %q is not a container stream; name one of %s, %s or %s",
			name, StreamAll, StreamStdout, StreamStderr)
	}
	if tailLines != nil && (name == StreamStdout || name == StreamStderr) {
		return fmt.Errorf(
			"k8s-logs: the Kubernetes API accepts a tail_lines bound only with the combined stream, "+
				"so tail_lines and stream=%q cannot be asked for together; drop one of them", name)
	}
	return nil
}

// splitTimestamp splits the RFC3339Nano prefix the API adds under Timestamps
// from the line's own text.
//
// A line with NO parseable prefix is not an entry. That is not a lost line
// swallowed: it is counted and reported, because a whole read of unparseable
// lines means the prefix was not requested or not returned, and silently
// producing zero entries would read as an empty container.
func splitTimestamp(line string) (time.Time, string, bool) {
	prefix, rest, found := strings.Cut(line, " ")
	if !found {
		// A timestamp with no message after it is a line the API should not
		// produce; treat it as unparseable rather than as an empty entry.
		return time.Time{}, "", false
	}
	ts, err := time.Parse(time.RFC3339Nano, prefix)
	if err != nil {
		return time.Time{}, "", false
	}
	return ts.UTC(), rest, true
}

// sortStrings sorts in place. It is a named helper so the two call sites that
// need a stable order say why they need one at their own site.
func sortStrings(s []string) { sort.Strings(s) }
