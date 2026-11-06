"use client"

import Link from "next/link"
import { useCallback, useEffect, useMemo, useState } from "react"
import { ErrorDetailsButton } from "@/components/error-details"
import { Badge } from "@/components/reui/badge"
import { Button } from "@/components/ui/button"
import { Disclosure } from "@/components/ui/collapsible"
import { ConfirmDisclosure } from "@/components/confirm-disclosure"
import { OnboardingClusterStep } from "@/components/onboarding-cluster-step"
import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowRight01Icon, ArrowUpRight01Icon, Cancel01Icon, Tick02Icon } from "@hugeicons/core-free-icons"
import { PageHeading } from "@/components/ui-kit"
import { useToast } from "@/components/toast-provider"
import { useWorkspaceSelection } from "@/hooks/workspace-selection"
import { api, errorMessage } from "@/lib/api"
import type { OnboardingStatus, OnboardingStep } from "@/lib/types"

const stepGoals: Record<string, string> = {
  workspace: "A workspace groups your clusters, repositories and applications, and controls who can see them.",
  cluster: "JustCD needs a path into your cluster. The agent is the simplest: it connects outbound, so nothing has to be exposed.",
  git: "Connect the repository that holds your manifests, Helm chart or Kustomize overlay.",
  application: "Pair a Git source with a cluster and namespace. JustCD plans the change, you review it, then it deploys.",
}

export default function OnboardingPage() {
  const toast = useToast()
  const { workspaceId, workspace } = useWorkspaceSelection()
  const [status, setStatus] = useState<OnboardingStatus | null>(null)
  const [error, setError] = useState<unknown | null>(null)
  const [busy, setBusy] = useState(false)
  const [picked, setPicked] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      const next = await api<OnboardingStatus>(workspaceId ? `/api/v1/onboarding?workspaceId=${encodeURIComponent(workspaceId)}` : "/api/v1/onboarding")
      setStatus(next); setError(null)
      window.dispatchEvent(new Event("justcd:onboarding-updated"))
    } catch (cause) {
      setError(cause)
    }
  }, [workspaceId])

  useEffect(() => { void Promise.resolve().then(load) }, [load])

  // Setup happens elsewhere (a terminal, another tab), so keep the page truthful while it is open.
  useEffect(() => {
    const timer = window.setInterval(() => { if (document.visibilityState === "visible") void load() }, 5000)
    return () => window.clearInterval(timer)
  }, [load])

  const activeId = picked ?? status?.nextStepId ?? status?.steps[status.steps.length - 1]?.id
  const active = useMemo(() => status?.steps.find((step) => step.id === activeId), [status, activeId])

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
        title="Get started"
        description={workspace ? <>Set up <strong className="font-medium text-foreground">{workspace.name}</strong> step by step. Progress is detected automatically.</> : "Set up your workspace step by step. Progress is detected automatically."}
      />
      {error != null && <div role="alert" className="mb-5 flex items-center justify-between gap-3 rounded-xl border border-destructive/20 bg-destructive/5 p-4 text-sm text-destructive"><span>{errorMessage(error)}</span><ErrorDetailsButton error={error} /></div>}
      {!status && error == null ? <div role="status" aria-label="Loading" className="h-64 animate-pulse rounded-xl border bg-muted/40" /> : status && <div className="space-y-6">
        <div className="grid items-start gap-6 lg:grid-cols-[280px_minmax(0,1fr)]">
          <nav aria-label="Setup steps" className="rounded-xl border bg-card p-2 lg:sticky lg:top-8">
            <p className="px-3 pt-2 pb-1 text-xs font-medium text-muted-foreground">{status.completed} of {status.total} done</p>
            <div role="progressbar" aria-label="Setup progress" aria-valuemin={0} aria-valuemax={status.total} aria-valuenow={status.completed} className="mx-3 mb-2 h-1.5 overflow-hidden rounded-full bg-muted"><div className="h-full rounded-full bg-primary transition-all" style={{ width: `${(status.completed / status.total) * 100}%` }} /></div>
            <ol>
              {status.steps.map((step, index) => <li key={step.id}><StepTab step={step} index={index} selected={step.id === activeId} next={step.id === status.nextStepId} onSelect={() => setPicked(step.id)} /></li>)}
            </ol>
          </nav>
          {active && <StepPanel key={active.id} step={active} status={status} workspaceId={workspaceId || status.workspaceId || ""} onChanged={() => void load()} />}
        </div>
        <Disclosure className="rounded-xl border bg-card px-4 py-3" summary={<span className="flex items-center gap-2 text-sm font-medium">Instance readiness{status.systemAttention > 0 ? <Badge size="xs" radius="full" variant="warning-light">{status.systemAttention} to review</Badge> : <Badge size="xs" radius="full" variant="success-light">All good</Badge>}</span>}>
          <p className="mt-3 text-sm text-muted-foreground">Checks for whoever operates this JustCD installation. They apply to the whole instance and never block the steps above.</p>
          <ul className="mt-3 divide-y">
            {status.system.map((step) => <li key={step.id} className="flex flex-wrap items-start justify-between gap-3 py-3">
              <div className="flex min-w-0 items-start gap-3"><StepMark complete={step.status === "complete"} failed={step.status === "failed"} /><div className="min-w-0"><p className="text-sm font-medium">{step.title}</p><p className="text-sm text-muted-foreground">{step.summary}</p>{step.remediation && step.status !== "complete" && <p className="mt-1 text-sm"><strong>How to fix:</strong> {step.remediation}</p>}</div></div>
              <div className="flex items-center gap-2">
                {step.id === "encryption-key" && step.status !== "complete" && <ConfirmDisclosure trigger="I backed up the key" triggerVariant="default" confirmVariant="default" disabled={busy} title="Confirm encryption key backup" description="Confirm that the instance encryption key is stored durably and backed up outside this server." confirmLabel="Confirm backup" onConfirm={acknowledgeEncryptionKey} />}
                {step.href && <Button render={<Link href={step.href} />} nativeButton={false} variant="ghost" size="sm">Open</Button>}
                {step.docsHref && <Button render={<a href={step.docsHref} target="_blank" rel="noreferrer" />} nativeButton={false} variant="ghost" size="sm" className="text-muted-foreground">Docs<HugeiconsIcon icon={ArrowUpRight01Icon} strokeWidth={1.8} data-icon="inline-end" aria-hidden="true" /></Button>}
              </div>
            </li>)}
          </ul>
        </Disclosure>
      </div>}
    </>
  )
}

function StepMark({ complete, failed }: { complete: boolean; failed: boolean }) {
  return <span className={`grid size-6 shrink-0 place-items-center rounded-full ${complete ? "bg-success/10 text-success-foreground dark:text-success" : failed ? "bg-destructive/10 text-destructive" : "bg-muted text-muted-foreground"}`}>
    {complete ? <HugeiconsIcon icon={Tick02Icon} strokeWidth={2} className="size-3.5" aria-hidden="true" /> : failed ? <HugeiconsIcon icon={Cancel01Icon} strokeWidth={2} className="size-3.5" aria-hidden="true" /> : <span aria-hidden="true" className="size-1.5 rounded-full bg-current" />}
    <span className="sr-only">{complete ? "Complete" : failed ? "Needs attention" : "To do"}</span>
  </span>
}

function StepTab({ step, index, selected, next, onSelect }: { step: OnboardingStep; index: number; selected: boolean; next: boolean; onSelect: () => void }) {
  const complete = step.status === "complete"
  return <button type="button" aria-current={selected ? "step" : undefined} onClick={onSelect} className={`flex w-full items-start gap-3 rounded-lg px-3 py-2.5 text-left transition-colors focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none ${selected ? "bg-muted" : "hover:bg-muted/50"}`}>
    <StepMark complete={complete} failed={step.status === "failed"} />
    <span className="min-w-0"><span className="flex items-center gap-2 text-sm font-medium"><span className="text-muted-foreground">{index + 1}.</span>{step.title}{next && !complete && <Badge size="xs" radius="full" variant="primary-light">Next</Badge>}</span></span>
  </button>
}

function StepPanel({ step, status, workspaceId, onChanged }: { step: OnboardingStep; status: OnboardingStatus; workspaceId: string; onChanged: () => void }) {
  const complete = step.status === "complete"
  return <section aria-label={step.title} className="min-w-0 space-y-5 rounded-xl border bg-card p-5 sm:p-6">
    <header className="space-y-1.5">
      <div className="flex flex-wrap items-center gap-2"><h2 className="text-lg font-semibold tracking-tight">{step.title}</h2>{complete && <Badge size="xs" radius="full" variant="success-light">Done</Badge>}{step.status === "failed" && <Badge size="xs" radius="full" variant="destructive-light">Needs attention</Badge>}</div>
      <p className="text-sm leading-5 text-muted-foreground">{stepGoals[step.id] ?? step.summary}</p>
    </header>
    {step.id === "cluster" && workspaceId ? <OnboardingClusterStep workspaceId={workspaceId} clusters={status.clusters} onChanged={onChanged} /> : <div className="space-y-3">
      <p className="text-sm">{step.summary}</p>
      {step.remediation && !complete && <p className="rounded-lg bg-muted/50 px-3 py-2 text-sm leading-5"><strong>How to fix:</strong> {step.remediation}</p>}
    </div>}
    <footer className="flex flex-wrap items-center gap-3 border-t pt-4">
      {step.id !== "cluster" && step.href && <Button render={<Link href={step.href} />} nativeButton={false} variant={complete ? "outline" : "default"}>{complete ? "Review" : actionLabel(step.id)}<HugeiconsIcon icon={ArrowRight01Icon} strokeWidth={1.8} data-icon="inline-end" aria-hidden="true" /></Button>}
      {step.docsHref && <Button render={<a href={step.docsHref} target="_blank" rel="noreferrer" />} nativeButton={false} variant="ghost" size="sm" className="text-muted-foreground">Documentation<HugeiconsIcon icon={ArrowUpRight01Icon} strokeWidth={1.8} data-icon="inline-end" aria-hidden="true" /></Button>}
    </footer>
  </section>
}

function actionLabel(id: string) {
  return id === "workspace" ? "Create a workspace" : id === "git" ? "Connect a Git source" : id === "application" ? "Create an application" : "Continue"
}
