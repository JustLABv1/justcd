package core

import (
	"encoding/json"
	"fmt"
	"slices"
)

// WorkloadImages returns the distinct image references in rendered Kubernetes workloads.
func WorkloadImages(resources []Resource) ([]string, error) {
	images := make(map[string]struct{})
	for _, resource := range resources {
		var manifest map[string]any
		if err := json.Unmarshal(resource.Manifest, &manifest); err != nil {
			return nil, fmt.Errorf("decode desired manifest for image inventory: %w", err)
		}
		pod := workloadPodSpec(resource.Identity, object(manifest, "spec"))
		for _, group := range []string{"containers", "initContainers", "ephemeralContainers"} {
			containers, _ := pod[group].([]any)
			for _, entry := range containers {
				container, _ := entry.(map[string]any)
				if image, _ := container["image"].(string); image != "" {
					images[image] = struct{}{}
				}
			}
		}
	}
	result := make([]string, 0, len(images))
	for image := range images {
		result = append(result, image)
	}
	slices.Sort(result)
	return result, nil
}

// IsDigestPinnedImage reports whether the image reference pins a SHA-256 digest.
func IsDigestPinnedImage(image string) bool {
	return immutableImage.MatchString(image)
}
