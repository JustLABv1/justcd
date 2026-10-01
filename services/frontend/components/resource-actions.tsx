"use client"

import { useState } from "react"
import { ArrowExpand01Icon, Delete02Icon, RefreshIcon } from "@hugeicons/core-free-icons"
import { ActionMenu, type ActionItem } from "@/components/action-menu"
import { FormField } from "@/components/ui-kit"
import { Input } from "@/components/ui/input"
import { apiPost } from "@/lib/api"
import { useToast } from "@/components/toast-provider"
import type { ManagedResource } from "@/lib/types"

export function ResourceActions({ applicationID, resource, disabled, onComplete }: { applicationID: string; resource: ManagedResource; disabled: boolean; onComplete: () => Promise<void> }) {
  const [replicas, setReplicas] = useState("1")
  const [busy, setBusy] = useState(false)
  const toast = useToast()
  const identity = resource.identity
  const scalable = identity.apiVersion === "apps/v1" && ["Deployment", "StatefulSet"].includes(identity.kind)
  const restartable = scalable || (identity.apiVersion === "apps/v1" && identity.kind === "DaemonSet")
  async function run(action: string) {
    setBusy(true)
    try {
      await apiPost(`/api/v1/applications/${encodeURIComponent(applicationID)}/resource-actions`, { identity, uid: resource.uid, action, ...(action === "rescale" ? { replicas: Number(replicas) } : {}) })
      toast.success("Resource action completed. Auto-sync is paused.")
    } finally {
      try { await onComplete() } finally { setBusy(false) }
    }
  }
  const description = `JustCD will pause auto-sync before changing ${identity.namespace}/${identity.name}. It stays paused until an owner resumes it. Resuming may overwrite this change with the Git configuration.`
  const items: ActionItem[] = [
    ...(scalable ? [{ label: "Rescale…", icon: ArrowExpand01Icon, disabled: disabled || busy, confirm: { title: `Rescale ${identity.name}`, description, confirmLabel: "Pause auto-sync and rescale", confirmDisabled: !/^\d+$/.test(replicas) || Number(replicas) > 10000, onConfirm: () => run("rescale"), children: <FormField label="Replicas" htmlFor={`replicas-${resource.uid}`}><Input id={`replicas-${resource.uid}`} type="number" min={0} max={10000} value={replicas} onChange={(event) => setReplicas(event.target.value)} /></FormField> } }] : []),
    ...(restartable ? [{ label: "Redeploy…", icon: RefreshIcon, disabled: disabled || busy, confirm: { title: `Redeploy ${identity.name}`, description: `This restarts the workload's pods using its current configuration. ${description}`, confirmLabel: "Pause auto-sync and redeploy", onConfirm: () => run("redeploy") } }] : []),
    { label: "Delete…", icon: Delete02Icon, destructive: true, disabled: disabled || busy, confirm: { title: `Delete ${identity.name}?`, description: `This deletes the live Kubernetes resource and may interrupt service or remove data. ${description}`, confirmLabel: `Delete ${identity.kind} ${identity.name}`, onConfirm: () => run("delete") } },
  ]
  return <section aria-label="Resource actions" className="space-y-3 border-t pt-4"><h4 className="text-sm font-semibold">Resource actions</h4><p className="text-sm leading-5 text-muted-foreground">Actions pause auto-sync for this application until you resume it.</p><ActionMenu label="Actions" size="sm" items={items} /></section>
}
