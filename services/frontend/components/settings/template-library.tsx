"use client"

import { useRef, useState } from "react"
import { RowActions } from "@/components/action-menu"
import { Card } from "@/components/panelui/card"
import { Badge } from "@/components/panelui/badge"
import { Button } from "@/components/panelui/button"
import { AppDialog, DialogCancel, DialogFooter } from "@/components/app-dialog"
import { FormSelect } from "@/components/form-select"
import { Input } from "@/components/panelui/input"
import { Textarea } from "@/components/panelui/textarea"
import { FormField } from "@/components/ui-kit"
import { ErrorNotice } from "@/components/workspace-ui"
import { api, apiPost } from "@/lib/api"
import type { TemplateLayoutRule } from "@/components/applications/template-catalog"
import {
  TemplateBadge,
  versionLabel,
  type TemplateVersionMeta,
} from "./template-editor"
import { SettingsPanel } from "@/components/settings/settings-section"

export type TemplateSummary = {
  template: {
    id: string
    name: string
    description: string
    category: string
    runtime: string
    entryPoint: string
    port?: number
    layout?: TemplateLayoutRule[]
    detail?: string
    custom?: boolean
    baseTemplate?: string
  }
  versions: TemplateVersionMeta[]
}

type Summary = TemplateSummary["template"]
type Mode =
  | { kind: "new" }
  | { kind: "copy"; base: Summary }
  | { kind: "edit"; template: Summary }
  | { kind: "import"; document: ExchangeFile }

type ExchangeFile = {
  format: string
  template: { id: string; name: string }
  [key: string]: unknown
}

const kinds = [
  { value: "server", label: "Server application · Python, R, Node and others" },
  { value: "static", label: "Static website · ready-made files" },
  { value: "static-build", label: "Static website · built with Python" },
]
const kindDefaults: Record<string, { entry: string; port: string }> = {
  server: { entry: "app.py", port: "8080" },
  static: { entry: ".", port: "" },
  "static-build": { entry: "build_html.py", port: "" },
}

/** One line per rule; "|" separates alternatives, "," separates files that are all needed. */
export function formatLayout(rules: TemplateLayoutRule[] = []) {
  return rules
    .map((rule) => rule.anyOf.map((group) => group.join(", ")).join(" | "))
    .join("\n")
}
export function parseLayout(text: string): TemplateLayoutRule[] {
  return text
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => ({
      anyOf: line
        .split("|")
        .map((group) =>
          group
            .split(",")
            .map((file) => file.trim())
            .filter(Boolean)
        )
        .filter((group) => group.length),
    }))
}

function slug(name: string) {
  const id = name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 40)
    .replace(/-+$/, "")
  return /^[a-z]/.test(id) ? id : id ? `t-${id}`.slice(0, 40) : ""
}

function download(name: string, data: unknown) {
  const url = URL.createObjectURL(
    new Blob([JSON.stringify(data, null, 2)], { type: "application/json" })
  )
  const link = document.createElement("a")
  link.href = url
  link.download = name
  link.click()
  URL.revokeObjectURL(url)
}

/**
 * Lists the built-in and workspace templates and manages the workspace's own:
 * create from a kind, copy any template, edit its definition, export and import
 * portable files, and delete templates no profile uses.
 */
export function TemplateLibrary({
  workspaceId,
  templates,
  onCustomize,
  onChanged,
}: {
  workspaceId: string
  templates: TemplateSummary[]
  onCustomize: (id: string) => void
  onChanged: (notice: string) => void
}) {
  const base = `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/publishing-templates`
  const [mode, setMode] = useState<Mode | null>(null)
  const [error, setError] = useState<unknown>(null)
  const fileInput = useRef<HTMLInputElement>(null)

  async function exportTemplate(id: string) {
    try {
      download(
        `${id}.justcd-template.json`,
        await api<unknown>(`${base}/${encodeURIComponent(id)}/export`)
      )
    } catch (cause) {
      setError(cause)
    }
  }
  async function remove(id: string) {
    await api(`${base}/${encodeURIComponent(id)}`, { method: "DELETE" })
    onChanged("Template deleted.")
  }
  async function pickFile(file: File | undefined) {
    if (!file) return
    setError(null)
    try {
      const document = JSON.parse(await file.text()) as ExchangeFile
      if (document.format !== "justcd.template/v1" || !document.template?.id)
        throw new Error("This is not a JustCD template file.")
      setMode({ kind: "import", document })
    } catch (cause) {
      setError(
        cause instanceof SyntaxError
          ? new Error("The file is not valid JSON.")
          : cause
      )
    }
  }

  return (
    <>
      <SettingsPanel surface="flat"
        title="Application templates"
        description="The files JustCD generates for each application type: Dockerfile, build pipeline and Kubernetes manifest. Customize them for your platform, or create your own template; every save is a new, immutable version that profiles can select."
        action={
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              onClick={() => fileInput.current?.click()}
            >
              Import
            </Button>
            <Button variant="outline" onClick={() => setMode({ kind: "new" })}>
              New template
            </Button>
            <Input
              ref={fileInput}
              type="file"
              accept="application/json,.json"
              className="sr-only"
              tabIndex={-1}
              aria-label="Template file to import"
              onChange={(event) => {
                void pickFile(event.target.files?.[0])
                event.target.value = ""
              }}
            />
          </div>
        }
      >
        <div className="space-y-3">
          {templates.map(({ template, versions }) => (
            <Card key={template.id} size="sm"><Card.Content className="flex flex-wrap items-center justify-between gap-4">
              <div className="min-w-0 space-y-1">
                <div className="flex flex-wrap items-center gap-2">
                  <h3 className="text-sm font-medium">{template.name}</h3>
                  <Badge variant="outline" size="sm">
                    {template.category}
                  </Badge>
                  {template.custom && (
                    <Badge variant="info" size="sm">
                      Custom
                    </Badge>
                  )}
                  <TemplateBadge version={versions[0]?.version} />
                </div>
                <p className="text-xs text-muted-foreground">
                  {template.custom
                    ? `${versions.length} version${versions.length === 1 ? "" : "s"} · latest ${versions[0] ? versionLabel(versions[0].version, versions[0].label) : "–"}${template.baseTemplate ? ` · copied from ${template.baseTemplate}` : ""}`
                    : versions.length
                      ? `${versions.length} custom version${versions.length === 1 ? "" : "s"} · latest ${versionLabel(versions[0].version, versions[0].label)}`
                      : "Using the built-in template."}
                </p>
              </div>
              <RowActions
                label={template.name}
                primary={
                  <Button
                    variant="outline"
                    onClick={() => onCustomize(template.id)}
                  >
                    {template.custom ? "Edit files" : "Customize"}
                  </Button>
                }
                items={[
                  {
                    label: "Copy as new template",
                    onSelect: () => setMode({ kind: "copy", base: template }),
                  },
                  ...(template.custom
                    ? [
                        {
                          label: "Template settings",
                          onSelect: () => setMode({ kind: "edit", template }),
                        },
                      ]
                    : []),
                  {
                    label: "Export",
                    onSelect: () => void exportTemplate(template.id),
                  },
                  ...(template.custom
                    ? [
                        {
                          label: "Delete template",
                          destructive: true,
                          confirm: {
                            title: "Delete template",
                            description: `Delete ${template.name} and all its versions? Templates used by a profile cannot be deleted.`,
                            confirmLabel: "Delete template",
                            onConfirm: () => remove(template.id),
                          },
                        },
                      ]
                    : []),
                ]}
              />
            </Card.Content></Card>
          ))}
          {!templates.length && (
            <p className="p-5 text-sm text-muted-foreground">
              Templates are loading or unavailable.
            </p>
          )}
        </div>
        {error != null && (
          <div className="p-5 pt-0">
            <ErrorNotice error={error} />
          </div>
        )}
      </SettingsPanel>
      {mode && (
        <TemplateDialog
          key={
            mode.kind +
            ("base" in mode
              ? mode.base.id
              : "template" in mode
                ? mode.template.id
                : "")
          }
          mode={mode}
          base={base}
          existing={templates.map((t) => t.template.id)}
          onClose={() => setMode(null)}
          onDone={(notice, openId) => {
            setMode(null)
            onChanged(notice)
            if (openId) onCustomize(openId)
          }}
        />
      )}
    </>
  )
}

function TemplateDialog({
  mode,
  base,
  existing,
  onClose,
  onDone,
}: {
  mode: Mode
  base: string
  existing: string[]
  onClose: () => void
  onDone: (notice: string, openId?: string) => void
}) {
  const source =
    mode.kind === "copy"
      ? mode.base
      : mode.kind === "edit"
        ? mode.template
        : null
  const imported = mode.kind === "import" ? mode.document : null
  const [name, setName] = useState(
    imported?.template.name ??
      (mode.kind === "copy"
        ? `Copy of ${mode.base.name}`
        : (source?.name ?? ""))
  )
  const [id, setId] = useState(
    imported?.template.id ??
      (mode.kind === "copy" ? slug(`${mode.base.id}-copy`) : (source?.id ?? ""))
  )
  const [idTouched, setIdTouched] = useState(mode.kind === "import")
  const [description, setDescription] = useState(source?.description ?? "")
  const [kind, setKind] = useState("server")
  const [entry, setEntry] = useState(
    source?.entryPoint ?? kindDefaults.server.entry
  )
  const [port, setPort] = useState(
    source?.port ? String(source.port) : kindDefaults.server.port
  )
  const [rules, setRules] = useState(formatLayout(source?.layout))
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const runtime = source?.runtime ?? (kind === "server" ? "server" : kind)
  const showPort =
    !imported &&
    (mode.kind === "new"
      ? kind === "server"
      : runtime !== "static" && runtime !== "python-static")
  const idTaken = mode.kind !== "edit" && existing.includes(id)
  const idInvalid =
    mode.kind !== "edit" && !/^[a-z][a-z0-9-]{1,38}[a-z0-9]$/.test(id)
  const title =
    mode.kind === "new"
      ? "New template"
      : mode.kind === "copy"
        ? `Copy ${mode.base.name}`
        : mode.kind === "edit"
          ? "Template settings"
          : "Import template"

  async function submit(event: React.FormEvent) {
    event.preventDefault()
    if (busy) return
    if (!name.trim() || idInvalid || idTaken) {
      setError(
        new Error(
          idTaken
            ? "A template with this ID already exists."
            : "Enter a name and a valid ID."
        )
      )
      return
    }
    setBusy(true)
    setError(null)
    const definition = {
      name: name.trim(),
      description,
      entryPoint: entry.trim(),
      port: showPort && port ? Number(port) : 0,
      layout: parseLayout(rules),
    }
    try {
      if (mode.kind === "import") {
        await apiPost(
          `${base}/import?${new URLSearchParams({ id, name: name.trim() })}`,
          mode.document
        )
        onDone(`Template ${name.trim()} imported.`, id)
      } else if (mode.kind === "edit") {
        await api(`${base}/${encodeURIComponent(mode.template.id)}`, {
          method: "PUT",
          body: JSON.stringify(definition),
        })
        onDone("Template settings saved.")
      } else {
        await apiPost(base, {
          ...definition,
          id,
          ...(mode.kind === "copy" ? { baseTemplate: mode.base.id } : { kind }),
        })
        onDone(`Template ${name.trim()} created.`, id)
      }
    } catch (cause) {
      setError(cause)
    } finally {
      setBusy(false)
    }
  }
  return (
    <AppDialog
      open
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
      busy={busy}
      size="lg"
      title={title}
      description={
        mode.kind === "import"
          ? "Review the name and ID before adding the template to this workspace. Its files are validated first."
          : mode.kind === "copy"
            ? "The copy starts with the latest files of the original and is yours to change."
            : mode.kind === "edit"
              ? "Changes apply to applications the next time they render. The ID and kind cannot change."
              : "Pick what kind of app it runs. JustCD adds starter files you can edit next."
      }
    >
      <form onSubmit={submit} noValidate className="space-y-4">
        <div className="grid gap-4 sm:grid-cols-2">
          <FormField label="Name" htmlFor="template-name">
            <Input
              id="template-name"
              required
              maxLength={80}
              value={name}
              onChange={(event) => {
                setName(event.target.value)
                if (!idTouched && mode.kind !== "edit")
                  setId(slug(event.target.value))
              }}
              placeholder="Dash analytics app"
            />
          </FormField>
          <FormField
            label="Template ID"
            htmlFor="template-id"
            hint="Lowercase letters, numbers and hyphens. Cannot change later."
            error={
              id &&
              (idInvalid
                ? "Use 3–40 lowercase letters, numbers and hyphens, starting with a letter."
                : idTaken
                  ? "This ID is already used."
                  : undefined)
            }
          >
            <Input
              id="template-id"
              required
              disabled={mode.kind === "edit"}
              value={id}
              onChange={(event) => {
                setIdTouched(true)
                setId(event.target.value)
              }}
            />
          </FormField>
        </div>
        {!imported && (
          <>
            {mode.kind === "new" && (
              <FormField label="Kind" htmlFor="template-kind">
                <FormSelect
                  id="template-kind"
                  value={kind}
                  items={kinds}
                  onValueChange={(next) => {
                    setKind(next)
                    setEntry(kindDefaults[next].entry)
                    setPort(kindDefaults[next].port)
                  }}
                />
              </FormField>
            )}
            <FormField
              label="Description"
              htmlFor="template-description"
              hint="Shown to analysts when they choose a template."
            >
              <Textarea
                id="template-description"
                maxLength={400}
                value={description}
                onChange={(event) => setDescription(event.target.value)}
              />
            </FormField>
            <div className="grid gap-4 sm:grid-cols-2">
              <FormField
                label="Default entry point"
                htmlFor="template-entry"
                hint="File or directory inside the uploaded project."
              >
                <Input
                  id="template-entry"
                  required
                  value={entry}
                  onChange={(event) => setEntry(event.target.value)}
                />
              </FormField>
              {showPort && (
                <FormField
                  label="Container port"
                  htmlFor="template-port"
                  hint="The port your app listens on."
                >
                  <Input
                    id="template-port"
                    type="number"
                    min={1}
                    max={65535}
                    required
                    value={port}
                    onChange={(event) => setPort(event.target.value)}
                  />
                </FormField>
              )}
            </div>
            <FormField
              label="Upload checks (optional)"
              htmlFor="template-rules"
              hint="One requirement per line. Use | between alternatives and , between files that are all needed. {entry} is the entry point. Example: requirements.txt | pyproject.toml"
            >
              <Textarea
                id="template-rules"
                className="font-mono text-xs"
                value={rules}
                onChange={(event) => setRules(event.target.value)}
                placeholder={"{entry}\nrequirements.txt | pyproject.toml"}
              />
            </FormField>
          </>
        )}
        {error != null && <ErrorNotice inline error={error} />}
        <DialogFooter>
          <DialogCancel disabled={busy} />
          <Button type="submit" loading={busy} loadingText="Saving…">
            {mode.kind === "import"
              ? "Import template"
              : mode.kind === "edit"
                ? "Save settings"
                : "Create template"}
          </Button>
        </DialogFooter>
      </form>
    </AppDialog>
  )
}
