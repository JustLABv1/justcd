package api

import (
	"context"
	"errors"
	"github.com/justlab/justcd/services/backend/internal/gitops"
	"github.com/justlab/justcd/services/backend/internal/scm"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func (s *Server) gitPRChangedPaths(ctx context.Context, source store.GitSource, connection store.SourceControlConnection, review store.PullRequestReview, token string) ([]string, error) {
	client := scm.Client{}
	current, err := client.Current(ctx, connection.Provider, connection.APIURL, connection.Repository, token, review.Number)
	if err != nil {
		return nil, errors.New("could not read the PR comparison base")
	}
	if current.HeadSHA != review.HeadSHA || current.Closed != review.Closed || current.Fork != review.Fork || current.BaseSHA == "" {
		return nil, errors.New("the PR comparison base is unavailable or the PR changed")
	}
	checkout, err := gitops.Fetch(ctx, s.Store, s.EncryptionKey, source, pullRequestRef(connection.Provider, review.Number))
	if err != nil {
		return nil, errors.New("could not fetch the PR using the Git source credential")
	}
	defer checkout.Close()
	paths, err := checkout.ChangedPaths(ctx, current.BaseSHA, review.HeadSHA, current.BaseIsMergeBase)
	if err != nil {
		return nil, err
	}
	latest, err := client.Current(ctx, connection.Provider, connection.APIURL, connection.Repository, token, review.Number)
	if err != nil || latest.HeadSHA != current.HeadSHA || latest.BaseSHA != current.BaseSHA || latest.Closed != current.Closed || latest.Fork != current.Fork {
		return nil, errors.New("the PR changed during the Git comparison")
	}
	return paths, nil
}
