// SPDX-License-Identifier: Apache-2.0

package main

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
)

// drain.go — THE CLUSTERING STAGE. Messages that share a skeleton become one
// template; the positions where they differ become wildcards.
//
// THE TEMPLATE ID IS RECOMPUTED ON EVERY BROADENING, which is the property most
// easily missed by a reimplementation: a template's id, its alias and therefore
// its node SymbolName all MOVE while the collect is still running, and settle
// only once no further entry widens the pattern. That is why per-entry template
// ids are resolved from the cluster POINTER after clustering finishes rather
// than captured as the entry is added.

// DrainConfig tunes the clustering.
type DrainConfig struct {
	// SimThreshold is the similarity a candidate cluster must EXCEED (not
	// merely reach) to absorb a message.
	SimThreshold float64
	// MaxDepth bounds the prefix tree, so only the first MaxDepth-1 tokens
	// branch and messages differing only later meet at the same leaf.
	MaxDepth int
	// MaxChildren bounds one node's children; past it every further key
	// collapses onto the wildcard child.
	MaxChildren int
	// MaxClusters caps the template count for the whole collect. Past it a
	// message is merged into its best global match rather than creating a
	// template, so a very diverse log group produces a bounded graph.
	MaxClusters int
}

// DefaultDrainConfig is the built-in pipeline's configuration, reproduced. The
// values are part of the parity target: changing one changes which messages
// cluster together, and therefore every template and chunk id.
func DefaultDrainConfig() DrainConfig {
	return DrainConfig{SimThreshold: 0.4, MaxDepth: 4, MaxChildren: 100, MaxClusters: 200}
}

// maxExampleVars caps the variable rows kept per template. The rows are what
// the consolidators match a stack-trace fragment on, so they are evidence
// rather than decoration.
const maxExampleVars = 3

// DrainEngine clusters messages into templates.
type DrainEngine struct {
	root     *drainNode
	clusters []*drainCluster
	config   DrainConfig
}

type drainNode struct {
	children map[string]*drainNode
	clusters []*drainCluster
}

// drainCluster pairs the live token state with the template it produces. The
// tokens are the merge state; the template is the published value.
type drainCluster struct {
	tokens   []string
	template *LogTemplate
}

// NewDrainEngine builds an engine with the given configuration.
func NewDrainEngine(cfg DrainConfig) *DrainEngine {
	return &DrainEngine{root: &drainNode{children: make(map[string]*drainNode)}, config: cfg}
}

// AddMessage clusters one entry and returns the template it joined, or nil for
// a message that tokenizes to nothing. The returned pointer is stable across
// later merges even though the template's ID field is not, which is what lets a
// caller resolve final ids after the whole collect has been clustered.
func (d *DrainEngine) AddMessage(entry LogEntry) *LogTemplate {
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

// Templates returns the current templates in cluster-creation order.
func (d *DrainEngine) Templates() []*LogTemplate {
	out := make([]*LogTemplate, len(d.clusters))
	for i, c := range d.clusters {
		out[i] = c.template
	}
	return out
}

// walkTree descends to the leaf node for these tokens, creating the path.
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

// handleOverflow merges into the best cluster anywhere once MaxClusters is
// reached, falling back to the most recently created cluster when nothing is
// similar enough. The fallback is what keeps the cap hard: without it an
// over-cap message would still create a template.
func (d *DrainEngine) handleOverflow(tokens []string, entry LogEntry) *LogTemplate {
	best := d.findBestGlobalMatch(tokens)
	if best == nil {
		best = d.clusters[len(d.clusters)-1]
	}
	d.updateCluster(best, tokens, entry)
	return best.template
}

// createCluster seeds a new cluster and its template from one entry.
func (d *DrainEngine) createCluster(node *drainNode, tokens []string, entry LogEntry) *LogTemplate {
	pattern := strings.Join(tokens, " ")
	tpl := &LogTemplate{
		ID:        templateID(pattern),
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

// updateCluster absorbs one entry into an existing cluster.
//
// FOUR THINGS MOVE HERE, and a reimplementation that moves fewer passes most
// tests: the merged pattern, the id recomputed from it, the aggregate severity
// raised when this entry outranks the cluster's, and THE ALIAS RE-DERIVED after
// both. Deriving the alias only at creation leaves a template whose SymbolName
// describes its first entry rather than its cluster.
func (d *DrainEngine) updateCluster(c *drainCluster, tokens []string, entry LogEntry) {
	vars := extractVars(c.tokens, tokens)
	c.tokens = mergeTokens(c.tokens, tokens)
	tpl := c.template
	tpl.Pattern = strings.Join(c.tokens, " ")
	tpl.ID = templateID(tpl.Pattern)
	tpl.Count++
	updateTimeRange(tpl, entry.Timestamp)
	if severityRank(entry.Severity) > severityRank(tpl.Severity) {
		tpl.Severity = entry.Severity
	}
	tpl.Alias = TemplateAliasFor(tpl)
	if vars != nil && len(tpl.ExampleVars) < maxExampleVars {
		tpl.ExampleVars = append(tpl.ExampleVars, vars)
	}
}

// getOrCreateChild returns the child for key, collapsing onto the wildcard
// child once the node is at MaxChildren.
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

// findMatchingCluster picks the leaf's most similar cluster above the
// threshold, or nil.
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

// findBestGlobalMatch is handleOverflow's search: the same scoring over every
// cluster, restricted to equal token counts.
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
// either side as agreement. Token counts that differ score zero, which is what
// keeps two messages of different lengths in separate templates.
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

// mergeTokens widens a template's tokens against a message's, replacing each
// disagreeing position with the wildcard.
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

// templateID is sha256 of the pattern truncated to the FIRST 16 BYTES and
// rendered hex — 32 hex characters, no prefix. Truncating to 16 hex CHARACTERS
// instead is the error this comment exists to prevent: it produces ids of the
// right shape that match nothing the built-in pipeline produced.
func templateID(pattern string) string {
	h := sha256.Sum256([]byte(pattern))
	return fmt.Sprintf("%x", h[:16])
}

// extractVars returns the message tokens sitting at the template's wildcard
// positions, or nil when the token counts disagree.
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

// updateTimeRange widens a template's observed range. FirstSeen is the MINIMUM
// timestamp and not the first arrival, so entries delivered out of order still
// bound the cluster correctly.
func updateTimeRange(tpl *LogTemplate, ts time.Time) {
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
