// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"cmp"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// pipelines_parse.go — the bitbucket-pipelines.yml parse, and the variable
// references a step's script names.
//
// THE SOURCE PROVIDER'S SUITE COVERS NONE OF THIS. It has no test of the parse,
// of the five trigger sections, of the reference extraction or of the built-in
// filter, which is why this module's own suite exceeds the parity target here
// rather than mirroring it.

// pipelinesFile is the top-level document.
type pipelinesFile struct {
	Pipelines pipelinesDef `yaml:"pipelines"`
}

// pipelinesDef holds the five trigger sections the provider defines.
type pipelinesDef struct {
	Default      []pipelineStep            `yaml:"default"`
	Branches     map[string][]pipelineStep `yaml:"branches"`
	PullRequests map[string][]pipelineStep `yaml:"pull-requests"`
	Custom       map[string][]pipelineStep `yaml:"custom"`
	Tags         map[string][]pipelineStep `yaml:"tags"`
}

// pipelineStep is one entry in a pipeline definition. `parallel` and `stage`
// entries carry no `step` key and are skipped, as the source provider skips
// them: they are groupings whose own steps this collector does not descend into.
type pipelineStep struct {
	Step *stepBody `yaml:"step"`
}

// stepBody is a step's configuration.
type stepBody struct {
	Name       string   `yaml:"name"`
	Script     []string `yaml:"script"`
	Deployment string   `yaml:"deployment"`
	RunsOn     []string `yaml:"runs-on"`
	Services   []string `yaml:"services"`
	Caches     []string `yaml:"caches"`
}

// parsedPipeline is one flattened pipeline definition.
type parsedPipeline struct {
	// Name is `default`, or `<section>/<trigger key>`.
	Name string
	// TriggerKey is the map key — a branch pattern, a tag pattern, a custom
	// pipeline's name — and is empty for the default pipeline.
	TriggerKey string
	Steps      []parsedStep
}

// parsedStep is what a step contributes to the graph.
type parsedStep struct {
	Name       string
	Deployment string
	RunsOn     []string
	Services   []string
	Caches     []string
	// VarRefs are the variable names the step's script references, deduplicated
	// within the step and in first-seen order.
	VarRefs []string
}

// parsePipelinesYAML parses a repository's pipeline definition.
func parsePipelinesYAML(data []byte) (*pipelinesFile, error) {
	var parsed pipelinesFile
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	return &parsed, nil
}

// extractPipelines flattens the five sections into one list.
//
// THE RESULT IS SORTED BY NAME AND THE SORT IS LOAD-BEARING. Four of the five
// sections are Go maps, and ranging a map yields a different order on every run
// of the same process — so two collects of a workspace that did not change would
// otherwise merge their pipelines in different orders. The graph builder sorts
// too, and this sort is what makes the intermediate result stable for the tests
// that read it before the builder runs.
func extractPipelines(def pipelinesDef) []parsedPipeline {
	var out []parsedPipeline
	if len(def.Default) > 0 {
		out = append(out, buildParsedPipeline("default", "", def.Default))
	}
	for _, section := range []struct {
		prefix string
		steps  map[string][]pipelineStep
	}{
		{"branches", def.Branches},
		{"pull-requests", def.PullRequests},
		{"custom", def.Custom},
		{"tags", def.Tags},
	} {
		for key, steps := range section.steps {
			out = append(out, buildParsedPipeline(section.prefix+"/"+key, key, steps))
		}
	}
	slices.SortStableFunc(out, func(a, b parsedPipeline) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

// buildParsedPipeline converts one section's steps.
func buildParsedPipeline(name, triggerKey string, steps []pipelineStep) parsedPipeline {
	parsed := parsedPipeline{Name: name, TriggerKey: triggerKey}
	for _, entry := range steps {
		if entry.Step == nil {
			// A `parallel` or `stage` entry. It is not a step and it carries no
			// script, so there is nothing here to convert.
			continue
		}
		parsed.Steps = append(parsed.Steps, parsedStep{
			Name:       entry.Step.Name,
			Deployment: entry.Step.Deployment,
			RunsOn:     entry.Step.RunsOn,
			Services:   entry.Step.Services,
			Caches:     entry.Step.Caches,
			VarRefs:    extractVarRefs(entry.Step.Script),
		})
	}
	return parsed
}

// varRefPattern matches a `$VAR` or `${VAR}` reference in a script line.
var varRefPattern = regexp.MustCompile(`\$\{?([A-Z_][A-Z0-9_]*)\}?`)

// extractVarRefs scans a step's script lines for variable references,
// deduplicating within the step and keeping first-seen order.
func extractVarRefs(lines []string) []string {
	seen := make(map[string]struct{})
	var refs []string
	for _, line := range lines {
		for _, match := range varRefPattern.FindAllStringSubmatch(line, -1) {
			name := match[1]
			if isProviderBuiltin(name) {
				continue
			}
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			refs = append(refs, name)
		}
	}
	return refs
}

// providerBuiltins are the shell variables and provider-injected names that are
// not pipeline variables. A reference to one of these is not a secret and would
// resolve to nothing, so it is filtered rather than carried and reported
// unresolved.
var providerBuiltins = map[string]bool{
	"HOME": true, "PATH": true, "PWD": true, "SHELL": true, "USER": true,
	"HOSTNAME": true, "LANG": true, "TERM": true, "TMPDIR": true, "OLDPWD": true,
	"SHLVL": true, "IFS": true, "PIPESTATUS": true, "CI": true,
	"BITBUCKET_CLONE_DIR": true, "BITBUCKET_WORKSPACE": true,
	"BITBUCKET_REPO_SLUG": true, "BITBUCKET_COMMIT": true, "BITBUCKET_BRANCH": true,
	"BITBUCKET_TAG": true, "BITBUCKET_PR_ID": true, "BITBUCKET_BUILD_NUMBER": true,
	"BITBUCKET_PIPELINE_UUID": true, "BITBUCKET_STEP_UUID": true,
	"BITBUCKET_REPO_FULL_NAME": true, "BITBUCKET_REPO_UUID": true,
	"BITBUCKET_DEPLOYMENT_ENVIRONMENT": true, "BITBUCKET_REPO_IS_PRIVATE": true,
	"BITBUCKET_GIT_HTTP_ORIGIN": true, "BITBUCKET_PROJECT_KEY": true,
	"BITBUCKET_EXIT_CODE": true, "BITBUCKET_STEP_TRIGGERER_UUID": true,
	"BITBUCKET_PARALLEL_STEP": true, "BITBUCKET_PARALLEL_STEP_COUNT": true,
}

// isProviderBuiltin reports whether a reference names a shell or provider
// built-in rather than a pipeline variable. The comparison is upper-cased
// because the extraction pattern already admits only upper-case names, and
// keeping the fold makes a widened pattern fail safe.
func isProviderBuiltin(name string) bool { return providerBuiltins[strings.ToUpper(name)] }
