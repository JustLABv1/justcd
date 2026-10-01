package api

const openAPISpec = `openapi: 3.1.0
info:
  title: JustCD API
  version: 1.0.0
  description: JustCD control-plane API. Kubernetes and Git credential material is never returned. API errors use the versioned ErrorResponse schema and retain the legacy error string.
servers:
  - url: /api/v1
paths:
  /repository-configurations:
    get:
      summary: List application discovery connections (workspace viewer)
      parameters:
        - { name: workspaceId, in: query, required: true, schema: { type: string } }
      responses:
        '200': { description: Discovery connections with enabled state, lastCheckedAt, lastCommit, and lastError }
        '403': { description: Workspace access required }
    post:
      summary: Connect a repository revision and discover justcd.yaml applications (workspace owner)
      description: Source access is checked against this workspace. The connection is persisted even if discovery fails; inspect lastError. Applications are never implicitly adopted from existing manual configuration.
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [workspaceId, sourceId, revision]
              properties:
                workspaceId: { type: string }
                sourceId: { type: string }
                revision: { type: string, minLength: 1, maxLength: 256 }
      responses:
        '201': { description: Discovery connection, including errors from initial discovery }
        '400': { description: Invalid configuration }
        '403': { description: Workspace owner or source access required }
        '409': { description: Repository revision already connected }
  /repository-configurations/{repositoryID}:
    put:
      summary: Pause or resume application configuration discovery (workspace owner)
      description: Pausing discovery leaves existing application deployment policies in effect.
      parameters:
        - { name: repositoryID, in: path, required: true, schema: { type: string } }
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [enabled]
              properties:
                enabled: { type: boolean }
      responses:
        '200': { description: Updated discovery connection }
        '403': { description: Workspace owner required }
        '404': { description: Connection not found }
  /repository-configurations/{repositoryID}/reconcile:
    post:
      summary: Discover and atomically update applications from Git (workspace owner)
      parameters:
        - { name: repositoryID, in: path, required: true, schema: { type: string } }
      responses:
        '200': { description: Updated connection and last successfully processed commit }
        '403': { description: Workspace owner required }
        '404': { description: Connection not found }
        '409': { description: Discovery paused, already running, or failed validation; previous application configuration is preserved }
  /repository-configurations/{repositoryID}/pull-requests:
    get:
      summary: List new applications discovered in PRs (workspace viewer)
      parameters:
        - { name: repositoryID, in: path, required: true, schema: { type: string } }
      responses:
        '200': { description: PR applications with applicationId, definitionName, phase, error and plan }
        '403': { description: Workspace access required }
  /repository-configurations/{repositoryID}/pull-requests/settings:
    put:
      summary: Configure trusted repository PR discovery policy (workspace owner)
      description: New justcd.yaml definitions cannot authorize destinations or credentials. Existing workspace bindings must be explicitly allowed. Forks are excluded. Close and clean up active PR applications before changing policy.
      parameters:
        - { name: repositoryID, in: path, required: true, schema: { type: string } }
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [enabled]
              properties:
                enabled: { type: boolean }
                provider: { type: string, enum: [github, gitlab] }
                apiUrl: { type: string, format: uri }
                credentialId: { type: string }
                mode: { type: string, enum: [review-only, isolated, existing] }
                destinations:
                  type: array
                  items:
                    type: object
                    required: [clusterId, namespace]
                    properties:
                      clusterId: { type: string }
                      namespace: { type: string }
                profile: { type: object, description: Trusted limits and numeric provider account ID to JustCD user ID approvalActors mapping }
      responses:
        '200': { description: Updated repository configuration }
        '403': { description: Workspace owner required }
        '409': { description: Invalid policy, inaccessible destination or credential, or active PR applications }
  /health:
    get:
      summary: Check API health
      responses:
        '200': { description: Healthy }
  /auth/login:
    post:
      summary: Create a local session
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [email, password]
              properties:
                email: { type: string, format: email }
                password: { type: string, minLength: 12 }
      responses:
        '200': { description: Authenticated; sets session and CSRF cookies }
        '401': { description: Invalid credentials }
  /auth/setup:
    get:
      summary: Check whether first-administrator signup is available
      responses:
        '200': { description: Signup availability }
  /auth/signup:
    post:
      summary: Create the first administrator and sign in (only on an uninitialized instance)
      responses:
        '200': { description: Administrator created; session and CSRF cookies set }
        '409': { description: Setup already completed }
  /auth/session:
    get:
      summary: Return the current user
      responses:
        '200': { description: Current user }
        '401': { description: No active session }
  /auth/providers:
    get:
      summary: List enabled OIDC login providers
      responses:
        '200': { description: Public provider labels }
  /auth/logout:
    post:
      summary: Revoke the current session
      responses:
        '200': { description: Session revoked }
  /onboarding:
    get:
      summary: Return the deterministic first-run readiness checklist (instance admin only)
      description: Derives progress from persisted configuration, acknowledgements, and saved safe connection tests. Credential values are never returned.
      responses:
        '200': { description: Ordered readiness steps, completion count, and next required step }
        '403': { description: Instance administrator role required }
  /onboarding/steps/{stepID}/complete:
    put:
      summary: Acknowledge a manually verified onboarding step (instance admin only)
      description: Currently accepts only encryption-key after an administrator confirms that the stable key is durably backed up.
      parameters:
        - in: path
          name: stepID
          required: true
          schema: { type: string, enum: [encryption-key] }
      responses:
        '200': { description: Acknowledgement persisted }
        '400': { description: Step is derived automatically or unknown }
  /workspaces:
    get:
      summary: List workspaces visible to the current user
      responses:
        '200': { description: Workspace list }
    post:
      summary: Create a workspace
      responses:
        '201': { description: Created workspace }
  /workspaces/{workspaceID}/members:
    get:
      summary: List workspace members
      parameters:
        - in: path
          name: workspaceID
          required: true
          schema: { type: string }
      responses:
        '200': { description: Member list }
    post:
      summary: Add a member or update a workspace role (owner only)
      responses:
        '200': { description: Membership updated }
  /workspaces/{workspaceID}/members/{userID}:
    put:
      summary: Change a direct workspace member's role (owner only)
      responses:
        '200': { description: Membership updated }
        '409': { description: Last-owner protection or membership managed by SSO }
    delete:
      summary: Remove a direct workspace member (owner only)
      responses:
        '204': { description: Membership removed }
        '409': { description: Last-owner protection or membership managed by SSO }
  /admin/users:
    get:
      summary: List platform users, including locked and deleted accounts (instance admin only)
      responses:
        '200': { description: User list }
    post:
      summary: Create a local platform user (instance admin only)
      responses:
        '201': { description: Created user }
  /admin/users/{userID}:
    put:
      summary: Edit, lock, unlock, or reset a platform user (instance admin only)
      responses:
        '200': { description: Updated user }
        '409': { description: Last-admin, last-workspace-owner, or deleted-account protection }
    delete:
      summary: Revoke access and anonymize a platform user while preserving history (instance admin only)
      responses:
        '204': { description: User anonymized and access revoked }
        '409': { description: Last-admin, last-workspace-owner, or deleted-account protection }
  /workspaces/{workspaceID}/approval-policy:
    put:
      summary: Set workspace sync and application deletion approval rules (owner only)
      responses:
        '200': { description: Saved workspace approval rules }
        '400': { description: Invalid approval count or approver selection }
  /credentials:
    get:
      summary: List credential metadata for a workspace
      parameters:
        - in: query
          name: workspaceId
          required: true
          schema: { type: string }
      responses:
        '200': { description: Credential metadata only }
    post:
      summary: Store an encrypted Git or Kubernetes credential
      responses:
        '201': { description: Stored credential metadata; secret is not echoed }
  /clusters:
    get:
      summary: List configured clusters
      responses:
        '200': { description: Cluster metadata }
    post:
      summary: Register a Kubernetes cluster
      responses:
        '201': { description: Created cluster }
  /clusters/{clusterID}/agent:
    get:
      summary: Get the cluster connection's agent status and workspace activity
      description: Returns heartbeat connectivity, workspace-filtered local profiles, queued/running counts, last success/failure times, and the latest 100 task metadata records from seven days. Task bodies, credentials, resource names, and arbitrary error text are excluded. Unknown outcomes require re-reading live state before retrying.
      parameters:
        - in: path
          name: clusterID
          required: true
          schema: { type: string }
        - in: query
          name: workspaceId
          required: true
          schema: { type: string }
      responses:
        '200':
          description: Agent status, activity and summary; direct connections return mode direct
        '403': { description: Workspace membership and cluster access required }
  /clusters/{clusterID}/bindings:
    get:
      summary: List workspace namespace bindings
      parameters:
        - in: path
          name: clusterID
          required: true
          schema: { type: string }
        - in: query
          name: workspaceId
          required: true
          schema: { type: string }
      responses:
        '200': { description: Bound namespaces }
    post:
      summary: Bind a workspace to a namespace and optional credential
      responses:
        '201': { description: Namespace binding }
  /clusters/{clusterID}/test:
    post:
      summary: Run and persist Kubernetes permission self-tests for a namespace
      description: Uses API discovery and SelfSubjectAccessReview only; it does not create, patch, or delete workload resources. Cluster-wide checks are opt-in and require workspace owner access.
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [workspaceId]
              properties:
                workspaceId: { type: string }
                namespace: { type: string }
                includeClusterScope: { type: boolean, default: false }
      responses:
        '200': { description: Permission report, including classified failures and suggested RBAC YAML }
        '403': { description: Workspace owner access is required for cluster-wide checks }
  /clusters/{clusterID}/tests:
    get:
      summary: List the latest saved Kubernetes permission report per namespace
      parameters:
        - in: path
          name: clusterID
          required: true
          schema: { type: string }
        - in: query
          name: workspaceId
          required: true
          schema: { type: string }
      responses:
        '200': { description: Latest permission reports and timestamps }
  /git-sources:
    get:
      summary: List workspace Git sources
      parameters:
        - in: query
          name: workspaceId
          required: true
          schema: { type: string }
      responses:
        '200': { description: Git source list }
    post:
      summary: Add an SSH or HTTPS Git source
      responses:
        '201': { description: Created Git source }
  /applications:
    get:
      summary: List applications for a workspace
      parameters:
        - in: query
          name: workspaceId
          required: true
          schema: { type: string }
      responses:
        '200': { description: Application list }
    post:
      summary: Register a Git-backed deployment application
      responses:
        '201': { description: Created application }
  /applications/{applicationID}:
    get:
      summary: Get application status and configuration
      parameters:
        - in: path
          name: applicationID
          required: true
          schema: { type: string }
      responses:
        '200': { description: Application }
  /applications/{applicationID}/source-control:
    get:
      summary: Read the application Git source's inferred PR provider and optional PR connection without returning secrets
      responses:
        '200': { description: Inferred source details, nullable connection, preview profile, webhook URL, and webhookConfigured flag }
    put:
      summary: Configure PR reporting for the application's existing Git source (workspace owner)
      description: Provider and repository are derived from the Git source; saving an unknown host as GitLab confirms the provider. An API token is required to poll PRs/MRs every two minutes and report commit statuses. A webhook secret is optional for faster updates. Empty secret fields preserve existing encrypted values on update. A preview profile is optional and disabled by default. Saving preserves the enabled state.
      responses:
        '200': { description: Connection saved }
        '400': { description: Provider, repository, credential, or preview profile is invalid }
        '409': { description: Active previews prevent configuration changes }
    patch:
      summary: Enable or disable PR reporting without removing its settings or review history (workspace owner)
      description: Body contains enabled (boolean). Active previews must be closed before disabling.
      responses:
        '204': { description: PR reporting state changed }
        '400': { description: enabled is required }
        '404': { description: Connection not found }
        '409': { description: Active previews prevent disabling }
    delete:
      summary: Delete PR reporting settings and review history (workspace owner)
      description: Any optional repository webhook must be removed separately. Active previews must be closed before deletion.
      responses:
        '204': { description: Connection deleted }
        '404': { description: Connection not found }
        '409': { description: Active previews prevent deletion }
  /applications/{applicationID}/source-control/webhook:
    delete:
      summary: Remove the optional webhook secret while preserving PR polling and review history (workspace owner)
      description: The repository webhook must also be removed at the Git provider. The old JustCD webhook URL stops accepting deliveries.
      responses:
        '204': { description: Webhook secret removed }
        '404': { description: Connection not found }
  /applications/{applicationID}/pull-requests:
    get:
      summary: List review-only PR plans and optional preview deployment states
      responses:
        '200': { description: Redacted review plans, status errors, and preview application links }
  /webhooks/source-control/{connectionID}:
    post:
      summary: Receive verified GitHub or GitLab PR/MR and branch push events
      description: Public webhook endpoint. GitHub uses X-Hub-Signature-256. GitLab supports Standard Webhooks HMAC signatures and legacy X-Gitlab-Token for older self hosted instances.
      responses:
        '202': { description: Event stored for reconciliation }
        '204': { description: Event type ignored }
        '401': { description: Webhook verification failed }
        '410': { description: PR reporting or optional webhook is disabled }
  /git-sources/{sourceID}/push-webhook:
    get:
      summary: Read generic signed push webhook configuration without exposing the secret
    put:
      summary: Configure a generic signed push webhook for a Git source (workspace owner)
  /webhooks/git-sources/{sourceID}:
    post:
      summary: Receive a signed GitHub, GitLab, or generic branch push event
      description: Native GitHub and GitLab push hooks use their provider signatures. Generic senders sign the raw JSON body with the configured secret in X-JustCD-Signature-256 and send an optional X-JustCD-Delivery id. Body fields are ref and after.
  /applications/{applicationID}/approval-policy:
    put:
      summary: Set or clear independent approval rule overrides (owner only)
      responses:
        '200': { description: Saved application approval overrides }
        '400': { description: Invalid approval count or approver selection }
  /applications/{applicationID}/plans:
    get:
      summary: List recent plans and their sanitized diffs
      responses:
        '200': { description: Plan history }
    post:
      summary: Render Git, inspect live state, and create a plan
      responses:
        '201': { description: Immutable review plan }
        '409': { description: An existing untracked resource blocks the plan; includes ownership conflict metadata }
        '422': { description: Could not create a safe plan }
  /applications/{applicationID}/retry:
    post:
      summary: Manually retry a failed sync using a newly calculated plan (deployer or owner)
      description: Never replays a failed immutable plan or resumes an interrupted operation. The endpoint reads current cluster state, creates a fresh plan, and either queues safe changes or returns a plan for review. Rollbacks and decommissioning use their dedicated reviewed flows.
      responses:
        '200': { description: Application was already up to date }
        '202': { description: Fresh plan queued or requires review }
        '409': { description: Operation is active or is not eligible for retry }
        '422': { description: A fresh plan could not be calculated }
  /applications/{applicationID}/rollback-targets:
    get:
      summary: List recorded successful deployments and failed-operation checkpoints
      responses:
        '200': { description: Rollback target metadata; encrypted manifest contents are not returned }
  /applications/{applicationID}/rollback-plans:
    post:
      summary: Create a reviewed rollback plan from a snapshot or Git revision (workspace owner only)
      description: Creates a new immutable plan only. No cluster resources are changed until the plan is approved and applied.
      responses:
        '201': { description: Rollback plan with target provenance and normal resource diffs }
        '409': { description: Snapshot target is no longer available }
        '422': { description: Git target, scope, or Kubernetes dry-run could not be validated }
  /applications/{applicationID}/rollback-state:
    post:
      summary: Keep a rollback pin or resume the previously tracked source (workspace owner only)
      responses:
        '200': { description: Updated application tracking state }
        '422': { description: The selected Git revision is required or could not be verified }
  /applications/{applicationID}/ownership-conflict:
    get:
      summary: Inspect all untracked resources blocking this application's plan
      responses:
        '200': { description: Conflict list plus the first conflict for compatibility }
        '422': { description: Plan inspection failed for another reason }
  /applications/{applicationID}/adopt:
    post:
      summary: Claim an unchanged, reviewed resource for this application (workspace owner only)
      description: Claims the ownership label and inventory record, pauses auto-sync, and requires a fresh plan before workload changes. A later marked takeover update requires owner approval before transferring field ownership.
      responses:
        '201': { description: Resource claimed without changing workload fields }
        '409': { description: Git, UID, resource version, or existing ownership changed }
  /applications/{applicationID}/adopt-batch:
    post:
      summary: Claim selected, freshly reviewed resources for this application (workspace owner only)
      description: Validates the full selection before claiming any resource. Each claim has UID and resource-version preconditions; a mid-run change is reported as a partial result. Auto-sync is paused and a fresh approved plan is required before workload changes.
      responses:
        '200': { description: Per-resource claimed or failed results }
        '409': { description: The review set became stale or contains an unsafe resource }
  /applications/{applicationID}/ignore-rules:
    get:
      summary: List exact-resource and field-level ignore rules
      responses:
        '200': { description: Persistent ignore rules }
    post:
      summary: Add an audited ignore rule (workspace owner only)
      responses:
        '201': { description: Created ignore rule }
        '422': { description: Invalid path or unsafe field ownership handoff }
  /applications/{applicationID}/ignore-rules/{ruleID}:
    delete:
      summary: Remove an audited ignore rule (workspace owner only)
      responses:
        '204': { description: Rule deleted }
  /plans/{planID}/selections:
    post:
      summary: Create a new immutable plan excluding selected resources or changed fields
      responses:
        '201': { description: Selected plan snapshot }
        '409': { description: Source plan is stale; includes refreshed plan }
  /applications/{applicationID}/pause:
    post:
      summary: Pause automatic reconciliation (workspace owner; rejects active operations)
      parameters:
        - { name: applicationID, in: path, required: true, schema: { type: string } }
      responses:
        '200': { description: Application paused; resume through rollback-state with action resume }
        '409': { description: Application has an active operation }
  /applications/{applicationID}/resource-actions:
    post:
      summary: Pause reconciliation and perform a namespaced managed resource action (workspace owner)
      parameters:
        - { name: applicationID, in: path, required: true, schema: { type: string } }
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [identity, uid, action]
              properties:
                identity: { type: object, description: Managed Kubernetes resource identity including clusterId, apiVersion, kind, namespace and name }
                uid: { type: string }
                action: { type: string, enum: [rescale, redeploy, delete] }
                replicas: { type: integer, minimum: 0, maximum: 10000 }
      responses:
        '200': { description: Action completed; reconciliation remains paused }
        '409': { description: Action rejected or failed; reconciliation stays paused if the action started }
  /applications/{applicationID}/resources:
    get:
      summary: List resources managed by the application
      responses:
        '200': { description: Resource inventory }
  /applications/{applicationID}/topology:
    get:
      summary: Show desired, managed, and read-only observed Kubernetes resources with their relationships
      responses:
        '200': { description: Resource graph with safe metadata and observation warnings }
  /applications/{applicationID}/health-history:
    get:
      summary: List recent Kubernetes health condition transitions
      parameters:
        - in: query
          name: limit
          schema: { type: integer, minimum: 1, maximum: 100, default: 25 }
      responses:
        '200': { description: Recent application health condition transitions, newest first }
        '400': { description: Invalid history limit }
  /applications/{applicationID}/operations:
    get:
      summary: List recent sync operations, including retry attempt and outcome metadata
      responses:
        '200': { description: Operation history and next retry time or terminal reason }
  /approval-inbox:
    get:
      summary: List current plans requiring this user's approval
      parameters:
        - in: query
          name: workspaceId
          schema: { type: string }
        - in: query
          name: applicationId
          schema: { type: string }
        - in: query
          name: risk
          schema: { type: string, enum: [deletion, cluster, takeover, sync] }
        - in: query
          name: age
          schema: { type: string, enum: [24h, 7d, older7d] }
        - in: query
          name: expiry
          schema: { type: string, enum: [1h, 24h, later] }
        - in: query
          name: page
          schema: { type: integer, minimum: 1, default: 1 }
        - in: query
          name: limit
          schema: { type: integer, minimum: 1, maximum: 100, default: 20 }
      responses:
        '200': { description: Authorized, actionable requests with risk summaries and pagination metadata }
  /approval-inbox/batch:
    post:
      summary: Independently recheck and approve up to 20 reviewed plans
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [items]
              properties:
                comment: { type: string, maxLength: 1000 }
                items:
                  type: array
                  minItems: 1
                  maxItems: 20
                  items:
                    type: object
                    required: [planId, planDigest]
                    properties:
                      planId: { type: string }
                      planDigest: { type: string }
      responses:
        '200': { description: One success or failure result for each plan; partial success is possible }
  /plans/{planID}:
    get:
      summary: Get a sanitized immutable plan
      responses:
        '200': { description: Review plan }
  /plans/{planID}/approvals:
    get:
      summary: List active approvals and the current user's eligibility for this plan
      responses:
        '200': { description: Approval rule, count, and current approvals }
    post:
      summary: Approve the exact plan as an eligible workspace member
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                comment: { type: string, maxLength: 1000 }
                planDigest: { type: string, description: Digest reviewed by the approver }
      responses:
        '201': { description: Single-use plan approval }
        '409': { description: Plan is stale or has enough approvals }
  /plans/{planID}/apply:
    post:
      summary: Recheck plan freshness and enqueue changes after required approvals
      responses:
        '202': { description: Sync operation queued with durable progress tracking }
        '409': { description: Plan or live state changed after review }
  /audit:
    get:
      summary: List audit events newest first (instance administrator only)
      parameters:
        - in: query
          name: before
          description: Return events older than this event ID
          schema: { type: integer, minimum: 1 }
      responses:
        '200': { description: Up to 50 audit events and a hasMore flag }
  /admin/oidc-providers:
    get:
      summary: List OIDC provider configuration without client secrets
      responses:
        '200': { description: Provider list }
    post:
      summary: Configure an OIDC provider
      responses:
        '201': { description: Created provider; secret is not echoed }
  /admin/oidc-providers/{providerID}:
    delete:
      summary: Delete an OIDC provider and its identity links and group grants
      parameters:
        - { name: providerID, in: path, required: true, schema: { type: string } }
      responses:
        '204': { description: Deleted; user accounts and sessions are retained }
        '404': { description: Provider not found }
    put:
      summary: Edit an OIDC provider; an empty client secret retains the existing secret
      parameters:
        - { name: providerID, in: path, required: true, schema: { type: string } }
      responses:
        '200': { description: Updated provider; secret is not echoed }
        '409': { description: Issuer change rejected for a provider with linked users }
components:
  schemas:
    ErrorResponse:
      type: object
      required: [schemaVersion, error, code, category, retryable, remediation]
      properties:
        schemaVersion: { type: integer, const: 1 }
        error: { type: string, description: Stable user-facing message retained for v1 clients }
        code: { type: string, description: Stable machine-readable error code }
        category: { type: string, description: Error domain such as validation, authorization, git, kubernetes, render, database, or internal }
        retryable: { type: boolean, description: Whether retrying may succeed without changing the request }
        remediation: { type: string, description: Safe operator guidance }
        remediationUrl: { type: string, description: Optional same-origin path to related settings or context }
        details:
          type: object
          description: Safe structured diagnostics. Some v1-specific fields are also retained at the top level for compatibility.
          additionalProperties: true
      additionalProperties: true
  securitySchemes:
    sessionCookie:
      type: apiKey
      in: cookie
      name: justcd_session
security:
  - sessionCookie: []
`
