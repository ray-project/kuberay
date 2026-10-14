package run

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// snapshot lists what a unit could leave behind. An error means the list is unknown, and sweep
// must not run on it: an empty list would make everything in the cluster look new.
func (r *runner) snapshot(ctx context.Context) ([]string, error) {
	out, err := r.output(ctx, "kubectl", "get",
		"namespaces,clusterroles,clusterrolebindings,customresourcedefinitions,mutatingwebhookconfigurations,validatingwebhookconfigurations",
		"-o", "name")
	if err != nil {
		return nil, fmt.Errorf("kubectl get: %w", err)
	}
	objs := strings.Fields(out)
	// Ray resources can be listed only while their CRDs are installed.
	var kinds []string
	for _, obj := range objs {
		if name, ok := strings.CutPrefix(obj, "customresourcedefinition.apiextensions.k8s.io/"); ok && strings.HasSuffix(name, ".ray.io") {
			kinds = append(kinds, name)
		}
	}
	if len(kinds) > 0 {
		out, err = r.output(ctx, "kubectl", "get", strings.Join(kinds, ","), "-A",
			"-o", `jsonpath={range .items[*]}{.kind}/{.metadata.namespace}/{.metadata.name}{"\n"}{end}`)
		if err != nil {
			return nil, fmt.Errorf("kubectl get ray resources: %w", err)
		}
		objs = append(objs, strings.Fields(out)...)
	}
	out, err = r.output(ctx, "helm", "list", "-A", "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("helm list: %w", err)
	}
	var releases []struct{ Name, Namespace string }
	if err := json.Unmarshal([]byte(out), &releases); err != nil {
		return nil, fmt.Errorf("helm list: %w", err)
	}
	for _, rel := range releases {
		objs = append(objs, "helm/"+rel.Namespace+"/"+rel.Name)
	}
	return objs, nil
}

// sweep deletes what the unit created and its cleanup left, and returns it for the report.
func (r *runner) sweep(ctx context.Context, before []string, log *os.File) []string {
	after, err := r.snapshot(ctx)
	if err != nil {
		fmt.Fprintf(log, "\nsweep skipped: %v\n", err)
		return nil
	}
	left := slices.DeleteFunc(after, func(obj string) bool { return slices.Contains(before, obj) })
	slices.Sort(left)
	var rest []string
	// Ray resources first, while the operator that removes their finalizers still runs.
	for _, obj := range left {
		if kind, namespace, name, ok := splitRayObject(obj); ok {
			r.toFile(ctx, log, "kubectl", "delete", strings.ToLower(kind)+".ray.io", name, "-n", namespace, "--timeout=60s")
		}
	}
	for _, obj := range left {
		if rel, ok := strings.CutPrefix(obj, "helm/"); ok {
			namespace, name, _ := strings.Cut(rel, "/")
			r.toFile(ctx, log, "helm", "uninstall", name, "-n", namespace, "--wait", "--timeout", "2m")
		} else if _, _, _, ray := splitRayObject(obj); !ray {
			rest = append(rest, obj)
		}
	}
	if len(rest) > 0 {
		// Wait, so the next unit can create a namespace of the same name.
		r.toFile(ctx, log, "kubectl", append([]string{"delete", "--timeout=120s"}, rest...)...)
	}
	return left
}

// splitRayObject splits "RayCluster/<namespace>/<name>".
func splitRayObject(obj string) (kind, namespace, name string, ok bool) {
	parts := strings.Split(obj, "/")
	if len(parts) != 3 || !strings.HasPrefix(parts[0], "Ray") {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

func (r *runner) evidence(ctx context.Context, dir string) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return
	}
	save := func(name string, args ...string) {
		file, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return
		}
		defer file.Close()
		r.toFile(ctx, file, "kubectl", args...)
	}
	save("pods.txt", "get", "pods", "-A", "-o", "wide")
	save("events.txt", "get", "events", "-A", "--sort-by=.lastTimestamp")
	save("ray-resources.yaml", "get", "rayclusters,rayjobs,rayservices,raycronjobs", "-A", "-o", "yaml")
	namespaces, _ := r.output(ctx, "kubectl", "get", "namespaces", "-o", "jsonpath={.items[*].metadata.name}")
	for namespace := range strings.FieldsSeq(namespaces) {
		if strings.HasPrefix(namespace, "kube-") || namespace == "local-path-storage" {
			continue
		}
		save("describe-pods.txt", "describe", "pods", "-n", namespace)
		pods, _ := r.output(ctx, "kubectl", "get", "pods", "-n", namespace, "-o", "name")
		for pod := range strings.FieldsSeq(pods) {
			save("pod-logs.txt", "logs", "-n", namespace, pod, "--all-containers", "--prefix", "--tail=500")
		}
	}
}
