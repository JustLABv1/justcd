"use client"

import { useEffect, useMemo, useRef, useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { Disclosure } from "@/components/ui/collapsible"
import { FormSelect } from "@/components/ui/form-select"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Badge } from "@/components/reui/badge"
import { FormField, Panel } from "@/components/ui-kit"
import { ErrorNotice } from "@/components/workspace-ui"
import { api, apiPost } from "@/lib/api"
import { contextRows, reviewTextRows, type ReviewRow } from "@/lib/line-diff"
import type { PublishingProfile } from "@/lib/publishing"

export type TemplateVersionMeta = {
  version: number
  label: string
  createdBy: string
  createdAt: string
}
type TemplateDetail = {
  template: { id: string; name: string; description: string; runtime: string }
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

const editorClass = "min-h-[28rem] font-mono text-xs leading-5 whitespace-pre"

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
  const [previewProfile, setPreviewProfile] = useState("")
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
    () => Object.keys(files).filter((path) => files[path] !== baseFiles[path]),
    [files, baseFiles]
  )
  const rows = useMemo(
    () => reviewTextRows(baseFiles[active] ?? "", files[active] ?? ""),
    [baseFiles, files, active]
  )
  const baseName = versionLabel(
    baseVersion,
    detail?.versions.find((v) => v.version === baseVersion)?.label
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
  async function preview() {
    setRendering(true)
    setError(null)
    try {
      const settings = profiles.find((p) => p.id === previewProfile)?.settings
      const result = await apiPost<{ files: Record<string, string> }>(
        `${base}/preview`,
        { files, ...(settings ? { settings } : {}), hasRenvLock: true }
      )
      setRendered(result.files)
    } catch (cause) {
      setRendered(null)
      setError(cause)
    } finally {
      setRendering(false)
    }
  }
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
      <Panel
        title={`Customize ${detail.template.name}`}
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
        <div className="space-y-4 p-5">
          <div className="grid gap-4 md:grid-cols-2">
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
                  { value: "0", label: "Built-in (read-only original)" },
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
              hint="Render with a profile's real settings, or sample values."
            >
              <FormSelect
                id="template-preview-profile"
                value={previewProfile}
                onValueChange={setPreviewProfile}
                items={[
                  { value: "", label: "Sample values" },
                  ...profiles
                    .filter((p) => p.settings.templateId === templateId)
                    .map((p) => ({ value: p.id, label: p.name })),
                ]}
              />
            </FormField>
          </div>
          <Tabs
            value={active}
            onValueChange={(value) => setActive(String(value))}
          >
            <TabsList
              variant="default"
              aria-label="Template files"
              className="flex-wrap"
            >
              {detail.fileOrder.map((path) => (
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
          <FormField
            label={active}
            htmlFor="template-file"
            hint={`Go template syntax: {{ .Name }}, {{ if .Host }}…{{ end }}, {{ range .ExposureLabels }}. Version: ${baseName}.`}
          >
            <Textarea
              id="template-file"
              ref={editor}
              spellCheck={false}
              className={editorClass}
              value={files[active] ?? ""}
              onChange={(event) =>
                setFiles({ ...files, [active]: event.target.value })
              }
            />
          </FormField>
          <div className="flex flex-wrap items-center gap-3">
            <Button
              type="button"
              variant="outline"
              disabled={files[active] === baseFiles[active]}
              onClick={() =>
                setFiles({ ...files, [active]: baseFiles[active] })
              }
            >
              Reset this file
            </Button>
            <Button
              type="button"
              variant="outline"
              loading={rendering}
              loadingText="Rendering…"
              onClick={() => void preview()}
            >
              Preview rendered files
            </Button>
          </div>
          <Disclosure summary="Placeholders and helper functions">
            <ul className="grid gap-2 md:grid-cols-2">
              {detail.placeholders.map((item) => {
                const isValue = item.name.startsWith(".")
                return (
                  <li key={item.name} className="rounded-lg border p-2.5">
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
          <Disclosure
            summary={`Changes in ${active} compared with ${baseName}`}
            defaultOpen={changed.includes(active)}
          >
            {rows.some((row) => row.changed) ? (
              <DiffView rows={rows} />
            ) : (
              <p className="text-xs text-muted-foreground">
                No changes in this file.
              </p>
            )}
          </Disclosure>
          {rendered && (
            <Disclosure summary={`Rendered ${active}`} defaultOpen>
              <pre className="max-h-96 overflow-auto rounded-lg border bg-muted/40 p-3 font-mono text-xs leading-5">
                {rendered[active]}
              </pre>
            </Disclosure>
          )}
        </div>
      </Panel>
      {error != null && <ErrorNotice error={error} />}
      <Panel title="Save as a new version">
        <div className="flex flex-wrap items-end gap-4 p-5">
          <div className="min-w-64 flex-1">
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
        <p className="px-5 pb-5 text-xs leading-5 text-muted-foreground">
          Versions are immutable. JustCD renders the template for many
          configurations and rejects unsafe or broken results (privileged
          containers, host access, root, foreign images, a pipeline that no
          longer publishes the image digest). Existing applications keep their
          version until their profile upgrade is reviewed.
        </p>
      </Panel>
    </div>
  )
}

function DiffView({ rows }: { rows: ReviewRow[] }) {
  const visible = contextRows(rows, 2)
  const out: React.ReactNode[] = []
  let skipped = 0
  const flush = (key: string) => {
    if (skipped)
      out.push(
        <div key={key} className="text-muted-foreground">
          ⋯ {skipped} unchanged lines
        </div>
      )
    skipped = 0
  }
  rows.forEach((row, index) => {
    if (!visible.has(index)) {
      skipped++
      return
    }
    flush(`skip-${index}`)
    if (!row.changed) {
      out.push(<div key={index}>{"  " + (row.after ?? "")}</div>)
      return
    }
    if (row.before !== undefined)
      out.push(
        <div key={`r${index}`} className="bg-destructive/15">
          {"- " + row.before}
        </div>
      )
    if (row.after !== undefined)
      out.push(
        <div key={`a${index}`} className="bg-success/15">
          {"+ " + row.after}
        </div>
      )
  })
  flush("skip-end")
  return (
    <pre className="max-h-96 overflow-auto rounded-lg border bg-muted/40 p-3 font-mono text-xs leading-5">
      {out}
    </pre>
  )
}

export function TemplateBadge({ version }: { version: number | undefined }) {
  return (
    <Badge variant={version ? "info-light" : "outline"} size="sm">
      {version ? `Custom v${version}` : "Built-in"}
    </Badge>
  )
}
