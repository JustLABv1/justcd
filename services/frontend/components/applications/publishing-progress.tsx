"use client"
import { useEffect, useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowRight01Icon, ArrowUpRight01Icon } from "@hugeicons/core-free-icons"
import { Card } from "@/components/panelui/card"
import { api } from "@/lib/api"
import { Alert } from "@/components/panelui/alert"
import { Button } from "@/components/panelui/button"
import { RunProgressStages } from "@/components/application/run-progress"
import type { RunProgress, StageStatus } from "@/lib/run-progress"

import type { Publication } from "@/lib/publishing-status"

type StepState = "done" | "active" | "failed" | "waiting"
type Step = { label: string; detail: string; state: StepState; /** What fits in the node, when the status word alone says too little. */ short?: string }

const buildStates: Record<string, { state: StepState; detail: string; short?: string }> = {
  build_created: { state: "active", detail: "Waiting for a runner", short: "Waiting for runner" },
  build_pending: { state: "active", detail: "Waiting for a runner", short: "Waiting for runner" },
  build_waiting_for_resource: {
    state: "active",
    detail: "Waiting for a runner",
    short: "Waiting for runner",
  },
  build_preparing: { state: "active", detail: "Preparing", short: "Preparing" },
  build_running: { state: "active", detail: "Building and pushing the image", short: "Building image" },
  build_failed: {
    state: "failed",
    detail: "The pipeline failed. Open it to see the job log.",
  },
  build_canceled: { state: "failed", detail: "The pipeline was canceled." },
  build_canceling: { state: "failed", detail: "The pipeline was canceled." },
}

function steps(p: Publication): Step[] {
  const status = p.completionStatus ?? ""
  const completed = status === "completed"
  const build = buildStates[status]
  const retrying = status === "retrying"
  const buildStep: Step = completed
    ? { label: "Build pipeline", detail: "Succeeded", state: "done" }
    : build
      ? { label: "Build pipeline", ...build }
      : retrying
        ? {
            label: "Build pipeline",
            detail: "Build finished; recording the result",
            short: "Recording result",
            state: "active",
          }
        : {
            label: "Build pipeline",
            detail: "Waiting for the first pipeline status",
            short: "Starting",
            state: "active",
          }
  return [
    {
      label: "Source committed",
      detail: "Your files are in the Git repository",
      state: "done",
    },
    buildStep,
    {
      label: "Image published",
      detail: completed
        ? "Pushed to the registry with a verified digest"
        : "Pushed after a successful build",
      state: completed
        ? "done"
        : buildStep.state === "failed"
          ? "waiting"
          : "waiting",
    },
    {
      label: "Deploy manifest",
      detail: retrying
        ? "Retrying automatically"
        : completed
          ? "deploy/application.yaml now points to the new image"
          : "JustCD commits it after verifying the build",
      state: completed ? "done" : retrying ? "failed" : "waiting",
    },
    {
      label: "Review and apply",
      detail: completed
        ? "Calculate a plan and apply it to deploy"
        : "Available after the manifest is committed",
      state: completed ? "active" : "waiting",
    },
  ]
}

const stageStatus: Record<StepState, StageStatus> = { done: "complete", active: "running", failed: "failed", waiting: "pending" }

function stageShort(step: Step) {
  return step.state === "active" || step.state === "failed" ? step.short : undefined
}

/** The publishing steps as run stages. "Review and apply" is ready to act on, so it reads as waiting for you. */
function progressFor(list: Step[]): RunProgress {
  const stages = list.map((step) => {
    const ready = step.label === "Review and apply" && step.state === "active"
    return {
      id: step.label === "Review and apply" ? "review" : step.label,
      title: step.label,
      status: ready ? ("queued" as const) : stageStatus[step.state],
      caption: ready ? "Ready" : stageShort(step),
      action: ready ? "Click to review" : undefined,
      details: [{ label: "Detail", value: step.detail }],
    }
  })
  return { current: stages.findIndex((stage) => stage.status !== "complete") + 1 || stages.length, stages, summary: "" }
}

// One poller shared by the tab indicator and its panel, even while another tab is open.
export function usePublication(applicationId: string | undefined) {
  const [publication, setPublication] = useState<{
    applicationId: string
    value: Publication
  } | null>(null)
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    if (!applicationId) return
    let active = true
    let timer: number | undefined
    const load = async () => {
      try {
        const result = await api<{ publication: Publication }>(
          `/api/v1/applications/${encodeURIComponent(applicationId)}/publication`
        )
        if (!active) return
        setPublication({ applicationId, value: result.publication })
        setFailed(false)
      } catch {
        if (active) setFailed(true)
      }
      if (active) timer = window.setTimeout(() => void load(), 15000)
    }
    void load()
    return () => {
      active = false
      window.clearTimeout(timer)
    }
  }, [applicationId])
  return {
    publication:
      publication && publication.applicationId === applicationId
        ? publication.value
        : null,
    failed,
  }
}

export function PublishingProgress({
  publication,
  failed,
  onReview,
}: {
  publication: Publication | null
  failed: boolean
  onReview: () => void
}) {
  if (!publication)
    return (
      <p role="status" className="py-5 text-sm text-muted-foreground">
        {failed
          ? "Publishing progress is unavailable. Retrying automatically…"
          : "Loading publishing progress…"}
      </p>
    )
  // Legacy pipelines have no tracked completion state.
  if (publication.managedCompletion !== true)
    return (
      <p className="py-5 text-sm text-muted-foreground">
        Publishing progress is tracked in this application’s GitLab pipeline.
      </p>
    )

  const list = steps(publication)
  const reviewable = list.some((step) => step.label === "Review and apply" && step.state === "active")
  return (
    <Card size="sm" aria-label="Publishing progress">
      <Card.Header className="flex flex-wrap items-start gap-3">
        <Card.Title as="h2" className="min-w-0 flex-1 basis-40">Publishing progress</Card.Title>
        <Card.Action className="flex flex-wrap items-center gap-2">
          {publication.pipelineUrl && <Button size="sm" variant="outline" nativeButton={false} render={<a href={publication.pipelineUrl} target="_blank" rel="noreferrer" />}>Open GitLab pipeline<HugeiconsIcon icon={ArrowUpRight01Icon} strokeWidth={2} aria-hidden="true" /></Button>}
          {reviewable && <Button type="button" size="sm" onClick={onReview}>Review changes<HugeiconsIcon icon={ArrowRight01Icon} strokeWidth={2} aria-hidden="true" /></Button>}
        </Card.Action>
      </Card.Header>
      <Card.Content>
      <RunProgressStages label="Publishing stages" progress={progressFor(list)} pressable={reviewable ? ["review"] : []} onStagePress={onReview} />
      {publication.completionError && <Alert tone="destructive" variant="plain" role="alert" className="mt-4"><Alert.Icon /><Alert.Title>{publication.completionError}</Alert.Title></Alert>}
      </Card.Content>
    </Card>
  )
}
