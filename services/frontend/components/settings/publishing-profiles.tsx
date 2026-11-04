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
import {
  TemplateBadge,
  TemplateEditor,
  versionLabel,
  type TemplateVersionMeta,
} from "./template-editor"
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
  exposure: "route",
  exposureLabels: "",
  exposureAnnotations: "",
  ingressClass: "",
  ingressTls: "hosts",
  templateVersion: 0,
  templateParameters: "",
  runnerServiceAccount: "",
  runnerScc: "",
  builderSetup: "",
  secretMode: "variables",
  vaultAddress: "",
  vaultRole: "",
  vaultPath: "",
  syncPolicy: "auto-safe",
}

type TemplateSummary = {
  template: { id: string; name: string; description: string; category: string }
  versions: TemplateVersionMeta[]
}

export function PublishingProfiles({ workspaceId }: { workspaceId: string }) {
  const [templates, setTemplates] = useState<TemplateSummary[]>([])
  const [templatesReload, setTemplatesReload] = useState(0)
  const [editingTemplate, setEditingTemplate] = useState<string | null>(null)
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
  useEffect(() => {
    let active = true
    api<ListResponse<TemplateSummary>>(
      `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/publishing-templates`
    )
      .then((result) => {
        if (active) setTemplates(result.items)
      })
      .catch(() => {
        if (active) setTemplates([])
      })
    return () => {
      active = false
    }
  }, [workspaceId, templatesReload])
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
  if (editingTemplate)
    return (
      <TemplateEditor
        key={editingTemplate}
        workspaceId={workspaceId}
        templateId={editingTemplate}
        profiles={profiles}
        onClose={(saved) => {
          setEditingTemplate(null)
          if (saved) {
            setTemplatesReload((n) => n + 1)
            setNotice(
              "Template version saved. Select it in a profile to use it."
            )
          }
        }}
      />
    )
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
                    <Badge
                      variant={
                        profile.enabled ? "success-light" : "destructive-light"
                      }
                    >
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
      <Panel
        title="Application templates"
        description="The files JustCD generates for each application type: Dockerfile, build pipeline and Kubernetes manifest. Customize them for your platform; every save is a new, immutable version that profiles can select."
      >
        <div className="divide-y">
          {templates.map(({ template, versions }) => (
            <div
              key={template.id}
              className="flex flex-wrap items-center justify-between gap-4 px-5 py-4"
            >
              <div className="min-w-0 space-y-1">
                <div className="flex flex-wrap items-center gap-2">
                  <h3 className="text-sm font-medium">{template.name}</h3>
                  <Badge variant="outline" size="sm">
                    {template.category}
                  </Badge>
                  <TemplateBadge version={versions[0]?.version} />
                </div>
                <p className="text-xs text-muted-foreground">
                  {versions.length
                    ? `${versions.length} custom version${versions.length === 1 ? "" : "s"} · latest ${versionLabel(versions[0].version, versions[0].label)}`
                    : "Using the built-in template."}
                </p>
              </div>
              <Button
                variant="outline"
                onClick={() => {
                  setEditingTemplate(template.id)
                  setNotice("")
                }}
              >
                Customize
              </Button>
            </div>
          ))}
          {!templates.length && (
            <p className="p-5 text-sm text-muted-foreground">
              Templates are loading or unavailable.
            </p>
          )}
        </div>
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
  const [templateVersions, setTemplateVersions] = useState<
    TemplateVersionMeta[]
  >([])
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
  useEffect(() => {
    let live = true
    api<{ versions: TemplateVersionMeta[] }>(
      `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/publishing-templates/${encodeURIComponent(settings.templateId)}`
    )
      .then((result) => live && setTemplateVersions(result.versions))
      .catch(() => live && setTemplateVersions([]))
    return () => {
      live = false
    }
  }, [workspaceId, settings.templateId])
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
            "exposure",
            "exposureLabels",
            "exposureAnnotations",
            "ingressClass",
            "ingressTls",
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
  const filled = (...keys: (keyof PublishingProfileSettings)[]) =>
    keys.some((key) => {
      const value = settings[key]
      return !!value && value !== "route" && value !== "hosts"
    })
  const domainSet = !!settings.hostSuffix
  return (
    <form onSubmit={save} className="space-y-5">
      <Panel
        title={profile ? `Edit ${profile.name}` : "Create publishing profile"}
        description="Choose the approved build and destination. Secret values are encrypted and never returned to the browser."
      >
        <fieldset disabled={busy} className="space-y-4 p-5">
          <div className="grid gap-4 md:grid-cols-2">
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
          </div>
          <SwitchField
            id="profile-enabled"
            label="Allow publishing with this profile"
            description="Disabling blocks new applications and source updates without stopping deployed workloads."
            checked={enabled}
            onCheckedChange={setEnabled}
          />
          <p className="text-xs leading-5 text-muted-foreground">
            {locked
              ? `Used by ${profile.applications} applications. Repository and destination are locked; build settings stay editable and publish a new version. Existing applications adopt it through their profile-update action.`
              : "Saving build settings publishes an immutable profile version. Name changes and credential rotation do not create one."}
          </p>
        </fieldset>
      </Panel>
      <fieldset disabled={busy} className="min-w-0 space-y-5">
        <Panel
          title="Destination"
          description="Where applications of this profile are deployed."
        >
          <div className="grid gap-4 p-5 md:grid-cols-2">
            <FormField label="Application template" htmlFor="profile-template">
              <FormSelect
                id="profile-template"
                value={settings.templateId}
                onValueChange={(templateId) =>
                  patch({ templateId, templateVersion: 0 })
                }
                items={templates.map((template) => ({
                  value: template.id,
                  label: template.name,
                }))}
                disabled={locked}
              />
            </FormField>
            <FormField
              label="Template version"
              htmlFor="profile-template-version"
              hint="Built-in, or a version you customized under Application templates."
            >
              <FormSelect
                id="profile-template-version"
                value={String(settings.templateVersion ?? 0)}
                onValueChange={(value) =>
                  patch({ templateVersion: Number(value) })
                }
                items={[
                  { value: "0", label: "Built-in" },
                  ...templateVersions.map((v) => ({
                    value: String(v.version),
                    label: `Custom ${versionLabel(v.version, v.label)}`,
                  })),
                ]}
              />
            </FormField>
            <div className="md:col-span-2">
              <FormField
                label="Template parameters"
                htmlFor="profile-template-parameters"
                hint={
                  "Optional non-secret key=value lines used by custom templates via .Parameters. For the ingress policy starter: ingressNamespace=your-ingress-namespace. Verify the namespace with your platform administrator. Changes create a profile version for applications to review."
                }
              >
                <Textarea
                  id="profile-template-parameters"
                  value={settings.templateParameters ?? ""}
                  onChange={(event) =>
                    patch({ templateParameters: event.target.value })
                  }
                  placeholder="ingressNamespace=your-ingress-namespace"
                  className="font-mono text-xs"
                />
              </FormField>
            </div>
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
                "Ends with a hyphen, e.g. apps- creates apps-my-app.",
                true,
                "apps-"
              )
            ) : (
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
              "Optional. my-app becomes my-app.apps.example.com.",
              false,
              "apps.example.com"
            )}
            {field(
              "pullSecret",
              "Image pull secret",
              "Existing read-only registry Secret (created per namespace by your provisioning).",
              false
            )}
          </div>
        </Panel>
        <Panel
          title="Source and build"
          description="GitLab location, images and registry used to build applications."
        >
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
              "Private projects are created in this group.",
              true,
              "analytics/apps"
            )}
            {settings.templateId !== "static" &&
              field(
                "baseImage",
                settings.templateId === "python-static"
                  ? "Python build image"
                  : "Application runtime image",
                "Approved image with native dependencies and internal certificate trust."
              )}
            {(settings.templateId === "python-static" ||
              settings.templateId === "static") &&
              field(
                "webServerImage",
                "Static web server image",
                "Unprivileged NGINX-compatible image: port 8080, UID 1001, OpenShift arbitrary UIDs."
              )}
            <FormField
              label="Build engine"
              htmlFor="profile-build-engine"
              hint="The engine provided by your CI builder image."
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
              "Podman: podman, grep, shell. BuildKit: rootless BuildKit, jq, shell."
            )}
            {field(
              "registryPrefix",
              "Registry prefix",
              "JustCD appends /application-name and publishes image digests.",
              true,
              "artifactory.example.com/analytics-local"
            )}
          </div>
        </Panel>
        <Panel
          title="Access and credentials"
          description={
            profile
              ? "Leave fields blank to keep stored values. Analysts never receive these values."
              : "Use service credentials scoped to the approved group and registry. Analysts never receive these values."
          }
        >
          <div className="space-y-4 p-5">
            <div className="grid gap-4 md:grid-cols-2">
              <FormField
                label="Registry secret storage"
                htmlFor="profile-secret-mode"
              >
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
                <>
                  {field(
                    "vaultAddress",
                    "Vault URL",
                    "GitLab must already trust this Vault server."
                  )}
                  {field(
                    "vaultRole",
                    "Vault JWT role",
                    "Restrict to the project/group and protected main branch."
                  )}
                  {field(
                    "vaultPath",
                    "KV v2 secret path",
                    "Secret with username and token fields.",
                    true,
                    "secret/data/analytics/registry"
                  )}
                </>
              )}
              {secret(
                "gitlabToken",
                "GitLab provisioning token",
                "Creates projects, CI variables and branches, commits source, starts pipelines."
              )}
              {secret(
                "gitReadToken",
                "Git repository read token",
                "Group token with read_repository. Rotating it updates all apps of this profile."
              )}
              {settings.secretMode === "variables" && (
                <>
                  {secret(
                    "registryUser",
                    "Registry username",
                    "Service account for pushing images."
                  )}
                  {secret(
                    "registryPassword",
                    "Registry push token",
                    "At least eight characters, no whitespace. Stored as a masked CI variable."
                  )}
                </>
              )}
            </div>
          </div>
        </Panel>
        <Panel
          title="Advanced settings"
          description="Most profiles do not need these. Groups with values open automatically."
        >
          <div className="space-y-3 p-5">
            <AdvancedGroup
              title="Routing and exposure"
              hint="Route or Ingress, with labels and annotations. Applies when an application domain is set."
              configured={filled(
                "exposure",
                "exposureLabels",
                "exposureAnnotations",
                "ingressClass",
                "ingressTls"
              )}
            >
              <div className="grid gap-4 md:grid-cols-2">
                <FormField label="Publish with" htmlFor="profile-exposure">
                  <FormSelect
                    id="profile-exposure"
                    value={settings.exposure || "route"}
                    onValueChange={(exposure) =>
                      patch({
                        exposure,
                        ...(exposure === "ingress"
                          ? {}
                          : { ingressClass: "", ingressTls: "hosts" }),
                      })
                    }
                    items={[
                      { value: "route", label: "OpenShift Route" },
                      { value: "ingress", label: "Kubernetes Ingress" },
                    ]}
                  />
                </FormField>
                {settings.exposure === "ingress" ? (
                  <>
                    {field(
                      "ingressClass",
                      "Ingress class (optional)",
                      "Sets ingressClassName.",
                      false,
                      "openshift-default"
                    )}
                    <FormField
                      label="Ingress TLS"
                      htmlFor="profile-ingress-tls"
                      hint="List the host, or render tls: [] when TLS is handled elsewhere."
                    >
                      <FormSelect
                        id="profile-ingress-tls"
                        value={settings.ingressTls || "hosts"}
                        onValueChange={(ingressTls) => patch({ ingressTls })}
                        items={[
                          { value: "hosts", label: "List the host (default)" },
                          { value: "none", label: "None (tls: [])" },
                        ]}
                      />
                    </FormField>
                  </>
                ) : null}
                <FormField
                  label="Labels (optional)"
                  htmlFor="profile-exposure-labels"
                  hint="One key=value per line, on the Route or Ingress only."
                >
                  <Textarea
                    id="profile-exposure-labels"
                    value={settings.exposureLabels ?? ""}
                    onChange={(event) =>
                      patch({ exposureLabels: event.target.value })
                    }
                    placeholder="router=external"
                  />
                </FormField>
                <FormField
                  label="Annotations (optional)"
                  htmlFor="profile-exposure-annotations"
                  hint="One key=value per line, on the Route or Ingress only."
                >
                  <Textarea
                    id="profile-exposure-annotations"
                    value={settings.exposureAnnotations ?? ""}
                    onChange={(event) =>
                      patch({ exposureAnnotations: event.target.value })
                    }
                    placeholder="haproxy.router.openshift.io/timeout=5m"
                  />
                </FormField>
              </div>
              {!domainSet && (
                <p className="mt-3 text-xs text-muted-foreground">
                  Set an application domain under Destination to use these.
                </p>
              )}
            </AdvancedGroup>
            <AdvancedGroup
              title="CI runner and trust"
              hint="Runner selection, rootless build permissions and custom certificate trust."
              configured={filled(
                "runnerTag",
                "runnerServiceAccount",
                "runnerScc",
                "builderSetup",
                "caVariable",
                "gitImage"
              )}
            >
              <div className="grid gap-4 md:grid-cols-2">
                {field(
                  "runnerTag",
                  "Runner tag",
                  "Tag of an approved GitLab runner.",
                  false
                )}
                {field(
                  "caVariable",
                  "Custom CA variable",
                  "Inherited CI variable with PEM certificates, e.g. PLAIN2_ROOT_CA.",
                  false,
                  "PLAIN2_ROOT_CA"
                )}
                {field(
                  "runnerServiceAccount",
                  "Runner service account",
                  "For runners that allow rootless builds only under a dedicated account.",
                  false,
                  "gitlab-runner-app-sa"
                )}
                {field(
                  "runnerScc",
                  "Required OpenShift SCC",
                  "Sets openshift.io/required-scc on the build pod.",
                  false,
                  "gitlab-runner-scc"
                )}
                {field(
                  "builderSetup",
                  "Builder setup script",
                  "Absolute path of an executable in the builder image, run before the build.",
                  false,
                  "/home/user/entrypoint.sh"
                )}
                {field(
                  "gitImage",
                  "Existing Git pipeline update image",
                  "Only for bootstrapping existing Git pipelines; blank reuses the builder image.",
                  false
                )}
              </div>
            </AdvancedGroup>
            {profile && (
              <AdvancedGroup
                title="Versions"
                hint="Publish a revision explicitly and review published profile versions."
                configured={publishRevision}
              >
                <div className="space-y-4">
                  <SwitchField
                    id="profile-publish-revision"
                    checked={publishRevision}
                    onCheckedChange={setPublishRevision}
                    disabled={busy}
                    label="Publish a new revision even if nothing changed"
                    description="Adopts updated JustCD build recipes. Existing applications stay pinned until upgraded."
                  />
                  {versions.map((version) => (
                    <Disclosure
                      key={version.revision}
                      summary={`v${version.revision}${version.revision === profile.currentRevision ? " · latest" : ""} · ${new Date(version.createdAt).toLocaleDateString()}`}
                    >
                      <dl className="space-y-2 text-xs">
                        <div>
                          <dt className="font-medium">Recipe version</dt>
                          <dd>{version.templateVersion}</dd>
                        </div>
                        {(
                          [
                            "baseImage",
                            "webServerImage",
                            "builderImage",
                          ] as const
                        )
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
              </AdvancedGroup>
            )}
          </div>
        </Panel>
      </fieldset>
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

function AdvancedGroup({
  title,
  hint,
  configured,
  children,
}: {
  title: string
  hint: string
  configured: boolean
  children: React.ReactNode
}) {
  return (
    <div className="rounded-xl border p-4">
      <Disclosure
        defaultOpen={configured}
        summary={
          <span className="flex flex-wrap items-center gap-2">
            {title}
            {configured && (
              <Badge size="sm" radius="full" variant="info-light">
                Configured
              </Badge>
            )}
          </span>
        }
      >
        <p className="mb-4 text-xs leading-5 text-muted-foreground">{hint}</p>
        {children}
      </Disclosure>
    </div>
  )
}
