// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// walk_storage.go — the storage kinds, the two scaling and availability kinds,
// and the CustomResourceDefinition listing that the custom-resource walk keys
// off.

func walkPersistentVolumeClaims(ctx context.Context, c clientBundle, w *walkResult) error {
	list, err := c.typed.CoreV1().PersistentVolumeClaims("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return listErr(w, "PersistentVolumeClaim", err)
	}
	noteContinue(w, "PersistentVolumeClaim", list.Continue)
	for i := range list.Items {
		pvc := &list.Items[i]
		extra := map[string]string{"phase": string(pvc.Status.Phase)}
		if sc := deref(pvc.Spec.StorageClassName); sc != "" {
			extra["storage_class"] = sc
		}
		if err := add(w, metaOf(pvc.ObjectMeta), "PersistentVolumeClaim", extra, pvc); err != nil {
			return err
		}
		id := resourceID(pvc.Namespace, "PersistentVolumeClaim", pvc.Name)
		// BOUND_TO: only a BOUND claim names a volume. A pending claim has no
		// volume name and gets no edge.
		if pvc.Spec.VolumeName != "" {
			w.addEdges(Edge{
				FromID: id,
				ToID:   resourceID("", "PersistentVolume", pvc.Spec.VolumeName),
				Type:   edgeBoundTo,
			})
		}
		if sc := deref(pvc.Spec.StorageClassName); sc != "" {
			w.addEdges(Edge{FromID: id, ToID: resourceID("", "StorageClass", sc), Type: edgeUsesStorageClass})
		}
	}
	return nil
}

func walkPersistentVolumes(ctx context.Context, c clientBundle, w *walkResult) error {
	list, err := c.typed.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return listErr(w, "PersistentVolume", err)
	}
	noteContinue(w, "PersistentVolume", list.Continue)
	for i := range list.Items {
		pv := &list.Items[i]
		extra := map[string]string{
			"phase":         string(pv.Status.Phase),
			"storage_class": pv.Spec.StorageClassName,
		}
		// The disk handle feeds the USES_DISK proxy family. It is lifted here
		// rather than re-parsed there so one function owns the five source
		// shapes a volume can name a cloud disk through.
		if handle, driver := volumeHandle(pv); handle != "" {
			extra["volume_handle"] = handle
			extra["volume_driver"] = driver
		}
		if err := add(w, metaOf(pv.ObjectMeta), "PersistentVolume", extra, pv); err != nil {
			return err
		}
		if pv.Spec.StorageClassName != "" {
			w.addEdges(Edge{
				FromID: resourceID("", "PersistentVolume", pv.Name),
				ToID:   resourceID("", "StorageClass", pv.Spec.StorageClassName),
				Type:   edgeUsesStorageClass,
			})
		}
	}
	return nil
}

// volumeHandle extracts the cloud disk a PersistentVolume is backed by, from
// the CSI source or from one of the in-tree sources that predate CSI, and
// returns the handle beside the driver that named it.
//
// THE IN-TREE ARMS ARE NOT DEAD CODE. Clusters upgraded across the CSI
// migration still carry volumes whose spec names the old source, and a walk
// reading only the CSI arm reports them as backed by nothing.
func volumeHandle(pv *corev1.PersistentVolume) (handle, driver string) {
	switch {
	case pv.Spec.CSI != nil && pv.Spec.CSI.VolumeHandle != "":
		return pv.Spec.CSI.VolumeHandle, pv.Spec.CSI.Driver
	case pv.Spec.AWSElasticBlockStore != nil && pv.Spec.AWSElasticBlockStore.VolumeID != "":
		return pv.Spec.AWSElasticBlockStore.VolumeID, "kubernetes.io/aws-ebs"
	case pv.Spec.GCEPersistentDisk != nil && pv.Spec.GCEPersistentDisk.PDName != "":
		return pv.Spec.GCEPersistentDisk.PDName, "kubernetes.io/gce-pd"
	case pv.Spec.AzureDisk != nil && pv.Spec.AzureDisk.DataDiskURI != "":
		return pv.Spec.AzureDisk.DataDiskURI, "kubernetes.io/azure-disk"
	default:
		return "", ""
	}
}

func walkStorageClasses(ctx context.Context, c clientBundle, w *walkResult) error {
	list, err := c.typed.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return listErr(w, "StorageClass", err)
	}
	noteContinue(w, "StorageClass", list.Continue)
	for i := range list.Items {
		sc := &list.Items[i]
		if err := add(w, metaOf(sc.ObjectMeta), "StorageClass", map[string]string{
			"provisioner":    sc.Provisioner,
			"reclaim_policy": string(deref(sc.ReclaimPolicy)),
		}, sc); err != nil {
			return err
		}
	}
	return nil
}

func walkHorizontalPodAutoscalers(ctx context.Context, c clientBundle, w *walkResult) error {
	list, err := c.typed.AutoscalingV2().HorizontalPodAutoscalers("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return listErr(w, "HorizontalPodAutoscaler", err)
	}
	noteContinue(w, "HorizontalPodAutoscaler", list.Continue)
	for i := range list.Items {
		h := &list.Items[i]
		if err := add(w, metaOf(h.ObjectMeta), "HorizontalPodAutoscaler", map[string]string{
			"min_replicas": itoa(deref(h.Spec.MinReplicas)),
			"max_replicas": itoa(h.Spec.MaxReplicas),
		}, h); err != nil {
			return err
		}
		// SCALES: an autoscaler scales the workload its scaleTargetRef names,
		// which is always in the autoscaler's own namespace.
		if ref := h.Spec.ScaleTargetRef; ref.Name != "" && ref.Kind != "" {
			w.addEdges(Edge{
				FromID: resourceID(h.Namespace, "HorizontalPodAutoscaler", h.Name),
				ToID:   resourceID(h.Namespace, ref.Kind, ref.Name),
				Type:   edgeScales,
			})
		}
	}
	return nil
}

func walkPodDisruptionBudgets(ctx context.Context, c clientBundle, w *walkResult) error {
	list, err := c.typed.PolicyV1().PodDisruptionBudgets("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return listErr(w, "PodDisruptionBudget", err)
	}
	noteContinue(w, "PodDisruptionBudget", list.Continue)
	for i := range list.Items {
		p := &list.Items[i]
		extra := map[string]string{
			"disruptions_allowed": itoa(p.Status.DisruptionsAllowed),
		}
		sel, err := selectorJSON(p.Spec.Selector)
		if err != nil {
			return err
		}
		if sel != "" {
			extra["selector"] = sel
		}
		if err := add(w, metaOf(p.ObjectMeta), "PodDisruptionBudget", extra, p); err != nil {
			return err
		}
	}
	return nil
}

func walkCustomResourceDefinitions(ctx context.Context, c clientBundle, w *walkResult) error {
	list, err := c.apiext.ApiextensionsV1().CustomResourceDefinitions().List(ctx, metav1.ListOptions{})
	if err != nil {
		return listErr(w, "CustomResourceDefinition", err)
	}
	noteContinue(w, "CustomResourceDefinition", list.Continue)
	for i := range list.Items {
		crd := &list.Items[i]
		if err := add(w, metaOf(crd.ObjectMeta), "CustomResourceDefinition", map[string]string{
			"group":     crd.Spec.Group,
			"crd_kind":  crd.Spec.Names.Kind,
			"crd_scope": string(crd.Spec.Scope),
		}, crd); err != nil {
			return err
		}
	}
	return nil
}
