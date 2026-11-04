"use client"
import { useEffect, useState } from "react"
import { Badge } from "@/components/reui/badge"
import { api } from "@/lib/api"
import { PublishingCard } from "./publishing-fields"

type Publication = {
  completionStatus?: string
  completionError?: string
  pipelineUrl?: string
  managedCompletion?: boolean
}
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
const dot: Record<StepState, string> = {
  done: "bg-success",
  active: "bg-primary animate-pulse",
  failed: "bg-destructive",
  waiting: "bg-muted-foreground/40",
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

export function PublishingProgress({
  applicationId,
  settled,
  onReview,
}: {
  applicationId: string
  settled: boolean
  onReview: () => void
}) {
  const [publication, setPublication] = useState<Publication | null>(null)
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    let active = true
    let timer: number | undefined
    const load = async () => {
      try {
        const result = await api<{ publication: Publication }>(
          `/api/v1/applications/${encodeURIComponent(applicationId)}/publication`
        )
        if (!active) return
        setPublication(result.publication)
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
  if (!publication || (failed && !publication)) return null
  // Legacy pipelines have no tracked state, and a finished, healthy release needs no panel.
  if (publication.managedCompletion !== true) return null
  if (publication.completionStatus === "completed" && settled) return null
  const list = steps(publication)
  return (
    <PublishingCard className="my-4 h-auto" contentClassName="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-sm font-semibold">Publishing progress</h2>
        {publication.pipelineUrl && (
          <a
            className="text-sm font-medium text-primary hover:underline"
            href={publication.pipelineUrl}
            target="_blank"
            rel="noreferrer"
          >
            Open GitLab pipeline →
          </a>
        )}
      </div>
      <ol className="space-y-2.5">
        {list.map((step) => (
          <li key={step.label} className="flex items-start gap-3">
            <span
              aria-hidden="true"
              className={`mt-1.5 size-2.5 shrink-0 rounded-full ${dot[step.state]}`}
            />
            <div className="min-w-0 flex-1">
              <p className="text-sm font-medium">
                {step.label}{" "}
                <span className="sr-only">: {stateLabel[step.state]}</span>
              </p>
              <p className="text-xs leading-5 text-muted-foreground">
                {step.detail}
              </p>
            </div>
            {step.label === "Review and apply" && step.state === "active" && (
              <button
                type="button"
                className="text-sm font-medium text-primary hover:underline"
                onClick={onReview}
              >
                Review changes →
              </button>
            )}
            {step.state === "failed" && (
              <Badge size="sm" radius="full" variant="destructive-light">
                {stateLabel[step.state]}
              </Badge>
            )}
          </li>
        ))}
      </ol>
      {publication.completionError && (
        <p role="alert" className="text-xs leading-5 text-destructive">
          {publication.completionError}
        </p>
      )}
    </PublishingCard>
  )
}
