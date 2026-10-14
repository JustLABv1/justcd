package syncer

import (
	"context"

	"github.com/justlab/justcd/services/backend/internal/core"
	"github.com/justlab/justcd/services/backend/internal/render"
)

func (s *Service) renderPlanResources(ctx context.Context, options render.Options, inputDigest string) ([]core.Resource, error) {
	if options.Renderer != "helm" && !(options.Renderer == "kustomize" && options.KustomizeHelmEnabled) {
		return render.Render(ctx, options)
	}
	// A fresh Helm invocation can generate different secrets and dependent
	// checksums from identical inputs. Rechecking approval must validate the
	// original artifact, rather than silently propose a new generated value.
	resources, found, err := s.Store.RenderArtifact(ctx, options.ApplicationID, inputDigest)
	if err != nil || found {
		return resources, err
	}
	resources, err = render.Render(ctx, options)
	if err != nil {
		return nil, err
	}
	return s.Store.SaveRenderArtifact(ctx, options.ApplicationID, inputDigest, resources)
}
