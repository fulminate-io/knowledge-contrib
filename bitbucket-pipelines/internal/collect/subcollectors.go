// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbclient"
)

// subcollectors.go — the six enumerations, in two phases.

// AfterRepos builds the five enumerations that run once the repository list is
// in hand. [Repositories] is the sixth and it runs before them, alone.
//
// THE REPOSITORY LIST IS SHARED RATHER THAN RE-LISTED PER ENUMERATION, and the
// trade is worth naming because the sibling CI/CD collector makes the opposite
// one. Sharing costs a dependency: these five cannot start until the repos read
// finishes, and a failure on that one listing takes all six down instead of one.
// What it buys here is the reason it is kept — four of the five need each
// repository's MAIN BRANCH as well as its slug, and a per-enumeration listing
// would have to re-read it; the source provider records the shared list as its
// own decision; and the two-phase shape is a floor this module reproduces rather
// than a choice it is free to make.
//
// THE ORDER DECIDES NOTHING. The walk fans out over these concurrently and the
// graph builder sorts, so the list is in this order for a reader rather than for
// the result.
func AfterRepos(client *bbclient.Client, repos []RepoInfo, historyDepth int) []Subcollector {
	return []Subcollector{
		PipelinesConfig(client, repos),
		PipelineRuns(client, repos, historyDepth),
		Runners(client, repos),
		Environments(client, repos),
		Variables(client, repos),
	}
}
