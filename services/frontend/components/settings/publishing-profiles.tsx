"use client"

import { useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { Disclosure } from "@/components/ui/collapsible"
import { FormSelect } from "@/components/ui/form-select"
import { AppDialog, DialogCancel, DialogFooter } from "@/components/ui/dialog"
import { Badge } from "@/components/reui/badge"
import { FormField, Panel, SwitchField } from "@/components/ui-kit"
import { ErrorNotice } from "@/components/workspace-ui"
import { api, apiPost } from "@/lib/api"
import type {
  PublishingProfile,
  PublishingProfileSettings,
} from "@/lib/publishing"
import type { ApplicationTemplate } from "@/components/applications/template-catalog"
import type { Cluster, ListResponse, NamespaceBinding } from "@/lib/types"

const emptySettings: PublishingProfileSettings = {
  templateId: "shiny",
  gitlabUrl: "",
  group: "",
  clusterId: "",
  namespace: "",
  namespaceMode: "fixed",
  namespacePrefix: "",
  webServerImage: "",
  baseImage: "",
  builderImage: "",
  registryPrefix: "",
  hostSuffix: "",
  pullSecret: "",
  gitImage: "",
  buildEngine: "buildkit",
  caVariable: "",
  runnerTag: "",
  runnerServiceAccount: "",
  runnerScc: "",
  builderSetup: "",
  secretMode: "variables",
  vaultAddress: "",
  vaultRole: "",
  vaultPath: "",
  syncPolicy: "auto-safe",
}

export function PublishingProfiles({ workspaceId }: { workspaceId: string }) {
  const [profiles, setProfiles] = useState<PublishingProfile[]>([])
  const [editing, setEditing] = useState<PublishingProfile | null>(null)
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [loading, setLoading] = useState(true)
  const [deleteTarget, setDeleteTarget] = useState<PublishingProfile | null>(
    null
  )
  const [deleting, setDeleting] = useState(false)
  const [notice, setNotice] = useState("")
  useEffect(() => {
    let active = true
    api<ListResponse<PublishingProfile>>(
      `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/publishing-profiles`
    )
      .then((result) => {
        if (active) setProfiles(result.items)
      })
      .catch((cause) => {
        if (active) setError(cause)
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [workspaceId])
  async function remove() {
    if (!deleteTarget || deleting) return
    setDeleting(true)
    try {
      await api(
        `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/publishing-profiles/${encodeURIComponent(deleteTarget.id)}`,
        { method: "DELETE" }
      )
      setProfiles((items) =>
        items.filter((item) => item.id !== deleteTarget.id)
      )
      setDeleteTarget(null)
      setNotice("Publishing profile deleted.")
    } catch (cause) {
      setError(cause)
    } finally {
      setDeleting(false)
    }
  }
  if (creating || editing)
    return (
      <ProfileEditor
        key={editing ? `${editing.id}:${editing.version}` : "new"}
        workspaceId={workspaceId}
        profile={editing}
        onCancel={() => {
          setCreating(false)
          setEditing(null)
        }}
        onSaved={(profile) => {
          setProfiles((items) =>
            [...items.filter((item) => item.id !== profile.id), profile].sort(
              (a, b) => a.name.localeCompare(b.name)
            )
          )
          setCreating(false)
          setEditing(null)
          setNotice("Publishing profile saved.")
        }}
      />
    )
  return (
    <div className="space-y-5">
      <Panel
        title="Publishing profiles"
        description="Configure infrastructure once. Workspace deployers can publish apps without handling GitLab, registry, or cluster credentials."
        action={
          <Button
            onClick={() => {
              setCreating(true)
              setNotice("")
            }}
          >
            Create profile
          </Button>
        }
      >
        {loading ? (
          <p role="status" className="p-5 text-sm text-muted-foreground">
            Loading publishing profiles…
          </p>
        ) : !profiles.length ? (
          <div className="space-y-2 p-5">
            <p className="text-sm font-medium">No publishing profiles yet</p>
            <p className="text-sm leading-6 text-muted-foreground">
              Create a profile for an approved template and destination, such as
              Shiny in the analytics development namespace.
            </p>
          </div>
        ) : (
          <div className="divide-y">
            {profiles.map((profile) => (
              <div
                key={profile.id}
                className="flex flex-wrap items-center justify-between gap-4 p-5"
              >
                <div className="min-w-0 space-y-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <h3 className="text-sm font-medium">{profile.name}</h3>
                    <Badge variant="outline">v{profile.currentRevision}</Badge>
                    <Badge variant={profile.enabled ? "secondary" : "outline"}>
                      {profile.enabled ? "Enabled" : "Disabled"}
                    </Badge>
                  </div>
                  <p className="text-xs text-muted-foreground">
                    {profile.settings.templateId} ·{" "}
                    {profile.settings.namespaceMode === "per-application"
                      ? `${profile.settings.namespacePrefix}<app-name>`
                      : profile.settings.namespace}{" "}
                    · {profile.applications} applications
                  </p>
                  {profile.description && (
                    <p className="text-sm text-muted-foreground">
                      {profile.description}
                    </p>
                  )}
                </div>
                <div className="flex gap-2">
                  <Button
                    variant="outline"
                    onClick={() => {
                      setEditing(profile)
                      setNotice("")
                    }}
                  >
                    Edit
                  </Button>
                  <Button
                    variant="destructive"
                    disabled={profile.applications > 0}
                    onClick={() => setDeleteTarget(profile)}
                  >
                    Delete
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </Panel>
      {notice && (
        <p role="status" className="text-sm text-muted-foreground">
          {notice}
        </p>
      )}
      {error != null && <ErrorNotice error={error} />}
      <p className="text-sm leading-6 text-muted-foreground">
        Profiles are available to workspace owners and deployers. Disable a
        profile to stop new builds and source updates. Existing workloads keep
        running. Infrastructure settings are locked once a profile is in use;
        credentials can still be rotated.
      </p>
      <AppDialog
        open={!!deleteTarget}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null)
        }}
        busy={deleting}
        title="Delete publishing profile"
        description={`Delete ${deleteTarget?.name ?? "this profile"} and its stored credentials? Only unused profiles can be deleted.`}
        size="md"
      >
        <DialogFooter>
          <DialogCancel disabled={deleting} />
          <Button
            variant="destructive"
            loading={deleting}
            onClick={() => void remove()}
          >
            Delete profile
          </Button>
        </DialogFooter>
      </AppDialog>
    </div>
  )
}

function ProfileEditor({
  workspaceId,
  profile,
  onCancel,
  onSaved,
}: {
  workspaceId: string
  profile: PublishingProfile | null
  onCancel: () => void
  onSaved: (profile: PublishingProfile) => void
}) {
  const [publishRevision, setPublishRevision] = useState(false)
  const [name, setName] = useState(profile?.name ?? "")
  const [description, setDescription] = useState(profile?.description ?? "")
  const [enabled, setEnabled] = useState(profile?.enabled ?? true)
  const [settings, setSettings] = useState(profile?.settings ?? emptySettings)
  const [secrets, setSecrets] = useState({
    gitlabToken: "",
    gitReadToken: "",
    registryUser: "",
    registryPassword: "",
  })
  const [templates, setTemplates] = useState<ApplicationTemplate[]>([])
  const [clusters, setClusters] = useState<Cluster[]>([])
  const [bindings, setBindings] = useState<NamespaceBinding[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [versions, setVersions] = useState<
    {
      revision: number
      createdAt: string
      templateVersion: string
      settings: PublishingProfileSettings
    }[]
  >([])
  useEffect(() => {
    if (!profile?.id) return
    let active = true
    api<
      ListResponse<{
        revision: number
        createdAt: string
        templateVersion: string
        settings: PublishingProfileSettings
      }>
    >(`/api/v1/publishing/profiles/${encodeURIComponent(profile.id)}/versions`)
      .then(({ items }) => {
        if (active) setVersions(items)
      })
      .catch((e) => {
        if (active) setError(e)
      })
    return () => {
      active = false
    }
  }, [profile?.id])
  const locked = !!profile?.applications
  const patch = (values: Partial<PublishingProfileSettings>) =>
    setSettings((current) => ({ ...current, ...values }))
  useEffect(() => {
    let active = true
    Promise.all([
      api<ListResponse<ApplicationTemplate>>("/api/v1/publishing/templates"),
      api<ListResponse<Cluster>>(
        `/api/v1/clusters?workspaceId=${encodeURIComponent(workspaceId)}`
      ),
    ])
      .then(([templateList, clusterList]) => {
        if (active) {
          setTemplates(templateList.items)
          setClusters(clusterList.items)
        }
      })
      .catch((cause) => {
        if (active) setError(cause)
      })
    return () => {
      active = false
    }
  }, [workspaceId])
  useEffect(() => {
    if (!settings.clusterId) return
    let active = true
    api<ListResponse<NamespaceBinding>>(
      `/api/v1/clusters/${encodeURIComponent(settings.clusterId)}/bindings?workspaceId=${encodeURIComponent(workspaceId)}`
    )
      .then((result) => {
        if (active) setBindings(result.items)
      })
      .catch((cause) => {
        if (active) setError(cause)
      })
    return () => {
      active = false
    }
  }, [settings.clusterId, workspaceId])
  async function save(event: React.FormEvent) {
    event.preventDefault()
    if (busy) return
    setBusy(true)
    setError(null)
    try {
      const url = `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/publishing-profiles`
      const body = {
        name,
        description,
        enabled,
        settings,
        secrets,
        version: profile?.version ?? 0,
        publishRevision,
      }
      const saved = profile
        ? await api<PublishingProfile>(
            `${url}/${encodeURIComponent(profile.id)}`,
            { method: "PUT", body: JSON.stringify(body) }
          )
        : await apiPost<PublishingProfile>(url, body)
      setSecrets({
        gitlabToken: "",
        gitReadToken: "",
        registryUser: "",
        registryPassword: "",
      })
      onSaved(saved)
    } catch (cause) {
      setError(cause)
    } finally {
      setBusy(false)
    }
  }
  const field = (
    key: keyof PublishingProfileSettings,
    label: string,
    hint?: string,
    required = true,
    placeholder?: string
  ) => (
    <FormField label={label} htmlFor={`profile-${key}`} hint={hint}>
      <Input
        id={`profile-${key}`}
        required={required}
        disabled={
          locked &&
          ![
            "baseImage",
            "builderImage",
            "webServerImage",
            "runnerTag",
            "runnerServiceAccount",
            "runnerScc",
            "builderSetup",
            "caVariable",
            "buildEngine",
            "gitImage",
            "pullSecret",
          ].includes(key)
        }
        value={settings[key] ?? ""}
        onChange={(event) => patch({ [key]: event.target.value })}
        placeholder={placeholder}
      />
    </FormField>
  )
  const secret = (key: keyof typeof secrets, label: string, hint: string) => (
    <FormField label={label} htmlFor={`profile-secret-${key}`} hint={hint}>
      <Input
        id={`profile-secret-${key}`}
        type={key === "registryUser" ? "text" : "password"}
        autoComplete="off"
        required={!profile}
        minLength={key === "registryUser" ? 1 : 8}
        value={secrets[key]}
        onChange={(event) =>
          setSecrets((current) => ({ ...current, [key]: event.target.value }))
        }
        placeholder={
          profile ? "Leave blank to keep the stored value" : undefined
        }
      />
    </FormField>
  )
  return (
    <form onSubmit={save} className="space-y-5">
      <Panel
        title={profile ? `Edit ${profile.name}` : "Create publishing profile"}
        description="Choose the approved build and destination. Secret values are encrypted and never returned to the browser."
      >
        <fieldset disabled={busy} className="space-y-4 p-5">
          <FormField
            label="Profile name"
            htmlFor="profile-name"
            hint="Shown to analysts when choosing a destination."
          >
            <Input
              id="profile-name"
              required
              maxLength={100}
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder="Analytics development"
            />
          </FormField>
          <FormField label="Description" htmlFor="profile-description">
            <Textarea
              id="profile-description"
              maxLength={1000}
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              placeholder="Internal Shiny apps for the analytics team"
            />
          </FormField>
          <SwitchField
            id="profile-enabled"
            label="Allow publishing with this profile"
            description="Disabling blocks new applications and source updates without stopping deployed workloads."
            checked={enabled}
            onCheckedChange={setEnabled}
          />
        </fieldset>
      </Panel>
      {!locked && (
        <p className="text-sm text-muted-foreground">
          Saving build settings publishes an immutable profile version. Name
          changes and credential rotation do not create a new release.
        </p>
      )}
      {locked && (
        <p className="text-sm text-muted-foreground">
          This profile is used by {profile.applications} applications. Build
          engines, images, CA variable names, runner tags, and image-pull
          secrets remain editable; repository and deployment destinations are
          locked. Build-setting changes publish a new immutable version.
          Existing applications stay on their pinned version; use their
          profile-update action to review and rebuild without uploading source
          again.
        </p>
      )}
      <fieldset disabled={busy} className="min-w-0 space-y-5">
        <Panel title="Template and destination">
          <div className="grid gap-4 p-5 md:grid-cols-2">
            <FormField label="Application template" htmlFor="profile-template">
              <FormSelect
                id="profile-template"
                value={settings.templateId}
                onValueChange={(templateId) => patch({ templateId })}
                items={templates.map((template) => ({
                  value: template.id,
                  label: template.name,
                }))}
                disabled={locked}
              />
            </FormField>
            <FormField label="Cluster" htmlFor="profile-cluster">
              <FormSelect
                id="profile-cluster"
                value={settings.clusterId}
                onValueChange={(clusterId) => {
                  setBindings([])
                  patch({ clusterId, namespace: "" })
                }}
                items={clusters.map((cluster) => ({
                  value: cluster.id,
                  label: cluster.name,
                }))}
                disabled={locked}
                placeholder="Select a cluster"
              />
            </FormField>
            <FormField
              label="Namespace policy"
              htmlFor="profile-namespace-mode"
              hint="Namespace creation uses the cluster’s existing provisioning permissions and deployment approval flow."
            >
              <FormSelect
                id="profile-namespace-mode"
                value={settings.namespaceMode || "fixed"}
                onValueChange={(namespaceMode) =>
                  patch({
                    namespaceMode,
                    namespace:
                      namespaceMode === "per-application"
                        ? ""
                        : (bindings[0]?.namespace ?? ""),
                    namespacePrefix:
                      namespaceMode === "per-application"
                        ? settings.namespacePrefix || "apps-"
                        : "",
                  })
                }
                items={[
                  { value: "fixed", label: "Use a fixed existing namespace" },
                  {
                    value: "per-application",
                    label: "Create a namespace per application",
                  },
                ]}
                disabled={locked}
              />
            </FormField>
            {settings.namespaceMode === "per-application" ? (
              field(
                "namespacePrefix",
                "Namespace prefix",
                "Ends with a hyphen. For example apps- creates apps-my-app. The final name must be at most 63 characters.",
                true,
                "apps-"
              )
            ) : (
              <>
                <FormField label="Namespace" htmlFor="profile-namespace">
                  <FormSelect
                    id="profile-namespace"
                    value={settings.namespace}
                    onValueChange={(namespace) => patch({ namespace })}
                    items={bindings.map((binding) => ({
                      value: binding.namespace,
                      label: binding.namespace,
                    }))}
                    disabled={locked}
                    placeholder="Select an approved namespace"
                  />
                </FormField>
              </>
            )}
            <FormField label="Deployment behavior" htmlFor="profile-sync">
              <FormSelect
                id="profile-sync"
                value={settings.syncPolicy}
                onValueChange={(syncPolicy) => patch({ syncPolicy })}
                items={[
                  {
                    value: "auto-safe",
                    label: "Publish automatically · follow approval rules",
                  },
                  {
                    value: "manual",
                    label: "Manual · review and apply each release",
                  },
                ]}
                disabled={locked}
              />
            </FormField>
            {field(
              "hostSuffix",
              "Application domain",
              "Optional OpenShift route suffix. App my-app becomes my-app.apps.example.com. The template does not add SSO.",
              false,
              "apps.example.com"
            )}
            {field(
              "pullSecret",
              "Image pull secret",
              settings.namespaceMode === "per-application"
                ? "Your provisioning setup must create this read-only registry Secret in each new namespace before deployment."
                : "Existing read-only registry Secret in the selected namespace.",
              false
            )}
          </div>
        </Panel>
        <Panel title="GitLab and build environment">
          <div className="grid gap-4 p-5 md:grid-cols-2">
            {field(
              "gitlabUrl",
              "GitLab URL",
              "HTTPS origin.",
              true,
              "https://gitlab.example.com"
            )}
            {field(
              "group",
              "GitLab group",
              "Private projects are created inside this group or subgroup.",
              true,
              "analytics/apps"
            )}
            {settings.templateId !== "static" &&
              field(
                "baseImage",
                settings.templateId === "python-static"
                  ? "Python build image"
                  : "Application runtime image",
                "Approved R/Shiny or Python image, including native dependencies and internal certificate trust."
              )}
            {(settings.templateId === "python-static" ||
              settings.templateId === "static") &&
              field(
                "webServerImage",
                "Static web server image",
                "Approved unprivileged NGINX-compatible image: port 8080, document root /usr/share/nginx/html, supporting UID 1001 and OpenShift arbitrary UIDs."
              )}
            <FormField
              label="Build engine"
              htmlFor="profile-build-engine"
              hint="Choose the engine provided by your CI builder image. Existing profiles default to BuildKit."
            >
              <FormSelect
                id="profile-build-engine"
                value={settings.buildEngine || "buildkit"}
                onValueChange={(buildEngine) => patch({ buildEngine })}
                items={[
                  { value: "buildkit", label: "BuildKit" },
                  { value: "podman", label: "Podman" },
                ]}
              />
            </FormField>
            {field(
              "builderImage",
              "CI builder image",
              "Podman: podman, grep and shell tools. BuildKit: rootless BuildKit, jq and shell. Managed publishing commits deployment changes in JustCD. Runner must support the selected engine."
            )}
            {field(
              "gitImage",
              "Existing Git pipeline update image",
              "Only for existing Git pipeline bootstrap. Managed publishing updates deployment manifests in JustCD and does not use this image. Leave blank to reuse the builder image for existing Git workflows.",
              false
            )}
            {field(
              "registryPrefix",
              "Artifactory registry prefix",
              "JustCD appends /application-name and publishes immutable image digests.",
              true,
              "artifactory.example.com/analytics-local"
            )}
            {field(
              "caVariable",
              "Custom CA variable (optional)",
              "Inherited GitLab CI variable containing PEM certificates, e.g. PLAIN2_ROOT_CA. Leave blank to use the builder image's default trust. Applies to CI Git and the selected build engine, not application build stages.",
              false,
              "PLAIN2_ROOT_CA"
            )}
            {field(
              "runnerTag",
              "Runner tag",
              "Optional tag for an approved GitLab runner.",
              false
            )}
            {field(
              "runnerServiceAccount",
              "Runner service account (optional)",
              "Kubernetes service account the build job runs as, for runners that only allow rootless builds under a dedicated account.",
              false,
              "gitlab-runner-app-sa"
            )}
            {field(
              "runnerScc",
              "Required OpenShift SCC (optional)",
              "Security context constraint the build pod must use (openshift.io/required-scc). Rootless Podman usually needs one that allows user namespaces.",
              false,
              "gitlab-runner-scc"
            )}
            {field(
              "builderSetup",
              "Builder setup script (optional)",
              "Absolute path of an executable in the builder image that runs before the build, e.g. its rootless Podman entrypoint script. No arguments.",
              false,
              "/home/user/entrypoint.sh"
            )}
          </div>
        </Panel>
        <Panel title="Registry secret source">
          <div className="space-y-4 p-5">
            <FormField label="Secret storage" htmlFor="profile-secret-mode">
              <FormSelect
                id="profile-secret-mode"
                value={settings.secretMode}
                onValueChange={(secretMode) => patch({ secretMode })}
                items={[
                  {
                    value: "variables",
                    label: "Protected GitLab CI/CD variables",
                  },
                  {
                    value: "vault",
                    label: "Existing Vault secret · GitLab OIDC",
                  },
                ]}
                disabled={locked}
              />
            </FormField>
            {settings.secretMode === "vault" && (
              <div className="grid gap-4 md:grid-cols-2">
                {field(
                  "vaultAddress",
                  "Vault URL",
                  "GitLab must already trust this Vault server and support native Vault secrets."
                )}
                {field(
                  "vaultRole",
                  "Vault JWT role",
                  "Restrict this role to the selected project/group and protected main branch."
                )}
                {field(
                  "vaultPath",
                  "KV v2 secret path",
                  "Existing secret with username and token fields.",
                  true,
                  "secret/data/analytics/registry"
                )}
              </div>
            )}
          </div>
        </Panel>
      </fieldset>
      {profile && (
        <Panel title="Publish a recipe revision">
          <div className="p-5">
            <SwitchField
              id="profile-publish-revision"
              checked={publishRevision}
              onCheckedChange={setPublishRevision}
              disabled={busy}
              label="Publish a new revision even if images are unchanged"
              description="Use this to adopt updated JustCD build recipes. Changing image references already creates a new revision. Existing applications remain pinned until upgraded."
            />
          </div>
        </Panel>
      )}
      {versions.length > 0 && (
        <Panel title="Published profile versions">
          <div className="space-y-3 p-5">
            {versions.map((version) => (
              <Disclosure
                key={version.revision}
                summary={`v${version.revision}${version.revision === profile?.currentRevision ? " · latest" : ""} · ${new Date(version.createdAt).toLocaleDateString()}`}
              >
                <dl className="mt-3 space-y-2 text-xs">
                  <div>
                    <dt className="font-medium">Recipe version</dt>
                    <dd>{version.templateVersion}</dd>
                  </div>
                  {(["baseImage", "webServerImage", "builderImage"] as const)
                    .filter((key) => version.settings[key])
                    .map((key) => (
                      <div key={key}>
                        <dt className="font-medium">{key}</dt>
                        <dd className="break-all text-muted-foreground">
                          {version.settings[key]}
                        </dd>
                      </div>
                    ))}
                </dl>
              </Disclosure>
            ))}
          </div>
        </Panel>
      )}
      <Panel
        title="Protected credentials"
        description={
          profile
            ? "Leave fields blank to retain existing values. Rotating the read token also updates access for applications using this profile."
            : "Use service credentials scoped to the approved group and registry. Analysts never receive these values."
        }
      >
        <fieldset disabled={busy} className="grid gap-4 p-5 md:grid-cols-2">
          {secret(
            "gitlabToken",
            "GitLab provisioning token",
            "API permission to create projects, configure protected CI variables and branches, commit source, and start pipelines."
          )}
          {secret(
            "gitReadToken",
            "Git repository read token",
            "Group token with read_repository access to projects created in the selected group."
          )}
          {settings.secretMode === "variables" && (
            <>
              {secret(
                "registryUser",
                "Registry username",
                "Service account for pushing images into the approved repository."
              )}
              {secret(
                "registryPassword",
                "Registry push token",
                "At least eight characters without whitespace. Stored as a masked, protected CI variable."
              )}
            </>
          )}
        </fieldset>
      </Panel>
      {error != null && <ErrorNotice error={error} />}
      <div className="flex justify-between gap-3">
        <Button
          type="button"
          variant="outline"
          disabled={busy}
          onClick={onCancel}
        >
          Cancel
        </Button>
        <Button type="submit" loading={busy} loadingText="Saving profile…">
          Save and publish settings
        </Button>
      </div>
    </form>
  )
}
