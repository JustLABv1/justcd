"use client"

import { useState } from "react"
import { ConnectionDialog } from "@/components/connection-dialog"
import { DialogCancel, DialogFooter } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { FormField } from "@/components/ui-kit"
import { api, errorMessage } from "@/lib/api"
import { useToast } from "@/components/toast-provider"
import type { Application } from "@/lib/types"

type LabelRow = { id: number; key: string; value: string }

export function ApplicationLabelsDialog({ application, onClose, onSaved }: {
  application: Application
  onClose: () => void
  onSaved: (labels: Record<string, string>) => void
}) {
  const [rows, setRows] = useState<LabelRow[]>(() => Object.entries(application.labels ?? {}).map(([key, value], id) => ({ id, key, value })))
  const [nextId, setNextId] = useState(rows.length)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState("")
  const toast = useToast()
  const managed = Boolean(application.repositoryConfigurationId || application.branchTest)
  async function save(event: React.FormEvent) {
    event.preventDefault()
    const keys = rows.map(row => row.key.trim())
    if (keys.some(key => !key) || new Set(keys).size !== keys.length) { setError("Label keys must be nonempty and unique."); return }
    setSaving(true); setError("")
    try {
      const labels = Object.fromEntries(rows.map(row => [row.key.trim(), row.value.trim()]))
      const updated = await api<Application>(`/api/v1/applications/${encodeURIComponent(application.id)}/labels`, { method: "PUT", body: JSON.stringify({ labels }) })
      onSaved(updated.labels ?? {}); toast.success("Application labels saved."); onClose()
    } catch (cause) { setError(errorMessage(cause)) }
    finally { setSaving(false) }
  }
  return <ConnectionDialog open onOpenChange={open => { if (!open) onClose() }} title={`Labels for ${application.name}`} description={managed ? "Labels are managed in Git. Edit metadata.labels in the application’s justcd.yaml." : "Use labels to organize and group applications. Labels do not change Kubernetes resources."} busy={saving} size="md">
    <form onSubmit={save} className="space-y-4">
      {managed && <pre className="overflow-x-auto rounded-lg border bg-muted/30 p-3 text-xs">{`metadata:\n  labels:\n${Object.entries(Object.keys(application.labels ?? {}).length ? application.labels! : { team: "platform", environment: "production" }).map(([key, value]) => `    ${key}: ${JSON.stringify(value)}`).join("\n")}`}</pre>}
      {!managed && <>
        {rows.length === 0 && <p className="text-sm text-muted-foreground">No labels yet. Add a label such as team or environment.</p>}
        {rows.map((row, index) => <div key={row.id} className="flex flex-wrap items-end gap-2">
          <div className="min-w-0 flex-1"><FormField label="Key" htmlFor={`label-key-${row.id}`}><Input id={`label-key-${row.id}`} aria-label={`Label ${index + 1} key`} value={row.key} maxLength={317} placeholder="team" disabled={saving} onChange={event => { const key = event.target.value; setRows(current => current.map(item => item.id === row.id ? { ...item, key } : item)) }} /></FormField></div>
          <div className="min-w-0 flex-1"><FormField label="Value" htmlFor={`label-value-${row.id}`}><Input id={`label-value-${row.id}`} aria-label={`Label ${index + 1} value`} value={row.value} maxLength={63} placeholder="platform" disabled={saving} onChange={event => { const value = event.target.value; setRows(current => current.map(item => item.id === row.id ? { ...item, value } : item)) }} /></FormField></div>
          <Button type="button" variant="ghost" size="sm" aria-label={`Remove label ${index + 1}`} disabled={saving} onClick={() => setRows(current => current.filter(item => item.id !== row.id))}>Remove</Button>
        </div>)}
        <Button type="button" variant="outline" size="sm" disabled={saving || rows.length >= 32} onClick={() => { setRows(current => [...current, { id: nextId, key: "", value: "" }]); setNextId(current => current + 1) }}>Add label</Button>
        <p className="text-xs text-muted-foreground">Up to 32 labels. Keys may include a DNS prefix, such as example.com/team. Values can be empty.</p>
      </>}
      {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
      <DialogFooter><DialogCancel disabled={saving}>{managed ? "Close" : "Cancel"}</DialogCancel>{!managed && <Button type="submit" loading={saving}>Save labels</Button>}</DialogFooter>
    </form>
  </ConnectionDialog>
}
