// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"

	gl "gitlab.com/gitlab-org/api/client-go"
)

// project_lister.go — the ONE project discovery every per-project enumeration
// shares, and the subgroup recursion it hides.
//
// SIX OF THE SEVEN ENUMERATIONS WALK EVERY PROJECT, AND THEY SHARE THIS LIST.
// That is the source provider's own shape and it is kept deliberately: the group
// enumeration costs one pass rather than six, and — the part that matters more
// than the round trips — two collects of an unchanged group see the SAME project
// set, because they read it once. The sibling GitHub collector makes the
// opposite trade for its own reasons, listing repositories per enumeration so
// that a refusal on one listing costs one enumeration instead of seven; here the
// listing is a group-level read that either works or does not, so the shared
// cache costs nothing an operator would notice and buys the stable set.
//
// A FAILURE TO LIST THE GROUP IS NOT A PARTIAL READ. Six enumerations start from
// this list, so a walk that could not read it never found out what the group
// contains; reporting that as a partial read would put an empty inventory behind
// a mark an operator might reasonably ignore. A failure BELOW the top level — one
// subgroup of many — is a partial read, and it is reported rather than swallowed.

// maxRecursionDepth bounds the subgroup walk.
//
// IT IS THE SOURCE PROVIDER'S OWN LIMIT, carried across so the same group yields
// the same project set. What is NOT carried across is what happens when it is
// reached: that provider logs a warning and returns an empty list, so the
// projects below the limit vanish from a walk that still reports itself
// complete. Here the truncation is recorded and the walk that contains it is
// incomplete, naming the group it stopped under.
const maxRecursionDepth = 10

// projectLister discovers every project in a group and its subgroups, once.
type projectLister struct {
	api   GroupsAPI
	group string

	once     sync.Once
	projects []*gl.Project
	// partial is what the discovery could not see: a subgroup whose own listing
	// failed, or a subtree deeper than the recursion limit. It is NOT an error —
	// the projects that were found are still walked — and the walk folds it into
	// its completeness verdict once.
	partial []string
	err     error
}

// newProjectLister builds the lister one walk shares.
func newProjectLister(api GroupsAPI, group string) *projectLister {
	return &projectLister{api: api, group: group}
}

// list returns every project in the group, discovering them on the first call.
//
// THE RESULT IS SORTED BY PROJECT PATH, and that is load-bearing rather than
// tidy. The provider returns the top-level group's projects and each subgroup's
// in whatever order it likes, and one enumeration — the runners — emits a
// project-scoped resource ONCE under whichever project reached it first. Without
// a fixed order, which project a shared runner belongs to would be decided by the
// provider's paging order, so two collects of an unchanged group could disagree.
func (pl *projectLister) list(ctx context.Context) ([]*gl.Project, error) {
	pl.once.Do(func() {
		pl.projects, pl.partial, pl.err = pl.fetchAll(ctx)
		slices.SortStableFunc(pl.projects, func(a, b *gl.Project) int {
			return cmp.Or(
				cmp.Compare(a.PathWithNamespace, b.PathWithNamespace),
				cmp.Compare(a.ID, b.ID),
			)
		})
	})
	return pl.projects, pl.err
}

// Incompleteness is what the discovery could not see, read by the walk after the
// fan-out has finished.
//
// IT IS REPORTED ONCE, BY THE WALK, RATHER THAN BY EACH ENUMERATION. Seven
// enumerations share one discovery, so a subgroup this lister could not read
// would otherwise be named seven times in one walk's reason — the same fact
// reported once per reader, which reads like seven failures.
func (pl *projectLister) Incompleteness() []string { return slices.Clone(pl.partial) }

// fetchAll discovers the group's own projects and every subgroup's.
func (pl *projectLister) fetchAll(ctx context.Context) ([]*gl.Project, []string, error) {
	own, err := pl.listGroupProjects(ctx, pl.group)
	if err != nil {
		return nil, nil, fmt.Errorf("listing the projects of the group %q: %w", pl.group, err)
	}

	below, partial, err := pl.walkSubgroups(ctx, pl.group, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("listing the subgroups of %q: %w", pl.group, err)
	}
	return append(own, below...), partial, nil
}

// walkSubgroups discovers the projects of every subgroup below group.
//
// A FAILURE AT THIS LEVEL IS RETURNED and a failure BELOW it is recorded, which
// is the same split fetchAll makes: the caller's own listing failing means the
// caller learned nothing, while one child of many failing means the caller
// learned most of it.
func (pl *projectLister) walkSubgroups(
	ctx context.Context, group string, depth int,
) (projects []*gl.Project, partial []string, err error) {
	if depth >= maxRecursionDepth {
		return nil, []string{fmt.Sprintf(
			"the subgroups of %q are deeper than this collector walks (%d levels), so the projects "+
				"below them were not enumerated", group, maxRecursionDepth)}, nil
	}

	subgroups, err := pl.listSubgroups(ctx, group)
	if err != nil {
		return nil, nil, err
	}

	for _, sub := range subgroups {
		if sub == nil {
			continue
		}
		own, listErr := pl.listGroupProjects(ctx, sub.FullPath)
		if listErr != nil {
			partial = append(partial, fmt.Sprintf(
				"the projects of the subgroup %q: %v", sub.FullPath, listErr))
			continue
		}
		projects = append(projects, own...)

		below, belowPartial, walkErr := pl.walkSubgroups(ctx, sub.FullPath, depth+1)
		partial = append(partial, belowPartial...)
		if walkErr != nil {
			partial = append(partial, fmt.Sprintf(
				"the subgroups of %q: %v", sub.FullPath, walkErr))
			continue
		}
		projects = append(projects, below...)
	}
	return projects, partial, nil
}

// listGroupProjects pages one group's own projects to exhaustion.
//
// SUBGROUPS ARE EXCLUDED FROM THIS CALL and walked separately, which is the
// source provider's own choice: the recursion is what bounds the walk, and asking
// the provider to include subgroups would take that bound away.
func (pl *projectLister) listGroupProjects(ctx context.Context, group string) ([]*gl.Project, error) {
	includeSubGroups := false
	opts := &gl.ListGroupProjectsOptions{
		ListOptions:      gl.ListOptions{PerPage: perPage},
		IncludeSubGroups: &includeSubGroups,
	}
	var out []*gl.Project
	for {
		page, resp, err := pl.api.ListGroupProjects(ctx, group, opts)
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		// THE PROVIDER DECIDES WHEN THE PAGING ENDS, not the item count. A full
		// page is not the last page and a short page is not necessarily the last
		// one either; NextPage == 0 is the provider saying so, and reading the
		// count instead is the off-by-one that loses the whole tail of a group
		// with exactly 100 projects.
		if resp == nil || resp.NextPage == 0 {
			return out, nil
		}
		opts.Page = resp.NextPage
	}
}

// listSubgroups pages one group's immediate subgroups to exhaustion.
func (pl *projectLister) listSubgroups(ctx context.Context, group string) ([]*gl.Group, error) {
	opts := &gl.ListSubGroupsOptions{ListOptions: gl.ListOptions{PerPage: perPage}}
	var out []*gl.Group
	for {
		page, resp, err := pl.api.ListSubGroups(ctx, group, opts)
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		if resp == nil || resp.NextPage == 0 {
			return out, nil
		}
		opts.Page = resp.NextPage
	}
}
