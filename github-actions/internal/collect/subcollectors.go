// SPDX-License-Identifier: Apache-2.0

package collect

// subcollectors.go — the seven enumerations, in one list.

// Caps bounds the two enumerations that read a repository's history rather than
// its current state. Both are collect parameters with the source provider's own
// defaults; see [DefaultMaxRuns] and [DefaultMaxDeployments].
type Caps struct {
	MaxRuns        int
	MaxDeployments int
}

// All builds every enumeration for one organization.
//
// THE ORDER IS THE SOURCE PROVIDER'S AND IT DECIDES NOTHING. The walk fans out
// over these concurrently and the graph builder sorts, so the list is in this
// order for a reader rather than for the result. A test runs them in a shuffled
// order and asserts the output is byte-identical, which is what keeps that true.
//
// EVERY ENUMERATION LISTS THE REPOSITORIES FOR ITSELF, and that is a deliberate
// trade rather than an oversight. Sharing one list would save six listings per
// collect and would make the fan-out a dependency graph: six enumerations could
// not start until the seventh finished, and a refusal on that one listing would
// take all seven down instead of one. The listing is cheap, paginated and
// cacheable at the provider; the coupling is not.
func All(api API, caps Caps) []Subcollector {
	maxRuns := caps.MaxRuns
	if maxRuns <= 0 {
		maxRuns = DefaultMaxRuns
	}
	maxDeployments := caps.MaxDeployments
	if maxDeployments <= 0 {
		maxDeployments = DefaultMaxDeployments
	}
	return []Subcollector{
		Repositories(api),
		Workflows(api),
		WorkflowRuns(api, maxRuns),
		Runners(api),
		Environments(api),
		Deployments(api, maxDeployments),
		Secrets(api),
	}
}
