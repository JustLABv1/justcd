"use client"

import { useEffect, useId, useState } from "react"
import { api, apiPost, errorMessage } from "@/lib/api"
import type { DeploymentFreeze, DeploymentPolicy, DeploymentSchedule, DeploymentWindow, Operation } from "@/lib/types"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { FormSelect } from "@/components/ui/form-select"
import { AppDialog, DialogCancel, DialogFooter } from "@/components/ui/dialog"
import { Badge } from "@/components/reui/badge"
import { SettingsGroup, SettingsRow } from "@/components/settings/settings-section"
import { FormField, Panel } from "@/components/ui-kit"
import { ErrorDetailsButton } from "@/components/error-details"
import { useToast } from "@/components/toast-provider"

const weekdays = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"]
const newWindow = (): DeploymentWindow => ({ timeZone: "UTC", days: [1, 2, 3, 4, 5], start: "09:00", end: "17:00" })

function WindowEditor({ window: value, onChange, disabled }: { window: DeploymentWindow; onChange: (value: DeploymentWindow) => void; disabled: boolean }) {
  const id = useId()
  return <div className="space-y-3">
    <div className="grid gap-3 sm:grid-cols-3">
      <FormField label="Time zone" htmlFor={`${id}-zone`} hint="IANA name, e.g. Europe/Berlin"><Input id={`${id}-zone`} required value={value.timeZone} disabled={disabled} onChange={(event) => onChange({ ...value, timeZone: event.target.value })} /></FormField>
      <FormField label="Start time" htmlFor={`${id}-start`}><Input id={`${id}-start`} type="time" required value={value.start} disabled={disabled} onChange={(event) => onChange({ ...value, start: event.target.value })} /></FormField>
      <FormField label="End time" htmlFor={`${id}-end`}><Input id={`${id}-end`} type="time" required value={value.end} disabled={disabled} onChange={(event) => onChange({ ...value, end: event.target.value })} /></FormField>
    </div>
    <fieldset disabled={disabled} className="space-y-2">
      <legend className="text-sm font-medium">Start days</legend>
      <div className="flex flex-wrap gap-x-4 gap-y-3">{weekdays.map((label, day) => <label key={day} className="flex items-center gap-2 text-sm"><Checkbox disabled={disabled} checked={value.days.includes(day)} onCheckedChange={(checked) => onChange({ ...value, days: checked ? [...value.days, day].sort() : value.days.filter((item) => item !== day) })} />{label}</label>)}</div>
    </fieldset>
    <p className="text-xs text-muted-foreground">An end before the start continues into the next day. Equal times cover the entire selected day.</p>
  </div>
}

function FreezeEditor({ freeze, onChange, disabled }: { freeze: DeploymentFreeze; onChange: (value: DeploymentFreeze) => void; disabled: boolean }) {
  const id = useId()
  const mode = freeze.schedule ? "recurring" : freeze.start || freeze.end ? "fixed" : "indefinite"
  const timestamp = (value?: string) => value ? new Date(value).toISOString().slice(0, 16) : ""
  return <div className="space-y-3">
    <div className="grid gap-3 sm:grid-cols-2">
      <FormField label="Freeze reason" htmlFor={`${id}-reason`}><Input id={`${id}-reason`} required maxLength={1000} value={freeze.reason} disabled={disabled} onChange={(event) => onChange({ ...freeze, reason: event.target.value })} /></FormField>
      <FormField label="Freeze type" htmlFor={`${id}-mode`}><FormSelect id={`${id}-mode`} value={mode} disabled={disabled} items={[{ value: "indefinite", label: "Until removed" }, { value: "fixed", label: "Date range (UTC)" }, { value: "recurring", label: "Weekly recurring" }]} onValueChange={(next) => onChange(next === "recurring" ? { reason: freeze.reason, schedule: newWindow() } : next === "fixed" ? { reason: freeze.reason, start: new Date().toISOString() } : { reason: freeze.reason })} /></FormField>
    </div>
    {freeze.schedule ? <WindowEditor window={freeze.schedule} disabled={disabled} onChange={(schedule) => onChange({ reason: freeze.reason, schedule })} /> : mode === "fixed" && <div className="grid gap-3 sm:grid-cols-2">
      <FormField label="Start (UTC)" htmlFor={`${id}-from`} hint="Empty means immediately"><Input id={`${id}-from`} type="datetime-local" value={timestamp(freeze.start)} disabled={disabled} onChange={(event) => onChange({ ...freeze, start: event.target.value ? new Date(`${event.target.value}Z`).toISOString() : undefined })} /></FormField>
      <FormField label="End (UTC)" htmlFor={`${id}-until`} hint="Empty means until removed"><Input id={`${id}-until`} type="datetime-local" value={timestamp(freeze.end)} disabled={disabled} onChange={(event) => onChange({ ...freeze, end: event.target.value ? new Date(`${event.target.value}Z`).toISOString() : undefined })} /></FormField>
    </div>}
  </div>
}

function normalizePolicy(policy: DeploymentPolicy): DeploymentPolicy {
  return { windows: policy.windows ?? [], freezes: policy.freezes ?? [] }
}

function windowSummary(value: DeploymentWindow) {
  const days = [...value.days].sort((a, b) => a - b)
  const dayLabel = days.length === 7 ? "Every day" : days.join(",") === "1,2,3,4,5" ? "Mon–Fri" : days.map((day) => weekdays[day]).join(", ")
  return `${dayLabel} · ${value.start === value.end ? "All day" : `${value.start}–${value.end}`} · ${value.timeZone}`
}

export function DeploymentSchedulePanel({ scope, id, canEdit, layout = "panel" }: { scope: "workspaces" | "applications"; id: string; canEdit: boolean; layout?: "panel" | "settings" }) {
  const path = `/api/v1/${scope}/${encodeURIComponent(id)}/deployment-schedule`
  const toast = useToast()
  const [data, setData] = useState<DeploymentSchedule | null>(null)
  const [policy, setPolicy] = useState<DeploymentPolicy>({ windows: [], freezes: [] })
  const [error, setError] = useState<unknown | null>(null)
  const [saveError, setSaveError] = useState<unknown | null>(null)
  const [busy, setBusy] = useState(false)
  const [editor, setEditor] = useState<"windows" | "freezes" | null>(null)
  useEffect(() => {
    let active = true
    const load = () => api<DeploymentSchedule>(path).then((value) => {
      if (active) { setData({ ...value, policy: normalizePolicy(value.policy) }); setError(null) }
    }).catch((cause) => { if (active) setError(cause) })
    void load()
    const timer = window.setInterval(() => void load(), 30_000)
    return () => { active = false; window.clearInterval(timer) }
  }, [path])
  const editable = canEdit && !data?.managedByGit
  const disabled = busy || !editable
  function openEditor(kind: "windows" | "freezes") {
    if (!data) return
    setPolicy(normalizePolicy(data.policy)); setSaveError(null); setEditor(kind)
  }
  async function save(event: React.FormEvent) {
    event.preventDefault(); setBusy(true); setSaveError(null)
    try {
      const value = await api<DeploymentSchedule>(path, { method: "PUT", body: JSON.stringify(policy) })
      setData({ ...value, policy: normalizePolicy(value.policy) }); setError(null); setEditor(null)
      toast.success("Deployment schedule saved.")
    } catch (cause) { setSaveError(cause) }
    finally { setBusy(false) }
  }
  const windows = data?.policy.windows ?? []
  const freezes = data?.policy.freezes ?? []
  const content = <>
    {error !== null && <div role="alert" className="mb-3 flex items-center gap-2 text-sm text-destructive">{errorMessage(error)}<ErrorDetailsButton error={error} /></div>}
    <SettingsGroup>
      {!data ? <SettingsRow label={error === null ? "Loading deployment schedule…" : "Deployment schedule unavailable"} /> : <>
        <SettingsRow label="Current availability" description={<div role="status" aria-live="polite" className="space-y-1">
          {data.decision.allowed ? <p>{windows.length === 0 && freezes.length === 0 ? (scope === "workspaces" ? "No workspace time restrictions are configured." : "Workspace and application schedules allow deployments now.") : "The current time is within the allowed deployment schedule."}</p> : <>
            {data.decision.reasons.map((reason, index) => <p key={`${index}-${reason}`}>{reason}</p>)}
            <p>{data.decision.nextAllowedAt ? `Next allowed start: ${new Date(data.decision.nextAllowedAt).toLocaleString()} (${Intl.DateTimeFormat().resolvedOptions().timeZone})` : "No allowed start in the next weekly cycle."}</p>
          </>}
        </div>} value={<Badge variant={data.decision.allowed ? "success-light" : "warning-light"} radius="full"><span aria-hidden="true" className={`size-1.5 shrink-0 rounded-full ${data.decision.allowed ? "bg-success" : "bg-warning"}`} />{data.decision.allowed ? "Deployments allowed" : "Deployments restricted"}</Badge>} />
        <SettingsRow label="Maintenance windows" description={windows.length ? windows.slice(0, 2).map(windowSummary).join("; ") + (windows.length > 2 ? `; +${windows.length - 2} more` : "") : scope === "workspaces" ? "Deploy at any time, unless a freeze is active." : "No application windows. Workspace restrictions still apply."}
          value={<span className="whitespace-nowrap text-muted-foreground">{windows.length ? `${windows.length} window${windows.length === 1 ? "" : "s"}` : "Any time"}</span>}
          action={<Button variant="outline" size="sm" onClick={() => openEditor("windows")}>{editable ? "Edit windows" : "View windows"}</Button>} />
        <SettingsRow label="Deployment freezes" description={freezes.length ? freezes.slice(0, 2).map((freeze) => freeze.reason).join("; ") + (freezes.length > 2 ? `; +${freezes.length - 2} more` : "") : "No freezes configured for this " + (scope === "workspaces" ? "workspace." : "application.")}
          value={<span className="whitespace-nowrap text-muted-foreground">{freezes.length ? `${freezes.length} freeze${freezes.length === 1 ? "" : "s"}` : "None"}</span>}
          action={<Button variant="outline" size="sm" onClick={() => openEditor("freezes")}>{editable ? "Edit freezes" : "View freezes"}</Button>} />
      </>}
    </SettingsGroup>
    {data?.managedByGit && <p className="mt-3 text-sm text-muted-foreground">Managed in Git. Edit <code>spec.deploymentSchedule</code> in <code>justcd.yaml</code>.</p>}

    <AppDialog open={editor !== null} onOpenChange={(open) => { if (!open) setEditor(null) }} busy={busy} variant={editable ? "form" : "info"} size="lg"
      title={editor === "freezes" ? "Deployment freezes" : "Maintenance windows"}
      description={editor === "freezes" ? "Pause deployments during incidents or scheduled maintenance. Every freeze needs a reason." : "Choose the days, times, and time zones when deployments may start. No windows means unrestricted hours."}>
      <form className="space-y-5" onSubmit={(event) => { if (editable) void save(event); else event.preventDefault() }}>
        {editor === "windows" ? <div className="space-y-4">
          {policy.windows.length === 0 && <div className="rounded-lg bg-muted/50 p-4 text-sm"><p className="font-medium">No maintenance windows</p><p className="mt-1 text-muted-foreground">Deployments can start at any time, subject to active freezes{scope === "applications" ? " and workspace restrictions" : ""}.</p></div>}
          {policy.windows.map((value, index) => <section key={index} aria-label={`Maintenance window ${index + 1}`} className="space-y-3 rounded-lg border p-4">
            <p className="text-sm font-semibold">Window {index + 1}</p>
            <WindowEditor window={value} disabled={disabled} onChange={(next) => setPolicy({ ...policy, windows: policy.windows.map((item, i) => i === index ? next : item) })} />
            {editable && <Button type="button" variant="destructive" size="sm" disabled={busy} onClick={() => setPolicy({ ...policy, windows: policy.windows.filter((_, i) => i !== index) })}>Remove window {index + 1}</Button>}
          </section>)}
          {editable && <Button type="button" variant="outline" size="sm" disabled={busy || policy.windows.length >= 16} onClick={() => setPolicy({ ...policy, windows: [...policy.windows, newWindow()] })}>Add maintenance window</Button>}
        </div> : <div className="space-y-4">
          {policy.freezes.length === 0 && <div className="rounded-lg bg-muted/50 p-4 text-sm"><p className="font-medium">No deployment freezes</p><p className="mt-1 text-muted-foreground">Add a freeze to block deployments until it is removed, during a date range, or on a weekly schedule.</p></div>}
          {policy.freezes.map((freeze, index) => <section key={index} aria-label={`Deployment freeze ${index + 1}`} className="space-y-3 rounded-lg border p-4">
            <p className="text-sm font-semibold">Freeze {index + 1}</p>
            <FreezeEditor freeze={freeze} disabled={disabled} onChange={(next) => setPolicy({ ...policy, freezes: policy.freezes.map((item, i) => i === index ? next : item) })} />
            {editable && <Button type="button" variant="destructive" size="sm" disabled={busy} onClick={() => setPolicy({ ...policy, freezes: policy.freezes.filter((_, i) => i !== index) })}>Remove freeze {index + 1}</Button>}
          </section>)}
          {editable && <Button type="button" variant="outline" size="sm" disabled={busy || policy.freezes.length >= 16} onClick={() => setPolicy({ ...policy, freezes: [...policy.freezes, { reason: "" }] })}>Add deployment freeze</Button>}
        </div>}
        {saveError !== null && <div role="alert" className="flex items-center gap-2 text-sm text-destructive">{errorMessage(saveError)}<ErrorDetailsButton error={saveError} /></div>}
        {editable && <DialogFooter><DialogCancel disabled={busy} /><Button type="submit" loading={busy} loadingText="Saving…">{editor === "windows" ? "Save windows" : "Save freezes"}</Button></DialogFooter>}
      </form>
    </AppDialog>
  </>
  return layout === "settings" ? content : <Panel surface="flat" title="Deployment schedule" description="When deployments can start, and any freezes that take precedence.">{content}</Panel>
}

export function ScheduledOperationActions({ operation, canOverride, canCancel, onUpdated }: { operation: Operation; canOverride: boolean; canCancel: boolean; onUpdated: () => Promise<void> }) {
  const [open, setOpen] = useState(false)
  const [reason, setReason] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown | null>(null)
  const id = useId()
  const toast = useToast()
  async function override(event: React.FormEvent) {
    event.preventDefault(); setBusy(true); setError(null)
    try {
      await apiPost(`/api/v1/operations/${encodeURIComponent(operation.id)}/schedule-override`, { reason })
      setOpen(false); setReason(""); toast.success("Emergency override recorded. The queued plan will be revalidated before applying."); await onUpdated()
    } catch (cause) { setError(cause) } finally { setBusy(false) }
  }
  async function cancel() {
    setBusy(true); setError(null)
    try { await apiPost(`/api/v1/operations/${encodeURIComponent(operation.id)}/cancel`, {}); toast.success("Queued apply cancelled. Create a fresh plan to deploy."); await onUpdated() }
    catch (cause) { setError(cause) } finally { setBusy(false) }
  }
  if (operation.status !== "queued") return null
  return <div className="space-y-2 text-sm">
    {operation.waitingReasons?.map((value, index) => <p key={`${index}-${value}`} className="text-muted-foreground">{value}</p>)}
    {operation.nextAllowedAt && <p className="text-muted-foreground">Next allowed start: {new Date(operation.nextAllowedAt).toLocaleString()} ({Intl.DateTimeFormat().resolvedOptions().timeZone})</p>}
    {operation.scheduleOverrideReason && <p className="text-muted-foreground">Emergency override: {operation.scheduleOverrideReason}</p>}
    <div className="flex flex-wrap gap-2">
      {canCancel && <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => void cancel()}>Cancel queued apply</Button>}
      {canOverride && !operation.scheduleOverrideReason && Boolean(operation.waitingReasons?.length) && <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => { setError(null); setOpen(true) }}>Emergency override</Button>}
    </div>
    {error !== null && !open && <p role="alert" className="text-destructive">{errorMessage(error)}</p>}
    <AppDialog open={open} onOpenChange={setOpen} title="Emergency schedule override" description="Allow this queued apply to bypass workspace and application windows and freezes. The reason and your identity are recorded in the audit log. Plan and approval checks still apply." busy={busy} size="md">
      <form className="space-y-4" onSubmit={(event) => void override(event)}>
        <FormField label="Required reason" htmlFor={id}><Textarea id={id} required maxLength={1000} value={reason} disabled={busy} onChange={(event) => setReason(event.target.value)} /></FormField>
        {error !== null && <p role="alert" className="text-destructive">{errorMessage(error)}</p>}
        <DialogFooter><DialogCancel disabled={busy} /><Button type="submit" disabled={!reason.trim()} loading={busy} loadingText="Recording…">Record emergency override</Button></DialogFooter>
      </form>
    </AppDialog>
  </div>
}
