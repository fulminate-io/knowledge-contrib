// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// walk_workloads.go — Namespaces, Nodes, and the seven kinds that carry a pod
// template.
//
// EVERY POD-TEMPLATE KIND EMITS THE SAME FOUR EDGES through podTemplateEdges,
// and they emit them from THEMSELVES rather than from the pods they create. A
// Deployment's relationship to the ConfigMap its template mounts is a property
// of the Deployment; the pods carry it too, and both are true.

func walkNamespaces(ctx context.Context, c clientBundle, w *walkResult) error {
	list, err := c.typed.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return listErr(w, "Namespace", err)
	}
	noteContinue(w, "Namespace", list.Continue)
	for i := range list.Items {
		ns := &list.Items[i]
		if err := add(w, metaOf(ns.ObjectMeta), "Namespace", map[string]string{
			"phase": string(ns.Status.Phase),
		}, ns); err != nil {
			return err
		}
	}
	return nil
}

func walkNodes(ctx context.Context, c clientBundle, w *walkResult) error {
	list, err := c.typed.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return listErr(w, "Node", err)
	}
	noteContinue(w, "Node", list.Continue)
	for i := range list.Items {
		n := &list.Items[i]
		// provider_id is what the BACKED_BY_VM family parses the cloud VM out
		// of. It is lifted to metadata here so one walker owns reading it and
		// the derivation never re-parses the object body.
		extra := map[string]string{
			"provider_id":      n.Spec.ProviderID,
			"kubelet_version":  n.Status.NodeInfo.KubeletVersion,
			"os_image":         n.Status.NodeInfo.OSImage,
			"unschedulable":    fmt.Sprint(n.Spec.Unschedulable),
			"instance_address": nodeInternalIP(n),
		}
		if err := add(w, metaOf(n.ObjectMeta), "Node", extra, n); err != nil {
			return err
		}
	}
	return nil
}

func nodeInternalIP(n *corev1.Node) string {
	for _, a := range n.Status.Addresses {
		if a.Type == corev1.NodeInternalIP {
			return a.Address
		}
	}
	return ""
}

func walkPods(ctx context.Context, c clientBundle, w *walkResult) error {
	list, err := c.typed.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return listErr(w, "Pod", err)
	}
	noteContinue(w, "Pod", list.Continue)
	for i := range list.Items {
		p := &list.Items[i]
		extra := map[string]string{
			"phase":     string(p.Status.Phase),
			"node_name": p.Spec.NodeName,
			"images":    strings.Join(containerImages(p.Spec), ","),
		}
		if p.Status.PodIP != "" {
			extra["pod_ip"] = p.Status.PodIP
		}
		if err := add(w, metaOf(p.ObjectMeta), "Pod", extra, p); err != nil {
			return err
		}
		id := resourceID(p.Namespace, "Pod", p.Name)
		// RUNS_ON: a SCHEDULED pod runs on a Node. An unscheduled pod carries
		// no node name and gets no edge — that is the gate.
		if p.Spec.NodeName != "" {
			w.addEdges(Edge{FromID: id, ToID: resourceID("", "Node", p.Spec.NodeName), Type: edgeRunsOn})
		}
		w.addEdges(podTemplateEdges(id, p.Namespace, p.Spec)...)
	}
	return nil
}

func walkDeployments(ctx context.Context, c clientBundle, w *walkResult) error {
	list, err := c.typed.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return listErr(w, "Deployment", err)
	}
	noteContinue(w, "Deployment", list.Continue)
	for i := range list.Items {
		d := &list.Items[i]
		extra, err := workloadMeta(d.Spec.Template.Spec, d.Spec.Selector)
		if err != nil {
			return err
		}
		extra["replicas"] = itoa(deref(d.Spec.Replicas))
		extra["ready_replicas"] = itoa(d.Status.ReadyReplicas)
		if err := add(w, metaOf(d.ObjectMeta), "Deployment", extra, d); err != nil {
			return err
		}
		w.addEdges(podTemplateEdges(resourceID(d.Namespace, "Deployment", d.Name), d.Namespace, d.Spec.Template.Spec)...)
	}
	return nil
}

func walkStatefulSets(ctx context.Context, c clientBundle, w *walkResult) error {
	list, err := c.typed.AppsV1().StatefulSets("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return listErr(w, "StatefulSet", err)
	}
	noteContinue(w, "StatefulSet", list.Continue)
	for i := range list.Items {
		s := &list.Items[i]
		extra, err := workloadMeta(s.Spec.Template.Spec, s.Spec.Selector)
		if err != nil {
			return err
		}
		extra["replicas"] = itoa(deref(s.Spec.Replicas))
		extra["service_name"] = s.Spec.ServiceName
		if err := add(w, metaOf(s.ObjectMeta), "StatefulSet", extra, s); err != nil {
			return err
		}
		id := resourceID(s.Namespace, "StatefulSet", s.Name)
		w.addEdges(podTemplateEdges(id, s.Namespace, s.Spec.Template.Spec)...)
		w.addEdges(volumeClaimTemplateEdges(id, s.Namespace, s.Name, deref(s.Spec.Replicas), s.Spec.VolumeClaimTemplates)...)
	}
	return nil
}

// volumeClaimTemplateEdges renders USES_PVC for a StatefulSet's per-replica
// claims.
//
// THEY ARE NOT IN THE POD TEMPLATE'S VOLUMES AT ALL. Kubernetes names them
// <template>-<set>-<ordinal> and creates one per replica, so a walk that read
// only the pod template would miss every storage relationship a StatefulSet
// has. The ordinals are derived from the DESIRED replica count, which is what
// the claims are created from.
func volumeClaimTemplateEdges(ownerID, namespace, setName string, replicas int32, templates []corev1.PersistentVolumeClaim) []Edge {
	var out []Edge
	for i := range templates {
		tmpl := &templates[i]
		if tmpl.Name == "" {
			continue
		}
		for ordinal := range replicas {
			claim := fmt.Sprintf("%s-%s-%d", tmpl.Name, setName, ordinal)
			out = append(out, Edge{
				FromID: ownerID,
				ToID:   resourceID(namespace, "PersistentVolumeClaim", claim),
				Type:   edgeUsesPVC,
				Method: "volumeClaimTemplate",
			})
		}
	}
	return out
}

func walkDaemonSets(ctx context.Context, c clientBundle, w *walkResult) error {
	list, err := c.typed.AppsV1().DaemonSets("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return listErr(w, "DaemonSet", err)
	}
	noteContinue(w, "DaemonSet", list.Continue)
	for i := range list.Items {
		d := &list.Items[i]
		extra, err := workloadMeta(d.Spec.Template.Spec, d.Spec.Selector)
		if err != nil {
			return err
		}
		extra["desired_scheduled"] = itoa(d.Status.DesiredNumberScheduled)
		if err := add(w, metaOf(d.ObjectMeta), "DaemonSet", extra, d); err != nil {
			return err
		}
		w.addEdges(podTemplateEdges(resourceID(d.Namespace, "DaemonSet", d.Name), d.Namespace, d.Spec.Template.Spec)...)
	}
	return nil
}

func walkReplicaSets(ctx context.Context, c clientBundle, w *walkResult) error {
	list, err := c.typed.AppsV1().ReplicaSets("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return listErr(w, "ReplicaSet", err)
	}
	noteContinue(w, "ReplicaSet", list.Continue)
	for i := range list.Items {
		r := &list.Items[i]
		extra, err := workloadMeta(r.Spec.Template.Spec, r.Spec.Selector)
		if err != nil {
			return err
		}
		extra["replicas"] = itoa(deref(r.Spec.Replicas))
		if err := add(w, metaOf(r.ObjectMeta), "ReplicaSet", extra, r); err != nil {
			return err
		}
		w.addEdges(podTemplateEdges(resourceID(r.Namespace, "ReplicaSet", r.Name), r.Namespace, r.Spec.Template.Spec)...)
	}
	return nil
}

func walkJobs(ctx context.Context, c clientBundle, w *walkResult) error {
	list, err := c.typed.BatchV1().Jobs("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return listErr(w, "Job", err)
	}
	noteContinue(w, "Job", list.Continue)
	for i := range list.Items {
		j := &list.Items[i]
		extra, err := workloadMeta(j.Spec.Template.Spec, j.Spec.Selector)
		if err != nil {
			return err
		}
		extra["succeeded"] = itoa(j.Status.Succeeded)
		extra["failed"] = itoa(j.Status.Failed)
		if err := add(w, metaOf(j.ObjectMeta), "Job", extra, j); err != nil {
			return err
		}
		w.addEdges(podTemplateEdges(resourceID(j.Namespace, "Job", j.Name), j.Namespace, j.Spec.Template.Spec)...)
	}
	return nil
}

func walkCronJobs(ctx context.Context, c clientBundle, w *walkResult) error {
	list, err := c.typed.BatchV1().CronJobs("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return listErr(w, "CronJob", err)
	}
	noteContinue(w, "CronJob", list.Continue)
	for i := range list.Items {
		cj := &list.Items[i]
		spec := cj.Spec.JobTemplate.Spec.Template.Spec
		extra, err := workloadMeta(spec, nil)
		if err != nil {
			return err
		}
		extra["schedule"] = cj.Spec.Schedule
		extra["suspend"] = fmt.Sprint(deref(cj.Spec.Suspend))
		if err := add(w, metaOf(cj.ObjectMeta), "CronJob", extra, cj); err != nil {
			return err
		}
		w.addEdges(podTemplateEdges(resourceID(cj.Namespace, "CronJob", cj.Name), cj.Namespace, spec)...)
	}
	return nil
}
