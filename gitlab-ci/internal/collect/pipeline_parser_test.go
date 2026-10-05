// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"slices"
	"testing"
)

// pipeline_parser_test.go — the pipeline reader's input classes.
//
// IT IS AN IN-PACKAGE TEST because the parser is not part of this package's
// surface: it exists to turn one document into three edge classes, and every
// caller of it is in this file's own package. Testing it through the enumeration
// would reach it only through a fixture document and would leave the shapes below
// — the ones a real `.gitlab-ci.yml` in the wild takes — unreached.

func TestADocumentThatIsNotYAMLIsRefused(t *testing.T) {
	if _, err := parseGitLabCI([]byte("build:\n  script:\n   - echo\n\t- a tab is not YAML\n")); err == nil {
		t.Fatal("a document that is not YAML parsed cleanly")
	}
	// The known positive: a document that IS YAML parses, so the refusal above is
	// about this input rather than about every input.
	if _, err := parseGitLabCI([]byte("build:\n  script: echo ok\n")); err != nil {
		t.Fatalf("a valid document was refused: %v", err)
	}
}

func TestOnlyNonReservedTopLevelMappingsAreJobs(t *testing.T) {
	config, err := parseGitLabCI([]byte(`stages:
  - build
variables:
  FOO: bar
default:
  image: alpine
image: alpine
cache:
  paths:
    - vendor
.hidden:
  script: echo template
loose: a scalar is not a job
build:
  script: echo ok
`))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}

	names := make([]string, 0, len(config.Jobs))
	for _, job := range config.Jobs {
		names = append(names, job.Name)
	}
	if want := []string{"build"}; !slices.Equal(names, want) {
		t.Errorf("the parser read the jobs %v, want %v — a reserved key, a hidden template and a "+
			"top-level scalar are none of them jobs", names, want)
	}
	if len(config.Stages) != 1 {
		t.Errorf("the parser read %d stages, want 1", len(config.Stages))
	}
}

func TestADocumentOfOnlyReservedKeysYieldsNoJobs(t *testing.T) {
	config, err := parseGitLabCI([]byte("stages:\n  - build\nvariables:\n  FOO: bar\n"))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if len(config.Jobs) != 0 {
		t.Errorf("a document of only reserved keys yielded %d jobs", len(config.Jobs))
	}
}

func TestTheEnvironmentIsReadInBothShapesAndNeitherIsInvented(t *testing.T) {
	for _, row := range []struct{ name, document, want string }{
		{name: "a bare name", document: "deploy:\n  environment: production\n", want: "production"},
		{name: "a mapping", document: "deploy:\n  environment:\n    name: production\n    url: https://x\n", want: "production"},
		{name: "a mapping with no name", document: "deploy:\n  environment:\n    url: https://x\n", want: ""},
		{name: "a list, which is neither", document: "deploy:\n  environment:\n    - production\n", want: ""},
		{name: "absent", document: "deploy:\n  script: echo ok\n", want: ""},
	} {
		t.Run(row.name, func(t *testing.T) {
			config, err := parseGitLabCI([]byte(row.document))
			if err != nil {
				t.Fatalf("parsing: %v", err)
			}
			if len(config.Jobs) != 1 {
				t.Fatalf("the parser read %d jobs, want 1", len(config.Jobs))
			}
			if got := config.Jobs[0].Environment; got != row.want {
				t.Errorf("the environment is %q, want %q", got, row.want)
			}
		})
	}
}

func TestTheScriptIsReadInBothShapesAndANonStringEntryIsDropped(t *testing.T) {
	for _, row := range []struct {
		name, document string
		want           []string
	}{
		{name: "a bare string", document: "build:\n  script: echo one\n", want: []string{"echo one"}},
		{name: "a list", document: "build:\n  script:\n    - echo one\n    - echo two\n", want: []string{"echo one", "echo two"}},
		{
			name:     "a list carrying a nested list",
			document: "build:\n  script:\n    - echo one\n    - - echo nested\n",
			want:     []string{"echo one"},
		},
		{name: "a mapping, which is neither", document: "build:\n  script:\n    run: echo one\n", want: nil},
	} {
		t.Run(row.name, func(t *testing.T) {
			config, err := parseGitLabCI([]byte(row.document))
			if err != nil {
				t.Fatalf("parsing: %v", err)
			}
			if len(config.Jobs) != 1 {
				t.Fatalf("the parser read %d jobs, want 1", len(config.Jobs))
			}
			if got := config.Jobs[0].Script; !slices.Equal(got, row.want) {
				t.Errorf("the script is %v, want %v", got, row.want)
			}
		})
	}
}

func TestTheVariableReferencesAreDeduplicatedAndThePredefinedOnesAreDropped(t *testing.T) {
	for _, row := range []struct {
		name   string
		script []string
		want   []string
	}{
		{name: "none", script: []string{"echo hello"}, want: nil},
		{name: "one", script: []string{"echo $DEPLOY_KEY"}, want: []string{"DEPLOY_KEY"}},
		{
			name:   "the same name twice, in both spellings",
			script: []string{"echo $DEPLOY_KEY", "echo ${DEPLOY_KEY}"},
			want:   []string{"DEPLOY_KEY"},
		},
		{
			name:   "two names on one line",
			script: []string{"curl -H $AUTH ${ENDPOINT}"},
			want:   []string{"AUTH", "ENDPOINT"},
		},
		{
			name:   "only predefined names",
			script: []string{"echo $CI_JOB_TOKEN $CI_COMMIT_SHA $HOME $PATH $PWD $SHELL"},
			want:   nil,
		},
		{
			name:   "a predefined name beside a declared one",
			script: []string{"echo $CI_COMMIT_SHA $DEPLOY_KEY"},
			want:   []string{"DEPLOY_KEY"},
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			if got := extractVarRefs(row.script); !slices.Equal(got, row.want) {
				t.Errorf("the references are %v, want %v", got, row.want)
			}
		})
	}
}

func TestTheIncludesAreCountedInEveryShape(t *testing.T) {
	for _, row := range []struct {
		name, document string
		want           int
	}{
		{name: "absent", document: "build:\n  script: echo ok\n", want: 0},
		{name: "a bare string", document: "include: /templates/base.yml\n", want: 1},
		{name: "a mapping", document: "include:\n  local: /templates/base.yml\n", want: 1},
		{
			name:     "a list",
			document: "include:\n  - local: /a.yml\n  - remote: https://example.com/b.yml\n",
			want:     2,
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			config, err := parseGitLabCI([]byte(row.document))
			if err != nil {
				t.Fatalf("parsing: %v", err)
			}
			if config.Includes != row.want {
				t.Errorf("the parser counted %d includes, want %d", config.Includes, row.want)
			}
		})
	}
}

// TestTheJobsComeBackSortedByName. They are read out of a YAML mapping, which Go
// iterates in a random order, so an unsorted result would make two parses of the
// same document produce their edges in different orders — and a carry-forward
// comparison would then depend on a map walk.
func TestTheJobsComeBackSortedByName(t *testing.T) {
	document := []byte("zeta:\n  script: echo z\nalpha:\n  script: echo a\nmid:\n  script: echo m\n")
	for range 20 {
		config, err := parseGitLabCI(document)
		if err != nil {
			t.Fatalf("parsing: %v", err)
		}
		names := make([]string, 0, len(config.Jobs))
		for _, job := range config.Jobs {
			names = append(names, job.Name)
		}
		if want := []string{"alpha", "mid", "zeta"}; !slices.Equal(names, want) {
			t.Fatalf("the parser read the jobs in the order %v, want %v", names, want)
		}
	}
}
