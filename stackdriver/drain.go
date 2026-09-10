// SPDX-License-Identifier: Apache-2.0

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// drain.go — the DRAIN CLUSTERER: a fixed-depth prefix tree over masked tokens,
// with similarity matching at the leaves and wildcard merging on match.
//
// THE TEMPLATE ID IS THE HASH OF THE PATTERN AND THE PATTERN MOVES. Merging a
// new entry into a cluster can broaden the pattern with a fresh wildcard, which
// changes the id — so a caller cannot record an entry's template id at the
// moment it clusters. It records the POINTER and reads the id after the whole
// pass, which is what pipeline.go does and why it does it.

// drainConfig tunes the clusterer.
type drainConfig struct {
	// SimThreshold is the similarity a candidate must EXCEED to be merged into.
	SimThreshold float64
	// MaxDepth bounds the prefix tree, so a long line does not build a long path.
	MaxDepth int
	// MaxChildren bounds one node's fan-out before further keys collapse onto
	// the wildcard branch.
	MaxChildren int
	// MaxClusters is the hard cap on distinct templates.
	MaxClusters int
}

// defaultDrainConfig is the tuning the emitted graph is defined at. These four
// numbers decide which entries share a template, so they decide the template
// set, the template ids, and every chunk id that folds one.
func defaultDrainConfig() drainConfig {
	return drainConfig{
		SimThreshold: 0.4,
		MaxDepth:     4,
		MaxChildren:  100,
		MaxClusters:  200,
	}
}

// maxExampleVars caps how many variable-value rows a template retains.
const maxExampleVars = 3

// drainEngine holds the clustering state for one collect. It is not safe for
// concurrent use and is not meant to be: the pass is serial by construction,
// because every message's cluster assignment depends on the ones before it.
type drainEngine struct {
	root     *drainNode
	clusters []*drainCluster
	config   drainConfig
}

type drainNode struct {
	children map[string]*drainNode
	clusters []*drainCluster
}

// drainCluster pairs the cluster's own token state with the template it owns.
type drainCluster struct {
	tokens   []string
	template *logTemplate
}

func newDrainEngine(cfg drainConfig) *drainEngine {
	return &drainEngine{
		root:   &drainNode{children: make(map[string]*drainNode)},
		config: cfg,
	}
}

// addMessage clusters one entry, returning the template it landed in. A message
// that tokenizes to nothing returns nil: it carries no pattern, so it belongs to
// no template, and the caller records that as "this entry has no template"
// rather than inventing an empty one.
func (d *drainEngine) addMessage(entry logEntry) *logTemplate {
	tokens := tokenize(preProcess(entry.Message))
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

// templates returns the current cluster templates in creation order.
func (d *drainEngine) templates() []*logTemplate {
	out := make([]*logTemplate, len(d.clusters))
	for i, c := range d.clusters {
		out[i] = c.template
	}
	return out
}

// walkTree descends to the leaf node for these tokens, creating the path.
func (d *drainEngine) walkTree(tokens []string) *drainNode {
	node := d.getOrCreateChild(d.root, tokenCountBucket(len(tokens)))
	for depth := 0; depth < d.config.MaxDepth-1 && depth < len(tokens); depth++ {
		token := tokens[depth]
		if isWildcardToken(token) {
			token = wildcard
		}
		node = d.getOrCreateChild(node, token)
	}
	return node
}

// handleOverflow merges into the best global match once the cluster cap is
// reached, falling back to the most recent cluster when nothing matches. The cap
// is what stops a pathological corpus from producing one template per line; the
// fallback is what stops an entry from being dropped when it does.
func (d *drainEngine) handleOverflow(tokens []string, entry logEntry) *logTemplate {
	best := d.findBestGlobalMatch(tokens)
	if best == nil {
		best = d.clusters[len(d.clusters)-1]
	}
	d.updateCluster(best, tokens, entry)
	return best.template
}

func (d *drainEngine) createCluster(node *drainNode, tokens []string, entry logEntry) *logTemplate {
	pattern := strings.Join(tokens, " ")
	tpl := &logTemplate{
		ID:        templateID(pattern),
		Pattern:   pattern,
		Severity:  entry.Severity,
		Count:     1,
		FirstSeen: entry.Timestamp,
		LastSeen:  entry.Timestamp,
	}
	tpl.Alias = templateAliasFor(tpl)
	c := &drainCluster{tokens: tokens, template: tpl}
	node.clusters = append(node.clusters, c)
	d.clusters = append(d.clusters, c)
	return tpl
}

// updateCluster merges an entry into a cluster.
//
// THREE OF THE FIVE WRITES BELOW MOVE A VALUE OTHER CODE KEYS ON. The pattern
// merge can introduce a wildcard, which moves the ID. The severity raise moves
// the SEVERITY, which is what the error-template filter and the alias suffix
// read. And because both of those are inputs to the alias, the alias is
// RE-DERIVED here rather than kept from creation — a cluster that first saw an
// INFO line and later a CRITICAL one carries the CRITICAL suffix, and a module
// that derives the alias once emits the wrong one on ordinary input.
func (d *drainEngine) updateCluster(c *drainCluster, tokens []string, entry logEntry) {
	vars := extractVars(c.tokens, tokens)
	c.tokens = mergeTokens(c.tokens, tokens)
	pattern := strings.Join(c.tokens, " ")
	tpl := c.template
	tpl.Pattern = pattern
	tpl.ID = templateID(pattern)
	tpl.Count++
	updateTimeRange(tpl, entry.Timestamp)
	if severityIndex(entry.Severity) > severityIndex(tpl.Severity) {
		tpl.Severity = entry.Severity
	}
	tpl.Alias = templateAliasFor(tpl)
	if vars != nil && len(tpl.ExampleVars) < maxExampleVars {
		tpl.ExampleVars = append(tpl.ExampleVars, vars)
	}
}

// getOrCreateChild returns the child for key, collapsing onto the wildcard
// branch once the node is at its fan-out cap.
func (d *drainEngine) getOrCreateChild(parent *drainNode, key string) *drainNode {
	if child, ok := parent.children[key]; ok {
		return child
	}
	if len(parent.children) >= d.config.MaxChildren {
		if child, ok := parent.children[wildcard]; ok {
			return child
		}
		key = wildcard
	}
	child := &drainNode{children: make(map[string]*drainNode)}
	parent.children[key] = child
	return child
}

// findMatchingCluster returns the leaf's best candidate above the threshold.
// The comparison is strict, so a candidate exactly AT the threshold does not
// match: the threshold is the floor of "similar enough", not a member of it.
func (d *drainEngine) findMatchingCluster(node *drainNode, tokens []string) *drainCluster {
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

// findBestGlobalMatch is the overflow path's search across every cluster.
func (d *drainEngine) findBestGlobalMatch(tokens []string) *drainCluster {
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
// either side as agreement. Token sequences of different lengths are never
// similar: the parse tree already bucketed by length, and comparing across
// lengths would merge a message with its own truncation.
func similarity(a, b []string) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	matches := 0
	for i := range a {
		if a[i] == b[i] || a[i] == wildcard || b[i] == wildcard {
			matches++
		}
	}
	return float64(matches) / float64(len(a))
}

// mergeTokens widens the template where the two disagree.
func mergeTokens(template, tokens []string) []string {
	if len(template) != len(tokens) {
		return template
	}
	result := make([]string, len(template))
	for i := range template {
		if template[i] == tokens[i] || tokens[i] == wildcard {
			result[i] = template[i]
		} else {
			result[i] = wildcard
		}
	}
	return result
}

// templateIDBytes is how much of the pattern hash the id carries. Sixteen bytes
// is the width the emitted graph's template ids are defined at.
const templateIDBytes = 16

// templateID is the stable id of a pattern: the first templateIDBytes of its
// sha256, hex, with no prefix.
func templateID(pattern string) string {
	h := sha256.Sum256([]byte(pattern))
	return hex.EncodeToString(h[:templateIDBytes])
}

// extractVars returns the tokens sitting at the template's wildcard positions.
func extractVars(templateTokens, msgTokens []string) []string {
	if len(templateTokens) != len(msgTokens) {
		return nil
	}
	var vars []string
	for i, t := range templateTokens {
		if t == wildcard && msgTokens[i] != wildcard {
			vars = append(vars, msgTokens[i])
		}
	}
	return vars
}

// updateTimeRange widens the template's window. A zero timestamp is ignored
// rather than treated as the epoch, which would put every template's FirstSeen
// in 1970 the first time an entry arrived without one.
func updateTimeRange(tpl *logTemplate, ts time.Time) {
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
