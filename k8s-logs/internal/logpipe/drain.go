// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
)

// drain.go — Drain clustering: a fixed-depth prefix tree over preprocessed
// tokens, with similarity matching at the leaves and wildcard merging on match.
//
// THE PROPERTY THAT DECIDES THE GRAPH, and the one an implementation is most
// likely to get subtly wrong: A TEMPLATE'S ID IS RECOMPUTED WHENEVER ITS
// PATTERN BROADENS. The id is a hash of the pattern alone, and absorbing an
// entry that differs in one position rewrites that position to a wildcard, so
// the id an entry saw at insertion time is not the id its cluster ends with.
// Callers must therefore hold the *Template POINTER while clustering and read
// .ID afterwards; resolving ids as entries arrive puts entries of one cluster
// under several ids.

// DrainConfig tunes the clustering.
type DrainConfig struct {
	// SimThreshold is the minimum similarity, STRICTLY exceeded, for an entry
	// to merge into a cluster rather than start one.
	SimThreshold float64
	// MaxDepth is the prefix tree's depth.
	MaxDepth int
	// MaxChildren bounds a tree node's fan-out before further keys collapse
	// onto the wildcard branch.
	MaxChildren int
	// MaxClusters is a hard cap on distinct templates. Past it an entry merges
	// into its best global match instead of creating one, which keeps a
	// pathological log from turning every line into a node.
	MaxClusters int
}

// DefaultDrainConfig returns the tuning the log graph is built on.
func DefaultDrainConfig() DrainConfig {
	return DrainConfig{
		SimThreshold: 0.4,
		MaxDepth:     4,
		MaxChildren:  100,
		MaxClusters:  200,
	}
}

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

// drainCluster pairs the mutable token state with the template it produces.
type drainCluster struct {
	tokens   []string
	template *Template
}

// maxExampleVars caps stored example variable sets per template. Examples are
// for a human reading the template, so three is a sample rather than a copy of
// the data the chunks already hold.
const maxExampleVars = 3

// NewDrainEngine builds an engine with the given tuning.
func NewDrainEngine(cfg DrainConfig) *DrainEngine {
	return &DrainEngine{
		root:   &drainNode{children: make(map[string]*drainNode)},
		config: cfg,
	}
}

// AddMessage clusters one entry and returns the template it landed in, or nil
// when the message holds no tokens at all. A nil return is a real answer: an
// empty or whitespace-only line belongs to no template and must be skipped when
// chunks are assembled, not attached to an arbitrary one.
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

// Templates returns the current template set in creation order.
func (d *DrainEngine) Templates() []*Template {
	out := make([]*Template, len(d.clusters))
	for i, c := range d.clusters {
		out[i] = c.template
	}
	return out
}

// walkTree finds or creates the leaf for these tokens: the token-count bucket
// first, then one level per leading token up to the configured depth.
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

// handleOverflow merges an entry into the best match anywhere once the cluster
// cap is reached, falling back to the newest cluster when nothing is similar
// enough. The fallback is deliberate: past the cap the choice is between a
// slightly wrong cluster and unbounded growth.
func (d *DrainEngine) handleOverflow(tokens []string, entry Entry) *Template {
	best := d.findBestGlobalMatch(tokens)
	if best == nil {
		best = d.clusters[len(d.clusters)-1]
	}
	d.updateCluster(best, tokens, entry)
	return best.template
}

// createCluster starts a new cluster from these tokens.
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

// updateCluster absorbs an entry: the pattern broadens where the tokens differ,
// which moves the id, the severity rises to the most severe member, and the
// alias is refreshed because it is derived from both.
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
	// Pattern and severity may both have shifted, so the alias is stale.
	tpl.Alias = TemplateAliasFor(tpl)
	if vars != nil && len(tpl.ExampleVars) < maxExampleVars {
		tpl.ExampleVars = append(tpl.ExampleVars, vars)
	}
}

// getOrCreateChild descends one level, collapsing onto the wildcard branch once
// a node's fan-out reaches the configured maximum.
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

// findMatchingCluster returns the leaf's most similar cluster above the
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

// findBestGlobalMatch is the overflow path's search across every cluster of the
// same token count.
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
// either side as agreement. Different lengths score zero: a template and a
// message of different token counts are not the same shape.
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

// mergeTokens broadens a template to cover new tokens: a disagreeing position
// becomes a wildcard. A length mismatch leaves the template untouched, which
// cannot arise from the matching path and is guarded so a future caller cannot
// silently truncate a pattern.
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

// TemplateID is the template's node id: the first sixteen bytes of the
// pattern's sha256, hex-encoded. THIRTY-TWO characters, and bare — a
// full-length hash or a prefixed one satisfies every type assertion and still
// names a different node than the log graph's.
func TemplateID(pattern string) string {
	h := sha256.Sum256([]byte(pattern))
	return fmt.Sprintf("%x", h[:16])
}

// extractVars returns the message tokens sitting at the template's wildcard
// positions, or nil when there are none or the lengths disagree.
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

// updateTimeRange widens a template's bounds to include ts. A zero timestamp is
// ignored rather than treated as the epoch, which would drag FirstSeen to 1970
// for a whole cluster on one unparseable line.
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
