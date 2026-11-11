package api

const openAPISpec = `openapi: 3.1.0
info:
  title: JustCD API
  version: 1.0.0
  description: JustCD control-plane API. Kubernetes and Git credential material is never returned. API errors use the versioned ErrorResponse schema and retain the legacy error string.
servers:
  - url: /api/v1
paths:
  /publishing/profiles:
    get:
      summary: List enabled publishing destinations without infrastructure or credentials (workspace deployer)
      parameters:
        - {name: workspaceId, in: query, required: true, schema: {type: string}}
      responses:
        '200': {description: Profile ids, names, descriptions, template ids, destination display metadata, and sync policies}
        '403': {description: Workspace deployer or owner required}
  /workspaces/{workspaceID}/publishing-profiles:
    parameters:
      - {name: workspaceID, in: path, required: true, schema: {type: string}}
    get:
      summary: List workspace profile configuration without secret values (owner)
      responses:
        '200': {description: Full profile settings, enabled state, version and application count; credential references and ciphertext omitted}
        '403': {description: Workspace owner required}
    post:
      summary: Create an administrator-configured publishing profile (owner)
      requestBody:
        required: true
        content: {application/json: {schema: {$ref: '#/components/schemas/PublishingProfileInput'}}}
      responses:
        '201': {description: Profile created and credentials stored encrypted}
        '400': {description: Invalid template, settings, credentials or namespace binding}
        '403': {description: Workspace owner and CSRF required}
        '409': {description: Profile name already exists}
  /workspaces/{workspaceID}/publishing-templates:
    get:
      summary: List application templates and their custom versions
      description: Workspace owner only. Each item contains the built-in template and the workspace's custom versions (without file contents), newest first.
      parameters:
        - {name: workspaceID, in: path, required: true, schema: {type: string}}
      responses:
        '200': {description: Templates with custom version metadata}
        '403': {description: Workspace owner required}
    post:
      summary: Create a workspace template
      description: Requires the workspace owner role and CSRF. Creates a custom template with its first version, either from a kind (server, static, static-build) or as a copy of any template (baseTemplate, optionally baseVersion). The template files are validated like any saved version.
      parameters:
        - {name: workspaceID, in: path, required: true, schema: {type: string}}
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [id, name]
              properties:
                id: {type: string, description: "3-40 lowercase letters, numbers and hyphens; must not match a built-in template"}
                name: {type: string, maxLength: 80}
                description: {type: string, maxLength: 400}
                category: {type: string, maxLength: 40}
                kind: {type: string, enum: [server, static, static-build]}
                baseTemplate: {type: string, description: Copy this template instead of starting from a kind}
                baseVersion: {type: integer, minimum: 0}
                entryPoint: {type: string}
                port: {type: integer, minimum: 0, maximum: 65535}
                layout: {type: array, items: {type: object, required: [anyOf], properties: {message: {type: string}, anyOf: {type: array, items: {type: array, items: {type: string}}}}}, description: "Files an uploaded ZIP must contain; {entry} is the entry point"}
                label: {type: string, maxLength: 100}
      responses:
        '201': {description: Template created with version 1}
        '400': {description: Invalid definition or template files}
        '403': {description: Workspace owner and CSRF required}
        '404': {description: Base template not found}
        '409': {description: The ID exists or the workspace has the maximum number of templates}
  /workspaces/{workspaceID}/publishing-templates/import:
    post:
      summary: Import a template from an exported file
      description: Requires the workspace owner role and CSRF. The body is a justcd.template/v1 file; the id and name query parameters override the file's values.
      parameters:
        - {name: workspaceID, in: path, required: true, schema: {type: string}}
        - {name: id, in: query, schema: {type: string}}
        - {name: name, in: query, schema: {type: string}}
      requestBody:
        required: true
        content:
          application/json:
            schema: {type: object, required: [format, template, files], properties: {format: {type: string, enum: ["justcd.template/v1"]}, template: {type: object}, label: {type: string}, files: {type: object, additionalProperties: {type: string}}}}
      responses:
        '201': {description: Template imported}
        '400': {description: Unsupported file or invalid template}
        '409': {description: The ID exists}
  /workspaces/{workspaceID}/publishing-templates/{templateID}:
    put:
      summary: Update a custom template's definition
      description: Requires the workspace owner role and CSRF. Changes name, description, entry point, port and upload checks; the ID and kind are fixed. The latest files must still validate.
      parameters:
        - {name: workspaceID, in: path, required: true, schema: {type: string}}
        - {name: templateID, in: path, required: true, schema: {type: string}}
      responses:
        '200': {description: Template updated}
        '400': {description: Invalid definition}
        '404': {description: Not a custom template of this workspace}
    delete:
      summary: Delete a custom template
      description: Requires the workspace owner role and CSRF. Fails while a profile or recorded profile revision references the template. Built-in templates cannot be deleted.
      parameters:
        - {name: workspaceID, in: path, required: true, schema: {type: string}}
        - {name: templateID, in: path, required: true, schema: {type: string}}
      responses:
        '204': {description: Deleted with all versions}
        '400': {description: Built-in template}
        '404': {description: Not a custom template of this workspace}
        '409': {description: A profile still uses the template}
    get:
      summary: Read a template, its built-in files and placeholders
      description: Workspace owner only. Returns the read-only built-in files, the ordered file list, the placeholders and helper functions templates can use, and the custom versions.
      parameters:
        - {name: workspaceID, in: path, required: true, schema: {type: string}}
        - {name: templateID, in: path, required: true, schema: {type: string}}
      responses:
        '200': {description: Template detail}
        '403': {description: Workspace owner required}
        '404': {description: Unknown template}
  /workspaces/{workspaceID}/publishing-templates/{templateID}/export:
    get:
      summary: Export a template version as a portable file
      description: Workspace owner only. Returns a justcd.template/v1 document with the definition and files of the requested (default latest) version.
      parameters:
        - {name: workspaceID, in: path, required: true, schema: {type: string}}
        - {name: templateID, in: path, required: true, schema: {type: string}}
        - {name: version, in: query, schema: {type: integer, minimum: 0}}
      responses:
        '200': {description: Template file}
        '404': {description: Template or version not found}
  /workspaces/{workspaceID}/publishing-templates/{templateID}/versions/{version}:
    get:
      summary: Read the files of a template version
      description: Version 0 is the built-in template. Workspace owner only.
      parameters:
        - {name: workspaceID, in: path, required: true, schema: {type: string}}
        - {name: templateID, in: path, required: true, schema: {type: string}}
        - {name: version, in: path, required: true, schema: {type: integer, minimum: 0}}
      responses:
        '200': {description: Template version with files}
        '403': {description: Workspace owner required}
        '404': {description: Template version not found}
  /workspaces/{workspaceID}/publishing-templates/{templateID}/versions:
    post:
      summary: Save a new, immutable template version
      description: Requires the workspace owner role and CSRF. The five required template files and up to ten optional .justcd/manifests/<name>.yaml files are parsed, rendered for a matrix of configurations and validated (safe manifest, digest-publishing pipeline) before the version is stored. Versions are never updated or deleted.
      parameters:
        - {name: workspaceID, in: path, required: true, schema: {type: string}}
        - {name: templateID, in: path, required: true, schema: {type: string}}
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [files]
              properties:
                label: {type: string, maxLength: 100}
                files: {type: object, additionalProperties: {type: string}, description: "Requires Dockerfile, .gitlab-ci.yml, .justcd/deployment.yaml, .dockerignore and .justcd/README.md. Up to ten optional .justcd/manifests/name.yaml files may contain ConfigMap, NetworkPolicy, PodDisruptionBudget or HorizontalPodAutoscaler resources; they are combined into the deployment manifest."}
      responses:
        '201': {description: Version created (metadata without files)}
        '400': {description: A file is missing, has a syntax error, or renders to something unsafe; the message names the problem}
        '403': {description: Workspace owner and CSRF required}
        '409': {description: The version could not be stored}
  /workspaces/{workspaceID}/publishing-templates/{templateID}/preview:
    post:
      summary: Render template files without saving
      description: Renders the supplied files with sample values or the settings of a profile and validates the result. Requires the workspace owner role and CSRF.
      parameters:
        - {name: workspaceID, in: path, required: true, schema: {type: string}}
        - {name: templateID, in: path, required: true, schema: {type: string}}
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [files]
              properties:
                files: {type: object, additionalProperties: {type: string}}
                settings: {type: object, description: Optional profile settings used for the sample values}
                services: {$ref: "#/components/schemas/PublishingServiceSelection"}
                hasRenvLock: {type: boolean}
      responses:
        '200': {description: "Rendered files (field files)"}
        '400': {description: Rendering or validation failed; the message names the problem}
        '403': {description: Workspace owner and CSRF required}
  /workspaces/{workspaceID}/publishing-profiles/{profileID}:
    parameters:
      - {name: workspaceID, in: path, required: true, schema: {type: string}}
      - {name: profileID, in: path, required: true, schema: {type: string}}
    put:
      summary: Edit a profile, disable publishing, or rotate credentials (owner)
      description: Supply the current version. Blank secret fields retain stored values. Build engines, build images, runner tags, runner service account/SCC/setup script, custom CA variable names, and image-pull secrets remain editable on in-use profiles; other infrastructure settings are immutable. Saving does not rebuild existing apps. Build-setting changes publish immutable revisions. Source uploads retain the application pin; explicit reviewed upgrades adopt newer revisions without a ZIP.
      requestBody:
        required: true
        content: {application/json: {schema: {$ref: '#/components/schemas/PublishingProfileInput'}}}
      responses:
        '200': {description: Profile saved with incremented version; no secret values returned}
        '403': {description: Workspace owner and CSRF required}
        '404': {description: Profile not found in workspace}
        '409': {description: Stale version, duplicate name or in-use infrastructure change}
    delete:
      summary: Delete an unused publishing profile and its private credentials (owner)
      responses:
        '204': {description: Profile and credentials deleted}
        '403': {description: Workspace owner and CSRF required}
        '409': {description: Profile or credentials are in use; disable instead}
  /publishing/repository/inspect:
    post:
      summary: Inspect an owned GitLab repository at a pinned branch commit without changing it
      requestBody:
        required: true
        content:
          application/json:
            schema: {$ref: '#/components/schemas/RepositoryPublishingInput'}
      responses:
        '200': {description: Commit SHA and detected Dockerfile, GitLab pipeline, and possible deployment files}
        '400': {description: Invalid repository, branch, or profile configuration}
        '403': {description: Workspace owner required for manual settings; deployer required for profiles}
        '404': {description: Git source or profile not owned by this workspace}
  /publishing/repository/plan:
    post:
      summary: Review create-only additions for an existing GitLab project
      requestBody:
        required: true
        content:
          application/json:
            schema: {$ref: '#/components/schemas/RepositoryPublishingInput'}
      responses:
        '200': {description: Proposed files and contents, prerequisites, and review hash; no repository writes}
        '400': {description: Unsafe path, conflicting existing file, or invalid build configuration}
  /publishing/repository:
    post:
      summary: Create a merge request and register a manual-sync application using the existing Git source
      description: Recomputes the reviewed plan. Never overwrites existing files, CI variables, project settings, or the target branch. Generated build prerequisites must be configured before merging. Profile infrastructure is a bootstrap snapshot, not a managed ZIP publication.
      requestBody:
        required: true
        content:
          application/json:
            schema: {$ref: '#/components/schemas/RepositoryPublishingInput'}
      responses:
        '201': {description: applicationId, mergeRequestUrl, manifestPath, and CI prerequisites}
        '409': {description: Stale review hash, existing conflicting application, or application registration failure; a registration failure may include the created merge request URL for recovery}
        '502': {description: GitLab proposal could not be created}
  /publishing/profiles/{profileID}/versions:
    get:
      summary: List immutable profile revision snapshots (workspace owner)
      parameters:
        - {name: profileID, in: path, required: true, schema: {type: string}}
      responses:
        '200': {description: Up to 100 published revisions with settings, recipe version, and publication time; credentials excluded}
        '403': {description: Workspace owner required}
  /applications/{applicationID}/build-setup/preview:
    post:
      summary: Review a build setup change of a manually published application
      description: Workspace owner only; not for profile-managed applications. Regenerates the Dockerfile, pipeline and package script from the stored settings plus the requested package sources and returns the settings and generated files that would change, with a hash. Nothing is committed. The GitLab token is used for the request only.
      parameters:
        - {name: applicationID, in: path, required: true, schema: {type: string}}
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [gitlabToken]
              properties:
                gitlabToken: {type: string, writeOnly: true}
                packageSources: {type: string, description: JSON list of package mirrors}
                packagePolicy: {type: string, enum: ["", enforce]}
                templateParameters: {type: string, description: "Non-secret key=value lines for the template; omit to keep the current ones"}
      responses:
        '200': {description: "Plan: applicationId, sourceSha, hash, changes, images (setting changes), upToDate"}
        '403': {description: Workspace owner required}
        '409': {description: The application uses a profile or its repository changed}
  /applications/{applicationID}/build-setup:
    post:
      summary: Apply a reviewed build setup change
      description: Workspace owner only. Commits the reviewed files (with per-file concurrency guards), sets the package credentials as masked protected CI variables, records the settings and starts a build. The hash must match a fresh review.
      parameters:
        - {name: applicationID, in: path, required: true, schema: {type: string}}
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [gitlabToken, hash]
              properties:
                gitlabToken: {type: string, writeOnly: true}
                packageSources: {type: string}
                packagePolicy: {type: string, enum: ["", enforce]}
                templateParameters: {type: string, description: "Non-secret key=value lines for the template; omit to keep the current ones"}
                packageUser: {type: string, writeOnly: true}
                packageToken: {type: string, writeOnly: true}
                hash: {type: string}
      responses:
        '200': {description: applicationId and pipelineUrl}
        '400': {description: Invalid package sources or missing package credentials}
        '409': {description: Stale review, concurrent publication, or repository changed}
  /applications/{applicationID}/adopt-profile:
    post:
      summary: Convert a manually published application to a publishing profile
      description: Workspace owner only. Succeeds only when the application already matches the profile's destination (template, cluster, namespace, registry, hostname, secret mode, GitLab group); otherwise returns the differences. With dryRun nothing is changed. After conversion the profile's build settings are adopted through the reviewed profile update.
      parameters:
        - {name: applicationID, in: path, required: true, schema: {type: string}}
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [profileId]
              properties:
                profileId: {type: string}
                dryRun: {type: boolean}
      responses:
        '200': {description: "profileId, profileName, compatible, applied, differences [{field, app, profile}]"}
        '403': {description: Workspace owner required}
        '409': {description: Already uses a profile, or the application changed}
  /applications/{applicationID}/profile-upgrade:
    parameters:
      - {name: applicationID, in: path, required: true, schema: {type: string}}
    get:
      summary: Review a managed application's latest profile revision using source already in Git
      description: Returns fromRevision, toRevision, sourceSha, hash, image changes, generated-file changes, and resume/upToDate flags. Read-only; requires workspace deployer access and an enabled original profile.
      responses:
        '200': {description: Immutable upgrade review or already-up-to-date status}
        '403': {description: Workspace deployer, cluster access, and enabled profile required}
        '409': {description: Application destination changed or recipe version is unsupported}
    post:
      summary: Apply a reviewed profile revision and start a build without a ZIP
      description: Retains source and deployment files. Writes only generated recipes, pins the application revision, and starts CI under existing deployment policies. Stale reviews are rejected. A pending build can be reviewed again and retried after a partial failure.
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              additionalProperties: false
              required: [revision, hash]
              properties:
                revision: {type: integer}
                hash: {type: string}
      responses:
        '200': {description: applicationId, profileRevision, and pipelineUrl; retries of completed upgrades do not start another build}
        '403': {description: Workspace deployer and enabled profile required}
        '409': {description: Stale review, application change, or concurrent publication; refresh the upgrade review}
        '502': {description: GitLab build could not start; application may have a pending upgraded revision}
  /publishing/templates:
    get:
      summary: List supported application templates (authenticated)
      description: With workspaceId (any workspace member) the workspace's custom templates follow the built-in ones.
      parameters:
        - {name: workspaceId, in: query, schema: {type: string}}
      responses:
        '200': {description: Built-in template definitions, plus custom ones for the workspace}
        '403': {description: Not a member of the workspace}
  /publishing/limits:
    get:
      summary: Read the active publishing archive limits (authenticated)
      responses:
        '200':
          description: Configured compressed and expanded byte limits
          content:
            application/json:
              schema:
                type: object
                properties:
                  archiveBytes: {type: integer, default: 104857600}
                  expandedBytes: {type: integer, default: 524288000}
  /publishing/validate-archive:
    post:
      summary: Validate a publishing ZIP without creating external resources
      description: Requires authentication, CSRF and workspace deployer access. Accepts multipart metadata (workspaceId, config.runtime, config.entryPoint), followed by archive. Enforces publishing limits, safe paths and template layout. One enclosing folder is removed only if the normalized layout is valid. Temporary files are always removed; no repository, pipeline or application is created.
      requestBody:
        required: true
        content:
          multipart/form-data:
            schema:
              type: object
              required: [metadata, archive]
              properties:
                metadata: {type: string, description: "JSON with workspaceId and config containing runtime and entryPoint."}
                archive: {type: string, format: binary}
      responses:
        '200':
          description: Archive is valid
          content:
            application/json:
              schema:
                type: object
                properties:
                  valid: {type: boolean}
                  rootDirectory: {type: string}
                  fileCount: {type: integer}
                  message: {type: string}
        '400': {description: Invalid ZIP or layout; error explains required files and paths}
        '403': {description: Workspace deployer access required}
        '413': {description: Archive exceeds configured limits}
  /publishing:
    post:
      summary: Create a managed application or upload a complete new source snapshot
      description: Creates a private GitLab repository, protected CI variables or Vault references, generated build files, and a manual JustCD application. CI builds and publishes asynchronously. Workspace deployers may use an enabled administrator-configured profile with profileId, name and entryPoint. Infrastructure fields and credentials are resolved server-side and cannot be overridden. Manual configuration requires a workspace owner. Existing unrelated repositories are never adopted. On partial setup failures, retry the same archive and settings. A new source upload must supply updateApplicationId and reuse the original configuration.
      requestBody:
        required: true
        content:
          multipart/form-data:
            schema:
              type: object
              required: [metadata, archive]
              properties:
                archive: {type: string, format: binary, description: ZIP archive; default 100 MiB compressed and 500 MiB expanded; limits configurable on the server}
                metadata:
                  type: object
                  required: [workspaceId]
                  oneOf:
                    - required: [profileId]
                    - required: [clusterId, gitlabUrl, group, gitlabToken, config]
                  properties:
                    databasePassword: {type: string, writeOnly: true, minLength: 16, maxLength: 1024, description: "Initial managed-database creation only. Empty or omitted generates a password; accepted only when the profile permits it. Excluded from drafts, Git and responses."}
                    services: {$ref: "#/components/schemas/PublishingServiceSelection"}
                    profileId: {type: string, description: Approved workspace profile; infrastructure overrides are rejected}
                    expectedProfileRevision: {type: integer, description: Optional current revision guard for initial publishing}
                    name: {type: string, description: Application name for initial profile publishing}
                    entryPoint: {type: string, description: Optional relative app entry point}
                    buildCommand: {type: string, description: Python static website build command}
                    outputDir: {type: string, description: Static output directory containing index.html}
                    workspaceId: {type: string}
                    clusterId: {type: string}
                    updateApplicationId: {type: string}
                    gitlabUrl: {type: string, format: uri}
                    group: {type: string}
                    gitlabToken: {type: string, writeOnly: true}
                    gitReadToken: {type: string, writeOnly: true, description: Required for initial publishing; encrypted in the JustCD credential store}
                    registryUser: {type: string, writeOnly: true}
                    registryPassword: {type: string, writeOnly: true}
                    packageUser: {type: string, writeOnly: true, description: "Read-only account for authenticated package mirrors"}
                    packageToken: {type: string, writeOnly: true}
                    config:
                      type: object
                      required: [name, runtime, entryPoint, builderImage, registryImage, namespace, secretMode]
                      properties:
                        templateId: {type: string, enum: [shiny, streamlit, shiny-geospatial, python-static, static]}
                        name: {type: string}
                        runtime: {type: string, enum: [shiny, streamlit, python-static, static, server]}
                        entryPoint: {type: string}
                        buildCommand: {type: string}
                        outputDir: {type: string}
                        webServerImage: {type: string, description: Required for static and python-static; unprivileged NGINX-compatible image on port 8080}
                        baseImage: {type: string, description: Required for Shiny and Python build runtimes; unused for ready-made static websites}
                        builderImage: {type: string}
                        registryImage: {type: string}
                        createNamespace: {type: boolean, default: false, description: Register a new namespace binding and provision it through the deployment flow}
                        namespace: {type: string}
                        host: {type: string}
                        pullSecret: {type: string}
                        secretMode: {type: string, enum: [variables, vault]}
                        vaultAddress: {type: string}
                        vaultRole: {type: string}
                        vaultPath: {type: string}
                        runnerTag: {type: string}
                        exposure: {type: string, enum: [route, ingress], description: "Resource that publishes the application: OpenShift Route (default) or Kubernetes Ingress."}
                        exposureLabels: {type: string, description: "Newline-separated key=value labels for the Route or Ingress only."}
                        exposureAnnotations: {type: string, description: "Newline-separated key=value annotations for the Route or Ingress only."}
                        ingressClass: {type: string, description: "Optional ingressClassName; requires exposure ingress."}
                        ingressTls: {type: string, enum: [hosts, none], description: "Ingress TLS block: hosts (default) lists the host, none renders tls: []."}
                        services: {type: string, description: "JSON-encoded resolved database and VaultStaticSecret configuration for manual publishing; no secret values."}
                        templateParameters: {type: string, description: "Optional non-secret key=value lines available as .Parameters in custom templates."}
                        packageSources: {type: string, description: "JSON list of package mirrors: [{ecosystem: pip|cran|npm|conda|apt|apk, url: https mirror, authenticated: boolean}]. Builds route that package manager through the mirror; credentials come from the package credentials (variables mode) or the Vault fields package_username and package_token."}
                        packagePolicy: {type: string, enum: ["", enforce], description: "enforce rejects uploads that name another package index, a Git dependency or a host outside the approved sources."}
                        templateVersion: {type: integer, minimum: 0, description: "Workspace template version used by this profile; 0 or omitted means the built-in template."}
                        runnerServiceAccount: {type: string, description: "Optional Kubernetes-executor service account override for the build job."}
                        runnerScc: {type: string, description: "Optional OpenShift SCC the build pod must run under (openshift.io/required-scc)."}
                        builderSetup: {type: string, description: "Optional executable in the builder image run before the build, for example a rootless Podman entrypoint script."}
                        gitImage: {type: string, description: "Deployment update job image containing Git and shell tools. Omitted reuses the builder image."}
                        buildEngine: {type: string, enum: [buildkit, podman], description: "Optional build engine; omitted means BuildKit."}
                        caVariable: {type: string, description: "Optional inherited GitLab variable containing PEM CA certificates for CI Git and the build engine."}
      responses:
        '201': {description: Application registered and pipeline started; returns applicationId, projectUrl, repositoryUrl, pipelineUrl, and non-secret publication configuration}
        '400': {description: Invalid source archive, configuration or target}
        '403': {description: Workspace owner and CSRF required}
        '413': {description: Archive, expanded source or metadata exceeds configured limits}
        '409': {description: Concurrent publication, unrelated existing project or settings mismatch}
        '502': {description: GitLab setup or pipeline failure; provider secrets are not returned}
  /applications/{applicationID}/publication:
    parameters:
      - {name: applicationID, in: path, required: true, schema: {type: string}}
    get:
      summary: Read managed publication settings for a source update (profile deployer or manual owner)
      responses:
        '200': {description: Publication configuration, workspaceId, and clusterId; no credentials}
        '403': {description: Workspace owner required}
        '404': {description: Application was not created with guided publishing}
  /workspaces/{workspaceID}/deployment-schedule:
    parameters:
      - { name: workspaceID, in: path, required: true, schema: { type: string } }
    get:
      summary: Read workspace deployment restrictions and next allowed start (viewer)
      responses:
        '200': { description: Deployment policy and decision, content: { application/json: { schema: { $ref: '#/components/schemas/DeploymentSchedule' } } } }
        '403': { description: Workspace access required }
    put:
      summary: Replace workspace maintenance windows and freezes (owner)
      requestBody:
        required: true
        content: { application/json: { schema: { $ref: '#/components/schemas/DeploymentPolicy' } } }
      responses:
        '200': { description: Saved policy and decision }
        '400': { description: Invalid time zone, schedule, timestamp or reason }
        '403': { description: Owner role and CSRF required }
  /applications/{applicationID}/deployment-schedule:
    parameters:
      - { name: applicationID, in: path, required: true, schema: { type: string } }
    get:
      summary: Read application policy and effective workspace/application decision (viewer)
      responses:
        '200': { description: Deployment policy and decision, content: { application/json: { schema: { $ref: '#/components/schemas/DeploymentSchedule' } } } }
        '403': { description: Workspace access required }
    put:
      summary: Replace a manual application's maintenance windows and freezes (owner)
      description: Git-managed applications use spec.deploymentSchedule in justcd.yaml. Application restrictions cannot bypass workspace restrictions.
      requestBody:
        required: true
        content: { application/json: { schema: { $ref: '#/components/schemas/DeploymentPolicy' } } }
      responses:
        '200': { description: Saved policy and effective decision }
        '400': { description: Invalid policy }
        '403': { description: Owner role and CSRF required }
        '409': { description: Application policy is managed by Git }
  /operations/{operationID}/schedule-override:
    parameters:
      - { name: operationID, in: path, required: true, schema: { type: string } }
    post:
      summary: Audit an emergency override for this queued apply (workspace owner)
      description: Bypasses windows and freezes only. Requires a current owner at request and execution time. Normal plan, approval, destination, and concurrency checks remain mandatory.
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [reason]
              properties:
                reason: { type: string, minLength: 1, maxLength: 1000 }
      responses:
        '200': { description: Updated queued operation }
        '403': { description: Owner role and CSRF required }
        '409': { description: Operation is no longer queued or reason is empty }
  /operations/{operationID}/cancel:
    parameters:
      - { name: operationID, in: path, required: true, schema: { type: string } }
    post:
      summary: Cancel a queued apply and invalidate its plan (deployer)
      responses:
        '200': { description: Queued operation cancelled and lease released }
        '403': { description: Deploy permission and CSRF required }
        '409': { description: Operation already started or finished }
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
  /repository-configurations/{repositoryID}/environment-settings:
    get:
      summary: Read GitLab environment reporting settings (workspace viewer)
      parameters:
        - { name: repositoryID, in: path, required: true, schema: { type: string } }
      responses:
        '200': { description: Settings with enabled, credentialId and optional apiUrl }
        '403': { description: Workspace access required }
    put:
      summary: Configure GitLab environment reporting (workspace owner)
      description: When enabled, JustCD records a GitLab deployment after each finished sync of applications defined by this repository. The environment defaults to cluster/namespace and can be overridden by spec.environment in justcd.yaml. The credential needs a token with the api scope.
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
                credentialId: { type: string }
                apiUrl: { type: string, format: uri }
      responses:
        '200': { description: Saved settings }
        '409': { description: Repository is not on GitLab or the credential is unavailable }
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
  /service-status:
    get:
      summary: Check service availability and session-validated administrator maintenance bypass
      responses:
        '200':
          description: Current availability (no-store; varies by session cookie)
          content:
            application/json:
              schema:
                type: object
                required: [status, maintenanceEnabled, adminBypass]
                properties:
                  status: { type: string, enum: [online, maintenance] }
                  maintenanceEnabled: { type: boolean }
                  adminBypass: { type: boolean }
        '500': { description: Could not read instance state }
  /auth/register:
    post:
      summary: Create an unverified password account (only while an administrator has opened registration)
      description: Rate limited; honours the allowed email domains. The account can sign in but has no workspace access until an administrator approves it. Responds 403 while registration is closed.
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [email, displayName, password]
              properties:
                email: { type: string }
                displayName: { type: string }
                password: { type: string, minLength: 12, maxLength: 1024 }
      responses:
        '200': { description: Account created and signed in }
        '403': { description: Registration is closed or the email domain is not allowed }
        '409': { description: An account with this email already exists }
        '429': { description: Too many attempts }
  /admin/users/{userID}/approve:
    post:
      summary: Approve a self-registered account (instance administrators only; CSRF required)
      parameters:
        - in: path
          name: userID
          required: true
          schema: { type: string }
      responses:
        '200': { description: Approved and audited }
        '404': { description: No pending account with that ID }
  /admin/access-settings:
    get:
      summary: Read sign-in, registration and workspace-creation settings (instance administrators only)
      description: Includes whether password sign-in can be turned off safely (an enabled provider and an administrator who has used it) and whether JUSTCD_FORCE_LOCAL_LOGIN is overriding the setting.
      responses:
        '200': { description: Access settings and readiness }
        '401': { description: Authentication required }
        '403': { description: Instance administrator role required }
    put:
      summary: Change any subset of the access settings (instance administrators only; CSRF required)
      description: Turning off password sign-in is refused with 409 unless at least one single sign-on provider is enabled and an active administrator has signed in through one. Domains are normalised; an unchanged save is not audited.
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                localLoginEnabled: { type: boolean }
                localRegistrationEnabled: { type: boolean, description: Let people create a password account; accounts start unverified and need administrator approval. Requires password sign-in. }
                oidcAutoProvision: { type: boolean, description: Create an account on first single sign-on sign-in }
                oidcAllowedDomains: { type: array, items: { type: string }, description: Email domains allowed to create accounts on first sign-in; empty allows any }
                workspaceCreation: { type: string, enum: [anyone, admins] }
      responses:
        '200': { description: Updated and audited access settings }
        '409': { description: Rejected, for example because it would lock everyone out }
  /admin/instance-settings:
    get:
      summary: Read instance maintenance settings (instance administrators only)
      responses:
        '200': { description: Instance settings including maintenanceEnabled and the access settings }
        '401': { description: Authentication required }
        '403': { description: Instance administrator role required }
    put:
      summary: Enable or disable persisted maintenance mode (instance administrators only; CSRF required)
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [maintenanceEnabled]
              properties:
                maintenanceEnabled: { type: boolean }
      responses:
        '200': { description: Updated and audited maintenance settings }
        '400': { description: Invalid or missing maintenanceEnabled }
        '401': { description: Authentication required }
        '403': { description: Administrator role and valid CSRF token required }
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
      summary: Return the guided-setup progress for one workspace (instance admin only)
      description: Progress is scoped to the workspace given by workspaceId, or the first accessible workspace. A cluster counts as connected when its agent checked in recently or a direct connection passed its access check. Instance-wide operator checks are returned separately as system and never block the guided steps. Credential values are never returned.
      parameters:
        - in: query
          name: workspaceId
          schema: { type: string }
      responses:
        '200': { description: Ordered guided steps, the workspace clusters with their connection state, the next required step, and separate system checks }
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
      description: Provider and repository are derived from the Git source; saving an unknown host as GitLab confirms the provider. An API token is required to poll PRs/MRs every two minutes and maintain one updated summary comment. commentMode accepts summary (default), action-required, or off. commentOnDrafts defaults to false and affects comments only; preview deployment policy is unchanged. Commit statuses require explicit pipelineStatusReporting=true (default false); GitLab can attach external jobs to existing pipelines and failures can block merges. A webhook secret is optional for faster updates. Empty secret fields preserve existing encrypted values on update. A preview profile is optional and disabled by default. Saving preserves the enabled state.
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
      summary: Queue plan generation without waiting for Git or Kubernetes
      parameters:
        - in: header
          name: Idempotency-Key
          schema: { type: string, maxLength: 128 }
          description: Retrying the same request returns its original task. Concurrent equivalent requests share active work.
      responses:
        '202':
          description: Durable background task. Poll its Location or /tasks/{taskID}; a successful result contains the immutable review plan and a failed result contains the structured API error.
          content:
            application/json:
              schema: { $ref: '#/components/schemas/BackgroundTask' }
        '409': { description: Idempotency key was reused with different input }
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
        '409': { description: Git, UID, live configuration, or existing ownership changed; older reviews also require the same resource version }
  /applications/{applicationID}/adopt-batch:
    post:
      summary: Claim selected, freshly reviewed resources for this application (workspace owner only)
      description: Validates the full selection against the reviewed UID, live configuration, ownership, and source inputs before claiming any resource. Status-only updates do not invalidate the review. Each claim uses the latest UID and resource-version preconditions, with up to three attempts for harmless concurrent updates; a configuration or ownership change is reported as a partial result. Auto-sync is paused and a fresh approved plan is required before workload changes.
      responses:
        '202': { description: Queued as a background task; the finished task result lists per-resource claimed or failed results }
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
      summary: Create a new immutable plan with exclusions or reviewed StatefulSet recreation
      description: Normal plans accept resources and fields exclusions and recreations entries with identity and propagationPolicy (Orphan, Foreground, Background). Recreation must target an immutable StatefulSet update, cannot combine with field exclusions, and requires Retain PVC policies and safe claim owner references. It creates a fresh owner-approved plan using the higher sync/deletion approval count. Decommission plans accept only keepNamespaces (boolean), restricted to workspace owners. The option retains explicitly managed Namespace resources while deleting other app resources. This creates a new immutable snapshot requiring fresh approval; live state is rechecked during approval and apply.
      responses:
        '201': { description: Selected plan snapshot }
        '409': { description: Source plan is stale; includes refreshed plan }
  /application-preferences:
    get:
      summary: List the current user's personal saved application views and accessible favorites
      responses:
        '200': { description: Saved views with default selection and favorite application IDs }
  /application-views:
    post:
      summary: Save a personal application view and select it as the default
      description: Up to 20 views per user. Configuration includes grouping, labelKey, search, status, cluster, filters, layout, sort and favoritesOnly.
      responses:
        '201': { description: Saved view }
        '400': { description: Invalid view configuration }
        '409': { description: Duplicate name or view limit reached }
  /application-views/default:
    put:
      summary: Select a saved view to reopen on the next visit
      description: Accepts viewId. An empty viewId selects the standard default view.
      responses:
        '200': { description: Default selection saved }
        '404': { description: View does not belong to this user }
  /application-views/{viewID}:
    put:
      summary: Update a personal saved view and select it as the default
      responses:
        '200': { description: Saved view }
        '404': { description: View does not belong to this user }
    delete:
      summary: Delete a personal saved view
      responses:
        '200': { description: Saved view deleted }
        '404': { description: View does not belong to this user }
  /applications/{applicationID}/labels:
    put:
      summary: Replace an application's organizational labels (workspace owner)
      description: Accepts labels as a string map. Maximum 32 labels with Kubernetes label syntax. Git-managed labels must be changed in metadata.labels in justcd.yaml. No Kubernetes objects are modified.
      responses:
        '200': { description: Updated application }
        '400': { description: Invalid labels }
        '403': { description: Workspace owner permission required }
        '409': { description: Labels are managed by Git or a branch test }
  /applications/{applicationID}/favorite:
    put:
      summary: Set the current user's favorite flag for an accessible application
      description: Accepts favorite as a boolean. Requires at least workspace viewer permission.
      responses:
        '200': { description: Favorite flag saved }
        '403': { description: Workspace access denied }
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
  /applications/{applicationID}/health-gate:
    get:
      summary: Post-deploy health gate and its latest check (workspace viewer)
      description: Declared with spec.healthGate in justcd.yaml. A successful sync stays pending until the application reports Healthy or the timeout passes. Failed checks mark the GitLab deployment failed, raise an unhealthy notification and, with onFailure prepare-rollback, prepare a rollback plan that still needs owner approval.
      parameters:
        - { name: applicationID, in: path, required: true, schema: { type: string } }
      responses:
        '200': { description: enabled flag, the declared gate (timeoutSeconds, onFailure) and latest check (status pending, passed or failed, message, deadline, rollbackPlanId) }
        '403': { description: Workspace access required }
  /applications/{applicationID}/environment:
    get:
      summary: GitLab environment and latest reported deployment (workspace viewer)
      parameters:
        - { name: applicationID, in: path, required: true, schema: { type: string } }
      responses:
        '200': { description: reporting flag, resolved environment (name, tier, url), whether it is declared in justcd.yaml, and the latest deployment report }
        '403': { description: Workspace access required }
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
      parameters:
        - in: query
          name: limit
          schema: { type: integer, minimum: 1, maximum: 100, default: 50 }
        - in: query
          name: offset
          schema: { type: integer, minimum: 0, maximum: 1000000, default: 0 }
        - in: query
          name: q
          description: Case-insensitive literal search of messages, operation/plan/actor IDs, error codes, and terminal reasons; applied before pagination.
          schema: { type: string, maxLength: 256 }
        - in: query
          name: status
          schema: { type: string, enum: [queued, running, succeeded, failed, cancelled] }
        - in: query
          name: type
          schema: { type: string, enum: [sync, rollback, resource_action] }
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
      summary: Queue independent approval checks for up to 20 reviewed plans
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
        '202': { description: One accepted task or validation error per plan. Poll each accepted task to determine approval success; partial success is possible. }
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
      summary: Queue approval of the exact reviewed plan as an eligible workspace member
      parameters:
        - in: header
          name: Idempotency-Key
          schema: { type: string, maxLength: 128 }
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [planDigest]
              properties:
                comment: { type: string, maxLength: 1000 }
                planDigest: { type: string, description: Digest reviewed by the approver }
      responses:
        '202':
          description: Durable background task. The worker rechecks current authorization and plan freshness before recording approval. Successful task results contain the approval and any queued automatic sync operation.
          content:
            application/json:
              schema: { $ref: '#/components/schemas/BackgroundTask' }
        '409': { description: Reviewed digest does not match or idempotency key was reused }
  /tasks:
    get:
      summary: List the current user's active and recent background tasks
      parameters:
        - in: query
          name: applicationId
          schema: { type: string }
        - in: query
          name: workspaceId
          schema: { type: string }
      responses:
        '200': { description: Up to 100 tasks, active work first. Requires applicationId or workspaceId and current workspace access. }
  /tasks/{taskID}:
    get:
      summary: Read the current user's durable task status and outcome
      parameters:
        - in: path
          name: taskID
          required: true
          schema: { type: string }
      responses:
        '200':
          description: Task status; result and httpStatus are populated on completion
          content:
            application/json:
              schema: { $ref: '#/components/schemas/BackgroundTask' }
        '404': { description: Task does not exist or belongs to another user }
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
    PublishingServiceSelection:
      type: object
      properties:
        database: {type: boolean, default: false}
        vaultSecrets: {type: array, items: {type: string}, description: Approved binding IDs from the selected profile}
        databaseConfig:
          type: object
          properties:
            name: {type: string}
            username: {type: string}
            envNames: {type: object, additionalProperties: {type: string}, description: "Connection field to environment-name mappings; allowed only when enabled in the profile. Empty mapping omits the field."}
        vaultConfig:
          type: object
          additionalProperties:
            type: object
            properties:
              pathSuffix: {type: string, description: Relative path beneath the approved binding path}
              envs:
                type: array
                items:
                  type: object
                  required: [name, key]
                  properties:
                    name: {type: string}
                    key: {type: string}
        environment:
          type: array
          maxItems: 30
          description: Non-secret app configuration permitted by the profile; persisted in Git
          items:
            type: object
            required: [name, value]
            properties:
              name: {type: string}
              value: {type: string, maxLength: 4096}
    PublishingProfileInput:
      type: object
      required: [name, enabled, settings]
      properties:
        name: {type: string, maxLength: 100}
        description: {type: string, maxLength: 1000}
        enabled: {type: boolean}
        version: {type: integer, description: Optimistic save counter required for updates; distinct from currentRevision}
        copyFromProfileId: {type: string, description: "Create only. Copy stored credentials from a profile in this workspace; explicit non-empty credentials override copied values. Requires workspace owner."}
        publishRevision: {type: boolean, default: false, description: Explicitly publish a recipe revision even if settings are unchanged}
        secrets:
          type: object
          writeOnly: true
          properties:
            gitlabToken: {type: string, writeOnly: true}
            gitReadToken: {type: string, writeOnly: true}
            registryUser: {type: string, writeOnly: true}
            registryPassword: {type: string, writeOnly: true}
            packageUser: {type: string, writeOnly: true, description: "Read-only account for authenticated package mirrors"}
            packageToken: {type: string, writeOnly: true}
        settings:
          type: object
          required: [templateId, gitlabUrl, group, clusterId, builderImage, registryPrefix, secretMode, syncPolicy]
          properties:
            templateId: {type: string, enum: [shiny, streamlit, shiny-geospatial, python-static, static]}
            gitlabUrl: {type: string, format: uri}
            group: {type: string}
            clusterId: {type: string}
            namespaceMode: {type: string, enum: [fixed, per-application], default: fixed}
            namespacePrefix: {type: string, description: Required in per-application mode; non-system DNS prefix ending in a hyphen}
            namespace: {type: string, description: Required only for fixed mode; must be empty in per-application mode}
            webServerImage: {type: string, description: Required for static and python-static; unprivileged NGINX-compatible image on port 8080}
            baseImage: {type: string, description: Required for Shiny and Python build runtimes; unused for ready-made static websites}
            builderImage: {type: string}
            registryPrefix: {type: string}
            hostSuffix: {type: string}
            pullSecret: {type: string}
            runnerTag: {type: string}
            exposure: {type: string, enum: [route, ingress], description: "Resource that publishes the application: OpenShift Route (default) or Kubernetes Ingress."}
            exposureLabels: {type: string, description: "Newline-separated key=value labels for the Route or Ingress only."}
            exposureAnnotations: {type: string, description: "Newline-separated key=value annotations for the Route or Ingress only."}
            ingressClass: {type: string, description: "Optional ingressClassName; requires exposure ingress."}
            ingressTls: {type: string, enum: [hosts, none], description: "Ingress TLS block: hosts (default) lists the host, none renders tls: []."}
            serviceCatalog: {type: string, description: "JSON-encoded optional approved StackGres database plan and VaultStaticSecret bindings; no secret values."}
            templateParameters: {type: string, description: "Optional non-secret key=value lines available as .Parameters in custom templates."}
            packageSources: {type: string, description: "JSON list of package mirrors: [{ecosystem: pip|cran|npm|conda|apt|apk, url: https mirror, authenticated: boolean}]. Builds route that package manager through the mirror; credentials come from the package credentials (variables mode) or the Vault fields package_username and package_token."}
            packagePolicy: {type: string, enum: ["", enforce], description: "enforce rejects uploads that name another package index, a Git dependency or a host outside the approved sources."}
            templateVersion: {type: integer, minimum: 0, description: "Workspace template version used by this profile; 0 or omitted means the built-in template."}
            runnerServiceAccount: {type: string, description: "Optional Kubernetes-executor service account override for the build job."}
            runnerScc: {type: string, description: "Optional OpenShift SCC the build pod must run under (openshift.io/required-scc)."}
            builderSetup: {type: string, description: "Optional executable in the builder image run before the build, for example a rootless Podman entrypoint script."}
            gitImage: {type: string, description: "Separate Git-capable deployment update job image; omitted reuses the builder image."}
            buildEngine: {type: string, enum: [buildkit, podman], description: "Optional build engine; omitted means BuildKit."}
            caVariable: {type: string, description: "Optional inherited GitLab variable containing PEM CA certificates."}
            secretMode: {type: string, enum: [variables, vault]}
            vaultAddress: {type: string}
            vaultRole: {type: string}
            vaultPath: {type: string}
            syncPolicy: {type: string, enum: [manual, auto-safe]}
    RepositoryPublishingInput:
      type: object
      additionalProperties: false
      required: [workspaceId, sourceId, revision, appDir]
      properties:
        workspaceId: {type: string}
        sourceId: {type: string, description: Existing Git source owned by this workspace; shared sources cannot be modified}
        revision: {type: string, description: Existing branch to inspect and target}
        appDir: {type: string, description: Repository-relative build context; use . for the root}
        profileId: {type: string, description: Optional approved profile; restricts writes to its GitLab origin and group}
        name: {type: string, description: App name when using a profile}
        entryPoint: {type: string}
        buildCommand: {type: string}
        outputDir: {type: string}
        gitlabToken: {type: string, writeOnly: true, description: Manual setup only; API token used for this request and never persisted}
        clusterId: {type: string, description: Manual setup only}
        config:
          type: object
          description: Manual publishing Config; same fields as ZIP publishing. Unused base or builder images may be omitted when reusing existing builds.
        image: {type: string, description: Required when keeping existing CI; immutable registry image reference with sha256 digest}
        planHash: {type: string, description: Required for creation; hash returned by the unchanged reviewed plan}
    DeploymentWindow:
      type: object
      additionalProperties: false
      required: [timeZone, days, start, end]
      properties:
        timeZone: { type: string, description: Explicit IANA time zone; Local is not permitted }
        days: { type: array, minItems: 1, maxItems: 7, uniqueItems: true, items: { type: integer, minimum: 0, maximum: 6 }, description: Start weekdays; Sunday is 0 }
        start: { type: string, pattern: '^([01][0-9]|2[0-3]):[0-5][0-9]$', description: Inclusive local HH:MM }
        end: { type: string, pattern: '^([01][0-9]|2[0-3]):[0-5][0-9]$', description: Exclusive local HH:MM; earlier means overnight; equal means full selected day }
    DeploymentFreeze:
      type: object
      additionalProperties: false
      required: [reason]
      properties:
        reason: { type: string, minLength: 1, maxLength: 1000 }
        start: { type: string, format: date-time, description: Optional inclusive fixed instant; cannot be used with schedule }
        end: { type: string, format: date-time, description: Optional exclusive fixed instant; cannot be used with schedule }
        schedule: { $ref: '#/components/schemas/DeploymentWindow' }
    DeploymentPolicy:
      type: object
      additionalProperties: false
      properties:
        windows: { type: array, maxItems: 16, items: { $ref: '#/components/schemas/DeploymentWindow' }, description: Union within scope; empty allows all hours subject to freezes }
        freezes: { type: array, maxItems: 16, items: { $ref: '#/components/schemas/DeploymentFreeze' }, description: Every active freeze blocks applies }
    DeploymentSchedule:
      type: object
      required: [policy, managedByGit, decision]
      properties:
        policy: { $ref: '#/components/schemas/DeploymentPolicy' }
        managedByGit: { type: boolean }
        decision:
          type: object
          required: [allowed, reasons]
          properties:
            allowed: { type: boolean }
            reasons: { type: array, items: { type: string } }
            nextAllowedAt: { type: string, format: date-time, description: Next allowed start found in the next weekly cycle after active finite freezes; omitted if none is found }
    BackgroundTask:
      type: object
      required: [id, applicationId, kind, status, createdAt]
      properties:
        id: { type: string }
        applicationId: { type: string }
        kind: { type: string, enum: [plan, approval] }
        status: { type: string, enum: [queued, running, succeeded, failed] }
        result: { description: Sanitized plan or approval on success, structured ErrorResponse on failure }
        httpStatus: { type: integer, description: Status returned by the background action }
        createdAt: { type: string, format: date-time }
        startedAt: { type: string, format: date-time }
        finishedAt: { type: string, format: date-time }
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
