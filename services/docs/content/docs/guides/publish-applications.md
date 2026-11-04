---
title: "Publish applications from templates"
description: "Create a private GitLab project, build an image in CI, and register a JustCD deployment from an application ZIP."
---

Choose **Applications → New → From a template**. The searchable catalog offers
Shiny, Streamlit, Shiny Geospatial, Static website, and Static website · Python. After selecting a template, choose
**Use a publishing profile** or **Manual setup**. Workspace owners can use the
manual wizard whether or not profiles exist; deployers use approved profiles. With an enabled publishing profile, workspace owners and deployers
choose a workspace, application name, and approved destination, then upload their
complete application ZIP. Infrastructure credentials and build configuration are
resolved on the server. An optional entry-point setting supports nested app folders.

## Configure publishing profiles

Open **Workspace settings → Publishing profiles** as a workspace owner or instance
administrator. Create a profile for each approved template/destination combination:

- A name and description analysts can understand, such as “Analytics development.”
- A supported template, existing workspace cluster and namespace binding.
- Approved runtime and builder image references, GitLab HTTPS origin/group,
  registry prefix, and optional runner tag / image pull secret.
- An optional OpenShift domain suffix. App `report` uses `report.<suffix>`.
  This creates a direct TLS route and does not configure SSO.
- Deployment behavior: `auto-safe` follows normal workspace approval rules after
  a successful build; `manual` waits for an authorized plan review and apply.
- A provisioning API token and separate Git repository read token, plus registry
  credentials or references to an existing Vault secret.

Profile secrets use JustCD's encrypted credential store. Only owners/admins can
create, inspect configuration, update, or delete profiles. Secret values are never
returned, including when editing. Leave secret fields blank to retain their stored
values. The analyst profile list contains only names, descriptions, template and
approved destination metadata; it omits image references, GitLab configuration,
credential references and secrets.

Workspace **deployers** and owners can publish through enabled profiles. Viewers
cannot publish. A request cannot override a profile's cluster, namespace, image,
registry, GitLab location, sync policy, or secrets. The selected workspace must
still have cluster access and the namespace binding at publishing time.

Each application retains its original profile. Uploading a new version reuses its
settings and stored credentials; no token prompt is shown. Credential rotation
updates the shared Git read credential by reference and refreshes registry CI
variables on subsequent publishing requests. Vault secret lifecycle remains with
Vault. Disabling a profile blocks new applications and source updates; it does
not stop existing workloads or cancel already-started CI builds.

Repository and deployment settings are locked after the first application uses a
profile. Runtime base images, static webserver images, CI builder images, runner
tags, and image-pull secret references remain editable, along with names, descriptions, enabled state, and credentials. Create
another profile for other infrastructure changes. Build-setting changes publish a
new immutable profile revision. Names, descriptions, enablement and credential
rotation do not create revisions unless the administrator explicitly requests a
new recipe revision. Existing apps remain pinned until explicitly
upgraded; normal source uploads retain their current revision and use current
credentials. Updates use optimistic save counters (separate from published
revision numbers), and
in-use profiles cannot be deleted. Deleting an unused profile also removes its
private credentials; references elsewhere prevent deletion.

The original six-step wizard remains available to workspace owners through
**Manual setup** on the publishing-options screen, whether or not profiles exist. Legacy manual
applications still require a provisioning token when uploading new source.

## Advanced manual setup

Workspace owners can upload a complete application ZIP and follow six steps:
application, repository, build, secrets, deployment, and review. JustCD generates the Dockerfile, GitLab pipeline,
and Kubernetes deployment template. Application code runs only in CI and the
selected cluster, never inside the JustCD API process.

## Platform prerequisites

- A workspace with a cluster and an existing namespace binding. The initial
  publishing flow does not create clusters or namespaces.
- An HTTPS GitLab instance supporting CI job-token Git pushes. GitLab 18.4+
  is recommended. A provisioning token needs API access, project creation in the
  selected group, protected branch configuration, commits, variables, and pipelines.
- A separate group access token with `read_repository` that can read newly created
  private projects. Profile credentials are stored encrypted. In advanced manual
  setup, only the read credential is retained; the provisioning token is used for
  the request only.
- A compatible GitLab runner and builder image for the selected engine. Podman
  requires Podman and basic shell tools (see Podman publishing below).
  BuildKit requires an approved rootless BuildKit image containing
  `buildctl-daemonless.sh`, `git`, `jq`, a POSIX shell, and internal CA trust.
  An example builder Dockerfile is supplied in `services/backend/packaging/publisher`.
  The runner must permit the rootless builder and reach GitLab, Artifactory,
  package mirrors, and (when selected) Vault. This flow does not install runners.
- An approved application base image containing R/Shiny or Python, system
  dependencies, and package mirror/CA configuration. For Shiny geospatial apps,
  provide GDAL/GEOS/PROJ and other required native libraries in that image.
- An Artifactory container repository and a token allowed to push into the selected
  image path. OpenShift uses a separate, pre-existing read-only image pull secret.

Pin approved builder and runtime image versions or digests. These references are
currently supplied in the wizard. The catalog contains versioned built-in recipes
served by `GET /api/v1/publishing/templates`; administrator-editable templates
are not part of this first implementation.

## Prepare an application archive

ZIP the **contents** of the project directory, preserving relative paths. A ZIP
containing an extra enclosing directory requires that directory in the entry
point, but dependency files still belong at the archive root.

For Shiny, include `renv.lock` or `packages.txt` at the archive root. The selected
app directory must contain `app.R`, or both `ui.R` and `server.R`. Choose `.` when
these files are at the root. A plain package list installs current package versions;
prefer a lockfile for reproducibility. The generated R build uses the base image's
`R_REPOSITORY` environment setting, falling back to CRAN.

For Streamlit, include `requirements.txt` at the archive root and select the Python
entry file, such as `app.py`. Pin dependency versions where appropriate.

Default limits are **100 MiB compressed**, **500 MiB expanded**, and 1500 archive
entries. The forms fetch the current limits from the backend. Configure
`JUSTCD_PUBLISH_MAX_ARCHIVE_MIB` and `JUSTCD_PUBLISH_MAX_EXPANDED_MIB` (integer MiB,
1–10240) and restart the backend to change them. Compose exposes both settings;
Helm uses `backend.publishing.maxArchiveMiB` and `maxExpandedMiB`.

Uploads use multipart/form-data: a bounded JSON `metadata` part followed by the
binary `archive` file. The frontend proxy forwards the request stream. After
workspace authorization, the backend streams the archive and expanded files into
a private temporary directory, hashes source incrementally, and stages GitLab's
JSON/base64 commit request on disk. It does not buffer the complete source or
commit payload in memory. `GET /api/v1/publishing/limits` returns the configured
byte limits to authenticated clients. Oversized uploads return HTTP 413.

Set `JUSTCD_PUBLISH_TEMP_DIR` (Helm `backend.publishing.tempDir`) to a writable,
disk-backed directory, or leave it empty for the OS temporary directory. Files
are private to the runtime user and removed when the request finishes, including
validation/provider failures and cancellation. An abrupt process kill can leave
stale directories requiring operational cleanup. Allow space per concurrent
request for the ZIP, expanded source, and approximately 4/3 of the source size
for the GitLab commit payload: roughly 1.3 GiB at the default limits. A memory-backed
volume would consume RAM and should be avoided for large projects.

The upload read deadline is five minutes and the overall response write deadline
is fifteen minutes. Configure ingress/reverse-proxy body limits and timeouts to
match the selected sizes. GitLab's own request, repository, and file limits still
apply; raising JustCD's limit does not raise those downstream limits.

Links,
unsafe paths, duplicate files, `.git`, `.env`, `.Renviron`, `.Rhistory`, `.RData`,
private SSH key filenames, and `secrets.toml` are rejected. These checks are not a
content secret scanner: remove embedded credentials before upload. Large datasets
should be provided through an independently managed data service or storage mount;
this initial template does not provision databases or data storage.

All uploaded files are committed below `source/`, separate from generated CI and
deployment configuration. Dependency files and startup code execute in the isolated
build environment, so restrict runner credentials to the application's needs.

## Choose secret storage

**GitLab CI/CD variables** creates protected `REGISTRY_USER` and masked/protected
`REGISTRY_PASSWORD` project variables through the GitLab API. The password must be
at least eight characters without whitespace. Values are never committed to Git
as source files. Profiles retain registry credentials encrypted in JustCD so
analysts can publish subsequent versions; advanced manual setup uses them for the
request only. The publishing branch is protected before CI starts.

**Vault** references an existing KV v2 secret through GitLab's native OIDC/Vault
integration. This requires a GitLab tier that supports external Vault secrets.
Provide the HTTPS Vault address, JWT role, and API path, for example
`secret/data/analytics/registry`. The secret contains `username` and `token` fields.
Configure the JWT auth mount at GitLab's default `jwt` path and bind the role to
this project/group, protected branch, and configured audience. No Vault administrator
token is requested. JustCD does not create Vault policies, write secret values,
or configure application runtime secrets in this initial flow.

Authenticated package mirrors must be configured by the platform's approved build
profile. Do not bake package credentials into a base image, source file, or image
URL. The generated first-version template does not yet supply package-mirror build
secrets; a mirror that requires per-build authentication needs an extended build
profile before this flow can be used with it.

## Build and deployment behavior

JustCD creates a private project and enables same-project CI job-token Git pushes.
The initial source commit skips CI until local registration and branch protection
are ready. JustCD then explicitly starts the pipeline.

The generated pipeline builds and pushes an image tagged by source commit, reads
its immutable digest, and commits `deploy/application.yaml` using the ephemeral
CI job token. It checks that the source commit is still the branch head before
publishing the deployment manifest. Newer source uploads prevent stale builds
from replacing the desired release. There is no force push.

The registered JustCD application uses `main`, renderer `yaml`, and path `deploy`.
Profile applications use the administrator-selected sync policy; advanced manual
publishing uses **manual** sync. Before the first successful build, this path contains only
`.gitkeep`, so planning may report no manifests. After the pipeline succeeds,
calculate a plan and apply it through normal workspace approval controls.

A hostname creates an OpenShift TLS Route directly to the app. **This initial
runtime template does not configure SSO.** Leave the hostname blank for internal
Service access, or add your platform's authentication configuration before exposure.
The template supplies a Deployment, Service, basic resource limits, TCP health
checks, disabled service account token mounting, and restricted container settings.
It must still be validated against the target cluster's SCC/admission policies.

## Upload a new version

On an application created by this flow, choose **More actions → Upload new
version**. Upload the complete source snapshot and provide a GitLab provisioning
token for legacy manual applications. Profile applications use stored credentials.
Existing runtime settings, registry variables/Vault references, Git read
credential, and deployment target are reused. Files absent from the new source
snapshot are removed from `source/`. The existing deployment manifest is retained
until the new build succeeds. Review and apply the new plan to deploy it.

This source-update flow retains the original publication settings. Manage source
through uploads consistently; manual edits in the generated repository can conflict
with subsequent generated configuration. Image rollback continues to use JustCD's
existing rollback plans; external database contents are not restored.

## Failures and retries

Provider error bodies are not forwarded because they may contain credentials.
GitLab HTTP status and a safe action summary are returned instead. Generated
repositories are never automatically deleted after a failure. Retry the same
archive and settings to resume a partial initial setup. Existing repositories are
only resumed when their description bears this workspace's deterministic managed
publication marker; unrelated projects are never adopted.

Git credentials, source, application, and publication metadata are registered in
one database transaction. A bounded database advisory lock serializes API setup
requests for the same repository across replicas. CI execution is asynchronous;
follow the returned GitLab pipeline link for logs and status. If a pipeline starts
but recording its URL fails, inspect GitLab before retrying, because a retry can
start another pipeline. Build progress is currently linked from GitLab rather
than streamed into JustCD.

## Static websites generated with Python

Choose **Static website · Python** for reports, documentation, HTML dashboards,
and downloadable PDFs generated by Python. Upload your scripts, templates,
content, and optionally `requirements.txt` at the ZIP root. Set the **Build command**
(for example `python build_html.py`) and **Output directory** (default `dist`).
The command must create `dist/index.html`; CSS, JavaScript, images, and PDFs in
that folder are served alongside it. A minimal build script is:

```python
from pathlib import Path

output = Path("dist")
output.mkdir(exist_ok=True)
(output / "index.html").write_text("<h1>My report</h1>", encoding="utf-8")
```

JustCD generates a multi-stage Dockerfile. The Python build stage installs
`requirements.txt` when present and runs your command in `/app`. It validates
that the output stays in the project directory, contains `index.html`, and has
no symlinks or special files. Only that output folder is copied into the final
webserver image, with readable files and traversable directories for arbitrary
OpenShift UIDs. Python source and build dependencies remain in the build stage.
The existing GitLab pipeline pushes the final image to Artifactory and registers
its immutable digest for deployment through JustCD.

The administrator configures both a **Python build image** (`python` and `sh`
required) and a **Static web server image** in the publishing profile. Manual
setup exposes the same fields. The server image must provide an NGINX-compatible
configuration serving `/usr/share/nginx/html` on port **8080**, inherit a working
startup command, and support UID 1001 and OpenShift arbitrary UIDs. Supply your
approved image with internal certificates and the required writable runtime
paths configured. A regular root-only NGINX image on port 80 will not satisfy
this contract. JustCD retains the image's startup command.

Build commands execute in the CI build environment, with the same runner policy
as other application builds. They are not executed by the JustCD backend.
Published source updates retain the original build command and output directory.
Static websites serve generated files; request-time Python APIs need a separate
application runtime template. Regenerate and publish again to refresh content.

## Reuse an existing Git project

After selecting a template, choose **Use existing Git project**. Connect the
repository under the workspace's Git sources first; JustCD reuses that connection
and its read credential. This path supports HTTPS GitLab repositories and an
existing branch. Choose the branch and application directory (`.` for the root,
`apps/report` for a nested project). The application entry point is relative to
that directory.

Select an approved publishing profile, or configure the destination and images
manually as a workspace owner. For manual setup, a GitLab API token is used only
for the request and never stored. Profile-based proposals require a workspace
deployer role and a repository inside the profile's configured GitLab origin and
group. Images supplied for existing CI must remain under the profile’s registry
prefix. Repositories shared from another workspace cannot be modified here.

**Inspect repository** reports an existing Dockerfile, `.gitlab-ci.yml`, and
possible manifests. Then choose the appropriate route:

- **Source only:** JustCD proposes the template Dockerfile, a GitLab build pipeline,
  and deployment templates. Shiny and Streamlit dependency checks still apply.
- **Existing Dockerfile:** JustCD uses it unchanged, with the selected application
  directory as its build context. It adds missing CI and deployment templates.
- **Existing pipeline:** JustCD preserves the complete build. Supply the immutable
  image reference it produces (`registry/repository/image@sha256:…`); the proposal
  adds deployment manifests referencing that digest. Future image updates remain
  the responsibility of your existing pipeline or Git changes.
- **Existing deployment files:** Workspace owners can choose **Use existing
  manifests**. The standard application wizard opens with workspace, Git source,
  branch, name, and manifest directory prefilled. Choose YAML, Kustomize, or Helm
  there as appropriate. No merge request or build changes are necessary.

**Review proposed files** displays every addition and its contents. Creation
rechecks the commit and settings; changes require a new review. The proposal
uses create-only actions on a new `justcd/setup-*` branch and opens a merge request.
It never writes the target branch or replaces existing files. Conflicts involving
existing generated paths, symlinks, or submodules are rejected.

JustCD also registers an ordinary Git application pointing to `deploy/<app-name>`
with **manual** deployment. Review and merge the proposal, finish the initial
build if using generated CI, then plan and apply through JustCD. The deployment
path is populated by the first successful generated build, so it will not render
until that build completes. The source is not enrolled in ZIP replacement
publishing; future changes come through your existing Git workflow.

For generated CI, a repository maintainer must configure protected
`REGISTRY_USER` and `REGISTRY_PASSWORD` variables (or the selected Vault
integration), protect the selected branch, and enable CI job-token repository
pushes before merging. Existing secrets and project settings are never changed.
After merging, start the selected branch pipeline manually if it did not run
automatically (for example, after a fast-forward merge of the CI-skipped proposal).
The generated job uses the selected build engine (BuildKit by default) and commits the immutable image digest
only if the source branch still matches the built commit. Existing Dockerfiles
must expose the selected template's port (3838 for Shiny, 8501 for Streamlit,
8080 for static websites) and support the platform's unprivileged user policy.

Publishing profiles supply bootstrap defaults for this Git workflow. Their
credentials are not copied into the repository, and later profile edits or
credential rotation do not rewrite the existing project's CI configuration.

The proposal uses GitLab's [batch commit API](https://docs.gitlab.com/api/commits/#create-a-commit)
with `start_sha` and create-only actions, followed by the
[merge request API](https://docs.gitlab.com/api/merge_requests/#create-a-merge-request).

## Publish ready-made HTML files

Choose **Static website** for a site that already contains HTML, CSS, JavaScript,
images, or PDFs. Upload a ZIP containing `index.html` and its assets. The website
directory defaults to `.` (archive root); select a folder such as `public` if the
site is nested. That directory must contain `index.html`.

No Python image, dependencies, build command, or output directory is needed.
JustCD generates a Dockerfile that copies only the selected directory into the
approved static webserver image, makes its files readable by arbitrary OpenShift
UIDs, and serves on port 8080. The same unprivileged NGINX-compatible image
contract as the Python static template applies: `/usr/share/nginx/html` document
root, working inherited startup command, and support for UID 1001 and arbitrary
OpenShift UIDs. The CI builder image is still required to package and push the
container to Artifactory; application files are not built or executed.

Publishing profiles need a webserver image and CI builder image, but no Python
or application runtime base image. Manual setup exposes the same fields.
Existing Git projects are supported too: the website directory is relative to
the selected application directory, existing Dockerfiles and pipelines are
reused, and missing files are proposed through the existing merge request flow.
Choose **Static website · Python** instead when Python scripts must first
produce the HTML.

## Create a namespace while publishing

In manual setup, **Where should the app run? → Create namespace if missing**
lets a workspace owner enter a new namespace name instead of selecting an
existing binding. The same choice is available when preparing an existing Git
project. JustCD registers the workspace binding atomically with the application;
failed uploads, repository inspections, and proposal previews leave no binding.

The application enables namespace provisioning through JustCD's existing
deployment flow. Kubernetes namespace creation follows deployment planning and
apply; OpenShift project requests may provision the project during planning so
that workload access can be established. No namespace is created merely by
uploading an application. The configured workspace/cluster default Kubernetes
credential or cluster agent must support provisioning, and cluster RBAC and
approval rules still apply. System namespaces cannot be provisioned through this
workflow, and a namespace bound exclusively to another workspace cannot be
claimed automatically. Existing image-pull Secrets must be provisioned separately
in the new namespace when required.

Administrators can also select a profile's **Namespace policy**:

- **Use a fixed existing namespace** retains the previous behavior.
- **Create a namespace per application** uses a required prefix such as `apps-`;
  publishing `report` targets `apps-report`. The fixed namespace field is empty
  in this mode, and the complete namespace name must fit within 63 characters.

Analysts see the derived destination before publishing and cannot override the
prefix or enable creation on a fixed-namespace profile. The policy is immutable
once a profile has published applications. Updates retain the application's
original namespace and provisioning choice.

## Resume a publishing draft

Publishing forms automatically save non-secret settings in this browser for
30 days. Drafts are scoped to the signed-in user, workspace, publishing flow,
and (for manual and existing Git setup) template. Reopening the same flow
restores your inputs and the manual wizard step after a reload or backend
restart. A draft notice reports autosave status and offers **Discard draft**.
Successful publishing removes the completed draft.

Credential fields, ZIP contents, acknowledgements, Git inspection results,
and reviewed proposal hashes are not saved. Reselect your ZIP and re-enter
credentials before submitting; existing Git projects require a fresh inspection
and review. Cluster and workspace access are still checked by the server.
Drafts stay on this browser and do not sync across devices. Browser storage
restrictions are reported in the notice. Values lost before draft support was
available cannot be restored retroactively.

## Profile versions and application upgrades

The profile editor shows its latest published version and read-only history.
Saving changed build settings publishes a new immutable snapshot of approved
images, settings and the JustCD recipe version. To release updated generated
recipes without changing images, enable **Publish a new revision even if images
are unchanged** before saving. Existing template applications
show **Profile v1 · v2 available** in the application list and a profile update
notice on their details page. New applications pin the latest published revision.

Select **Review profile update** on a managed template application. JustCD reads
the existing Git repository, shows old/new images and generated-file diffs, and
checks the source commit again when the update is applied. No ZIP is required.
Only generated build recipes are committed; `source/` and the running
`deploy/application.yaml` are preserved. The app is pinned to the selected
revision and a new CI build starts. Successful builds continue through the app's
existing sync and approval policy; automatic apps may deploy after the build.

Ordinary source uploads keep the pinned revision, even after an administrator
publishes newer images. Disabled profiles cannot be upgraded. Repository,
namespace and destination changes require a separate migration/new profile.
The generated repository must retain its original managed identity and `main`
default branch. A recipe version that this server cannot generate is rejected.
If a build cannot start after the revision is recorded, the app shows **build
pending** and its review action can retry starting CI without another ZIP.

Existing installations are migrated using each application's recorded image
settings. Apps whose images differ from current defaults receive historical
revision snapshots and correctly show a newer version available; migration does
not rebuild or move workloads. Historical ordering before this feature was not
recorded, so imported snapshots are numbered deterministically with current
defaults last. Existing Git onboarding remains an ordinary Git/CI workflow and
is not enrolled in managed-template revision upgrades.

### Optional internal CA from GitLab

Set **Custom CA variable** in a publishing profile or manual setup to the name of
an inherited GitLab CI variable containing PEM certificate text, for example
`PLAIN2_ROOT_CA`. Leave it blank for normal builder-image trust. JustCD stores the
variable name, not the certificate, and does not create or overwrite that variable.
It must be available to the generated project's protected default-branch pipeline.
A configured but empty or unavailable variable fails with an actionable error.

The generated job combines the supplied PEM with the builder's system CA bundle
in a temporary directory and configures Git and BuildKit through `GIT_SSL_CAINFO`
and `SSL_CERT_FILE`. TLS verification remains enabled; the certificate is not
copied into application images or committed to Git. Publish a new profile version
and explicitly update/rebuild existing applications to apply this setting.

This does not configure the runner's initial image pull or Git checkout, nor
package downloads inside Dockerfile `RUN` stages. Configure runner trust separately
and use runtime/build base images with the required package-mirror CA trust.


### Podman publishing

Choose **Podman** under **Build engine** in the publishing profile or manual setup,
then supply your maintained rootless Podman image as **CI builder image**. An empty
engine setting preserves the previous BuildKit behavior. Changing an in-use
profile's engine creates a revision; explicitly review and upgrade existing apps
to regenerate their pipeline and rebuild without uploading source again.

Podman jobs use `podman login --password-stdin`, `podman build --format docker`,
and `podman push --digestfile`. The build job requires Podman, grep, a POSIX shell,
CA certificates and basic utilities, but does not require BuildKit, Git or jq when a separate deployment update image is configured. The pushed
registry digest is validated before committing the deployment manifest. Passwords
are passed via standard input; temporary registry authentication is removed when
the job exits. TLS verification stays enabled.

The optional custom CA variable is supported with Alpine and RHEL system CA bundle
paths. Podman login, build and push receive a temporary certificate directory.
Application build stages and the runner's initial pulls/checkout still need their
own trust configuration.

The image and GitLab runner must already support rootless Podman (user namespaces,
storage, writable runtime directories, and applicable OpenShift security policy).
JustCD does not grant privileges, override the image's storage configuration, or
add insecure TLS flags. Selecting Podman does not certify any particular image or
its vulnerability policy; validate your maintained image on your runner.

### Runners that require a service account and SCC

Some OpenShift runners only allow rootless builds under a dedicated service
account and security context constraint (SCC). Without them, Podman fails
immediately with `cannot clone: Operation not permitted` and
`cannot re-exec process`. Set these optional profile fields to match your
platform's working build job:

- **Runner service account** sets `KUBERNETES_SERVICE_ACCOUNT_OVERWRITE`.
- **Required OpenShift SCC** sets the pod annotation
  `openshift.io/required-scc`.
- **Builder setup script** is an absolute path of an executable in the builder image (for example a
  rootless Podman entrypoint script) that runs in `before_script`. Arguments are not
  allowed.

JustCD does not create the service account or SCC; the platform team provides them.
Changing these settings creates a profile revision; upgrade existing apps to
regenerate their pipeline.

### Separate build and Git jobs

Generated pipelines have a build stage (`publish` job) and a deployment-manifest
stage (`update-deployment` job). Set **Deployment update image** to a maintained
image containing Git, sed, grep, CA certificates and a POSIX shell. If omitted,
the deployment job reuses the builder image for compatibility; that image must
then still contain Git. With a separate image, Podman builds require no Git or jq;
BuildKit builds still require jq, but no Git for the publishing workflow itself.
Dockerfiles that explicitly use Git retain their own build-time requirements.

The build job pushes the application image and exports only its immutable digest
as an artifact with a one-day lifetime. The deployment job explicitly downloads
that job's artifact, validates the digest, rejects stale source commits, and
commits/pushes the manifest under the existing serialization lock. Vault registry
secrets are requested only by the build job. Both jobs apply the optional CI CA
variable independently. The runner must be able to pull and run both images.

Git remains JustCD's deployment source of truth; this is not Git-less publishing.
Changing the deployment update image creates a profile revision. Review and apply
that revision on existing managed applications to regenerate CI. Existing Git
onboarding preserves pipelines already owned by the user.
