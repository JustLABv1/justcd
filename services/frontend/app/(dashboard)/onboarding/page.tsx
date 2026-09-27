"use client"

import Link from "next/link"
import { useCallback, useEffect, useMemo, useState } from "react"
import { ErrorDetailsButton } from "@/components/error-details"
import { Button } from "@/components/ui/button"
import { PageHeading } from "@/components/ui-kit"
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
    } catch (cause) {
      toast.error(errorMessage(cause), cause)
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <PageHeading
        title="Get started with JustCD"
        description="Follow a safe, resumable path from installation to a healthy first application. You can leave this page at any time and revisit completed steps."
        actions={<Button variant="outline" onClick={() => void load()} disabled={busy}>Refresh checks</Button>}
      />
      {error && <div role="alert" className="mb-5 flex items-center justify-between gap-3 rounded-xl border border-destructive/20 bg-destructive/5 p-4 text-sm text-destructive"><span>{errorMessage(error)}</span><ErrorDetailsButton error={error} /></div>}
      {!status && !error ? <div role="status" className="h-44 animate-pulse rounded-2xl border bg-muted/40" /> : status && <div className="grid items-start gap-8 xl:grid-cols-[minmax(0,1fr)_320px]">
        <div className="space-y-4">
          <section className="rounded-2xl border bg-card p-5 sm:p-6">
            <div className="flex flex-wrap items-end justify-between gap-3">
              <div><p className="text-xs font-medium text-muted-foreground">Setup progress</p><p className="mt-1 text-2xl font-semibold tracking-tight">{status.completed} of {status.total} ready</p></div>
              <span className="text-xs text-muted-foreground">{Math.round((status.completed / status.total) * 100)}%</span>
            </div>
            <div className="mt-4 h-2 overflow-hidden rounded-full bg-muted"><div className="h-full rounded-full bg-primary transition-[width]" style={{ width: `${(status.completed / status.total) * 100}%` }} /></div>
            <p className="mt-4 text-sm">{nextStep ? <>Next required step: <strong>{nextStep.title}</strong></> : "Setup complete. Your readiness checks remain available here."}</p>
          </section>
          <ol className="space-y-3">
            {status.steps.map((step, index) => <StepCard key={step.id} step={step} index={index} isNext={step.id === status.nextStepId} busy={busy} acknowledgeEncryptionKey={acknowledgeEncryptionKey} />)}
          </ol>
        </div>
        <aside className="space-y-4 xl:sticky xl:top-8">
          <section className="rounded-2xl border bg-card p-5">
            <h2 className="text-sm font-semibold">Cluster connection workflow</h2>
            <p className="mt-2 text-xs leading-5 text-muted-foreground">JustCD keeps credential values masked and backend-only. The permission check uses Kubernetes discovery and SelfSubjectAccessReview; it never creates, changes, or deletes workloads.</p>
            <ol className="mt-4 space-y-3 text-xs">
              <WorkflowItem number="1" title="Add credentials" detail="Save a workspace token or kubeconfig." />
              <WorkflowItem number="2" title="Add cluster details" detail="Provide the HTTPS API URL and CA." />
              <WorkflowItem number="3" title="Bind a namespace" detail="Limit where this workspace can deploy." />
              <WorkflowItem number="4" title="Verify access" detail="Classify connectivity and RBAC issues." />
            </ol>
            <Link href={status.firstWorkspaceId ? `/workspaces/${status.firstWorkspaceId}/clusters/new` : "/workspaces/new"} className="mt-5 inline-flex text-xs font-medium text-primary hover:underline">Start cluster connection →</Link>
          </section>
          <section className="rounded-2xl border bg-muted/30 p-5 text-xs leading-5 text-muted-foreground"><strong className="text-foreground">Experienced administrator?</strong><p className="mt-1">The checklist is guidance, not a gate. All regular navigation and settings remain available.</p></section>
        </aside>
      </div>}
    </>
  )
}

function StepCard({ step, index, isNext, busy, acknowledgeEncryptionKey }: { step: OnboardingStep; index: number; isNext: boolean; busy: boolean; acknowledgeEncryptionKey: () => Promise<void> }) {
  const complete = step.status === "complete"
  const failed = step.status === "failed"
  return <li className={`rounded-2xl border bg-card p-5 ${isNext ? "border-primary/40 ring-2 ring-primary/10" : ""}`}>
    <div className="flex items-start gap-4">
      <span className={`grid size-8 shrink-0 place-items-center rounded-full text-xs font-semibold ${complete ? "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300" : failed ? "bg-rose-500/10 text-rose-700 dark:text-rose-300" : "bg-muted text-muted-foreground"}`}>{complete ? "✓" : index + 1}</span>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2"><h2 className="text-sm font-semibold">{step.title}</h2>{isNext && <span className="rounded-full bg-primary/10 px-2 py-0.5 text-[10px] font-medium text-primary">Next</span>}{step.category && !complete && <span className="rounded-full border px-2 py-0.5 text-[10px] text-muted-foreground">{categoryLabels[step.category] ?? step.category}</span>}</div>
        <p className="mt-1.5 text-xs leading-5 text-muted-foreground">{step.summary}</p>
        {step.remediation && !complete && <p className="mt-2 rounded-lg bg-muted/50 px-3 py-2 text-xs leading-5"><strong>How to fix:</strong> {step.remediation}</p>}
        <div className="mt-3 flex flex-wrap items-center gap-3">
          {step.id === "encryption-key" && !complete && <Button size="sm" loading={busy} loadingText="Saving…" onClick={() => void acknowledgeEncryptionKey()}>I backed up the key</Button>}
          {step.href && <Link href={step.href} className="text-xs font-medium text-primary hover:underline">{complete ? "Review settings" : "Continue setup"} →</Link>}
          {step.docsHref && <a href={step.docsHref} target="_blank" rel="noreferrer" className="text-xs text-muted-foreground hover:text-foreground hover:underline">Documentation ↗</a>}
        </div>
      </div>
    </div>
  </li>
}

function WorkflowItem({ number, title, detail }: { number: string; title: string; detail: string }) {
  return <li className="flex gap-3"><span className="grid size-6 shrink-0 place-items-center rounded-full border bg-background font-medium">{number}</span><span><strong className="block text-foreground">{title}</strong><span className="text-muted-foreground">{detail}</span></span></li>
}
