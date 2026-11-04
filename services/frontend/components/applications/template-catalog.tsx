"use client"

import { useEffect, useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import {
  ArrowRight01Icon,
  CodeIcon,
  Globe02Icon,
  Search01Icon,
} from "@hugeicons/core-free-icons"
import { api } from "@/lib/api"
import { ErrorNotice } from "@/components/workspace-ui"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"

export type ApplicationTemplate = {
  id: string
  name: string
  description: string
  runtime: "shiny" | "streamlit" | "python-static" | "static"
  category: "R" | "Python" | "Geospatial" | "Websites"
  entryPoint: string
  detail: string
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
      ) : template.runtime === "streamlit" ? (
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
}: {
  onSelect: (template: ApplicationTemplate) => void
}) {
  const [templates, setTemplates] = useState<ApplicationTemplate[]>([])
  const [error, setError] = useState<unknown>(null)
  const [loading, setLoading] = useState(true)
  const [reload, setReload] = useState(0)
  useEffect(() => {
    let active = true
    api<{ items: ApplicationTemplate[] }>("/api/v1/publishing/templates")
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
  return (
    <div className="space-y-4 pb-5">
      <header className="space-y-2">
        <h1 className="text-2xl font-medium tracking-tight">
          Choose an app template
        </h1>
        <p className="max-w-3xl text-sm leading-6 text-muted-foreground">
          Your application, ready for your platform. Pick a template to set up
          its repository, build, and deployment.
        </p>
      </header>
      <div className="space-y-4">
        <div className="relative max-w-xl">
          <HugeiconsIcon
            icon={Search01Icon}
            aria-hidden="true"
            className="pointer-events-none absolute top-1/2 left-3.5 z-10 size-4 -translate-y-1/2 text-muted-foreground"
            strokeWidth={1.7}
          />
          <Input
            aria-label="Search templates"
            placeholder="Search templates…"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            className="h-10 rounded-xl border-border bg-card pr-4 pl-10 text-sm shadow-none focus-visible:ring-2 dark:border-transparent dark:bg-muted/50"
          />
        </div>
        <ToggleGroup
          aria-label="Template category"
          value={[category]}
          onValueChange={(next) => {
            if (next[0]) setCategory(next[0])
          }}
          className="max-w-full gap-2 overflow-x-auto rounded-none border-0 bg-transparent p-0"
        >
          {["All", "R", "Python", "Geospatial", "Websites"].map((value) => (
            <ToggleGroupItem
              key={value}
              value={value}
              className="h-8 rounded-lg px-3.5 text-sm font-medium"
            >
              {value}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </div>
      {error != null && <ErrorNotice error={error} />}
      {loading && (
        <p role="status" className="text-sm text-muted-foreground">
          Loading templates…
        </p>
      )}
      <div
        className="grid gap-4 md:grid-cols-2 lg:grid-cols-3"
        aria-label="Application templates"
      >
        {visible.map((template) => (
          <Button
            key={template.id}
            type="button"
            variant="ghost"
            onClick={() => onSelect(template)}
            aria-label={`Use ${template.name} template`}
            className="group h-auto min-h-32 w-full items-start justify-start gap-3.5 rounded-2xl border-border bg-card p-5 text-left whitespace-normal transition-colors duration-150 hover:bg-accent focus-visible:ring-2 dark:border-transparent dark:bg-muted/45 dark:hover:bg-muted/75"
          >
            <TemplateGlyph template={template} />
            <span className="min-w-0 flex-1 space-y-2">
              <span className="flex items-center justify-between gap-3">
                <span className="text-base font-medium tracking-tight">
                  {template.name}
                </span>
                <HugeiconsIcon
                  icon={ArrowRight01Icon}
                  className="size-4 text-muted-foreground transition-colors group-hover:text-foreground"
                  strokeWidth={1.8}
                />
              </span>
              <span className="line-clamp-2 text-[13px] leading-5 font-normal text-muted-foreground">
                {template.description}
              </span>
              <span className="block pt-0.5 text-xs font-normal text-muted-foreground">
                {template.detail}
              </span>
            </span>
          </Button>
        ))}
      </div>
      {!loading && !error && !visible.length && (
        <div
          role="status"
          className="rounded-3xl border border-border bg-card px-6 py-12 text-center dark:bg-muted/40"
        >
          <p className="font-medium">No templates found</p>
          <p className="mt-2 text-sm text-muted-foreground">
            Try another search or category.
          </p>
          <Button
            type="button"
            variant="outline"
            className="mt-5"
            onClick={() => {
              setSearch("")
              setCategory("All")
            }}
          >
            Clear filters
          </Button>
        </div>
      )}
      {error != null && (
        <Button
          type="button"
          variant="outline"
          onClick={() => {
            setLoading(true)
            setReload((current) => current + 1)
          }}
        >
          Retry loading templates
        </Button>
      )}
      <p className="text-sm leading-6 text-muted-foreground">
        Create a new GitLab project from your application ZIP, or reuse an
        existing repository and build. Your platform’s credentials and
        deployment approval rules stay in control.
      </p>
    </div>
  )
}
