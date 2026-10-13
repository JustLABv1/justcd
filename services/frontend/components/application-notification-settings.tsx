"use client"
import { useEffect, useState } from "react"
import Link from "next/link"
import {
  SettingsGroup,
  SettingsRow,
} from "@/components/settings/settings-section"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { FormSelect } from "@/components/ui/form-select"
import { AppDialog, DialogCancel, DialogFooter } from "@/components/ui/dialog"
import { FormField, Panel } from "@/components/ui-kit"
import {
  notificationEvents,
  type Destination,
} from "@/components/notification-settings"
import { api, errorMessage } from "@/lib/api"
type Policy = {
  mode: string
  destinationIds: string[]
  events: string[]
  minimumSeverity: string
}
type Response = { policy: Policy; destinations: Destination[] }
export function ApplicationNotificationSettings({
  applicationId,
  workspaceId,
}: {
  applicationId: string
  workspaceId: string
}) {
  const base = `/api/v1/applications/${encodeURIComponent(applicationId)}/notifications`
  const [data, setData] = useState<Response | null>(null)
  const [draft, setDraft] = useState<Policy>({
    mode: "inherit",
    destinationIds: [],
    events: notificationEvents,
    minimumSeverity: "info",
  })
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const [retry, setRetry] = useState(0)
  useEffect(() => {
    let active = true
    api<Response>(base)
      .then((response) => {
        if (active) {
          setData(response)
          setError("")
        }
      })
      .catch((e) => {
        if (active) setError(errorMessage(e))
      })
    return () => {
      active = false
    }
  }, [base, retry])
  function edit() {
    if (!data) return
    setDraft({
      ...data.policy,
      destinationIds: data.policy.destinationIds.filter((id) =>
        data.destinations.some((d) => d.id === id)
      ),
      events: data.policy.events.length
        ? data.policy.events
        : notificationEvents,
    })
    setError("")
    setOpen(true)
  }
  async function save(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError("")
    try {
      const response = await api<Response>(base, {
        method: "PUT",
        body: JSON.stringify(draft),
      })
      setData(response)
      setOpen(false)
      setNotice("Application notification routing saved.")
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }
  const summary = !data
    ? "Loading…"
    : data.policy.mode === "inherit"
      ? "Workspace defaults"
      : data.policy.mode === "disabled"
        ? "Disabled for this application"
        : "Custom routing"
  const missing =
    data?.policy.destinationIds.filter(
      (id) => !data.destinations.some((d) => d.id === id)
    ) ?? []
  return (
    <>
      <Panel
        title="Notifications"
        description="Set this application's destinations, events and severity independently of workspace defaults."
      >
        <div className="space-y-3 p-5">
          <SettingsGroup>
            <SettingsRow
              label="Routing"
              description={
                data?.policy.mode === "custom"
                  ? `${data.policy.events.map((e) => e.replaceAll("_", " ")).join(", ")} · Minimum ${data.policy.minimumSeverity}`
                  : "Inherit workspace defaults, choose custom routing, or turn notifications off."
              }
              value={summary}
              action={
                <Button
                  variant="outline"
                  size="sm"
                  onClick={edit}
                  disabled={busy || !data}
                >
                  Edit routing
                </Button>
              }
            />
            {data?.policy.mode === "custom" && (
              <SettingsRow
                label="Selected destinations"
                description={
                  data.destinations
                    .filter((d) => data.policy.destinationIds.includes(d.id))
                    .map((d) => `${d.name}${d.enabled ? "" : " (disabled)"}`)
                    .join(", ") || "No available destinations"
                }
              />
            )}
            <SettingsRow
              label="Workspace destinations"
              description="Connections are managed by workspace owners."
              action={
                <Button
                  variant="outline"
                  size="sm"
                  render={
                    <Link href={`/workspaces/${workspaceId}?tab=settings`} />
                  }
                  nativeButton={false}
                >
                  Manage destinations
                </Button>
              }
            />
          </SettingsGroup>
          {missing.length > 0 && (
            <p role="status" className="text-sm text-muted-foreground">
              A selected destination was deleted. Edit routing to choose its
              replacement.
            </p>
          )}
          {error && !open && (
            <div role="alert" className="text-sm text-destructive">
              {error}
              <Button variant="ghost" onClick={() => setRetry((v) => v + 1)}>
                Retry
              </Button>
            </div>
          )}
          {notice && (
            <p role="status" className="text-sm text-muted-foreground">
              {notice}
            </p>
          )}
        </div>
      </Panel>
      <AppDialog
        open={open}
        onOpenChange={setOpen}
        busy={busy}
        title="Application notification routing"
        description="Custom routing replaces destination event and severity defaults for this application. Workspace-disabled destinations stay off."
      >
        <form className="space-y-4 pt-4" onSubmit={save}>
          <FormField label="Routing mode" htmlFor="app-notification-mode">
            <FormSelect
              id="app-notification-mode"
              value={draft.mode}
              onValueChange={(mode) => setDraft({ ...draft, mode })}
              disabled={busy}
              items={[
                { value: "inherit", label: "Inherit workspace defaults" },
                { value: "custom", label: "Custom routing" },
                {
                  value: "disabled",
                  label: "Disable application notifications",
                },
              ]}
            />
          </FormField>
          {draft.mode === "custom" && (
            <>
              <fieldset className="space-y-2">
                <legend className="mb-2 text-sm font-medium">
                  Destinations
                </legend>
                {data?.destinations.map((d) => (
                  <label key={d.id} className="flex items-center gap-2 text-sm">
                    <Checkbox
                      disabled={busy}
                      checked={draft.destinationIds.includes(d.id)}
                      onCheckedChange={(v) =>
                        setDraft({
                          ...draft,
                          destinationIds: v
                            ? [...draft.destinationIds, d.id]
                            : draft.destinationIds.filter((id) => id !== d.id),
                        })
                      }
                    />
                    {d.name} · {d.provider}
                    {d.enabled ? "" : " (disabled at workspace)"}
                  </label>
                ))}
                {!data?.destinations.length && (
                  <p className="text-sm text-muted-foreground">
                    Add a workspace destination first.
                  </p>
                )}
              </fieldset>
              <fieldset className="space-y-2">
                <legend className="mb-2 text-sm font-medium">Events</legend>
                {notificationEvents.map((event) => (
                  <label
                    key={event}
                    className="flex items-center gap-2 text-sm"
                  >
                    <Checkbox
                      disabled={busy}
                      checked={draft.events.includes(event)}
                      onCheckedChange={(v) =>
                        setDraft({
                          ...draft,
                          events: v
                            ? [...draft.events, event]
                            : draft.events.filter((e) => e !== event),
                        })
                      }
                    />
                    {event.replaceAll("_", " ")}
                  </label>
                ))}
              </fieldset>
              <FormField
                label="Minimum severity"
                htmlFor="app-notification-severity"
              >
                <FormSelect
                  id="app-notification-severity"
                  disabled={busy}
                  value={draft.minimumSeverity}
                  onValueChange={(minimumSeverity) =>
                    setDraft({ ...draft, minimumSeverity })
                  }
                  items={["info", "warning", "error"].map((value) => ({
                    value,
                    label: value,
                  }))}
                />
              </FormField>
            </>
          )}
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <DialogFooter>
            <DialogCancel disabled={busy} />
            <Button
              type="submit"
              disabled={
                busy ||
                (draft.mode === "custom" &&
                  (!draft.events.length || !draft.destinationIds.length))
              }
            >
              {busy ? "Saving…" : "Save routing"}
            </Button>
          </DialogFooter>
        </form>
      </AppDialog>
    </>
  )
}
