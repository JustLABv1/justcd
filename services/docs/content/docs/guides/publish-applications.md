---
title: "Publish applications from templates"
description: "Create a private GitLab project, build an image in CI, and register a JustCD deployment from an application ZIP."
---

Choose **Applications → New → From a template**. The searchable catalog offers
Shiny, Streamlit, Shiny Geospatial, Static website, Static website · Python, and any
custom templates your workspace owners created. After selecting a template, choose
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

ZIP the **contents** of the project directory, preserving relative paths. Shiny,
Streamlit and static-site uploads may also have one enclosing project folder:
JustCD removes that folder automatically only when all application files are
inside it and the resulting layout matches the selected template and entry point.
It never guesses between multiple apps. Explicit app directories such as
`src/dashboard` or `public` are checked first and preserved when valid.

The upload component checks ZIP integrity, paths, size limits and template layout
as soon as you select an archive. It shows a success message (including any folder
normalization) or an actionable error beside the upload. Publishing and the manual
wizard cannot continue until the check succeeds. Changing the entry point triggers
another check. This validation uploads a temporary copy but creates no GitLab
repository, pipeline or application; temporary files are removed after validation.
The publishing request repeats the same checks. Python-generated sites validate
archive safety here; the build command and output remain CI checks.

Finder metadata (`__MACOSX/` and `.DS_Store`) is ignored. Dependency files must
be at the project root after any enclosing folder is removed.

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

Package managers can be routed through your own mirror; see
[Package sources and Artifactory](#package-sources-and-artifactory).

## Package sources and Artifactory

Platforms that allow package downloads only through an internal mirror, such as
JFrog Artifactory remote repositories, configure **Package sources** in the
publishing profile (**Build and credentials → Package sources**). Without them,
builds install packages from whatever the base image allows, which for the R
recipe falls back to public CRAN.

For each package manager add one source:

| Package manager | Used by | Typical Artifactory URL |
| --- | --- | --- |
| Python (pip) | Streamlit, Python static, server templates | `https://HOST/artifactory/api/pypi/<repo>/simple` |
| R (CRAN, renv) | Shiny | `https://HOST/artifactory/api/cran/<repo>` |
| Node.js (npm) | custom Node templates | `https://HOST/artifactory/api/npm/<repo>` |
| Conda | custom templates | `https://HOST/artifactory/api/conda/<repo>` |
| Debian/Ubuntu (apt), Alpine (apk) | custom templates that install system packages | `https://HOST/artifactory/<repo>` |

Copy the exact URL from the repository's **Set Me Up** in Artifactory; layouts
differ between versions and repository types. Container registry remotes (Docker,
GCR, GHCR, Quay and similar) are not package sources: reference base images by
their full Artifactory path in the profile. Pulls sign in with the profile's
registry credentials, so base image and destination registry should share a host.

**Sign-in.** Turn on **Requires sign-in** for sources that need it and enter a
username and token. They are stored encrypted with the other profile credentials;
in Vault mode add the fields `package_username` and `package_token` to the
profile's Vault secret instead. Use a **read-only** account: installing packages
runs project code (for example a package's build script), so the token must not
be able to publish. A custom CA for the mirror uses the profile's *Custom CA
variable*; the same bundle is used for package downloads.

**How it works.** JustCD writes `.justcd/package-sources.sh` into the repository.
It contains only URLs. During the build the pipeline joins it with the
credentials into one build secret, which only the install steps mount. Tokens are
never written to an image layer, the source, the repository or the running app.
Your own templates use it by prefixing install steps:
`RUN {{ .PackageMount }}pip install -r requirements.txt`. `.PackageSources` lists
the configured mirrors.

**Job logs.** R prints the URL it downloads from, which contains the sign-in. JustCD
leaves ordinary token characters unencoded in that URL so GitLab recognizes the
masked `JUSTCD_PKG_TOKEN` and hides it in the log. A token containing characters
such as `/` is encoded and may appear in the log; use a token made of letters,
digits and `-_.` (Artifactory access and reference tokens are). If a token ever
appears in a job log, rotate it and erase that job's log in GitLab.

**R and geospatial images.** `renv::restore` rebuilds the whole lockfile from
source, including older versions of packages the base image already has, which
takes very long and fails for `sf` or `terra` without the system libraries. On a
base image that already provides your packages, set the template parameter
`renvRestore=missing`: JustCD then installs only the lockfile packages the image
lacks. Use the Shiny Geospatial template or an image with GDAL, GEOS and PROJ for
spatial packages.

**Other sources.** With **Allow**, the mirror is the default and projects may add
sources. With **Only the sources above**, JustCD rejects the ZIP, naming the file
and reason, when it finds another package index in `requirements*.txt`,
`pip.conf`, `.npmrc`, `.condarc`, `pyproject.toml` or `Pipfile`; Git or direct URL
dependencies; `renv.lock` packages that do not come from a repository;
`package.json` or `package-lock.json` entries from other hosts; or conda channels
on other hosts. The check runs as soon as a ZIP is chosen. It covers the files
listed here; it is a guardrail for honest mistakes, not a sandbox, so keep
network egress controls on the builders as well.

Changing sources publishes a new profile revision. Existing applications adopt it
through the reviewed profile upgrade, which also writes the new script.

## Updating a manually published app

An app created with **manual setup** keeps its build files (Dockerfile, pipeline,
manifest) in its private GitLab repository, written once at publication. Three
actions on the application page (**More actions**, workspace owners) keep it
current without recreating it.

**Upload new version** replaces only the application source and rebuilds. It asks
for a GitLab token (used once, never stored) and reuses the stored settings.

**Update build setup** changes how builds get packages, for example adding your
Artifactory mirrors to an existing app. Enter the package sources and credentials
and a GitLab token, then **Review changes**. JustCD regenerates the Dockerfile,
pipeline and `.justcd/package-sources.sh` from the app's stored settings and
shows every setting and file that would change, including files you edited in
GitLab since publishing. Nothing is committed until you choose **Apply and
rebuild**; a file changed in GitLab after your review stops the update so you can
review again. Applying commits the files, sets `JUSTCD_PKG_USER` and
`JUSTCD_PKG_TOKEN` as masked, protected CI variables in the project, and starts a
build. The registry credentials are not touched. Removing a source removes its use
from the generated files; the old script file stays in Git and is simply unused.

**Convert to profile** binds the app to a publishing profile, so profile updates
(including package sources managed centrally) reach it like any profile app.
Because a profile keeps its destination locked, the app must already match what
the profile would produce: same template, cluster, namespace, registry location,
hostname, secret mode and GitLab group. **Check compatibility** lists every
difference as app value versus profile value; adjust the profile (or create one)
until they agree, then **Convert**. The conversion itself changes no files. The
app shows *Finish converting to the profile*; review that update to adopt the
profile's build settings, which replace the app's own (so mirrors set by hand
disappear unless the profile has them). Uploading a new ZIP waits until that review
is done.

New manual apps can set package sources in the wizard (Build step) the same way.

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

### Route or Ingress, with custom labels

When a profile has an application domain, JustCD publishes the app with an
OpenShift Route by default. Choose **Kubernetes Ingress** to generate a
`networking.k8s.io/v1` Ingress instead (optionally with an ingress class).
**Route / Ingress labels** and **annotations** (one `key=value` per line,
up to 20 each) are applied to that resource only, for example a router-sharding
label your platform requires. Changing them creates a profile revision; existing
apps pick them up through the reviewed profile upgrade, which regenerates
`.justcd/deployment.yaml`. These settings do not touch the Deployment or Service.

### Customizing application templates

Every application template (Shiny, Streamlit, static website, Python static
website, Shiny geospatial) consists of five files that JustCD generates into the
application repository: `Dockerfile`, `.gitlab-ci.yml`, `.justcd/deployment.yaml`
(the Kubernetes manifest), `.dockerignore` and `.justcd/README.md`. Workspace owners
can see and edit them under **Settings → Publishing profiles → Application
templates**.

1. Choose **Customize** for a template. The editor starts from the latest custom
   version, or the built-in original.
2. Edit the files. They use Go template syntax with placeholders such as
   `{{ .Name }}`, `{{ .Namespace }}`, `{{ .Host }}`, `{{ .BaseImage }}` and
   `{{ .RegistryImage }}`, and conditions such as `{{ if .Host }}…{{ end }}`.
   The **Placeholders** list in the editor shows every value with its meaning and
   can insert it at the cursor.
3. Review **Compare changes** for a diff against the starting version. In **Edit**,
   rendered output appears beside the template and updates automatically. Select
   **Preview with** to render with the real settings of one of your profiles.
4. **Save as new version.** Versions are immutable and numbered per template
   (`v1`, `v2`, …); the built-in template stays untouched.
5. In a profile, choose the **Template version**. Saving that selection publishes a
   new profile revision. Existing applications keep their version until their
   owner reviews the profile upgrade, which shows the diff of the regenerated
   files (including `.justcd/deployment.yaml` and `.gitlab-ci.yml`) before anything
   is committed.

```yaml
# .justcd/deployment.yaml – add a label, more replicas and a NetworkPolicy
spec:
  replicas: 2
  template:
    metadata:
      labels: {app: {{ .Name }}, cost-center: "4711"}
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: {{ .Name }}-allow-router
  namespace: {{ .Namespace }}
spec:
  podSelector: {matchLabels: {app: {{ .Name }}}}
  policyTypes: [Ingress]
```

### Creating your own templates

Besides customizing the built-in templates you can add templates of your own, for
example for Dash, Quarto, Plumber or Node apps. Owners manage them under
**Settings → Publishing profiles → Application templates**; custom templates are
shown to analysts in the catalog with a **Custom** badge.

- **New template** asks for a name, an ID (fixed once created), a kind and, for
  server applications, the container port and default entry point. The kinds are
  *Server application* (a base image, your start command in the `Dockerfile` and a
  port), *Static website* (ready-made files) and *Static website built with Python*.
  JustCD adds editable starter files; the server starter reads its start command
  from the profile's template parameter `startCommand` (for example
  `startCommand=python app.py`) or from the `CMD` line.
- **Copy as new template** (in a row's menu) copies any built-in or custom
  template, including its latest files, upload checks and port.
- **Template settings** changes the name, description, entry point, port and
  upload checks of a custom template. The ID and kind cannot change.
- **Export** downloads a template version as a `justcd.template/v1` JSON file;
  **Import** adds such a file to another workspace after validating its files. You
  can change the name and ID before importing.
- **Delete template** removes a custom template and its versions, but only while no
  profile (or earlier profile revision) uses it.

**Upload checks** list the files a ZIP must contain, one requirement per line.
Separate alternatives with `|` and files that are all required with `,`; `{entry}`
stands for the entry point. For example `{entry}` followed by
`requirements.txt | pyproject.toml` requires the entry file and either dependency
file. Leave the box empty to accept any non-empty project.

A custom template always runs a pinned version: profiles use the newest version
unless you pick another, and applications keep their version until the reviewed
profile upgrade. Creating templates is limited to workspace owners; custom
templates are subject to the same sandbox and safety validation described below.

**Safety rules.** Templates are rendered in a sandbox: only the listed placeholders
and the helpers `quote`, `json`, `list` and `default` exist; there is no `define`,
`template` or `block`, no file or network access, and the output is limited to
1 MiB. On every save JustCD renders the template for a matrix of settings (both
build engines, Vault and variables, with and without a domain, Route and Ingress,
with and without `renv.lock`) and rejects the version if any rendering fails
validation:

- The manifest must contain exactly one Deployment and one Service named after the
  application, all resources in the application namespace, and only these kinds:
  Deployment, Service, Route, Ingress, ConfigMap, NetworkPolicy,
  PodDisruptionBudget, HorizontalPodAutoscaler. Route and Ingress must use the
  application host and target its Service.
- The `app` container keeps `JUSTCD_IMAGE_DIGEST` as its image. Additional
  containers must come from the profile's registry host and carry a tag or digest.
- Rejected: privileged containers or privilege escalation, running as root, added
  Linux capabilities, host network/PID/IPC, `hostPath` and other unsafe volume types,
  `hostPort`, service account changes, enabling the service account token,
  `NodePort`/`LoadBalancer` services, and certificate private keys.
- The pipeline must keep the build job that publishes the image digest as the
  artifact `.justcd-build/image-digest`; JustCD reads and verifies it to commit the
  deployment manifest. The Dockerfile must contain a `FROM` instruction.

Existing-project onboarding (merge request proposals) always uses the built-in
files. The pipeline is trusted administrator content that runs in your CI with the
profile's registry credentials, so review changes to it like any other code.

### Optional network policies

Built-in templates add no network policies. Workspace owners can opt in for
platforms that require them:

1. Open **Workspace settings → Publishing profiles → Application templates → Customize**.
2. Click **Add ingress network policy**. The editor adds
   `.justcd/manifests/network-policy.yaml`, using the application's name,
   namespace and runtime port. Edit the policy to match your platform's callers.
3. Save a new template version. Select that version in the publishing profile.
4. Set **Template parameters** to `ingressNamespace=<actual ingress controller namespace>`.
   This is a non-secret `key=value` field, not a credential store. Verify the
   namespace and any required pod selectors with your platform administrator.
5. In the template editor, choose that profile in **Preview with** to inspect the
   rendered policy. Existing apps upgrade through the profile update review;
   new apps use the profile's selected template version.

The starter restricts ingress to TCP on the application's runtime port from the
configured namespace. Its fallback `replace-with-ingress-namespace` is a
placeholder, not a discovered cluster setting. It intentionally does not assume
OpenShift or any particular ingress namespace. Add a `podSelector` alongside
`namespaceSelector` in the **same peer** if only specific ingress-controller pods
should be allowed. Separate peers are alternatives (OR); selectors in the same
peer must both match (AND). Host-network ingress controllers may require a
platform-specific policy instead of this starter.

Selecting app pods with an ingress NetworkPolicy isolates their incoming traffic.
Include other required callers, such as monitoring and other services, before
applying. Network policies are additive and require a network plugin that enforces
them; JustCD does not change cluster-wide network configuration.

Use **Add another optional manifest** for additional policies or other supported
resources. Custom versions accept up to ten optional files matching
`.justcd/manifests/<lowercase-name>.yaml` (128 KiB each). These may contain
NetworkPolicy, ConfigMap, PodDisruptionBudget or HorizontalPodAutoscaler resources.
They are rendered in filename order and combined with `.justcd/deployment.yaml`
into the single manifest consumed by CI and JustCD. The rendered deployment
preview shows the combined result. Every resource must target the app namespace;
a NetworkPolicy must select exactly `matchLabels: {app: <application name>}`.
Duplicate identities, unsupported resource kinds and malformed policies are rejected.
The combined deployment is limited to 20 resources and 1 MiB.

Templates access optional profile parameters using Go templates, for example:

```yaml
kubernetes.io/metadata.name: {{ quote (default "replace-with-ingress-namespace" (index .Parameters "ingressNamespace")) }}
```

Provide defaults for template validation with sample settings. The profile is also
validated with its actual parameters when saved. Parameter edits create a profile
revision and appear in the application upgrade review. Removing the optional
manifest creates a new immutable template version; review the resulting resource
removal through the normal deployment plan and approval process.

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

## Optional databases and runtime secrets

Workspace owners can configure **Optional application services** in a publishing
profile. A profile can offer one StackGres PostgreSQL plan and independently
selectable Vault static secret bindings. Users choose approved services and the application parameters their owner allows;
database infrastructure and Vault authentication remain controlled by the profile. Leaving all
services off preserves the ordinary application deployment.

### Platform setup

Install StackGres on clusters where database plans are offered. Install Vault
Secrets Operator where Vault bindings are offered. JustCD does not install either
operator. Its cluster identity and workspace policies must permit `SGCluster`,
`SGScript`, and/or `VaultStaticSecret` resources in the destination namespace.
Missing CRDs and admission failures appear during deployment planning.

For a database plan, configure PostgreSQL version, instances, storage, and optional
existing SGInstanceProfile, storage class, and SGObjectStorage backup destination.
Namespaced referenced resources must exist in each application namespace.
Omitting backup storage adds no backup configuration; configure backups before
using a plan for data that must survive infrastructure failures.

JustCD generates an SGCluster and an SGScript. For **platform/Vault-managed
credentials**, the platform must supply:

- An application credentials Secret with the configured username and password keys.
- A bootstrap Secret whose configured key contains idempotent SQL that creates the
  named database and a restricted application role, with matching credentials.

Both Secrets can be the same Secret synchronized by a required Vault binding.
For example, `{app}-database` can contain `username`, `password`, and `init.sql`.
Secret names and Vault paths support `{app}` and `{namespace}` substitutions.
In platform/Vault-managed mode, JustCD references these values; it does not
generate database passwords or write bootstrap SQL into Git. Never use StackGres superuser credentials for the app.
The database name in the plan must match the bootstrap SQL.

The app receives `DB_HOST`, `DB_PORT`, `DB_NAME`, `DB_USER`, and `DB_PASSWORD`.
Application code must read these variables and include its PostgreSQL client
library. Resource apply order accounts for Vault-generated Secrets and SQL
scripts; application code should still retry connections while PostgreSQL starts.
Network access remains subject to your cluster policies. Custom policy templates
can use `.HasDatabase`, `.DatabaseClusterName`, and `.DatabaseName`; database pods
have the `app: <DatabaseClusterName>` label. Configure database ingress, app egress,
DNS, replication and operator access as required by your platform.

### Vault secrets beyond the database

Add an approved binding for each secret users may need, such as S3 credentials or
an encryption key. Configure an existing authorized VaultAuth reference, KV mount,
relative path, KV v1/v2, refresh interval, and destination Kubernetes Secret. Map
individual Secret keys to explicit environment variable names. No Vault token or
secret value belongs in the profile. VaultAuth policies and namespace permissions
remain the platform's responsibility.

A binding can synchronize a Secret without exposing any keys as environment
variables, for example a database bootstrap Secret. Mark it as the database's
required binding to include it automatically when a database is selected. Other
bindings remain independently optional, including for apps without a database.

VSO restarts the app Deployment when synchronized data changes. Database password
rotation also needs SQL to update the PostgreSQL role; changing the Kubernetes
Secret alone does not rotate the role's password. Design and test that SQL and
rotation process with your database administrator.

### Updates and retention

Source uploads retain the application's selected services. Profile updates
re-resolve those choices against the new approved profile version and show changes
in the generated manifest review. Removing a selected capability from a profile
blocks the upgrade rather than silently dropping it. Template previews let owners
select services to inspect the rendered manifests before publishing a version.

Database clusters, bootstrap scripts, and their required Vault bindings carry
`justcd.io/retain: "true"`. JustCD excludes their deletion from prune and app
retirement plans, and retains their namespace to prevent cascading deletion.
Deleting the application therefore does not reclaim its database. Administrators
must review backups, retained credentials and storage, then explicitly clean up
retained resources outside that app retirement flow. Unrelated optional Vault
bindings follow the ordinary application lifecycle.

The manual publishing wizard also supports these settings; every service
configured there is included directly in that application.

### App-specific settings and profile permissions

Profiles define infrastructure and permissions; they do not have to fix every
application setting. In **Optional application services**, owners can enable:

- **JustCD-managed database credentials**: JustCD generates an application password,
  or accepts a user-supplied password if permitted (16–1024 characters). The owner
  can allow users to choose their database name and restricted username.
- **Custom database environment names**: users can map connection fields to the
  names their code expects, such as `PGHOST` and `PGPASSWORD`, or omit a field.
- **Vault path suffixes**: users select a relative secret path below the binding's
  approved path. Absolute paths and parent traversal are rejected. VaultAuth,
  KV mount, destination Secret, refresh behavior and namespace stay fixed.
- **Vault environment mappings**: users select Secret keys and environment names.
- **Additional environment variables**: users add non-secret configuration. These
  values are stored in Git and browser drafts. Use Vault for secret values.

Existing profiles keep their current locks until an owner enables these options.
New database plans default to JustCD-managed credentials with editable application
names and environment mappings. A platform/Vault-managed database keeps its
SQL database name and username fixed because those must match externally supplied
bootstrap SQL. The password is changed at its external source.

For JustCD-managed credentials, the configured credentials and bootstrap Secret
names must be identical and include `{app}`. JustCD stores the credential bundle
encrypted in its existing credential store, separately from publication metadata.
Applying the reviewed SGScript provisions the retained Secret directly in the
application namespace before StackGres executes SQL. The cluster identity needs
Secret read/create permission. Existing Secrets are never adopted or overwritten;
a conflicting Secret stops deployment. Back up JustCD's database and encryption
key along with the PostgreSQL backup and retained namespace resources.

The Secret contains the configured username/password keys, the role SQL under the
bootstrap key, and standalone database creation SQL under `<bootstrap-key>.database`.
StackGres executes them as separate managed script entries. Database creation is a
one-time operation; it is not retried automatically after an ambiguous failure.
Review StackGres script status before retrying a failed database creation manually.
Passwords and SQL never enter generated Git files, template previews, browser
storage, or publishing responses. Custom passwords are only accepted at initial
creation; source uploads and profile updates reuse the stored credentials. This
flow does not implement database password rotation or renaming existing databases.

Users' non-secret service parameters survive source uploads and profile upgrades.
A profile upgrade that changes an existing database's identity, credential mode,
or Secret references is rejected, rather than silently provisioning a different
database. Other infrastructure changes remain visible in the profile review.

**Copy profile** opens an unsaved editable copy, including its template version,
service rules and infrastructure defaults. It starts disabled. Credentials are
copied on the server into independent encrypted records when saved; edits and
credential rotations on the copy do not affect the original profile.
