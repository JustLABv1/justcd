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
import { Disclosure } from "@/components/ui/collapsible"
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

export function DeploymentSchedulePanel({ scope, id, canEdit }: { scope: "workspaces" | "applications"; id: string; canEdit: boolean }) {
  const path = `/api/v1/${scope}/${encodeURIComponent(id)}/deployment-schedule`
  const toast = useToast()
  const [data, setData] = useState<DeploymentSchedule | null>(null)
  const [policy, setPolicy] = useState<DeploymentPolicy>({ windows: [], freezes: [] })
  const [error, setError] = useState<unknown | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    let active = true
    const load = (initial: boolean) => api<DeploymentSchedule>(path).then((value) => {
      if (active) { setData(value); setError(null); if (initial) setPolicy({ windows: value.policy.windows ?? [], freezes: value.policy.freezes ?? [] }) }
    }).catch((cause) => { if (active) setError(cause) })
    void load(true)
    const timer = window.setInterval(() => void load(false), 30_000)
    return () => { active = false; window.clearInterval(timer) }
  }, [path])
  const editable = canEdit && !data?.managedByGit
  const disabled = busy || !editable
  async function save(event: React.FormEvent) {
    event.preventDefault(); setBusy(true); setError(null)
    try { const value = await api<DeploymentSchedule>(path, { method: "PUT", body: JSON.stringify(policy) }); setData(value); toast.success("Deployment schedule saved.") }
    catch (cause) { setError(cause) }
    finally { setBusy(false) }
  }
  return <Panel title="Deployment schedule" description="Planning and approval remain available. These restrictions control when an apply can start.">
    <div className="space-y-4 p-5">
      {error !== null && <div role="alert" className="flex items-center gap-2 text-sm text-destructive">{errorMessage(error)}<ErrorDetailsButton error={error} /></div>}
      {!data ? <p role="status" className="text-sm text-muted-foreground">Loading deployment schedule…</p> : <>
        <div role="status" aria-live="polite" className="space-y-1 text-sm">
          <p className="font-medium">{data.decision.allowed ? "Applies are allowed now" : "Applies are waiting"}</p>
          {data.decision.reasons.map((reason, index) => <p key={`${index}-${reason}`} className="text-muted-foreground">{reason}</p>)}
          {!data.decision.allowed && <p className="text-muted-foreground">{data.decision.nextAllowedAt ? `Next allowed start: ${new Date(data.decision.nextAllowedAt).toLocaleString()} (${Intl.DateTimeFormat().resolvedOptions().timeZone})` : "No allowed start found in the next weekly cycle. Review windows and freezes."}</p>}
        </div>
        {data.managedByGit && <p className="text-sm text-muted-foreground">Managed in Git. Edit <code>spec.deploymentSchedule</code> in <code>justcd.yaml</code>.</p>}
        <Disclosure summary={editable ? "Configure windows and freezes" : "View windows and freezes"}>
          <form className="space-y-5" onSubmit={(event) => void save(event)}>
            <div className="space-y-3">
              <p className="text-sm font-medium">Maintenance windows</p>
              <p className="text-sm text-muted-foreground">Any window in this scope can allow an apply. Workspace and application restrictions both apply. No windows means unrestricted hours.</p>
              {policy.windows.map((value, index) => <section key={index} aria-label={`Maintenance window ${index + 1}`} className="space-y-3 rounded-lg border p-4">
                <WindowEditor window={value} disabled={disabled} onChange={(next) => setPolicy({ ...policy, windows: policy.windows.map((item, i) => i === index ? next : item) })} />
                {editable && <Button type="button" variant="destructive" size="sm" disabled={busy} onClick={() => setPolicy({ ...policy, windows: policy.windows.filter((_, i) => i !== index) })}>Remove window {index + 1}</Button>}
              </section>)}
              {editable && <Button type="button" variant="outline" size="sm" disabled={busy || policy.windows.length >= 16} onClick={() => setPolicy({ ...policy, windows: [...policy.windows, newWindow()] })}>Add maintenance window</Button>}
            </div>
            <div className="space-y-3">
              <p className="text-sm font-medium">Deployment freezes</p>
              <p className="text-sm text-muted-foreground">A freeze blocks applies even inside a maintenance window.</p>
              {policy.freezes.map((freeze, index) => <section key={index} aria-label={`Deployment freeze ${index + 1}`} className="space-y-3 rounded-lg border p-4">
                <FreezeEditor freeze={freeze} disabled={disabled} onChange={(next) => setPolicy({ ...policy, freezes: policy.freezes.map((item, i) => i === index ? next : item) })} />
                {editable && <Button type="button" variant="destructive" size="sm" disabled={busy} onClick={() => setPolicy({ ...policy, freezes: policy.freezes.filter((_, i) => i !== index) })}>Remove freeze {index + 1}</Button>}
              </section>)}
              {editable && <Button type="button" variant="outline" size="sm" disabled={busy || policy.freezes.length >= 16} onClick={() => setPolicy({ ...policy, freezes: [...policy.freezes, { reason: "" }] })}>Add deployment freeze</Button>}
            </div>
            {editable && <div className="flex justify-end"><Button type="submit" loading={busy} loadingText="Saving…">Save deployment schedule</Button></div>}
          </form>
        </Disclosure>
      </>}
    </div>
  </Panel>
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
