"use client"

import { useCallback, useEffect, useState } from "react"
import type { FormEvent } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { Copy01Icon, Delete02Icon, GitBranchIcon, PauseIcon, PlayIcon, Settings02Icon, Tick02Icon } from "@hugeicons/core-free-icons"
import { RowActions } from "@/components/action-menu"
import { RepositoryPRSettingsDialog } from "@/components/repository-pr-settings"
import { Badge } from "@/components/reui/badge"
import { AppDialog, DialogCancel, DialogFooter } from "@/components/ui/dialog"
import { Skeleton } from "@/components/ui/skeleton"
import { Button } from "@/components/ui/button"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { ConnectionRow, FormField, Panel } from "@/components/ui-kit"
import { useToast } from "@/components/toast-provider"
import { api, apiDelete, apiPost, errorMessage } from "@/lib/api"
import type {
  GitSource,
  ListResponse,
  RepositoryConfiguration,
  Workspace,
} from "@/lib/types"

const example = `apiVersion: justcd.io/v1alpha1
kind: Application
metadata:
  name: shop-production
spec:
  source:
    renderer: kustomize
    path: .
  destination:
    cluster: production
    namespace: shop
  syncPolicy: manual`

function ExampleYaml() {
  const [copied, setCopied] = useState(false)
  const toast = useToast()
  useEffect(() => {
    if (!copied) return
    const timer = setTimeout(() => setCopied(false), 2000)
    return () => clearTimeout(timer)
  }, [copied])
  return <div className="rounded-lg border bg-muted/30 p-3">
    <div className="mb-2 flex items-center justify-between gap-3">
      <p className="text-sm font-medium">Example justcd.yaml</p>
      <Button type="button" size="xs" variant="outline" onClick={() => { navigator.clipboard.writeText(example).then(() => setCopied(true), () => toast.error("Could not copy to the clipboard.")) }}>
        <HugeiconsIcon icon={copied ? Tick02Icon : Copy01Icon} strokeWidth={1.8} aria-hidden="true" />{copied ? "Copied" : "Copy"}
      </Button>
    </div>
    <pre className="overflow-x-auto text-xs leading-5">{example}</pre>
    <span role="status" aria-live="polite" className="sr-only">{copied ? "Example copied to clipboard" : ""}</span>
  </div>
}

export function RepositoryConfigurations({
  workspace,
  sources,
}: {
  workspace: Workspace
  sources: GitSource[]
}) {
  const toast = useToast()
  const [items, setItems] = useState<RepositoryConfiguration[]>([])
  const [error, setError] = useState("")
  const [loading, setLoading] = useState(true)
  const [open, setOpen] = useState(false)
  const [sourceId, setSourceId] = useState("")
  const [revision, setRevision] = useState("main")
  const [busy, setBusy] = useState("")
  const [settingsFor, setSettingsFor] = useState<RepositoryConfiguration | null>(null)
  const load = useCallback(async () => {
    try {
      const result = await api<ListResponse<RepositoryConfiguration>>(
        `/api/v1/repository-configurations?workspaceId=${encodeURIComponent(workspace.id)}`
      )
      setItems(result.items)
      setError("")
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setLoading(false)
    }
  }, [workspace.id])
  useEffect(() => {
    let active = true
    const refresh = () =>
      api<ListResponse<RepositoryConfiguration>>(
        `/api/v1/repository-configurations?workspaceId=${encodeURIComponent(workspace.id)}`
      )
        .then((result) => {
          if (active) {
            setItems(result.items)
            setError("")
          }
        })
        .catch((cause) => {
          if (active) setError(errorMessage(cause))
        })
        .finally(() => {
          if (active) setLoading(false)
        })
    void refresh()
    const timer = setInterval(() => void refresh(), 60_000)
    return () => {
      active = false
      clearInterval(timer)
    }
  }, [workspace.id])

  async function connect(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy("connect")
    try {
      const result = await apiPost<RepositoryConfiguration>(
        "/api/v1/repository-configurations",
        { workspaceId: workspace.id, sourceId, revision }
      )
      setOpen(false)
      if (result.lastError)
        toast.error(
          `Repository connected; discovery needs attention: ${result.lastError}`
        )
      else
        toast.success(
          "Repository connected. Application definitions discovered."
        )
      await load()
    } catch (cause) {
      toast.error(errorMessage(cause), cause)
    } finally {
      setBusy("")
    }
  }

  async function update(
    item: RepositoryConfiguration,
    action: "refresh" | "toggle"
  ) {
    setBusy(item.id)
    try {
      if (action === "refresh")
        await apiPost(
          `/api/v1/repository-configurations/${encodeURIComponent(item.id)}/reconcile`,
          {}
        )
      else
        await api(
          `/api/v1/repository-configurations/${encodeURIComponent(item.id)}`,
          { method: "PUT", body: JSON.stringify({ enabled: !item.enabled }) }
        )
      toast.success(
        action === "refresh"
          ? "Application definitions refreshed."
          : item.enabled
            ? "Repository discovery paused."
            : "Repository discovery resumed."
      )
    } catch (cause) {
      toast.error(errorMessage(cause), cause)
    } finally {
      await load()
      setBusy("")
    }
  }

  const owner = workspace.role === "owner"
  const nameOf = (item: RepositoryConfiguration) => `${sources.find((source) => source.id === item.sourceId)?.name ?? "Repository"} · ${item.revision}`
  return (
    <Panel
      title="Applications managed by Git"
      description="Place a justcd.yaml beside each overlay or chart. JustCD discovers applications and keeps their configuration current."
      action={owner ? <Button size="sm" variant="outline" disabled={!sources.length || Boolean(busy)} onClick={() => { setSourceId(sources[0]?.id ?? ""); setOpen(true) }}>Connect repository branch</Button> : undefined}
    >
      {error && (
        <div role="alert" className="flex flex-wrap items-center justify-between gap-3 px-5 py-4 text-sm text-destructive">
          <span>{error}</span>
          <Button type="button" size="sm" variant="outline" onClick={() => { setLoading(true); void load() }}>Retry</Button>
        </div>
      )}
      {loading ? (
        <div role="status" aria-label="Loading repository discovery" className="space-y-3 p-5"><Skeleton className="h-12 rounded-lg" /><Skeleton className="h-12 rounded-lg" /></div>
      ) : !items.length && !error ? (
        <p className="px-5 py-4 text-sm text-muted-foreground">
          No branches connected for application discovery yet.{" "}
          {owner ? (sources.length ? "Use “Connect repository branch” to start." : "Connect a Git source first.") : "Ask a workspace owner to connect one."}
        </p>
      ) : (
        <ul className="divide-y">
          {items.map((item) => (
            <ConnectionRow
              key={item.id}
              icon={<HugeiconsIcon icon={GitBranchIcon} strokeWidth={1.8} className="size-4" />}
              title={nameOf(item)}
              badges={<>
                <Badge size="sm" radius="full" variant={item.enabled ? "success-light" : "outline"}>{item.enabled ? "Discovery on" : "Discovery paused"}</Badge>
                {item.prSettings?.enabled && <Badge size="sm" radius="full" variant="info-light">Pull requests on</Badge>}
              </>}
              meta={<>
                {item.lastCommit && <span className="font-mono">{item.lastCommit.slice(0, 12)}</span>}
                {item.lastCheckedAt && <>{item.lastCommit ? " · " : ""}Checked {new Date(item.lastCheckedAt).toLocaleString()}</>}
                {item.lastError && <p role="alert" className="mt-1 break-words text-sm text-destructive">{item.lastError}</p>}
              </>}
              actions={owner ? <RowActions
                label={nameOf(item)}
                primary={<Button size="sm" variant="outline" disabled={Boolean(busy) || !item.enabled} loading={busy === item.id} onClick={() => void update(item, "refresh")}>Discover now</Button>}
                items={[
                  { label: "PR discovery settings", icon: Settings02Icon, onSelect: () => setSettingsFor(item) },
                  { label: item.enabled ? "Pause discovery" : "Resume discovery", icon: item.enabled ? PauseIcon : PlayIcon, disabled: Boolean(busy), onSelect: () => void update(item, "toggle") },
                  { label: "Remove", icon: Delete02Icon, destructive: true, disabled: Boolean(busy), confirm: {
                    title: `Remove ${nameOf(item)}?`,
                    description: "Remove this branch’s application discovery connection. Pause discovery and remove its managed applications first. The Git source and repository remain.",
                    confirmLabel: "Remove from discovery",
                    onConfirm: async () => { await apiDelete(`/api/v1/repository-configurations/${encodeURIComponent(item.id)}`); setItems((current) => current.filter((value) => value.id !== item.id)); toast.success("Repository discovery removed.") },
                  } },
                ]}
              /> : undefined}
            />
          ))}
        </ul>
      )}
      {settingsFor && <RepositoryPRSettingsDialog repository={items.find((item) => item.id === settingsFor.id) ?? settingsFor} open onOpenChange={(next) => { if (!next) setSettingsFor(null) }} onSaved={load} />}
      <AppDialog
        open={open}
        onOpenChange={setOpen}
        busy={Boolean(busy)}
        title="Connect repository branch"
        description="Connect a branch to this workspace. Existing cluster and namespace access define where its applications can deploy."
      >
        <form className="space-y-4" onSubmit={(event) => void connect(event)}>
          <FormField label="Repository" htmlFor="discovery-source">
            <FormSelect
              id="discovery-source"
              value={sourceId}
              onValueChange={setSourceId}
              items={sources.map((source) => ({ value: source.id, label: source.name }))}
            />
          </FormField>
          <FormField label="Tracked branch or revision" htmlFor="discovery-revision">
            <Input id="discovery-revision" value={revision} onChange={(event) => setRevision(event.target.value)} required maxLength={256} />
          </FormField>
          <ExampleYaml />
          <p className="text-sm leading-5 text-muted-foreground">
            Paths are relative to this file. Use an existing cluster name or ID and a namespace this workspace has access to. For Helm, set renderer to helm and optionally source.valuesFiles. Definitions are checked every minute. Removing a definition stops automatic processing and keeps workloads in place.
          </p>
          <DialogFooter>
            <DialogCancel disabled={Boolean(busy)} />
            <Button type="submit" loading={busy === "connect"} loadingText="Discovering applications…" disabled={!sourceId || !revision.trim() || Boolean(busy)}>Connect and discover</Button>
          </DialogFooter>
        </form>
      </AppDialog>
    </Panel>
  )
}
