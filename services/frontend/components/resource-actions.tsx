"use client"

import { useState } from "react"
import { ConfirmDisclosure } from "@/components/confirm-disclosure"
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
      toast.success("Resource action completed. Reconciliation is paused.")
    } finally {
      try { await onComplete() } finally { setBusy(false) }
    }
  }
  const description = `JustCD will pause reconciliation before changing ${identity.namespace}/${identity.name}. It stays paused until an owner resumes it. Resuming may overwrite this change with the Git configuration.`
  return <section aria-label="Resource actions" className="space-y-3 border-t pt-4"><h4 className="text-xs font-semibold">Resource actions</h4><p className="text-sm leading-5 text-muted-foreground">Actions pause reconciliation for this application until you resume it.</p><div className="flex flex-wrap gap-2">
    {scalable && <ConfirmDisclosure trigger="Rescale" title={`Rescale ${identity.name}`} description={description} confirmLabel="Pause sync and rescale" triggerVariant="outline" confirmVariant="default" disabled={disabled || busy} confirmDisabled={!/^\d+$/.test(replicas) || Number(replicas) > 10000} onConfirm={() => run("rescale")}><FormField label="Replicas" htmlFor={`replicas-${resource.uid}`}><Input id={`replicas-${resource.uid}`} type="number" min={0} max={10000} value={replicas} onChange={(event) => setReplicas(event.target.value)} /></FormField></ConfirmDisclosure>}
    {restartable && <ConfirmDisclosure trigger="Redeploy" title={`Redeploy ${identity.name}`} description={`This restarts the workload's pods using its current configuration. ${description}`} confirmLabel="Pause sync and redeploy" triggerVariant="outline" confirmVariant="default" disabled={disabled || busy} onConfirm={() => run("redeploy")} />}
    <ConfirmDisclosure trigger="Delete" title={`Delete ${identity.name}?`} description={`This deletes the live Kubernetes resource and may interrupt service or remove data. ${description}`} confirmLabel="Pause sync and delete" disabled={disabled || busy} onConfirm={() => run("delete")} />
  </div></section>
}
