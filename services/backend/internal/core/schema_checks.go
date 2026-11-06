package core

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes/scheme"
)

// Schema findings need no cluster or network access. Built-in Kubernetes kinds
// are decoded strictly against the client-go types (the same check
// kubeconform performs for core resources). Custom resources have no local
// schema and are validated by the cluster dry-run instead.
var strictDecoder = json.NewSerializerWithOptions(json.DefaultMetaFactory, scheme.Scheme, scheme.Scheme, json.SerializerOptions{Strict: true})

var typeMismatch = regexp.MustCompile(`Go struct field ([A-Za-z0-9_.\[\]-]+) of type ([A-Za-z0-9_.\[\]*]+)`)

type schemaFinding struct{ rule, evidence, remediation string }

const (
	ruleInvalidSchema    = "invalid-schema"
	ruleInvalidMetadata  = "invalid-metadata"
	ruleSelectorMismatch = "selector-mismatch"
)

// Evidence names fields and types only, never values, so Secret contents and
// credentials cannot leak into findings or audit events.
func checkResourceSchema(r Resource, manifest map[string]any) []schemaFinding {
	findings := metadataFindings(r, manifest)
	object, _, err := strictDecoder.Decode(r.Manifest, nil, nil)
	switch {
	case err == nil:
		findings = append(findings, selectorFindings(object)...)
	case runtime.IsNotRegisteredError(err), runtime.IsMissingKind(err), runtime.IsMissingVersion(err):
		// Custom resource, unknown API group, or an incomplete fixture: no local schema.
	default:
		var strict interface{ Errors() []error }
		if errors.As(err, &strict) {
			for _, item := range strict.Errors() {
				findings = append(findings, schemaFinding{ruleInvalidSchema, truncate(item.Error()), "Fix the field name or type in the manifest; Kubernetes rejects unknown and mistyped fields."})
			}
		} else {
			// Decoder errors may quote manifest content; report only the field path and type.
			evidence := "manifest does not match the Kubernetes API schema"
			if m := typeMismatch.FindStringSubmatch(err.Error()); m != nil {
				evidence = fmt.Sprintf("field %s must be of type %s", m[1], m[2])
			}
			findings = append(findings, schemaFinding{ruleInvalidSchema, evidence, "Fix the field's type to match the Kubernetes API schema."})
		}
	}
	return findings
}

func truncate(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 300 {
		return value[:300] + "…"
	}
	return value
}

func metadataFindings(r Resource, manifest map[string]any) []schemaFinding {
	var findings []schemaFinding
	add := func(evidence string) {
		findings = append(findings, schemaFinding{ruleInvalidMetadata, evidence, "Use names and labels that satisfy Kubernetes naming rules."})
	}
	name := r.Identity.Name
	if name == "" {
		add("metadata.name is empty")
	} else if r.Identity.APIVersion == "v1" || r.Identity.APIVersion == "apps/v1" || r.Identity.APIVersion == "batch/v1" || r.Identity.APIVersion == "networking.k8s.io/v1" {
		// RBAC and other groups allow wider name syntax, so only check kinds
		// whose names must be DNS labels or subdomains.
		check := validation.IsDNS1123Subdomain
		switch r.Identity.Kind {
		case "Namespace":
			check = validation.IsDNS1123Label
		case "Service":
			check = validation.IsDNS1035Label
		case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job", "CronJob", "ConfigMap", "Secret", "ServiceAccount", "PersistentVolumeClaim", "Ingress", "Pod":
		default:
			check = nil
		}
		if check != nil {
			for _, problem := range check(name) {
				add(fmt.Sprintf("metadata.name %q is invalid: %s", truncate(name), problem))
			}
		}
	}
	meta, _ := manifest["metadata"].(map[string]any)
	if values, ok := meta["labels"].(map[string]any); ok {
		for key, value := range values {
			if problems := validation.IsQualifiedName(key); len(problems) > 0 {
				add(fmt.Sprintf("metadata.labels key %q is invalid", truncate(key)))
			}
			text, isString := value.(string)
			if !isString {
				add(fmt.Sprintf("metadata.labels[%q] must be a string", truncate(key)))
			} else if problems := validation.IsValidLabelValue(text); len(problems) > 0 {
				add(fmt.Sprintf("metadata.labels[%q] has an invalid value", truncate(key)))
			}
		}
	}
	return findings
}

// A workload whose selector does not match its pod template labels is
// rejected by the API server; a common mistake after editing kustomize labels.
func selectorFindings(object runtime.Object) []schemaFinding {
	var selector *metav1.LabelSelector
	var templateLabels map[string]string
	switch v := object.(type) {
	case *appsv1.Deployment:
		selector, templateLabels = v.Spec.Selector, v.Spec.Template.Labels
	case *appsv1.StatefulSet:
		selector, templateLabels = v.Spec.Selector, v.Spec.Template.Labels
	case *appsv1.DaemonSet:
		selector, templateLabels = v.Spec.Selector, v.Spec.Template.Labels
	case *appsv1.ReplicaSet:
		selector, templateLabels = v.Spec.Selector, v.Spec.Template.Labels
	case *batchv1.Job:
		if v.Spec.Selector == nil {
			return nil
		}
		selector, templateLabels = v.Spec.Selector, v.Spec.Template.Labels
	default:
		return nil
	}
	if selector == nil {
		return []schemaFinding{{ruleSelectorMismatch, "spec.selector is required", "Add spec.selector matching spec.template.metadata.labels."}}
	}
	parsed, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return []schemaFinding{{ruleSelectorMismatch, "spec.selector is invalid", "Fix the selector's matchLabels and matchExpressions."}}
	}
	if parsed.Empty() || !parsed.Matches(labels.Set(templateLabels)) {
		return []schemaFinding{{ruleSelectorMismatch, "spec.selector does not match spec.template.metadata.labels", "Make the pod template carry every label the selector requires."}}
	}
	return nil
}
