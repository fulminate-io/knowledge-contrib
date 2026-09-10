// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"encoding/base64"

	gl "gitlab.com/gitlab-org/api/client-go"
)

// parity_fixture_runners_test.go — the recorded runners, environments, jobs and
// variables.
//
// THEY ARE THE SHAPED HALF OF THE CORPUS. Each one exists to reach something a
// simpler fixture would not: two runners sharing a tag at two scopes, a third
// returned by two projects, a protected environment for an environment the
// listing does not return, a job with a runner and a job without, and a variable
// value planted as a sentinel. The group's projects and its pipeline document are
// in parity_fixture_test.go.

// fixtureGroupRunner is the group-level runner, whose parent is the group node.
func fixtureGroupRunner() *gl.Runner {
	return &gl.Runner{
		ID: 1, Description: "group runner", Status: "online", Online: true,
	}
}

// fixtureProjectRunners is the project-level listing. Runner 3 is returned by
// TWO projects, which is what exercises the once-only emission and the
// deterministic choice of parent.
func fixtureProjectRunners() map[int64]pages[gl.Runner] {
	shared := &gl.Runner{ID: 3, Description: "shared runner", Status: "online", Online: true}
	return map[int64]pages[gl.Runner]{
		apiID: {{
			{ID: 2, Description: "api runner", Status: "offline", Paused: true},
			shared,
		}},
		webID: {{shared}},
	}
}

// fixtureRunnerDetails is the per-runner detail read, which is the ONLY place a
// tag list comes back.
//
// RUNNERS 1 AND 2 SHARE `docker` AT TWO DIFFERENT SCOPES, which is what the
// distinct-tag rule has to deduplicate — a per-runner mint would produce that node
// twice. Runner 1 carries THREE tags, so the many-tags arm has a subject that is
// not also the shared one.
func fixtureRunnerDetails() map[int64]*gl.RunnerDetails {
	return map[int64]*gl.RunnerDetails{
		1: {ID: 1, TagList: []string{"docker", "linux", "arm64"}},
		2: {ID: 2, TagList: []string{"docker"}},
		3: {ID: 3, TagList: []string{"shared"}},
	}
}

// fixtureEnvironments is the api project's environments. `canary` is deliberately
// ABSENT: a protected environment names it, which is the one edge in this graph
// whose SOURCE can dangle.
func fixtureEnvironments() []*gl.Environment {
	return []*gl.Environment{
		{
			ID: 10, Name: "production", State: "available", Tier: "production",
			ExternalURL: "https://api.example.com",
		},
		{ID: 11, Name: "staging", State: "available"},
	}
}

// fixtureProtectedEnvironments carries one rule that requires approvals for an
// environment the listing DID return, one for an environment it did NOT, and one
// requiring NO approvals — which must produce nothing at all.
func fixtureProtectedEnvironments() []*gl.ProtectedEnvironment {
	return []*gl.ProtectedEnvironment{
		{Name: "production", RequiredApprovalCount: 2},
		{Name: "canary", RequiredApprovalCount: 1},
		{Name: "staging", RequiredApprovalCount: 0},
	}
}

// fixtureJobs is the job listing under the recorded run: one job the provider
// says a runner executed, and one it does not.
func fixtureJobs() []*gl.Job {
	executed := &gl.Job{
		ID: 900, Name: "build", Stage: "build", Status: "success", Ref: "main",
		TagList: []string{"docker"},
	}
	executed.Runner.ID = 2
	return []*gl.Job{
		executed,
		{ID: 901, Name: "deploy", Stage: "deploy", Status: "created", Ref: "main"},
	}
}

// fixtureProjectVariables is the project-scoped variable listing, with a planted
// VALUE that no byte of the output may carry.
func fixtureProjectVariables() map[int64]pages[gl.ProjectVariable] {
	return map[int64]pages[gl.ProjectVariable]{
		apiID: {{
			{Key: "API_KEY", Value: projectVarValue, Protected: true, Masked: true},
			{Key: "DEPLOY_KEY", Value: projectVarValue},
		}},
	}
}

// encodedFile is a repository file as the provider returns it: base64, with the
// encoding named.
func encodedFile(body string) *gl.File {
	return &gl.File{
		FileName: ".gitlab-ci.yml",
		FilePath: ".gitlab-ci.yml",
		Encoding: "base64",
		Content:  base64.StdEncoding.EncodeToString([]byte(body)),
	}
}
