"use client"

import Link from "next/link"
import { useCallback, useEffect, useMemo, useState } from "react"
import { ErrorDetailsButton } from "@/components/error-details"
import { Badge } from "@/components/reui/badge"
import { Button } from "@/components/ui/button"
import { ConfirmDisclosure } from "@/components/confirm-disclosure"
import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowRight01Icon, ArrowUpRight01Icon, Cancel01Icon, Tick02Icon } from "@hugeicons/core-free-icons"
import { PageHeading, Panel } from "@/components/ui-kit"
import { useToast } from "@/components/toast-provider"
import { api, errorMessage } from "@/lib/api"
import type { OnboardingStatus, OnboardingStep } from "@/lib/types"

const categoryLabels: Record<string, string> = {
  configuration: "Configuration",
  permission: "Permission",
  authorization: "Permission",
  connectivity: "Connectivity",
  service_health: "Service health",
}

export default function OnboardingPage() {
  const toast = useToast()
  const [status, setStatus] = useState<OnboardingStatus | null>(null)
  const [error, setError] = useState<unknown | null>(null)
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    setError(null)
    try {
      setStatus(await api<OnboardingStatus>("/api/v1/onboarding"))
    } catch (cause) {
      setError(cause)
    }
  }, [])

  useEffect(() => { void Promise.resolve().then(load) }, [load])

  const nextStep = useMemo(
    () => status?.steps.find((step) => step.id === status.nextStepId),
    [status]
  )

  async function acknowledgeEncryptionKey() {
    setBusy(true)
    try {
      await api("/api/v1/onboarding/steps/encryption-key/complete", { method: "PUT" })
      toast.success("Encryption-key persistence confirmed.")
      await load()
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <PageHeading
        title="Get started with JustCD"
        description="A resumable checklist from installation to your first healthy application."
        actions={<Button variant="outline" onClick={() => void load()} disabled={busy}>Refresh checks</Button>}
      />
      {error && <div role="alert" className="mb-5 flex items-center justify-between gap-3 rounded-xl border border-destructive/20 bg-destructive/5 p-4 text-sm text-destructive"><span>{errorMessage(error)}</span><ErrorDetailsButton error={error} /></div>}
      {!status && !error ? <div role="status" className="h-44 animate-pulse rounded-xl border bg-muted/40" /> : status && <div className="grid items-start gap-8 xl:grid-cols-[minmax(0,1fr)_320px]">
        <div className="space-y-4">
          <Panel title="Setup progress" className="overflow-hidden">
            <div className="p-5 sm:p-6">
              <div className="flex flex-wrap items-end justify-between gap-3">
                <p className="text-2xl font-semibold tracking-tight">{status.completed} of {status.total} ready</p>
                <span className="text-sm text-muted-foreground">{Math.round((status.completed / status.total) * 100)}%</span>
              </div>
              <div role="progressbar" aria-label="Setup progress" aria-valuemin={0} aria-valuemax={status.total} aria-valuenow={status.completed} className="mt-4 h-2 overflow-hidden rounded-full bg-muted"><div className="h-full rounded-full bg-primary transition-[width]" style={{ width: `${(status.completed / status.total) * 100}%` }} /></div>
              <p className="mt-4 text-sm">{nextStep ? <>Next required step: <strong>{nextStep.title}</strong></> : "Setup complete. Your readiness checks remain available here."}</p>
            </div>
          </Panel>
          <ol className="space-y-3">
            {status.steps.map((step, index) => <StepCard key={step.id} step={step} index={index} isNext={step.id === status.nextStepId} busy={busy} acknowledgeEncryptionKey={acknowledgeEncryptionKey} />)}
          </ol>
        </div>
        <aside className="space-y-4 xl:sticky xl:top-8">
          <Panel title="Connect a cluster" description="Credential values stay masked and backend-only. The access check only reads Kubernetes discovery and permission information; it never creates, changes, or deletes workloads." className="overflow-hidden">
            <div className="p-5">
              <Button render={<Link href={status.firstWorkspaceId ? `/workspaces/${status.firstWorkspaceId}/clusters/new` : "/workspaces/new"} />} nativeButton={false} variant="outline" size="sm">{status.firstWorkspaceId ? "Connect cluster" : "Create workspace"}<HugeiconsIcon icon={ArrowRight01Icon} strokeWidth={1.8} aria-hidden="true" /></Button>
            </div>
          </Panel>
          <aside className="rounded-xl border bg-muted/30 p-5 text-sm leading-5 text-muted-foreground"><strong className="text-foreground">Experienced administrator?</strong><p className="mt-1">The checklist is guidance, not a gate. All regular navigation and settings remain available.</p></aside>
        </aside>
      </div>}
    </>
  )
}

function StepCard({ step, index, isNext, busy, acknowledgeEncryptionKey }: { step: OnboardingStep; index: number; isNext: boolean; busy: boolean; acknowledgeEncryptionKey: () => Promise<void> }) {
  const complete = step.status === "complete"
  const failed = step.status === "failed"
  return <li className={`rounded-xl border bg-card p-5 ${isNext ? "border-primary/40 ring-2 ring-primary/10" : ""}`}>
    <div className="flex items-start gap-4">
      <StepNumber index={index} complete={complete} failed={failed} />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2"><h2 className="text-sm font-semibold">{step.title}</h2>{isNext && <Badge size="xs" radius="full" variant="primary-light">Next</Badge>}{step.category && !complete && <Badge size="xs" radius="full" variant="outline" className="text-muted-foreground">{categoryLabels[step.category] ?? step.category}</Badge>}</div>
        <p className="mt-1.5 text-sm leading-5 text-muted-foreground">{step.summary}</p>
        {step.remediation && !complete && <p className="mt-2 rounded-lg bg-muted/50 px-3 py-2 text-sm leading-5"><strong>How to fix:</strong> {step.remediation}</p>}
        <div className="mt-3 flex flex-wrap items-center gap-3">
          {step.id === "encryption-key" && !complete && <ConfirmDisclosure trigger="I backed up the key" triggerVariant="default" confirmVariant="default" disabled={busy} title="Confirm encryption key backup" description="Confirm that the instance encryption key is stored somewhere safe outside this server. Without it, saved credentials cannot be recovered. This step cannot be undone." confirmLabel="Confirm backup" onConfirm={acknowledgeEncryptionKey} />}
          {step.href && <Button render={<Link href={step.href} />} nativeButton={false} variant="link" size="sm" className="px-0">{complete ? "Review settings" : "Continue setup"}<HugeiconsIcon icon={ArrowRight01Icon} strokeWidth={1.8} aria-hidden="true" /></Button>}
          {step.docsHref && <Button render={<a href={step.docsHref} target="_blank" rel="noreferrer" />} nativeButton={false} variant="ghost" size="sm" className="text-muted-foreground">Documentation<HugeiconsIcon icon={ArrowUpRight01Icon} strokeWidth={1.8} aria-hidden="true" /><span className="sr-only"> (opens in a new tab)</span></Button>}
        </div>
      </div>
    </div>
  </li>
}

function StepNumber({ index, complete, failed }: { index: number; complete: boolean; failed: boolean }) {
  return <span className={`grid size-8 shrink-0 place-items-center rounded-full text-xs font-semibold ${complete ? "bg-success/10 text-success-foreground dark:text-success" : failed ? "bg-destructive/10 text-destructive" : "bg-muted text-muted-foreground"}`}>
    {complete ? <HugeiconsIcon icon={Tick02Icon} strokeWidth={2} className="size-4" aria-hidden="true" /> : failed ? <HugeiconsIcon icon={Cancel01Icon} strokeWidth={2} className="size-4" aria-hidden="true" /> : index + 1}
    <span className="sr-only">{complete ? "Step complete" : failed ? "Step failed" : `Step ${index + 1}`}</span>
  </span>
}
