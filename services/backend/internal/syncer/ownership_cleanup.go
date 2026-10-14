package syncer

import (
	"context"
	"fmt"
	"sort"
	"strings"

	jsonpatch "gopkg.in/evanphx/json-patch.v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"
	"sigs.k8s.io/structured-merge-diff/v6/value"
)

// Force only transfers fields included in an Apply request. Fields omitted from
// that request can remain owned by the same manager's earlier Update operation.
// Remove those leftovers and retire the Update entry, preserving shared fields.
// Planning submits the complete predicted result as one dry-run merge patch:
// separate dry-run requests cannot see each other's ownership changes.
func finishOwnershipMigration(ctx context.Context, rc dynamic.ResourceInterface, name string, applied *unstructured.Unstructured, options metav1.PatchOptions) (*unstructured.Unstructured, error) {
	if applied == nil {
		return applied, nil
	}
	cleaned, changed, err := cleanUpdateOwnership(applied, options.FieldManager)
	if err != nil || !changed {
		return applied, err
	}
	base := applied
	if len(options.DryRun) > 0 {
		base, err = rc.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		if base.GetUID() != applied.GetUID() || base.GetResourceVersion() != applied.GetResourceVersion() {
			return nil, fmt.Errorf("ownership migration target %s changed during dry-run; create a new plan", name)
		}
	}
	original, err := base.MarshalJSON()
	if err != nil {
		return nil, err
	}
	modified, err := cleaned.MarshalJSON()
	if err != nil {
		return nil, err
	}
	patch, err := jsonpatch.CreateMergePatch(original, modified)
	if err != nil {
		return nil, err
	}
	// Always include the optimistic concurrency precondition, even when it
	// would otherwise disappear from the diff because the value is unchanged.
	precondition := &unstructured.Unstructured{Object: map[string]interface{}{"metadata": map[string]interface{}{"resourceVersion": base.GetResourceVersion()}}}
	raw, err := precondition.MarshalJSON()
	if err != nil {
		return nil, err
	}
	patch, err = jsonpatch.MergePatch(patch, raw)
	if err != nil {
		return nil, err
	}
	options.Force = nil // Force is only valid for Apply patches.
	return rc.Patch(ctx, name, types.MergePatchType, patch, options)
}

func cleanUpdateOwnership(object *unstructured.Unstructured, manager string) (*unstructured.Unstructured, bool, error) {
	stale, retained := fieldpath.NewSet(), fieldpath.NewSet()
	entries := object.GetManagedFields()
	needsMigration := false
	for _, entry := range entries {
		if entry.Manager == manager && entry.Operation == metav1.ManagedFieldsOperationUpdate && entry.Subresource == "" {
			needsMigration = true
		}
	}
	if !needsMigration {
		return object, false, nil
	}
	filtered := make([]metav1.ManagedFieldsEntry, 0, len(entries))
	hasUpdate, hasApply := false, false
	for _, entry := range entries {
		if entry.Subresource == "status" {
			filtered = append(filtered, entry)
			continue
		}
		if entry.FieldsV1 == nil || entry.FieldsType != "FieldsV1" || entry.APIVersion != object.GetAPIVersion() {
			// Comparing ownership from different API versions requires schema
			// conversion. Refuse an unsafe cleanup instead of guessing.
			return nil, false, fmt.Errorf("cannot migrate %s field ownership in API version %s", entry.Manager, entry.APIVersion)
		}
		fields := fieldpath.NewSet()
		if err := fields.FromJSON(strings.NewReader(string(entry.FieldsV1.Raw))); err != nil {
			return nil, false, err
		}
		if entry.Manager == manager && entry.Operation == metav1.ManagedFieldsOperationUpdate && entry.Subresource == "" {
			hasUpdate = true
			stale = stale.Union(fields)
			continue
		}
		if entry.Manager == manager && entry.Operation == metav1.ManagedFieldsOperationApply && entry.Subresource == "" {
			hasApply = true
		}
		retained = retained.Union(fields)
		filtered = append(filtered, entry)
	}
	if !hasUpdate || !hasApply {
		return object, false, nil
	}
	cleaned := object.DeepCopy()
	var paths []fieldpath.Path
	stale.Difference(retained).Iterate(func(path fieldpath.Path) {
		if len(path) == 0 || path[0].FieldName == nil {
			return
		}
		if *path[0].FieldName == "status" {
			return
		}
		if *path[0].FieldName == "metadata" {
			if len(path) < 2 || path[1].FieldName == nil || (*path[1].FieldName != "labels" && *path[1].FieldName != "annotations" && *path[1].FieldName != "ownerReferences" && *path[1].FieldName != "finalizers") {
				return
			}
		}
		// A granular map/list membership marker does not authorize deleting
		// a whole parent when another manager or the new Apply owns a child.
		children := retained
		for _, element := range path {
			children = children.WithPrefix(element)
		}
		if children.Empty() {
			paths = append(paths, append(fieldpath.Path(nil), path...))
		}
	})
	sort.Slice(paths, func(i, j int) bool {
		if len(paths[i]) != len(paths[j]) {
			return len(paths[i]) < len(paths[j])
		}
		return paths[i].Compare(paths[j]) > 0
	})
	for _, path := range paths {
		removeOwnedPath(cleaned.Object, path)
	}
	cleaned.SetManagedFields(filtered)
	return cleaned, true, nil
}

// ManagedFields carries the server's list semantics, including composite keys
// and set values. Never identify volume mounts by volume name: mountPath is
// their Kubernetes list key, and a volume can legitimately be mounted twice.
func removeOwnedPath(node interface{}, path fieldpath.Path) (interface{}, bool) {
	if len(path) == 0 {
		return nil, true
	}
	element := path[0]
	switch parent := node.(type) {
	case map[string]interface{}:
		if element.FieldName != nil {
			key := *element.FieldName
			if child, exists := parent[key]; exists {
				if updated, remove := removeOwnedPath(child, path[1:]); remove {
					delete(parent, key)
				} else {
					parent[key] = updated
				}
			}
		}
	case []interface{}:
		for index, child := range parent {
			matches := element.Index != nil && *element.Index == index
			if element.Value != nil {
				matches = value.Equals(value.NewValueInterface(child), *element.Value)
			}
			if element.Key != nil {
				item, ok := child.(map[string]interface{})
				matches = ok
				for _, field := range *element.Key {
					v, exists := item[field.Name]
					matches = matches && exists && value.Equals(value.NewValueInterface(v), field.Value)
				}
			}
			if matches {
				if updated, remove := removeOwnedPath(child, path[1:]); remove {
					parent = append(parent[:index], parent[index+1:]...)
				} else {
					parent[index] = updated
				}
				break
			}
		}
		return parent, false
	}
	return node, false
}
