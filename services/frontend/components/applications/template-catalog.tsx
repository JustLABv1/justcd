"use client"

import { useEffect, useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import {
  CodeIcon,
  Globe02Icon,
  Search01Icon,
} from "@hugeicons/core-free-icons"
import { api } from "@/lib/api"
import { templatesUrl } from "@/lib/publishing"
import { ErrorNotice } from "@/components/workspace-ui"
import { Alert } from "@/components/panelui/alert"
import { Badge } from "@/components/panelui/badge"
import { Button } from "@/components/panelui/button"
import { Frame } from "@/components/panelui/frame"
import { Input } from "@/components/panelui/input"
import { Tabs } from "@/components/panelui/tabs"

export type TemplateLayoutRule = { message?: string; anyOf: string[][] }

export type ApplicationTemplate = {
  id: string
  name: string
  description: string
  runtime: "shiny" | "streamlit" | "python-static" | "static" | "server"
  category: string
  entryPoint: string
  detail: string
  port?: number
  layout?: TemplateLayoutRule[]
  /** Owned by a workspace rather than shipped with JustCD. */
  custom?: boolean
  baseTemplate?: string
}

export function entryLabel(template: Pick<ApplicationTemplate, "runtime">) {
  if (template.runtime === "static") return "Website directory"
  if (template.runtime === "shiny") return "Shiny app directory"
  if (template.runtime === "server") return "Entry file or directory"
  return "Python entry file"
}

/** Plain-language list of the files a template's upload rules require. */
export function layoutSummary(
  template: ApplicationTemplate,
  entry = template.entryPoint
) {
  return (template.layout ?? [])
    .map((rule) =>
      rule.anyOf
        .map((group) =>
          group.map((file) => file.replaceAll("{entry}", entry)).join(" + ")
        )
        .join(" or ")
    )
    .join("; ")
}

export function TemplateGlyph({
  template,
  className = "",
}: {
  template: ApplicationTemplate
  className?: string
}) {
  return (
    <span
      aria-hidden="true"
      className={`inline-flex size-10 shrink-0 items-center justify-center rounded-xl bg-background ${className}`}
    >
      {template.category === "Geospatial" ||
      template.runtime === "python-static" ||
      template.runtime === "static" ? (
        <HugeiconsIcon
          icon={Globe02Icon}
          className="size-5 text-emerald-600 dark:text-emerald-400"
          strokeWidth={1.7}
        />
      ) : template.runtime === "streamlit" || template.runtime === "server" ? (
        <HugeiconsIcon
          icon={CodeIcon}
          className="size-5 text-rose-500"
          strokeWidth={1.7}
        />
      ) : (
        <span className="text-2xl font-semibold tracking-tight text-blue-600 dark:text-blue-400">
          R<span className="text-primary">.</span>
        </span>
      )}
    </span>
  )
}

export function TemplateCatalog({
  onSelect,
  aside,
}: {
  onSelect: (template: ApplicationTemplate) => void
  /** Shown beside the page title, for example a link to manage publishing profiles. */
  aside?: React.ReactNode
}) {
  const [templates, setTemplates] = useState<ApplicationTemplate[]>([])
  const [error, setError] = useState<unknown>(null)
  const [loading, setLoading] = useState(true)
  const [reload, setReload] = useState(0)
  useEffect(() => {
    let active = true
    api<{ items: ApplicationTemplate[] }>(templatesUrl())
      .then((result) => {
        if (active) {
          setTemplates(result.items)
          setError(null)
        }
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
  }, [reload])
  const [search, setSearch] = useState("")
  const [category, setCategory] = useState("All")
  const visible = templates.filter(
    (template) =>
      (category === "All" ||
        template.category === category ||
        (category === "R" && template.runtime === "shiny")) &&
      `${template.name} ${template.description} ${template.category}`
        .toLowerCase()
        .includes(search.trim().toLowerCase())
  )
  const categories = [
    "All",
    ...Array.from(new Set(["R", "Python", "Geospatial", "Websites", ...templates.map((template) => template.category)])),
  ]
  return (
    <div className="space-y-5 pb-5">
      <header className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3">
        <div className="min-w-0 flex-1 basis-72">
          <h1 className="text-2xl font-semibold tracking-tight">Choose an app template</h1>
          <p className="mt-1.5 max-w-2xl text-sm leading-6 text-muted-foreground">
            Pick a runtime and JustCD sets up the repository, build and deployment for you.
          </p>
        </div>
        {aside}
      </header>

      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="max-w-full overflow-x-auto">
          <Tabs variant="segmented" value={category} onValueChange={(next) => { if (typeof next === "string") setCategory(next) }}>
            <Tabs.List aria-label="Template category">
              {categories.map((value) => <Tabs.Trigger key={value} value={value}>{value}</Tabs.Trigger>)}
            </Tabs.List>
          </Tabs>
        </div>
        <Input.Group className="w-full sm:w-72">
          <Input.Addon><HugeiconsIcon icon={Search01Icon} aria-hidden="true" strokeWidth={1.7} /></Input.Addon>
          <Input aria-label="Search templates" placeholder="Search templates…" value={search} onChange={(event) => setSearch(event.target.value)} />
        </Input.Group>
      </div>

      {error != null && <ErrorNotice error={error} />}
      {loading && <p role="status" className="text-sm text-muted-foreground">Loading templates…</p>}

      {visible.length > 0 && (
        <Frame>
          <Frame.Header>
            <Frame.Title as="h2">{category === "All" ? "All templates" : category}</Frame.Title>
            <Frame.Description>{visible.length} {visible.length === 1 ? "template" : "templates"}</Frame.Description>
          </Frame.Header>
          <Frame.Panel aria-label="Application templates">
            {visible.map((template) => (
              <Frame.Row key={template.id} chevron onClick={() => onSelect(template)} aria-label={`Use ${template.name} template`} className="items-start gap-4 py-4">
                <Frame.Media><TemplateGlyph template={template} /></Frame.Media>
                <Frame.Content className="gap-1">
                  <span className="flex flex-wrap items-center gap-2">
                    <Frame.Title as="p">{template.name}</Frame.Title>
                    {template.custom && <Badge size="sm" variant="outline">Custom</Badge>}
                  </span>
                  <Frame.Description className="whitespace-normal">{template.description}</Frame.Description>
                  <span className="text-xs text-muted-foreground">{template.detail}</span>
                </Frame.Content>
              </Frame.Row>
            ))}
          </Frame.Panel>
        </Frame>
      )}

      {!loading && !error && !visible.length && (
        <div role="status" className="rounded-2xl border border-dashed px-6 py-12 text-center">
          <p className="font-medium">No templates found</p>
          <p className="mt-2 text-sm text-muted-foreground">Try another search or category.</p>
          <Button type="button" variant="outline" className="mt-5" onClick={() => { setSearch(""); setCategory("All") }}>Clear filters</Button>
        </div>
      )}
      {error != null && (
        <Button type="button" variant="outline" onClick={() => { setLoading(true); setReload((current) => current + 1) }}>Retry loading templates</Button>
      )}

      <Alert tone="info" variant="plain">
        <Alert.Icon />
        <Alert.Description>
          Create a new GitLab project from your application ZIP, or reuse an existing repository and build. Your platform’s credentials and deployment approval rules stay in control.
        </Alert.Description>
      </Alert>
    </div>
  )
}
