---
title: "Pull request plans and previews"
---

Open an application's **Pull requests** tab and choose **Enable PR reporting**
(or **Edit settings** for an existing connection). JustCD derives the provider
and repository from the application's existing Git source; you do not connect
the repository a second time. For a custom Git host, JustCD treats a saved
connection as self-managed GitLab, so verify that the host runs GitLab and
check its HTTPS API URL. The
review list remains the default view; reporting settings open only on
request. For GitLab.com JustCD uses
`https://gitlab.com/api/v4`; for a self hosted instance use its HTTPS API URL,
such as `https://gitlab.example.com/api/v4`. JustCD polls open PRs/MRs when
reporting is enabled and checks previously tracked reviews for closure every
two minutes. Choose an existing Git HTTPS credential or a separate API token.
Git fetches keep using the Git source credential. The API token reads PRs/MRs
and comments and writes review comments. Commit statuses are disabled by default
for both new and existing connections. Enable **Report commit statuses** explicitly
in application PR settings or repository PR discovery settings when desired. On
GitLab this adds an external job to a pipeline for the commit; pending or failed
JustCD statuses can block merges. Turning this off stops future status updates;
it does not remove statuses already attached to GitLab pipelines.
For GitHub, use a fine-grained token for the repository with **Pull requests:
read and write**; add **Commit statuses: read and write** only if status reporting
is enabled. For GitLab, use a project or
personal access token with the `api` scope and access to the project. A webhook
is optional for faster PR updates: set a secret in JustCD, then copy the
displayed URL and the same secret into the repository webhook settings and
enable pull request or merge request events. GitHub uses its webhook Secret
field; GitLab supports a signing token or legacy secret token. You can also
enable push events. A branch push triggers a fresh plan for every application in
the workspace tracking that repository and branch, including application-group
children. Manual applications stop at plan review; auto-safe applications keep
their existing safety and approval rules. The host running JustCD must trust the GitLab instance's
TLS certificate.

Workspace owners can disable PR reporting without deleting the connection or
its review history. Deleting the connection removes the saved credentials and
review history; remove any optional repository webhook separately. Active preview
deployments must be closed before either action.
The optional webhook can be removed independently in PR reporting settings;
polling, saved provider access, and review history remain available. Remove
the repository webhook at the Git provider as well.

PR handling can also be managed through `spec.pullRequests` in `justcd.yaml`,
using an existing HTTPS credential reference. See [declarative PR handling](/guides/repository-applications#pull-request-handling) for review-only, isolated, and shared-environment examples.

PR reporting is configured per application. Each configured application discovers
open PRs for the repository. Before planning, JustCD checks changed file paths
against that application's manifest paths, Helm values files, and optional
preview path. Renames check both old and new paths. PRs without a matching path
are hidden from that application's review list and do not receive its status.
Kustomize follows local bases, components, patches, replacements, and generator
files outside the overlay path, including Helm chart directories and values files.
Pinned remote Helm charts are supported. Unrelated applications are skipped. Remote inputs,
custom plugins, unpinned Helm charts, missing inputs, or unsupported dependency features
keep repository-wide checking to avoid hiding relevant changes. If a provider cannot return a complete file list, JustCD fetches the PR with
the Git source credential and compares its exact head to the PR merge base in
Git. Renamed and removed files are included; an empty diff is skipped. If the
base, Git history, or current PR state cannot be verified, JustCD keeps the PR
in scope and shows the failure reason. Status contexts include the application
ID so multiple applications do not overwrite each other's status.

For a Git source without a PR reporting connection, a workspace owner can configure
a push webhook in the Git source settings or with
`PUT /api/v1/git-sources/{sourceID}/push-webhook` and JSON
`{"secret":"at-least-16-characters"}`. The response contains a webhook URL.
That URL accepts native GitHub or GitLab push events using their normal webhook
signature headers, or a generic signed envelope from another provider or CI.
POST `{"ref":"refs/heads/main","after":"<40-character-commit-SHA>"}` to that URL,
setting `X-JustCD-Signature-256` to `sha256=` followed by the hex HMAC-SHA256
of the exact request body using the secret. Set `X-JustCD-Delivery` to a unique
delivery ID when available. This endpoint accepts branch updates only, not
tag or branch-deletion events. Delivery IDs are deduplicated; the audit trail
records the reported SHA and the commit actually resolved when planning.

Each PR or MR is checked against the provider's current head and rendered from
its `head` Git ref. JustCD verifies that the rendered commit equals the head
SHA reported by the provider. Without a preview profile, the resulting plan is
stored for review only and cannot be applied to production. A review comment on
the PR or MR links to the plan in JustCD.

PR deployment can be review-only, isolated per PR, or use the existing application
environment. Draft PRs follow the same workflow. Each new commit gets a fresh plan;
JustCD deploys automatically when policy permits and otherwise waits for approval.
The PR receives a plan summary, full-plan link, plan ID, and digest. Under **PR
comment approvals**, map numeric GitHub/GitLab account IDs to JustCD user IDs
using an account ID field and a workspace member selector. An eligible user can post
`/justcd approve <plan-id> <digest>`. JustCD checks their current workspace role,
approval threshold, plan expiry, and current PR head before deployment. Old plans
and approvals cannot approve another commit.

Existing-environment deployments require explicit confirmation and permit one PR
at a time, without forks. Reconciliation stays paused while the PR controls the
application. Closing or merging restores the tracked source and creates a return
plan for review; it does not undo migrations or data changes or resume automatically.

For isolated deployments, JustCD offers to reuse a matching isolated Test branch
preview before creating another environment. An owner can accept or choose a new
preview. Adoption validates the current PR head and preview profile and invalidates
old manual plans. Future commits use the PR lifecycle; cleanup retains the adopted
namespace, its binding, and untracked resources.

For isolated deployments, configure the application's preview profile. Provide a
preview manifest path or Helm values, a database strategy, a namespace prefix,
an ingress host suffix, CPU and memory quotas, maximum active previews, and a
maximum lifetime. The profile may reference secrets already provisioned inside
the preview namespace; JustCD never copies production secrets or databases.
The configured values or overlay must point to preview-safe data services.
For Helm values, `{{namespace}}`, `{{number}}`, and `{{sha}}` are replaced with
the preview namespace, PR/MR number, and exact head SHA. For example, an
Ingress host can be `web.{{namespace}}.preview.example.com`; every rendered
Ingress host and TLS host must be inside that namespace-specific domain. A
wildcard DNS record and certificate for the preview domain simplify routing.

JustCD creates a dedicated namespace and ResourceQuota for each preview. The
cluster needs a cluster-scope credential that can create and delete namespaces
and manage ResourceQuotas. The workspace also needs a namespace credential for
the generated application. Rendered cluster-scoped resources, Secret objects,
unexpected secret references, external-facing Services, and out-of-domain
Ingress hosts are rejected. Fork previews are disabled by default; opting in
requires owner approval and a profile with no allowed secrets. Fork workloads
must disable service account token mounting.

Closing, merging, or expiring a preview creates a normal decommission plan.
After that plan is applied and all JustCD-managed resources are gone, JustCD
deletes the owned namespace and releases its preview slot. Everything placed
in that namespace, including separately provisioned preview secrets, is
removed with it. Cleanup that cannot verify ownership remains pending for
review.
