---
title: "Publish Shiny and Streamlit applications"
description: "Create a private GitLab project, build an image in CI, and register a JustCD deployment from an application ZIP."
---

Choose **Applications → New → From a template**. The searchable catalog offers
Shiny, Streamlit, and Shiny Geospatial. Select a template to open its guided
configuration. Workspace owners can upload a
complete application ZIP and follow six steps: application, repository, build,
secrets, deployment, and review. JustCD generates the Dockerfile, GitLab pipeline,
and Kubernetes deployment template. Application code runs only in CI and the
selected cluster, never inside the JustCD API process.

## Platform prerequisites

- A workspace with a cluster and an existing namespace binding. The initial
  publishing flow does not create clusters or namespaces.
- An HTTPS GitLab instance supporting CI job-token Git pushes. GitLab 18.4+
  is recommended. A provisioning token needs API access, project creation in the
  selected group, protected branch configuration, commits, variables, and pipelines.
- A separate group access token with `read_repository` that can read newly created
  private projects. JustCD stores only this read credential, encrypted using its
  existing credential store. The provisioning token is used for the request only.
- A compatible GitLab runner and an approved rootless BuildKit image containing
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
or retained in JustCD. The publishing branch is protected before CI starts.

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

The registered JustCD application uses `main`, renderer `yaml`, path `deploy`,
and **manual** sync. Before the first successful build, this path contains only
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
token. Existing runtime settings, registry variables/Vault references, Git read
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
