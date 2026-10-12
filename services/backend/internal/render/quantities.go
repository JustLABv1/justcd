package render

import (
	"encoding/json"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
)

// Normalize only known built-in quantity fields, and only when their values
// are equal. Retain the live spelling for review; callers supply comparison
// copies so neither the apply manifest nor live inventory is rewritten.
func normalizeEquivalentQuantities(desired, live map[string]interface{}) {
	apiVersion, _ := desired["apiVersion"].(string)
	kind, _ := desired["kind"].(string)
	group := ""
	if index := strings.IndexByte(apiVersion, '/'); index >= 0 {
		group = apiVersion[:index]
	}
	var podPath []string
	switch {
	case group == "" && kind == "Pod":
		podPath = []string{"spec"}
	case group == "" && kind == "PodTemplate":
		podPath = []string{"template", "spec"}
	case (group == "apps" && (kind == "Deployment" || kind == "StatefulSet" || kind == "DaemonSet" || kind == "ReplicaSet")) ||
		(group == "" && kind == "ReplicationController") || (group == "batch" && kind == "Job"):
		podPath = []string{"spec", "template", "spec"}
	case group == "batch" && kind == "CronJob":
		podPath = []string{"spec", "jobTemplate", "spec", "template", "spec"}
	case group == "" && kind == "PersistentVolumeClaim":
		normalizeResourceRequirements(quantityMapAt(desired, "spec", "resources"), quantityMapAt(live, "spec", "resources"))
	case group == "" && kind == "PersistentVolume":
		normalizeQuantityMap(quantityMapAt(desired, "spec", "capacity"), quantityMapAt(live, "spec", "capacity"))
	case group == "" && kind == "ResourceQuota":
		normalizeQuantityMap(quantityMapAt(desired, "spec", "hard"), quantityMapAt(live, "spec", "hard"))
	}
	if podPath == nil {
		return
	}
	wanted, actual := quantityMapAt(desired, podPath...), quantityMapAt(live, podPath...)
	normalizeResourceRequirements(quantityMapAt(wanted, "resources"), quantityMapAt(actual, "resources"))
	normalizeQuantityMap(quantityMapAt(wanted, "overhead"), quantityMapAt(actual, "overhead"))
	for _, field := range []string{"containers", "initContainers", "ephemeralContainers"} {
		wantList, _ := wanted[field].([]interface{})
		liveList, _ := actual[field].([]interface{})
		liveByName := make(map[string]map[string]interface{}, len(liveList))
		for _, item := range liveList {
			container, _ := item.(map[string]interface{})
			if name, ok := container["name"].(string); ok {
				liveByName[name] = container
			}
		}
		for _, item := range wantList {
			container, _ := item.(map[string]interface{})
			name, _ := container["name"].(string)
			normalizeResourceRequirements(quantityMapAt(container, "resources"), quantityMapAt(liveByName[name], "resources"))
		}
	}
}

func quantityMapAt(object map[string]interface{}, path ...string) map[string]interface{} {
	for _, field := range path {
		object, _ = object[field].(map[string]interface{})
	}
	return object
}

func normalizeResourceRequirements(desired, live map[string]interface{}) {
	for _, field := range []string{"requests", "limits"} {
		normalizeQuantityMap(quantityMapAt(desired, field), quantityMapAt(live, field))
	}
}

func normalizeQuantityMap(desired, live map[string]interface{}) {
	for key, value := range desired {
		actual, exists := live[key]
		if !exists {
			continue
		}
		wantedQuantity, wantedOK := comparisonQuantity(value)
		liveQuantity, liveOK := comparisonQuantity(actual)
		if wantedOK && liveOK && wantedQuantity.Cmp(liveQuantity) == 0 {
			desired[key] = actual
		}
	}
}

func comparisonQuantity(value interface{}) (resource.Quantity, bool) {
	var text string
	switch typed := value.(type) {
	case string:
		text = typed
	case float64, int64, json.Number:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return resource.Quantity{}, false
		}
		text = string(encoded)
	default:
		return resource.Quantity{}, false
	}
	quantity, err := resource.ParseQuantity(text)
	return quantity, err == nil
}
