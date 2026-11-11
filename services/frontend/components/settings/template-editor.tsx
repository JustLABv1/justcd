"use client"

import { useEffect, useMemo, useRef, useState } from "react"
import { Workbench } from "@/components/panelui/workbench"
import { Button } from "@/components/panelui/button"
import { Input } from "@/components/panelui/input"
import { Textarea } from "@/components/panelui/textarea"
import { Disclosure } from "@/components/ui/collapsible"
import { FormSelect } from "@/components/form-select"
import { Tabs, TabsList, TabsTrigger } from "@/components/panelui/tabs"
import { Badge } from "@/components/panelui/badge"
import { FormField } from "@/components/ui-kit"
import { ErrorNotice } from "@/components/workspace-ui"
import { api, apiPost } from "@/lib/api"
import { reviewTextRows, type ReviewRow } from "@/lib/line-diff"
import { PublishingServicesPicker } from "@/components/applications/publishing-services"
import type { ServiceCatalog, ServiceSelection } from "@/lib/publishing"
import type { PublishingProfile } from "@/lib/publishing"
import { SettingsPanel } from "@/components/settings/settings-section"

export type TemplateVersionMeta = {
  version: number
  label: string
  createdBy: string
  createdAt: string
}
type TemplateDetail = {
  template: { id: string; name: string; description: string; runtime: string; custom?: boolean }
  builtin: Record<string, string>
  versions: TemplateVersionMeta[]
  fileOrder: string[]
  placeholders: { name: string; type: string; description: string }[]
}
type VersionFiles = { files: Record<string, string> }

export function versionLabel(version: number, label?: string) {
  if (version === 0) return "Built-in"
  return label ? `v${version} · ${label}` : `v${version}`
}

const networkPolicyPath = ".justcd/manifests/network-policy.yaml"
const ingressPolicy = `apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: {{ .Name }}-allow-ingress
  namespace: {{ .Namespace }}
spec:
  podSelector:
    matchLabels:
      app: {{ .Name }}
  policyTypes: [Ingress]
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: {{ quote (default "replace-with-ingress-namespace" (index .Parameters "ingressNamespace")) }}
      ports:
        - protocol: TCP
          port: {{ .Port }}
`

const editorClass =
  "h-full min-h-0 max-h-none flex-1 resize-none [field-sizing:fixed] rounded-none border-0 font-mono text-xs leading-5 whitespace-pre focus-visible:ring-inset"

/**
 * Admins see the files of an application template with their placeholders,
 * edit them, compare against the starting version, preview the rendered result
 * and save a new immutable template version. The server validates every save.
 */
export function TemplateEditor({
  workspaceId,
  templateId,
  profiles,
  onClose,
}: {
  workspaceId: string
  templateId: string
  profiles: PublishingProfile[]
  onClose: (saved: boolean) => void
}) {
  const base = `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/publishing-templates/${encodeURIComponent(templateId)}`
  const [detail, setDetail] = useState<TemplateDetail | null>(null)
  const [baseVersion, setBaseVersion] = useState(0)
  const [baseFiles, setBaseFiles] = useState<Record<string, string>>({})
  const [files, setFiles] = useState<Record<string, string>>({})
  const [active, setActive] = useState("Dockerfile")
  const [label, setLabel] = useState("")
  const [manifestName, setManifestName] = useState("")
  const [view, setView] = useState("edit")
  const [previewProfile, setPreviewProfile] = useState("")
  const [previewServices, setPreviewServices] = useState<ServiceSelection>({ database: false, vaultSecrets: [] })
  const serviceOptions = useMemo(() => {
    try {
      const catalog: ServiceCatalog = JSON.parse(profiles.find(p => p.id === previewProfile)?.settings.serviceCatalog || "{}") || {}
      return { allowEnvironment:catalog.allowEnvironment, database:catalog.database, databaseName: catalog.database?.name, databaseVaultSecret: catalog.database?.requiredVaultSecret, vaultSecrets: (catalog.vaultSecrets || []).map(b => ({ id: b.id, name: b.name, path:b.path, envs:b.envs, allowPathSuffix:b.allowPathSuffix, allowEnvMapping:b.allowEnvMapping })) }
    } catch { return undefined }
  }, [profiles, previewProfile])
  const [rendered, setRendered] = useState<Record<string, string> | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)
  const [rendering, setRendering] = useState(false)
  const editor = useRef<HTMLTextAreaElement>(null)

  async function loadVersion(version: number) {
    const loaded = await api<VersionFiles>(`${base}/versions/${version}`)
    setBaseVersion(version)
    setBaseFiles(loaded.files)
    setFiles(loaded.files)
    setActive("Dockerfile")
    setRendered(null)
  }
  useEffect(() => {
    let live = true
    api<TemplateDetail>(base)
      .then(async (value) => {
        if (!live) return
        setDetail(value)
        setActive(value.fileOrder[0] ?? "Dockerfile")
        const latest = value.versions[0]?.version ?? 0
        const loaded = await api<VersionFiles>(`${base}/versions/${latest}`)
        if (!live) return
        setBaseVersion(latest)
        setBaseFiles(loaded.files)
        setFiles(loaded.files)
      })
      .catch((cause) => live && setError(cause))
    return () => {
      live = false
    }
  }, [base])

  const changed = useMemo(
    () =>
      Array.from(
        new Set([...Object.keys(files), ...Object.keys(baseFiles)])
      ).filter((path) => files[path] !== baseFiles[path]),
    [files, baseFiles]
  )
  const fileOrder = detail
    ? [
        ...detail.fileOrder,
        ...Array.from(
          new Set([...Object.keys(files), ...Object.keys(baseFiles)])
        )
          .filter((path) => !detail.fileOrder.includes(path))
          .sort(),
      ]
    : []
  const rows = useMemo(
    () => reviewTextRows(baseFiles[active] ?? "", files[active] ?? ""),
    [baseFiles, files, active]
  )

  function insert(text: string) {
    const el = editor.current
    const current = files[active] ?? ""
    const start = el?.selectionStart ?? current.length
    const end = el?.selectionEnd ?? current.length
    setFiles({
      ...files,
      [active]: current.slice(0, start) + text + current.slice(end),
    })
    window.setTimeout(() => {
      el?.focus()
      el?.setSelectionRange(start + text.length, start + text.length)
    })
  }
  useEffect(() => {
    if (!detail) return
    let live = true
    const timer = window.setTimeout(async () => {
      setRendered(null)
      setRendering(true)
      try {
        const settings = profiles.find((p) => p.id === previewProfile)?.settings
        const result = await apiPost<{ files: Record<string, string> }>(
          `${base}/preview`,
          { files, ...(settings ? { settings } : {}), hasRenvLock: true, services: previewServices }
        )
        if (live) {
          setRendered(result.files)
          setError(null)
        }
      } catch (cause) {
        if (live) setError(cause)
      } finally {
        if (live) setRendering(false)
      }
    }, 500)
    return () => {
      live = false
      window.clearTimeout(timer)
    }
  }, [base, detail, files, previewProfile, profiles, previewServices])

  async function save() {
    setBusy(true)
    setError(null)
    try {
      await apiPost(`${base}/versions`, { label, files })
      onClose(true)
    } catch (cause) {
      setError(cause)
    } finally {
      setBusy(false)
    }
  }
  if (!detail)
    return error != null ? (
      <ErrorNotice error={error} />
    ) : (
      <p role="status" className="text-sm text-muted-foreground">
        Loading template…
      </p>
    )
  return (
    <div className="space-y-5">
      <SettingsPanel padded
        title={`${detail.template.custom ? "Edit" : "Customize"} ${detail.template.name}`}
        description="Edit the files JustCD generates for this application template. Saving creates a new immutable version; profiles choose which version to use."
        action={
          <Button
            type="button"
            variant="outline"
            onClick={() => onClose(false)}
          >
            Back to templates
          </Button>
        }
      >
        <div className="space-y-4">
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            <FormField
              label="Start from"
              htmlFor="template-base"
              hint="Switching discards unsaved edits."
            >
              <FormSelect
                id="template-base"
                value={String(baseVersion)}
                onValueChange={(value) =>
                  void loadVersion(Number(value)).catch(setError)
                }
                items={[
                  ...(detail.template.custom ? [] : [{ value: "0", label: "Built-in (read-only original)" }]),
                  ...detail.versions.map((v) => ({
                    value: String(v.version),
                    label: versionLabel(v.version, v.label),
                  })),
                ]}
              />
            </FormField>
            <FormField
              label="Preview with"
              htmlFor="template-preview-profile"
              hint="Preview updates automatically with the selected profile or sample values."
            >
              <FormSelect
                id="template-preview-profile"
                value={previewProfile}
                emptyOption="Sample values"
                onValueChange={value => { setPreviewProfile(value); setPreviewServices({ database: false, vaultSecrets: [] }) }}
                items={[
                  ...profiles
                    .filter((p) => p.settings.templateId === templateId)
                    .map((p) => ({ value: p.id, label: p.name })),
                ]}
              />
            </FormField>
          </div>
          <PublishingServicesPicker preview options={serviceOptions} value={previewServices} onChange={setPreviewServices} />
          <div className="flex flex-wrap items-center justify-between gap-3 py-3">
            <p className="max-w-2xl text-xs leading-5 text-muted-foreground">
              Optional manifests are included in the deployment plan. An ingress
              policy isolates incoming traffic for this app; configure
              ingressNamespace in the profile and allow any other required
              callers.
            </p>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={!!files[networkPolicyPath]}
              onClick={() => {
                setFiles({ ...files, [networkPolicyPath]: ingressPolicy })
                setActive(networkPolicyPath)
                setView("edit")
              }}
            >
              Add ingress network policy
            </Button>
          </div>
          <Disclosure summary="Add another optional manifest">
            <div className="flex flex-wrap items-end gap-3">
              <div className="min-w-0 flex-1">
                <FormField
                  label="Manifest filename"
                  htmlFor="template-manifest-name"

                >
                  <Input
                    id="template-manifest-name"
                    aria-describedby="template-manifest-hint"
                    value={manifestName}
                    onChange={(event) => setManifestName(event.target.value)}
                    placeholder="monitoring-policy.yaml"
                  />
                </FormField>
              </div>
              <Button
                type="button"
                variant="outline"
                disabled={
                  !/^[a-z0-9][a-z0-9-]{0,62}\.yaml$/.test(manifestName) ||
                  files[`.justcd/manifests/${manifestName}`] !== undefined ||
                  Object.keys(files).filter((path) =>
                    path.startsWith(".justcd/manifests/")
                  ).length >= 10
                }
                onClick={() => {
                  const path = `.justcd/manifests/${manifestName}`
                  setFiles({
                    ...files,
                    [path]:
                      "# Add ConfigMap, NetworkPolicy, PodDisruptionBudget or HorizontalPodAutoscaler resources here.\n# Use {{ .Namespace }} for metadata.namespace.\n",
                  })
                  setActive(path)
                  setManifestName("")
                  setView("edit")
                }}
              >
                Add manifest
              </Button>
            </div>
            <p className="mt-2 text-sm leading-4 text-muted-foreground" id="template-manifest-hint">Lowercase letters, numbers and hyphens, ending in .yaml. Up to 10 optional files; all resources must target this app&apos;s namespace.</p>
          </Disclosure>
          <Tabs
            value={active}
            onValueChange={(value) => setActive(String(value))}
          >
            <TabsList

              aria-label="Template files"
              className="flex-wrap"
            >
              {fileOrder.map((path) => (
                <TabsTrigger
                  key={path}
                  value={path}
                  className="font-mono text-xs"
                >
                  {path}
                  {changed.includes(path) && (
                    <span
                      aria-label="modified"
                      className="size-1.5 rounded-full bg-primary"
                    />
                  )}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
          <Tabs value={view} onValueChange={(value) => setView(String(value))}>
            <TabsList aria-label="Template editor view">
              <TabsTrigger value="edit">Edit</TabsTrigger>
              <TabsTrigger value="compare">Compare changes</TabsTrigger>
            </TabsList>
          </Tabs>
          {view === "edit" ? (
            <Workbench className="h-[56rem] lg:h-[32rem]" focusShortcut={false}><Workbench.Canvas className="!items-stretch !justify-stretch !p-0 !bg-none"><div className="grid w-full min-w-0 grid-cols-1 lg:grid-cols-2">
              <div className="flex min-h-0 min-w-0 flex-col">
                <div className="flex shrink-0 items-center justify-between bg-muted/40 px-3 py-2">
                  <label
                    htmlFor="template-file"
                    className="text-xs font-medium"
                  >
                    Your template
                  </label>
                  <Badge
                    variant={
                      changed.includes(active) ? "info" : "outline"
                    }
                    size="sm"
                  >
                    {files[active] === undefined
                      ? "Removed"
                      : changed.includes(active)
                        ? "Modified"
                        : "Unchanged"}
                  </Badge>
                </div>
                <Textarea
                  id="template-file"
                  autoResize={false}
                  ref={editor}
                  spellCheck={false}
                  wrap="off"
                  className={editorClass}
                  value={files[active] ?? ""}
                  onChange={(event) =>
                    setFiles({ ...files, [active]: event.target.value })
                  }
                />
              </div>
              <div className="flex min-h-0 min-w-0 flex-col border-t lg:border-t-0 lg:border-l">
                <div className="flex shrink-0 items-center justify-between gap-2 bg-muted/40 px-3 py-2">
                  <label
                    htmlFor="template-rendered"
                    className="text-xs font-medium"
                  >
                    Rendered output
                  </label>
                  <Badge variant="outline" size="sm">
                    {profiles.find((p) => p.id === previewProfile)?.name ??
                      "Sample values"}
                  </Badge>
                </div>
                <Textarea
                  id="template-rendered"
                  autoResize={false}
                  readOnly
                  spellCheck={false}
                  wrap="off"
                  className={editorClass}
                  aria-busy={rendering}
                  value={
                    (rendered
                      ? (rendered[active] ??
                        "# This optional manifest was removed.")
                      : undefined) ??
                    (rendering
                      ? "Rendering preview…"
                      : "Preview unavailable. Review the validation error below.")
                  }
                />
                <span className="sr-only" role="status">
                  {rendering
                    ? "Rendering preview"
                    : rendered
                      ? "Preview updated"
                      : "Preview unavailable"}
                </span>
              </div>
            </div></Workbench.Canvas></Workbench>
          ) : (
            <Workbench className="h-[34rem]" focusShortcut={false}><Workbench.Canvas className="!items-stretch !justify-stretch !p-0 !bg-none"><DiffView rows={rows} /></Workbench.Canvas></Workbench>
          )}
          <p className="text-xs text-muted-foreground">
            Go template syntax:{" "}
            {
              "{{ .Name }}, {{ if .Host }}…{{ end }}, {{ range .ExposureLabels }}"
            }
            .
          </p>
          <div className="flex flex-wrap items-center gap-3">
            <Button
              type="button"
              variant="outline"
              disabled={files[active] === baseFiles[active]}
              onClick={() =>
                setFiles({ ...files, [active]: baseFiles[active] ?? "" })
              }
            >
              Reset this file
            </Button>
            {active.startsWith(".justcd/manifests/") && (
              <Button
                type="button"
                variant="destructive"
                size="sm"
                disabled={files[active] === undefined}
                onClick={() => {
                  const next = { ...files }
                  delete next[active]
                  setFiles(next)
                  setActive("Dockerfile")
                }}
              >
                Remove manifest
              </Button>
            )}
          </div>
          <Disclosure summary="Placeholders and helper functions">
            <ul className="grid grid-cols-1 gap-2 md:grid-cols-2">
              {detail.placeholders.map((item) => {
                const isValue = item.name.startsWith(".")
                return (
                  <li key={item.name} className="py-2.5">
                    <div className="flex items-center justify-between gap-2">
                      <code className="text-xs font-medium">{item.name}</code>
                      {isValue && (
                        <Button
                          type="button"
                          size="sm"
                          variant="ghost"
                          onClick={() => insert(`{{ ${item.name} }}`)}
                        >
                          Insert
                        </Button>
                      )}
                    </div>
                    <p className="mt-1 text-xs leading-5 text-muted-foreground">
                      {item.description}
                    </p>
                  </li>
                )
              })}
            </ul>
          </Disclosure>
        </div>
      </SettingsPanel>
      {error != null && <ErrorNotice error={error} />}
      <SettingsPanel padded title="Save as a new version">
        <div className="flex flex-wrap items-end gap-4">
          <div className="min-w-0 flex-1 basis-64">
            <FormField
              label="Version label (optional)"
              htmlFor="template-label"
              hint="For example: Corporate labels and network policy."
            >
              <Input
                id="template-label"
                maxLength={100}
                value={label}
                onChange={(event) => setLabel(event.target.value)}
              />
            </FormField>
          </div>
          <div className="flex flex-col items-end gap-1">
            <Button
              type="button"
              disabled={!changed.length}
              loading={busy}
              loadingText="Saving…"
              onClick={() => void save()}
            >
              Save as new version
            </Button>
            <span className="text-xs text-muted-foreground">
              {changed.length
                ? `${changed.length} file${changed.length === 1 ? "" : "s"} changed`
                : "Change a file to save a version"}
            </span>
          </div>
        </div>
        <p className="mt-3 text-xs leading-5 text-muted-foreground">
          Versions are immutable. JustCD renders the template for many
          configurations and rejects unsafe or broken results (privileged
          containers, host access, root, foreign images, a pipeline that no
          longer publishes the image digest). Existing applications keep their
          version until their profile upgrade is reviewed.
        </p>
      </SettingsPanel>
    </div>
  )
}

function DiffView({ rows }: { rows: ReviewRow[] }) {
  return (
    <div
      className="max-h-[32rem] w-full overflow-auto bg-background"
      tabIndex={0}
      role="region"
      aria-label="Template changes"
    >
      <div className="min-w-0 sm:min-w-[40rem]">
        <div className="sticky top-0 z-10 grid grid-cols-1 bg-muted text-xs font-medium sm:grid-cols-2">
          <div className="hidden border-r px-3 py-2 sm:block">Original</div>
          <div className="px-3 py-2"><span className="sm:hidden">Template changes</span><span className="hidden sm:inline">Your changes</span></div>
        </div>
        {rows.map((row, index) => (
          <div
            key={index}
            className="grid grid-cols-1 font-mono text-xs leading-5 sm:grid-cols-2"
          >
            {(["before", "after"] as const).map((side) => {
              const line = side === "before" ? row.oldLine : row.newLine
              const content = row[side]
              return (
                <div
                  key={side}
                  className={`${content === undefined || (side === "before" && !row.changed) ? "hidden sm:flex" : "flex"} min-w-0 ${side === "before" ? "sm:border-r" : ""} ${row.changed && content !== undefined ? (side === "before" ? "bg-destructive/10" : "bg-success/10") : ""}`}
                >
                  <span className="w-10 shrink-0 border-r px-2 text-right text-muted-foreground select-none">
                    {line}
                  </span>
                  <span className="w-5 shrink-0 text-center text-muted-foreground select-none">
                    {row.changed && content !== undefined
                      ? side === "before"
                        ? "−"
                        : "+"
                      : ""}
                  </span>
                  <code className="min-w-0 flex-1 pr-3 break-all whitespace-pre-wrap">
                    {content ?? " "}
                  </code>
                </div>
              )
            })}
          </div>
        ))}
      </div>
    </div>
  )
}

export function TemplateBadge({ version }: { version: number | undefined }) {
  return (
    <Badge variant={version ? "info" : "outline"} size="sm">
      {version ? `Custom v${version}` : "Built-in"}
    </Badge>
  )
}
