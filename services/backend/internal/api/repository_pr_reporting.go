package api

import (
	"context"
	"fmt"
	"html"

	"github.com/justlab/justcd/services/backend/internal/scm"
	"github.com/justlab/justcd/services/backend/internal/store"
)

func repositoryPRBindingError(mode, file, namespace string) error {
	if mode == "review-only" {
		return fmt.Errorf("%s: cannot prepare a live review for namespace %q without an existing workspace binding; review-only does not register bindings or create namespaces. Grant this workspace access to the namespace to review its plan", file, namespace)
	}
	if mode == "isolated" {
		return fmt.Errorf("%s: declared destination namespace %q requires an existing workspace binding for isolated PR discovery", file, namespace)
	}
	return fmt.Errorf("%s: namespace %q has no workspace binding; automatic registration requires Allow creation of matching dev namespaces in repository settings and createNamespaces: true in justcd.yaml", file, namespace)
}

// Discovery can fail before an application or review exists. Report directly
// through the repository's trusted provider connection, deduplicating retries.
func (s *Server) reportRepositoryPRDiscoveryFailure(ctx context.Context, client scm.Client, connection store.SourceControlConnection, repo store.RepositoryConfiguration, event scm.Event, token string, failure error) error {
	if connection.CommentMode == "off" || (event.Draft && !connection.CommentOnDrafts) {
		return nil
	}
	return s.Store.UpdatePRComment(ctx, client, connection, event.Number, token, "discovery:"+repo.ID, discoveryFailureComment(event, failure), true)
}

func discoveryFailureComment(event scm.Event, failure error) string {
	message := []rune(failure.Error())
	if len(message) > 6000 {
		message = append(message[:6000], []rune("… (see repository discovery settings for the complete error)")...)
	}
	sha := event.HeadSHA
	if len(sha) > 8 {
		sha = sha[:8]
	}
	body := "### Application discovery blocked\n\nCommit: `" + sha + "`\n\nJustCD could not register an application for review from this PR/MR.\n\n<details>\n<summary>Discovery error details</summary>\n\n<pre>" + html.EscapeString(string(message)) + "</pre>\n\n</details>\n\nCorrect the definition or repository/workspace settings, then run **Discover now**. No deployment was attempted by this discovery scan.\n"
	return body
}
