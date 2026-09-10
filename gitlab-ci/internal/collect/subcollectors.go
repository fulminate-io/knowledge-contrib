// SPDX-License-Identifier: Apache-2.0

package collect

// subcollectors.go — the seven enumerations, in one list, behind one project
// discovery.

// Caps bounds the two enumerations that read a project's history rather than its
// current state. Both are collect parameters with the source provider's own
// defaults; see [DefaultMaxPipelineRuns] and [DefaultMaxDeployments].
type Caps struct {
	MaxPipelineRuns int
	MaxDeployments  int
}

// Walk is one walk's enumerations together with the project discovery they
// share. The discovery is returned beside them because its own incompleteness is
// the walk's to report ONCE, not each enumeration's to repeat.
type Walk struct {
	Subcollectors []Subcollector
	Projects      *projectLister
}

// All builds every enumeration for one group.
//
// THE ORDER IS THE SOURCE PROVIDER'S AND IT DECIDES ALMOST NOTHING. The walk fans
// out over these concurrently and the graph builder sorts, so the list is in this
// order for a reader rather than for the result. A test runs them in a shuffled
// order and asserts the output is byte-identical, which is what keeps that true.
//
// EVERY PER-PROJECT ENUMERATION SHARES ONE PROJECT DISCOVERY, and that is a
// deliberate trade rather than an oversight. Six of the seven walk every project,
// so listing per enumeration would cost six passes over the group — and, more
// than the round trips, two collects of an unchanged group could see two
// different project sets. The sibling GitHub collector makes the opposite trade
// because its own listing is per-repository work that a refusal can take down
// alone; here the listing is one group-level read.
func All(api API, caps Caps, group string) Walk {
	maxRuns := caps.MaxPipelineRuns
	if maxRuns <= 0 {
		maxRuns = DefaultMaxPipelineRuns
	}
	maxDeployments := caps.MaxDeployments
	if maxDeployments <= 0 {
		maxDeployments = DefaultMaxDeployments
	}
	lister := newProjectLister(api.Groups, group)
	return Walk{
		Subcollectors: []Subcollector{
			Projects(api, lister, group),
			Pipelines(api, lister, group),
			PipelineRuns(api, lister, group, maxRuns),
			Runners(api, lister, group),
			Environments(api, lister, group),
			Deployments(api, lister, group, maxDeployments),
			Variables(api, lister, group),
		},
		Projects: lister,
	}
}
