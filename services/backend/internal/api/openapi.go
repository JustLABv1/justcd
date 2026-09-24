package api

const openAPISpec = `openapi: 3.1.0
info:
  title: JustCD API
  version: 1.0.0
  description: JustCD control-plane API. Kubernetes and Git credential material is never returned.
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
  /applications/{applicationID}/plans:
    get:
      summary: List recent plans and their sanitized diffs
      responses:
        '200': { description: Plan history }
    post:
      summary: Render Git, inspect live state, and create a plan
      responses:
        '201': { description: Immutable review plan }
        '422': { description: Could not create a safe plan }
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
  /applications/{applicationID}/operations:
    get:
      summary: List recent sync operations
      responses:
        '200': { description: Operation history }
  /plans/{planID}:
    get:
      summary: Get a sanitized immutable plan
      responses:
        '200': { description: Review plan }
  /plans/{planID}/approvals:
    post:
      summary: Approve the exact deletion or cluster-scope set (project owner only)
      responses:
        '201': { description: Single-use approval }
        '409': { description: Plan is stale or does not need owner approval }
  /plans/{planID}/apply:
    post:
      summary: Recalculate plan freshness and apply approved changes
      responses:
        '200': { description: Sync operation }
        '409': { description: Plan or live state changed after review }
  /audit:
    get:
      summary: List recent audit events (instance administrator only)
      responses:
        '200': { description: Audit events }
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
  securitySchemes:
    sessionCookie:
      type: apiKey
      in: cookie
      name: justcd_session
security:
  - sessionCookie: []
`
