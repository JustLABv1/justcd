"use client"
import { useEffect, useState } from "react"
import { Badge } from "@/components/panelui/badge"
import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowRight01Icon, ArrowUpRight01Icon, Cancel01Icon } from "@hugeicons/core-free-icons"
import { Card } from "@/components/panelui/card"
import { api } from "@/lib/api"
import { Button } from "@/components/panelui/button"
import { Steps } from "@/components/panelui/steps"

import type { Publication } from "@/lib/publishing-status"

type StepState = "done" | "active" | "failed" | "waiting"
type Step = { label: string; detail: string; state: StepState }

const buildStates: Record<string, { state: StepState; detail: string }> = {
  build_created: { state: "active", detail: "Waiting for a runner" },
  build_pending: { state: "active", detail: "Waiting for a runner" },
  build_waiting_for_resource: {
    state: "active",
    detail: "Waiting for a runner",
  },
  build_preparing: { state: "active", detail: "Preparing" },
  build_running: { state: "active", detail: "Building and pushing the image" },
  build_failed: {
    state: "failed",
    detail: "The pipeline failed. Open it to see the job log.",
  },
  build_canceled: { state: "failed", detail: "The pipeline was canceled." },
  build_canceling: { state: "failed", detail: "The pipeline was canceled." },
}
const stateLabel: Record<StepState, string> = {
  done: "Done",
  active: "In progress",
  failed: "Failed",
  waiting: "Waiting",
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
            state: "active",
          }
        : {
            label: "Build pipeline",
            detail: "Waiting for the first pipeline status",
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
      label: "Deployment manifest committed",
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
  // Connectors fill up to the first step that is not yet done.
  const current = list.findIndex((step) => step.state !== "done") + 1 || list.length
  return (
    <Card size="sm" aria-label="Publishing progress">
      <Card.Header className="flex flex-wrap items-start gap-3">
        <Card.Title as="h2" className="min-w-0 flex-1 basis-40">Publishing progress</Card.Title>
        {publication.pipelineUrl && (
          <Card.Action>
          <Button
            size="sm"
            variant="outline"
            nativeButton={false}
            render={
              <a
                href={publication.pipelineUrl}
                target="_blank"
                rel="noreferrer"
              />
            }
          >
            Open GitLab pipeline<HugeiconsIcon icon={ArrowUpRight01Icon} strokeWidth={2} aria-hidden="true" />
          </Button>
          </Card.Action>
        )}
      </Card.Header>
      <Card.Content>
      <Steps orientation="vertical" value={current} aria-label="Publishing stages">
        {list.map((step, index) => <Steps.Item key={step.label} step={index + 1} completed={step.state === "done"} loading={step.state === "active" && step.label !== "Review and apply"}>
          <div className="grid w-full grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1">
            <Steps.Indicator className={`row-span-2 ${step.state === "failed" ? "bg-destructive text-destructive-foreground ring-destructive/20" : step.state === "done" ? "bg-success text-success-foreground" : ""}`}>{step.state === "failed" ? <HugeiconsIcon icon={Cancel01Icon} className="size-4" aria-hidden="true" /> : undefined}</Steps.Indicator>
            <Steps.Title className={step.state === "failed" ? "text-destructive" : undefined}>{step.label} <span className="sr-only">: {stateLabel[step.state]}</span>{step.state === "failed" && <Badge size="sm" variant="destructive" className="ml-2">Failed</Badge>}</Steps.Title>
            <Steps.Description>{step.detail}</Steps.Description>
            {step.label === "Review and apply" && step.state === "active" && <div className="col-start-2 mt-2"><Button type="button" size="sm" variant="outline" onClick={onReview}>Review changes<HugeiconsIcon icon={ArrowRight01Icon} strokeWidth={2} aria-hidden="true" /></Button></div>}
          </div>
          {index < list.length - 1 && <Steps.Separator className={step.state === "done" ? "bg-success/60" : "bg-border"} />}
        </Steps.Item>)}
      </Steps>
      {publication.completionError && (
        <p
          role="alert"
          className="mt-4 text-sm leading-5 text-destructive"
        >
          {publication.completionError}
        </p>
      )}
      </Card.Content>
    </Card>
  )
}
