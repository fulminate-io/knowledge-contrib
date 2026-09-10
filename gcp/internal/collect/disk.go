// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"fmt"
	"strconv"

	computepb "cloud.google.com/go/compute/apiv1/computepb"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpcontent"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
)

// disk.go — persistent disks and their provenance.
//
// THE ATTACHMENT EDGE IS THE INSTANCE'S, NOT THE DISK'S. A disk's own Users list
// and an instance's own Disks list describe the same attachment, and emitting
// both would put two edges in the graph for one fact. The instance converter
// owns that edge; this one owns the disk's provenance and its encryption.

// Disks enumerates the project's persistent disks.
func Disks(list Lister[*computepb.Disk]) Subcollector {
	return New("gcp-disks", list, convertDisk)
}

func convertDisk(_ string, disk *computepb.Disk) (gcpgraph.Result, error) {
	if disk == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil disk")
	}
	selfLink := disk.GetSelfLink()
	if selfLink == "" {
		return gcpgraph.Result{}, fmt.Errorf("disk %q carries no self link", disk.GetName())
	}

	zone := gcpgraph.LastSegment(disk.GetZone())
	if zone == "" {
		zone = gcpgraph.LastSegment(disk.GetRegion())
	}
	fields := map[string]string{}
	setIfNotEmpty(fields, "sourceImage", disk.GetSourceImage())
	setIfNotEmpty(fields, "sourceSnapshot", disk.GetSourceSnapshot())
	setIfNotEmpty(fields, "kmsKeyName", disk.GetDiskEncryptionKey().GetKmsKeyName())
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name:       disk.GetName(),
		SelfLink:   selfLink,
		State:      disk.GetStatus(),
		Location:   zone,
		CreateTime: disk.GetCreationTimestamp(),
		Labels:     disk.GetLabels(),
		Fields:     fields,
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}

	metadata := map[string]string{
		"size_gb":     strconv.FormatInt(disk.GetSizeGb(), 10),
		"attachments": strconv.Itoa(len(disk.GetUsers())),
	}
	setIfNotEmpty(metadata, "status", disk.GetStatus())
	setIfNotEmpty(metadata, "disk_type", gcpgraph.LastSegment(disk.GetType()))
	setIfNotEmpty(metadata, "zone", zone)
	setIfNotEmpty(metadata, "creation_time", disk.GetCreationTimestamp())
	for k, v := range disk.GetLabels() {
		metadata["label/"+k] = v
	}

	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID:           selfLink,
		Name:         disk.GetName(),
		ResourceType: gcpgraph.ResourceTypeDisk,
		Region:       zone,
		Content:      raw,
		Metadata:     metadata,
	}}}

	if image := disk.GetSourceImage(); image != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: selfLink, To: image, Type: gcpgraph.EdgeFromImage,
		})
	}
	if snapshot := disk.GetSourceSnapshot(); snapshot != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: selfLink, To: snapshot, Type: gcpgraph.EdgeFromSnapshot,
		})
	}
	if key := disk.GetDiskEncryptionKey().GetKmsKeyName(); key != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: selfLink, To: key, Type: gcpgraph.EdgeEncryptsWith,
		})
	}
	return out, nil
}
