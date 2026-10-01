"use client"

import { useCallback, useEffect, useState } from "react"
import type { FormEvent } from "react"
import { ConfirmDisclosure } from "@/components/confirm-disclosure"
import { RepositoryPRSettingsControl } from "@/components/repository-pr-settings"
import { ConnectionDialog } from "@/components/connection-dialog"
import { Button } from "@/components/ui/button"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { FormField } from "@/components/ui-kit"
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

  return (
    <section className="overflow-hidden rounded-xl border bg-card">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-5 py-4">
        <div>
          <h3 className="text-sm font-medium">Applications managed by Git</h3>
          <p className="mt-1 text-sm text-muted-foreground">
            Place a justcd.yaml beside each overlay or chart. JustCD discovers
            applications and keeps their configuration current.
          </p>
        </div>
        {workspace.role === "owner" && (
          <Button
            size="sm"
            variant="outline"
            disabled={!sources.length || Boolean(busy)}
            onClick={() => {
              setSourceId(sources[0]?.id ?? "")
              setOpen(true)
            }}
          >
            Manage applications from Git
          </Button>
        )}
      </div>
      {error && (
        <p role="alert" className="px-5 py-4 text-sm text-destructive">
          {error}
        </p>
      )}
      {loading ? (
        <p role="status" className="px-5 py-4 text-sm text-muted-foreground">
          Loading repository discovery…
        </p>
      ) : !items.length && !error ? (
        <p className="px-5 py-4 text-sm text-muted-foreground">
          No branches connected for application discovery yet.
        </p>
      ) : (
        <div className="divide-y">
          {items.map((item) => (
            <div
              key={item.id}
              className="flex flex-col gap-3 px-5 py-4 sm:flex-row sm:justify-between"
            >
              <div className="min-w-0">
                <p className="text-sm font-medium">
                  {sources.find((source) => source.id === item.sourceId)
                    ?.name ?? "Repository"}{" "}
                  <span className="font-mono text-muted-foreground">
                    · {item.revision}
                  </span>
                </p>
                <p className="mt-1 text-sm text-muted-foreground">
                  {item.enabled ? "Discovery enabled" : "Discovery paused"}
                  {item.lastCommit && ` · ${item.lastCommit.slice(0, 12)}`}
                  {item.lastCheckedAt &&
                    ` · Checked ${new Date(item.lastCheckedAt).toLocaleString()}`}
                </p>
                {item.lastError && (
                  <p
                    role="alert"
                    className="mt-2 text-xs break-words text-destructive"
                  >
                    {item.lastError}
                  </p>
                )}
              </div>
              {workspace.role === "owner" && (
                <div className="flex shrink-0 flex-wrap gap-2">
                  <RepositoryPRSettingsControl repository={item} onSaved={load} />
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={Boolean(busy) || !item.enabled}
                    loading={busy === item.id}
                    onClick={() => void update(item, "refresh")}
                  >
                    Discover now
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={Boolean(busy)}
                    onClick={() => void update(item, "toggle")}
                  >
                    {item.enabled ? "Pause discovery" : "Resume discovery"}
                  </Button>
                  <ConfirmDisclosure trigger="Remove" title="Remove repository discovery?" description="Remove this branch’s application discovery connection. Pause discovery and remove its managed applications first. The Git source and repository remain." confirmLabel="Remove discovery" disabled={Boolean(busy)} onConfirm={async () => { await apiDelete(`/api/v1/repository-configurations/${encodeURIComponent(item.id)}`); setItems((current) => current.filter((value) => value.id !== item.id)); toast.success("Repository discovery removed.") }} />
                </div>
              )}
            </div>
          ))}
        </div>
      )}
      <ConnectionDialog
        open={open}
        onOpenChange={setOpen}
        busy={Boolean(busy)}
        title="Manage applications from Git"
        description="Connect a branch to this workspace. Existing cluster and namespace connections define where its applications can deploy."
      >
        <form className="space-y-4" onSubmit={(event) => void connect(event)}>
          <FormField label="Repository" htmlFor="discovery-source">
            <FormSelect
              id="discovery-source"
              value={sourceId}
              onValueChange={setSourceId}
              items={sources.map((source) => ({
                value: source.id,
                label: source.name,
              }))}
            />
          </FormField>
          <FormField
            label="Tracked branch or revision"
            htmlFor="discovery-revision"
          >
            <Input
              id="discovery-revision"
              value={revision}
              onChange={(event) => setRevision(event.target.value)}
              required
              maxLength={256}
            />
          </FormField>
          <div className="rounded-lg border bg-muted/30 p-3">
            <p className="mb-2 text-xs font-medium">Example justcd.yaml</p>
            <pre className="overflow-x-auto text-xs leading-5">{example}</pre>
          </div>
          <p className="text-sm leading-5 text-muted-foreground">
            Paths are relative to this file. Use an existing cluster name or ID
            and a namespace bound to this workspace. For Helm, set renderer to
            helm and optionally source.valuesFiles. Definitions are checked
            every minute. Removing a definition stops automatic processing and
            keeps workloads in place.
          </p>
          <Button
            type="submit"
            size="sm"
            loading={busy === "connect"}
            loadingText="Discovering applications…"
            disabled={!sourceId || !revision.trim() || Boolean(busy)}
          >
            Connect and discover
          </Button>
        </form>
      </ConnectionDialog>
    </section>
  )
}
