// SPDX-License-Identifier: Apache-2.0

package resolve

import (
	"fmt"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpcontent"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
)

// MethodSharedVPC is the method discriminator on a Shared VPC linkage edge.
const MethodSharedVPC = "gcp-shared-vpc"

// SharedVPC derives Shared VPC linkage: a subnetwork whose parent network lives
// in ANOTHER project is a subnet shared into this one, and the edge runs FROM
// the host project's network TO the local subnet, so walking forward from a host
// VPC finds every subnet sharing it.
//
// ONLY THE LOCAL HALF IS EMITTED, and that is a consequence of the collector
// shape rather than a simplification. The built-in resolver writes the SAME edge
// into the HOST project's graph as well, which is a cross-graph write; a
// collector produces ONE graph, and a cross-collector cascade is not something
// this design has. The host project's own collect records that side, and until
// it runs the reference is unresolved — exactly how a cross-graph reference
// behaves everywhere else.
func SharedVPC(projectID string, in gcpgraph.Result) (gcpgraph.Result, error) {
	var out gcpgraph.Result
	for _, res := range in.Resources {
		if res.ResourceType != gcpgraph.ResourceTypeSubnetwork {
			continue
		}
		var spec gcpcontent.Subnetwork
		present, err := gcpcontent.Unmarshal(res.Content, &spec)
		if err != nil {
			return gcpgraph.Result{}, fmt.Errorf(
				"gcp shared-vpc resolver: reading subnetwork %q (id=%q): %w", res.Name, res.ID, err)
		}
		if !present || spec.Network == "" {
			continue
		}
		hostProject := gcpgraph.ProjectFromSelfLink(spec.Network)
		// An empty host project means the parent link was not a self-link this
		// collector understands, which is not the same as a same-project subnet
		// and yields nothing either way.
		if hostProject == "" || hostProject == projectID {
			continue
		}
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: spec.Network, To: res.ID, Type: gcpgraph.EdgeSharedWith,
			Method: MethodSharedVPC,
			Metadata: map[string]string{
				"host_project":    hostProject,
				"service_project": projectID,
			},
		})
	}
	return out, nil
}
