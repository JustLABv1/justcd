package core

import (
	"slices"
	"testing"
)

func TestWorkloadImages(t *testing.T) {
	resources := []Resource{
		{Identity: Identity{Kind: "Deployment", APIVersion: "apps/v1"}, Manifest: []byte(`{"spec":{"template":{"spec":{"containers":[{"image":"app:latest"}],"initContainers":[{"image":"setup:v1"}]}}}}`)},
		{Identity: Identity{Kind: "CronJob", APIVersion: "batch/v1"}, Manifest: []byte(`{"spec":{"jobTemplate":{"spec":{"template":{"spec":{"containers":[{"image":"app:latest"}]}}}}}}`)},
		{Identity: Identity{Kind: "Pod", APIVersion: "v1"}, Manifest: []byte(`{"spec":{"ephemeralContainers":[{"image":"debug:v1"}]}}`)},
		{Identity: Identity{Kind: "ConfigMap", APIVersion: "v1"}, Manifest: []byte(`{"spec":{"containers":[{"image":"ignored"}]}}`)},
	}
	images, err := WorkloadImages(resources)
	if err != nil || !slices.Equal(images, []string{"app:latest", "debug:v1", "setup:v1"}) {
		t.Fatalf("images: %v (%v)", images, err)
	}
}
