// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
)

// drain.go — the Drain clustering engine.
//
// THE TEMPLATE ID IS NOT STABLE UNDER A CHANGED ENTRY SET, and that is a
// property to assert rather than a defect to route around. updateCluster
// rewrites both the pattern and the id on every merge, so an entry that
// broadens an existing cluster changes that cluster's id and, through the chunk
// key, every chunk id under it. A carry-forward diff therefore sees a delete
// plus a create for templates and chunks where it sees an update for streams
// and labels. drain_test.go asserts the move; a test asserting stability here
// would be asserting the wrong thing.

// DrainConfig tunes the clustering.
type DrainConfig struct {
	// SimThreshold is the minimum similarity to merge into a cluster. The
	// comparison is strictly greater, so a threshold of 0.4 admits 0.5 and
	// refuses 0.4.
	SimThreshold float64
	// MaxDepth is the depth of the prefix parse tree.
	MaxDepth int
	// MaxChildren is the branching cap per tree node; past it the branch
	// collapses onto the wildcard child.
	MaxChildren int
	// MaxClusters is the hard cap on distinct clusters; past it a message
	// merges into its best global match instead of creating a cluster.
	MaxClusters int
}

// DefaultDrainConfig is the tuning the emitted graph is defined against.
func DefaultDrainConfig() DrainConfig {
	return DrainConfig{SimThreshold: 0.4, MaxDepth: 4, MaxChildren: 100, MaxClusters: 200}
}

// DrainEngine clusters messages into templates:
//
//  1. preprocess and tokenize,
//  2. walk a fixed-depth prefix tree, branching on token count then on prefix
//     tokens,
//  3. score similarity against the leaf's clusters,
//  4. merge by replacing differing tokens with the wildcard.
type DrainEngine struct {
	root     *drainNode
	clusters []*drainCluster
	config   DrainConfig
}

type drainNode struct {
	children map[string]*drainNode
	clusters []*drainCluster
}

// drainCluster pairs the internal token state with the template a caller sees.
type drainCluster struct {
	tokens   []string
	template *Template
}

// maxExampleVars caps the example variable rows retained per template.
const maxExampleVars = 3

// NewDrainEngine builds an engine with the given tuning.
func NewDrainEngine(cfg DrainConfig) *DrainEngine {
	return &DrainEngine{root: &drainNode{children: make(map[string]*drainNode)}, config: cfg}
}

// AddMessage clusters one entry and returns the template it landed in, or nil
// when the message tokenizes to nothing.
//
// The returned POINTER is what a caller must retain: the template's id moves as
// later messages broaden the cluster, so an id read at this moment is stale by
// the end of the walk.
func (d *DrainEngine) AddMessage(entry Entry) *Template {
	tokens := Tokenize(PreProcess(entry.Message))
	if len(tokens) == 0 {
		return nil
	}
	node := d.walkTree(tokens)
	if cluster := d.findMatchingCluster(node, tokens); cluster != nil {
		d.updateCluster(cluster, tokens, entry)
		return cluster.template
	}
	if len(d.clusters) >= d.config.MaxClusters {
		return d.handleOverflow(tokens, entry)
	}
	return d.createCluster(node, tokens, entry)
}

// Templates returns the clusters' templates in creation order.
func (d *DrainEngine) Templates() []*Template {
	out := make([]*Template, len(d.clusters))
	for i, c := range d.clusters {
		out[i] = c.template
	}
	return out
}

// walkTree traverses, creating as it goes, the prefix path for tokens.
func (d *DrainEngine) walkTree(tokens []string) *drainNode {
	node := d.getOrCreateChild(d.root, tokenCountBucket(len(tokens)))
	for depth := 0; depth < d.config.MaxDepth-1 && depth < len(tokens); depth++ {
		token := tokens[depth]
		if isWildcard(token) {
			token = Wildcard
		}
		node = d.getOrCreateChild(node, token)
	}
	return node
}

// handleOverflow merges into the best global match once the cluster cap is hit,
// falling back to the most recently created cluster when nothing matches.
func (d *DrainEngine) handleOverflow(tokens []string, entry Entry) *Template {
	best := d.findBestGlobalMatch(tokens)
	if best == nil {
		best = d.clusters[len(d.clusters)-1]
	}
	d.updateCluster(best, tokens, entry)
	return best.template
}

func (d *DrainEngine) createCluster(node *drainNode, tokens []string, entry Entry) *Template {
	pattern := strings.Join(tokens, " ")
	tpl := &Template{
		ID:        TemplateID(pattern),
		Pattern:   pattern,
		Severity:  entry.Severity,
		Count:     1,
		FirstSeen: entry.Timestamp,
		LastSeen:  entry.Timestamp,
	}
	tpl.Alias = TemplateAliasFor(tpl)
	c := &drainCluster{tokens: tokens, template: tpl}
	node.clusters = append(node.clusters, c)
	d.clusters = append(d.clusters, c)
	return tpl
}

// updateCluster broadens a cluster with a new message and refreshes everything
// derived from the pattern or the severity — INCLUDING the id and the alias,
// both of which move.
func (d *DrainEngine) updateCluster(c *drainCluster, tokens []string, entry Entry) {
	vars := extractVars(c.tokens, tokens)
	c.tokens = mergeTokens(c.tokens, tokens)
	pattern := strings.Join(c.tokens, " ")
	tpl := c.template
	tpl.Pattern = pattern
	tpl.ID = TemplateID(pattern)
	tpl.Count++
	updateTimeRange(tpl, entry.Timestamp)
	if severityRank(entry.Severity) > severityRank(tpl.Severity) {
		tpl.Severity = entry.Severity
	}
	// Both alias inputs may have moved, so the alias is recomputed rather than
	// kept. An INFO entry followed by a CRITICAL one of the same shape leaves
	// one template at one pattern and one id whose alias suffix moved.
	tpl.Alias = TemplateAliasFor(tpl)
	if vars != nil && len(tpl.ExampleVars) < maxExampleVars {
		tpl.ExampleVars = append(tpl.ExampleVars, vars)
	}
}

func (d *DrainEngine) getOrCreateChild(parent *drainNode, key string) *drainNode {
	if child, ok := parent.children[key]; ok {
		return child
	}
	if len(parent.children) >= d.config.MaxChildren {
		if child, ok := parent.children[Wildcard]; ok {
			return child
		}
		key = Wildcard
	}
	child := &drainNode{children: make(map[string]*drainNode)}
	parent.children[key] = child
	return child
}

func (d *DrainEngine) findMatchingCluster(node *drainNode, tokens []string) *drainCluster {
	var best *drainCluster
	bestSim := d.config.SimThreshold
	for _, c := range node.clusters {
		if sim := similarity(c.tokens, tokens); sim > bestSim {
			bestSim = sim
			best = c
		}
	}
	return best
}

func (d *DrainEngine) findBestGlobalMatch(tokens []string) *drainCluster {
	var best *drainCluster
	bestSim := d.config.SimThreshold
	for _, c := range d.clusters {
		if len(c.tokens) != len(tokens) {
			continue
		}
		if sim := similarity(c.tokens, tokens); sim > bestSim {
			bestSim = sim
			best = c
		}
	}
	return best
}

// similarity is the fraction of positions that agree, counting a wildcard on
// either side as agreement. Different lengths never compare.
func similarity(a, b []string) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	matches := 0
	for i := range a {
		if a[i] == b[i] || a[i] == Wildcard || b[i] == Wildcard {
			matches++
		}
	}
	return float64(matches) / float64(len(a))
}

// mergeTokens widens the template at every position the message disagrees with.
func mergeTokens(template, tokens []string) []string {
	if len(template) != len(tokens) {
		return template
	}
	result := make([]string, len(template))
	for i := range template {
		if template[i] == tokens[i] || tokens[i] == Wildcard {
			result[i] = template[i]
		} else {
			result[i] = Wildcard
		}
	}
	return result
}

// TemplateID is the first 16 bytes of sha256 over the pattern, hex-rendered.
// It is exported because the emitted node's id IS this value and a test asserts
// it against the documented preimage rather than against this function's own
// output on a value this function produced.
func TemplateID(pattern string) string {
	h := sha256.Sum256([]byte(pattern))
	return fmt.Sprintf("%x", h[:16])
}

// extractVars returns the message's tokens at the template's wildcard
// positions, or nil when nothing varies.
func extractVars(templateTokens, msgTokens []string) []string {
	if len(templateTokens) != len(msgTokens) {
		return nil
	}
	var vars []string
	for i, t := range templateTokens {
		if t == Wildcard && msgTokens[i] != Wildcard {
			vars = append(vars, msgTokens[i])
		}
	}
	return vars
}

func updateTimeRange(tpl *Template, ts time.Time) {
	if ts.IsZero() {
		return
	}
	if tpl.FirstSeen.IsZero() || ts.Before(tpl.FirstSeen) {
		tpl.FirstSeen = ts
	}
	if ts.After(tpl.LastSeen) {
		tpl.LastSeen = ts
	}
}
