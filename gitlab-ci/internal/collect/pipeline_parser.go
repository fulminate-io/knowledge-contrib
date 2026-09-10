// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"cmp"
	"regexp"
	"slices"

	"gopkg.in/yaml.v3"
)

// pipeline_parser.go — the `.gitlab-ci.yml` reader, and the ONE place this
// collector reads a document rather than an API answer.
//
// WHY A PARSER AT ALL. Three of this graph's edge classes are declared nowhere
// in the provider's API and only in the pipeline definition: which runner tags a
// job asks for, which environment it deploys to, and which CI/CD variables its
// scripts reference. Without reading the document those three relationships do
// not exist.
//
// IT IS DELIBERATELY SHALLOW. A `.gitlab-ci.yml` may `include:` other documents,
// use YAML anchors, and extend hidden templates; this reader resolves none of
// that and records only that includes are present. Resolving them means fetching
// other projects' files with this credential, which is a wider read than an
// inventory needs.

// pipelineConfig is the parsed representation of a `.gitlab-ci.yml`.
type pipelineConfig struct {
	Stages   []string
	Jobs     []jobDef
	Includes int
}

// jobDef holds the parsed fields of a single job definition.
type jobDef struct {
	Name        string
	Stage       string
	Tags        []string
	Environment string
	Script      []string
	// VarRefs are the variable names the job's scripts reference, deduplicated
	// and with the provider's own predefined names removed.
	VarRefs []string
}

// varRefPattern matches ${VAR_NAME} and $VAR_NAME in script lines.
var varRefPattern = regexp.MustCompile(`\$\{?([A-Z_][A-Z0-9_]*)\}?`)

// knownCIVars are the provider's PREDEFINED variables, which are not references
// to anything an operator declared.
//
// THEY ARE FILTERED OUT BECAUSE AN EDGE TO ONE WOULD BE A FALSE POSITIVE, and a
// noisy one: nearly every script line mentions one, so a USES_SECRET edge per
// mention would bury the handful of edges that name a real variable.
var knownCIVars = map[string]bool{
	"CI":                                  true,
	"CI_COMMIT_SHA":                       true,
	"CI_COMMIT_REF_NAME":                  true,
	"CI_COMMIT_REF_SLUG":                  true,
	"CI_COMMIT_BRANCH":                    true,
	"CI_COMMIT_TAG":                       true,
	"CI_COMMIT_MESSAGE":                   true,
	"CI_PIPELINE_ID":                      true,
	"CI_PIPELINE_SOURCE":                  true,
	"CI_PROJECT_ID":                       true,
	"CI_PROJECT_NAME":                     true,
	"CI_PROJECT_PATH":                     true,
	"CI_PROJECT_DIR":                      true,
	"CI_PROJECT_URL":                      true,
	"CI_PROJECT_NAMESPACE":                true,
	"CI_JOB_ID":                           true,
	"CI_JOB_NAME":                         true,
	"CI_JOB_STAGE":                        true,
	"CI_JOB_TOKEN":                        true,
	"CI_JOB_URL":                          true,
	"CI_REGISTRY":                         true,
	"CI_REGISTRY_IMAGE":                   true,
	"CI_SERVER_URL":                       true,
	"CI_API_V4_URL":                       true,
	"CI_ENVIRONMENT_NAME":                 true,
	"CI_ENVIRONMENT_SLUG":                 true,
	"CI_ENVIRONMENT_URL":                  true,
	"CI_DEFAULT_BRANCH":                   true,
	"CI_MERGE_REQUEST_IID":                true,
	"CI_MERGE_REQUEST_SOURCE_BRANCH_NAME": true,
	"CI_RUNNER_ID":                        true,
	"CI_RUNNER_TAGS":                      true,
	"GITLAB_USER_LOGIN":                   true,
	"GITLAB_USER_EMAIL":                   true,
	"HOME":                                true,
	"PATH":                                true,
	"PWD":                                 true,
	"SHELL":                               true,
}

// reservedKeys are the top-level keys that are NOT job definitions. Everything
// else at the top level is a job, which is the provider's own rule.
var reservedKeys = map[string]bool{
	"stages": true, "include": true, "variables": true,
	"default": true, "workflow": true, "image": true,
	"services": true, "before_script": true, "after_script": true,
	"cache": true, "pages": true,
}

// parseGitLabCI parses a `.gitlab-ci.yml` document.
//
// THE JOBS COME BACK SORTED BY NAME. They are read out of a YAML mapping, which
// Go iterates in a random order, so an unsorted result would make two parses of
// the same document produce their edges in different orders.
func parseGitLabCI(raw []byte) (*pipelineConfig, error) {
	var doc map[string]yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}

	cfg := &pipelineConfig{}
	if node, ok := doc["stages"]; ok {
		var stages []string
		if err := node.Decode(&stages); err == nil {
			cfg.Stages = stages
		}
	}
	if node, ok := doc["include"]; ok {
		cfg.Includes = countIncludes(&node)
	}
	for key, node := range doc {
		// An empty key, a reserved one and a hidden template (`.build:`) are not
		// jobs. The hidden ones especially: they exist to be extended and running
		// nothing, so edges from them would assert work that never happens.
		if key == "" || reservedKeys[key] || key[0] == '.' {
			continue
		}
		if job := decodeJob(key, &node); job != nil {
			cfg.Jobs = append(cfg.Jobs, *job)
		}
	}
	slices.SortStableFunc(cfg.Jobs, func(a, b jobDef) int { return cmp.Compare(a.Name, b.Name) })
	return cfg, nil
}

// countIncludes counts the include directives, in any of the three shapes the
// provider accepts. The COUNT is all this collector records: what a document
// includes is another document it does not fetch.
func countIncludes(node *yaml.Node) int {
	switch node.Kind {
	case yaml.ScalarNode, yaml.MappingNode:
		return 1
	case yaml.SequenceNode:
		return len(node.Content)
	default:
		return 0
	}
}

// decodeJob parses a single job definition from a YAML mapping node. A node that
// is not a mapping is not a job, which is how a stray scalar at the top level is
// rejected rather than turned into an empty job.
func decodeJob(name string, node *yaml.Node) *jobDef {
	if node.Kind != yaml.MappingNode {
		return nil
	}

	var raw struct {
		Stage       string   `yaml:"stage"`
		Tags        []string `yaml:"tags"`
		Environment any      `yaml:"environment"`
		Script      any      `yaml:"script"`
	}
	if err := node.Decode(&raw); err != nil {
		return nil
	}

	job := &jobDef{Name: name, Stage: raw.Stage, Tags: raw.Tags}
	job.Environment = decodeEnvironment(raw.Environment)
	job.Script = decodeScript(raw.Script)
	job.VarRefs = extractVarRefs(job.Script)
	return job
}

// decodeEnvironment extracts the environment name from either of the two shapes
// the provider accepts: a bare name, or a mapping carrying one.
func decodeEnvironment(v any) string {
	switch env := v.(type) {
	case string:
		return env
	case map[string]any:
		if name, ok := env["name"].(string); ok {
			return name
		}
	}
	return ""
}

// decodeScript normalizes a job's script to a list of lines. A non-string entry
// in the list is dropped: the provider allows a nested list there, and flattening
// one would invent lines the document does not contain.
func decodeScript(v any) []string {
	switch script := v.(type) {
	case string:
		return []string{script}
	case []any:
		var lines []string
		for _, item := range script {
			if line, ok := item.(string); ok {
				lines = append(lines, line)
			}
		}
		return lines
	}
	return nil
}

// extractVarRefs finds the operator-declared variable names a job's scripts
// reference, in first-appearance order and without duplicates.
func extractVarRefs(scripts []string) []string {
	seen := make(map[string]bool)
	var refs []string
	for _, line := range scripts {
		for _, match := range varRefPattern.FindAllStringSubmatch(line, -1) {
			name := match[1]
			if knownCIVars[name] || seen[name] {
				continue
			}
			seen[name] = true
			refs = append(refs, name)
		}
	}
	return refs
}
