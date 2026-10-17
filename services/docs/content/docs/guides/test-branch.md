---
title: "Test a branch"
---

Application owners can use **Test branch** to select a branch, tag, or commit
and then create, review, and apply a normal deployment plan. Application group
targets and applications with an active rollback pin must be resolved first.

**Deploy branch to existing dev** temporarily replaces the application's Git
revision and pauses automatic reconciliation. It uses the existing environment,
credentials, and data, so confirmation is required. Repository discovery leaves
the complete application definition unchanged during the test, even if its base
definition changes or disappears. **Resume tracked source** restores the tracked
revision and refreshes a Git-managed definition. Reconciliation stays paused:
review and deploy the return plan before resuming it. Database migrations and
data changes are not reversed.

**Create isolated preview** creates a separate manual application in a new,
unused namespace with a repository-relative preview overlay or chart path.
It inherits the workspace cluster credential; configure preview-safe values,
secrets, ingress hosts, and databases in Git. External services are not copied
or isolated automatically. The preview can only manage namespaced resources
inside its own destination. Cluster-scoped manifests are rejected.

Remove this manual preview through its application settings. Resource deletion
uses the normal reviewed deletion plan and verifies resource ownership. The
parent application remains unchanged. The namespace and its workspace binding
remain available; remove them separately once any untracked resources have
been reviewed. These manual branch tests do not use the automatic PR preview
lifecycle described above.
