package api

const openAPISpec = `openapi: 3.1.0
info:
  title: JustCD API
  version: 1.0.0
  description: JustCD control-plane API. Kubernetes and Git credential material is never returned. API errors use the versioned ErrorResponse schema and retain the legacy error string.
servers:
  - url: /api/v1
paths:
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
  /projects:
    get:
      summary: List projects visible to the current user
      responses:
        '200': { description: Project list }
    post:
      summary: Create a project
      responses:
        '201': { description: Created project }
  /projects/{projectID}/members:
    get:
      summary: List project members
      parameters:
        - in: path
          name: projectID
          required: true
          schema: { type: string }
      responses:
        '200': { description: Member list }
    post:
      summary: Add a member or update a project role (owner only)
      responses:
        '200': { description: Membership updated }
  /projects/{projectID}/members/{userID}:
    put:
      summary: Change a direct project member's role (owner only)
      responses:
        '200': { description: Membership updated }
        '409': { description: Last-owner protection or membership managed by SSO }
    delete:
      summary: Remove a direct project member (owner only)
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
        '409': { description: Last-admin, last-project-owner, or deleted-account protection }
    delete:
      summary: Revoke access and anonymize a platform user while preserving history (instance admin only)
      responses:
        '204': { description: User anonymized and access revoked }
        '409': { description: Last-admin, last-project-owner, or deleted-account protection }
  /projects/{projectID}/approval-policy:
    put:
      summary: Set project sync and application deletion approval rules (owner only)
      responses:
        '200': { description: Saved project approval rules }
        '400': { description: Invalid approval count or approver selection }
  /credentials:
    get:
      summary: List credential metadata for a project
      parameters:
        - in: query
          name: projectId
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
  /clusters/{clusterID}/bindings:
    get:
      summary: List project namespace bindings
      parameters:
        - in: path
          name: clusterID
          required: true
          schema: { type: string }
        - in: query
          name: projectId
          required: true
          schema: { type: string }
      responses:
        '200': { description: Bound namespaces }
    post:
      summary: Bind a project to a namespace and optional credential
      responses:
        '201': { description: Namespace binding }
  /clusters/{clusterID}/test:
    post:
      summary: Run and persist Kubernetes permission self-tests for a namespace
      description: Uses API discovery and SelfSubjectAccessReview only; it does not create, patch, or delete workload resources. Cluster-wide checks are opt-in and require project owner access.
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [projectId]
              properties:
                projectId: { type: string }
                namespace: { type: string }
                includeClusterScope: { type: boolean, default: false }
      responses:
        '200': { description: Permission report, including classified failures and suggested RBAC YAML }
        '403': { description: Project owner access is required for cluster-wide checks }
  /clusters/{clusterID}/tests:
    get:
      summary: List the latest saved Kubernetes permission report per namespace
      parameters:
        - in: path
          name: clusterID
          required: true
          schema: { type: string }
        - in: query
          name: projectId
          required: true
          schema: { type: string }
      responses:
        '200': { description: Latest permission reports and timestamps }
  /git-sources:
    get:
      summary: List project Git sources
      parameters:
        - in: query
          name: projectId
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
      summary: List applications for a project
      parameters:
        - in: query
          name: projectId
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
      summary: Create a reviewed rollback plan from a snapshot or Git revision (project owner only)
      description: Creates a new immutable plan only. No cluster resources are changed until the plan is approved and applied.
      responses:
        '201': { description: Rollback plan with target provenance and normal resource diffs }
        '409': { description: Snapshot target is no longer available }
        '422': { description: Git target, scope, or Kubernetes dry-run could not be validated }
  /applications/{applicationID}/rollback-state:
    post:
      summary: Keep a rollback pin or resume the previously tracked source (project owner only)
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
      summary: Claim an unchanged, reviewed resource for this application (project owner only)
      description: Claims the ownership label and inventory record, pauses auto-sync, and requires a fresh plan before workload changes. A later marked takeover update requires owner approval before transferring field ownership.
      responses:
        '201': { description: Resource claimed without changing workload fields }
        '409': { description: Git, UID, resource version, or existing ownership changed }
  /applications/{applicationID}/adopt-batch:
    post:
      summary: Claim selected, freshly reviewed resources for this application (project owner only)
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
      summary: Add an audited ignore rule (project owner only)
      responses:
        '201': { description: Created ignore rule }
        '422': { description: Invalid path or unsafe field ownership handoff }
  /applications/{applicationID}/ignore-rules/{ruleID}:
    delete:
      summary: Remove an audited ignore rule (project owner only)
      responses:
        '204': { description: Rule deleted }
  /plans/{planID}/selections:
    post:
      summary: Create a new immutable plan excluding selected resources or changed fields
      responses:
        '201': { description: Selected plan snapshot }
        '409': { description: Source plan is stale; includes refreshed plan }
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
      summary: Approve the exact plan as an eligible project member
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
