"use client"
import { useCallback, useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { Checkbox } from "@/components/ui/checkbox"
import { Switch } from "@/components/ui/switch"
import { FormSelect } from "@/components/ui/form-select"
import { AppDialog, DialogCancel, DialogFooter } from "@/components/ui/dialog"
import { FormField, StatusBadge } from "@/components/ui-kit"
import { api, apiPost, apiDelete, errorMessage } from "@/lib/api"
import {
  SettingsSection,
  SettingsGroup,
  SettingsRow,
} from "@/components/settings/settings-section"
import { RowActions } from "@/components/action-menu"
export type Destination = {
  id: string
  name: string
  provider: string
  enabled: boolean
  events: string[]
  minimumSeverity: string
}
type Activity = {
  id: string
  eventType: string
  status: string
  attempts: number
  resultCode: string
  statusCode: number
  history: {
    attempt: number
    resultCode: string
    statusCode: number
    createdAt: string
  }[]
}
function destinationInput(d: Destination) {
  return {
    name: d.name,
    provider: d.provider,
    enabled: d.enabled,
    events: d.events,
    minimumSeverity: d.minimumSeverity,
  }
}
export const notificationEvents = [
  "drift",
  "approval_requested",
  "sync_failed",
  "unhealthy",
  "operation_completed",
  "rollback",
]
const initial = {
  id: "",
  name: "",
  provider: "slack",
  enabled: true,
  events: notificationEvents,
  minimumSeverity: "info",
}
export function NotificationSettings({ workspaceId }: { workspaceId: string }) {
  const base = `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/notifications`
  const [destinations, setDestinations] = useState<Destination[]>([])
  const [activity, setActivity] = useState<Activity[]>([])
  const [draft, setDraft] = useState<Destination>(initial)
  const [config, setConfig] = useState<Record<string, string>>({})
  const [open, setOpen] = useState(false)
  const [historyOpen, setHistoryOpen] = useState(false)
  const [replace, setReplace] = useState(false)
  const [busy, setBusy] = useState(false)
  const [loaded, setLoaded] = useState(false)
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const [deleting, setDeleting] = useState<Destination | null>(null)
  const reload = useCallback(async () => {
    const [d, a] = await Promise.all([
      api<{ items: Destination[] }>(base),
      api<{ items: Activity[] }>(`${base}/activity`),
    ])
    setDestinations(d.items)
    setActivity(a.items)
    setLoaded(true)
  }, [base])
  useEffect(() => {
    const refresh = () => {
      void reload().catch((e) => setError(errorMessage(e)))
    }
    refresh()
    const timer = window.setInterval(refresh, 5000)
    return () => window.clearInterval(timer)
  }, [reload])
  async function act(action: () => Promise<unknown>, message: string) {
    setBusy(true)
    setError("")
    setNotice("")
    try {
      await action()
      await reload()
      setNotice(message)
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }
  function edit(d: Destination) {
    setDraft(d)
    setConfig({})
    setReplace(!d.id)
    setError("")
    setOpen(true)
  }
  async function save(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError("")
    try {
      const settings = replace
        ? draft.provider === "telegram"
          ? {
              botToken: config.botToken,
              chatId: config.chatId,
              messageThreadId: Number(config.messageThreadId || 0),
            }
          : draft.provider === "email"
            ? {
                smtpHost: config.smtpHost,
                smtpPort: Number(config.smtpPort ?? 587),
                from: config.from,
                to: (config.to ?? "")
                  .split(",")
                  .map((s) => s.trim())
                  .filter(Boolean),
                username: config.username,
                password: config.password,
              }
            : {
                url: config.url,
                signingSecret: config.signingSecret,
                method: config.method ?? "POST",
                headers: JSON.parse(config.headers || "{}"),
                body: config.body,
              }
        : undefined
      await api(draft.id ? `${base}/${draft.id}` : base, {
        method: draft.id ? "PUT" : "POST",
        body: JSON.stringify({ ...destinationInput(draft), config: settings }),
      })
      setOpen(false)
      setConfig({})
      await reload()
      setNotice("Destination saved.")
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }
  function field(key: string, label: string, secret = false, required = false) {
    return (
      <FormField key={key} label={label} htmlFor={`notification-${key}`}>
        <Input
          id={`notification-${key}`}
          aria-label={label}
          type={secret ? "password" : "text"}
          autoComplete={secret ? "new-password" : "off"}
          value={config[key] ?? ""}
          required={required}
          disabled={busy}
          onChange={(e) => setConfig({ ...config, [key]: e.target.value })}
        />
      </FormField>
    )
  }
  return (
    <>
      <SettingsSection
        title="Notifications"
        description="Workspace defaults for Slack, Teams, Telegram, email and webhooks. Applications can override their routing, events and severity."
      >
        <div className="space-y-3">
          <SettingsGroup>
            <SettingsRow
              label="Destinations"
              description="Choose where workspace events are sent."
              value={loaded ? `${destinations.length} configured` : "Loading…"}
              action={
                <Button
                  variant="outline"
                  size="sm"
                  disabled={busy || !loaded}
                  onClick={() => edit(initial)}
                >
                  Add destination
                </Button>
              }
            />
            {loaded && !destinations.length && (
              <SettingsRow
                label="No destinations configured"
                description="Add a destination to start receiving events."
              />
            )}
            {destinations.map((d) => (
              <SettingsRow
                key={d.id}
                label={d.name}
                description={`${d.provider} · ${d.events.map((e) => e.replaceAll("_", " ")).join(", ")} · Minimum ${d.minimumSeverity}`}
                value={
                  <StatusBadge status={d.enabled ? "Enabled" : "Disabled"} />
                }
                action={
                  <RowActions
                    label={d.name}
                    primary={
                      <Button
                        variant="outline"
                        size="sm"
                        disabled={busy}
                        onClick={() => edit(d)}
                      >
                        Edit
                      </Button>
                    }
                    items={[
                      {
                        label: "Test delivery",
                        disabled: busy || !d.enabled,
                        onSelect: () =>
                          void act(
                            () => apiPost(`${base}/${d.id}/test`),
                            "Test queued. View delivery history for the result."
                          ),
                      },
                      {
                        label: d.enabled ? "Disable" : "Enable",
                        disabled: busy,
                        onSelect: () =>
                          void act(
                            () =>
                              api(`${base}/${d.id}`, {
                                method: "PUT",
                                body: JSON.stringify({
                                  ...destinationInput(d),
                                  enabled: !d.enabled,
                                }),
                              }),
                            d.enabled
                              ? "Destination disabled."
                              : "Destination enabled."
                          ),
                      },
                      {
                        label: "Delete",
                        destructive: true,
                        disabled: busy,
                        onSelect: () => setDeleting(d),
                      },
                    ]}
                  />
                }
              />
            ))}
            <SettingsRow
              label="Delivery history"
              description={
                activity.length
                  ? `${activity.length} recent deliveries. Inspect attempts and failures.`
                  : "No delivery activity yet."
              }
              action={
                <Button
                  variant="outline"
                  size="sm"
                  disabled={busy || !loaded}
                  onClick={() => setHistoryOpen(true)}
                >
                  View history
                </Button>
              }
            />
          </SettingsGroup>
          {error && !open && (
            <p role="alert" className="text-sm text-destructive">
              {error}
              <Button
                variant="ghost"
                disabled={busy}
                onClick={() => void act(reload, "Refreshed.")}
              >
                Retry
              </Button>
            </p>
          )}
          {notice && (
            <p role="status" className="text-sm text-muted-foreground">
              {notice}
            </p>
          )}
        </div>
      </SettingsSection>
      <AppDialog
        open={historyOpen}
        onOpenChange={setHistoryOpen}
        variant="info"
        title="Notification delivery history"
        description="Recent deliveries and their attempt history."
      >
        <div className="space-y-3 pt-4">
          <h3 className="text-sm font-semibold">Recent deliveries</h3>
          {!activity.length && (
            <p className="text-sm text-muted-foreground">
              No delivery activity yet.
            </p>
          )}
          {activity.map((a) => (
            <div key={a.id} className="space-y-2 rounded-lg border p-3 text-sm">
              <div className="flex flex-wrap items-center gap-2">
                <span>{a.eventType.replaceAll("_", " ")}</span>
                <StatusBadge status={a.status} />
                <span className="text-muted-foreground">
                  {a.attempts}/5 attempts · {a.resultCode || "Pending"}
                  {a.statusCode ? ` · HTTP ${a.statusCode}` : ""}
                </span>
              </div>
              <p className="text-xs break-all text-muted-foreground">
                Idempotency ID: {a.id}
              </p>
              {a.history.length > 0 && (
                <details>
                  <summary>Attempt history</summary>
                  <ol className="mt-2 space-y-1">
                    {a.history.map((h) => (
                      <li key={h.attempt}>
                        Attempt {h.attempt}: {h.resultCode} {h.statusCode || ""}{" "}
                        · {new Date(h.createdAt).toLocaleString()}
                      </li>
                    ))}
                  </ol>
                </details>
              )}
            </div>
          ))}
        </div>
      </AppDialog>
      <AppDialog
        open={open}
        onOpenChange={setOpen}
        busy={busy}
        title={draft.id ? "Edit destination" : "Add destination"}
        description="Credentials are encrypted and cannot be read back. Editing routing preserves saved credentials."
      >
        <form className="space-y-4 pt-4" onSubmit={save}>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <FormField label="Name" htmlFor="notification-name">
            <Input
              id="notification-name"
              aria-label="Destination name"
              value={draft.name}
              required
              maxLength={100}
              disabled={busy}
              onChange={(e) => setDraft({ ...draft, name: e.target.value })}
            />
          </FormField>
          <FormField label="Provider" htmlFor="notification-provider">
            <FormSelect
              id="notification-provider"
              ariaLabel="Provider"
              value={draft.provider}
              disabled={busy || (!!draft.id && !replace)}
              items={["slack", "teams", "telegram", "email", "webhook"].map(
                (value) => ({
                  value,
                  label: value,
                })
              )}
              onValueChange={(provider) => {
                setDraft({ ...draft, provider })
                setConfig({})
              }}
            />
          </FormField>
          <label className="flex items-center gap-2 text-sm">
            <Switch
              checked={draft.enabled}
              disabled={busy}
              onCheckedChange={(enabled) => setDraft({ ...draft, enabled })}
            />
            Enabled
          </label>
          <fieldset disabled={busy} className="space-y-2">
            <legend className="mb-2 text-sm font-medium">Events</legend>
            {notificationEvents.map((event) => (
              <label className="flex items-center gap-2 text-sm" key={event}>
                <Checkbox
                  disabled={busy}
                  checked={draft.events.includes(event)}
                  onCheckedChange={(checked) =>
                    setDraft({
                      ...draft,
                      events: checked
                        ? [...draft.events, event]
                        : draft.events.filter((t) => t !== event),
                    })
                  }
                />
                {event.replaceAll("_", " ")}
              </label>
            ))}
          </fieldset>
          <FormField label="Minimum severity" htmlFor="notification-severity">
            <FormSelect
              id="notification-severity"
              ariaLabel="Minimum severity"
              value={draft.minimumSeverity}
              disabled={busy}
              items={["info", "warning", "error"].map((value) => ({
                value,
                label: value,
              }))}
              onValueChange={(minimumSeverity) =>
                setDraft({ ...draft, minimumSeverity })
              }
            />
          </FormField>
          {draft.id && (
            <label className="flex items-center gap-2 text-sm">
              <Checkbox
                checked={replace}
                disabled={busy}
                onCheckedChange={(v) => setReplace(!!v)}
              />
              Replace connection configuration
            </label>
          )}
          {replace &&
            (draft.provider === "telegram" ? (
              <>
                {field("botToken", "Telegram bot token", true, true)}
                {field("chatId", "Chat ID or @channel", false, true)}
                {field("messageThreadId", "Topic ID (optional)")}
                <p className="text-sm text-muted-foreground">
                  Add your bot to the target chat and grant permission to send
                  messages. Use a topic ID for forum groups.
                </p>
              </>
            ) : draft.provider === "email" ? (
              <>
                {field("smtpHost", "SMTP host", false, true)}
                {field("smtpPort", "SMTP port (default 587)")}
                {field("from", "Sender email", false, true)}
                {field("to", "Recipient emails (comma separated)", false, true)}
                {field("username", "SMTP username")}
                {field("password", "SMTP password", true)}
                <p className="text-sm text-muted-foreground">
                  SMTP must support STARTTLS with a trusted certificate.
                </p>
              </>
            ) : (
              <>
                {field("url", "HTTPS endpoint", true, true)}
                {field(
                  "signingSecret",
                  "Signing secret (at least 32 characters)",
                  true,
                  true
                )}
                {draft.provider === "webhook" && (
                  <>
                    <FormField
                      label="HTTP method"
                      htmlFor="notification-method"
                    >
                      <FormSelect
                        id="notification-method"
                        ariaLabel="HTTP method"
                        value={config.method ?? "POST"}
                        disabled={busy}
                        items={["POST", "PUT", "PATCH"].map((value) => ({
                          value,
                          label: value,
                        }))}
                        onValueChange={(method) =>
                          setConfig({ ...config, method })
                        }
                      />
                    </FormField>
                    <FormField
                      label="Headers (JSON object)"
                      htmlFor="notification-headers"
                    >
                      <Textarea
                        id="notification-headers"
                        aria-label="Headers"
                        value={config.headers ?? ""}
                        disabled={busy}
                        onChange={(e) =>
                          setConfig({ ...config, headers: e.target.value })
                        }
                      />
                    </FormField>
                    <FormField
                      label="Body (JSON template, optional)"
                      htmlFor="notification-body"
                    >
                      <Textarea
                        id="notification-body"
                        aria-label="Body template"
                        value={config.body ?? ""}
                        disabled={busy}
                        onChange={(e) =>
                          setConfig({ ...config, body: e.target.value })
                        }
                      />
                    </FormField>
                    <p className="text-xs text-muted-foreground">
                      Use {"{{event}}"} for the full event or{" "}
                      {
                        "{{id}}, {{type}}, {{severity}}, {{url}}, {{workspaceId}}, {{applicationId}}, {{resourceId}}"
                      }{" "}
                      for JSON values, without surrounding quotes.
                    </p>
                  </>
                )}
              </>
            ))}
          <DialogFooter>
            <DialogCancel disabled={busy} />
            <Button type="submit" disabled={busy || !draft.events.length}>
              {busy ? "Saving…" : "Save destination"}
            </Button>
          </DialogFooter>
        </form>
      </AppDialog>
      <AppDialog
        open={!!deleting}
        onOpenChange={(v) => {
          if (!v) setDeleting(null)
        }}
        busy={busy}
        title="Delete destination"
        description="This removes the destination, queued deliveries and its delivery history."
      >
        <DialogFooter>
          <DialogCancel disabled={busy} />
          <Button
            variant="destructive"
            disabled={busy}
            onClick={() => {
              if (deleting)
                void act(async () => {
                  await apiDelete(`${base}/${deleting.id}`)
                  setDeleting(null)
                }, "Destination deleted.")
            }}
          >
            Delete destination
          </Button>
        </DialogFooter>
      </AppDialog>
    </>
  )
}
