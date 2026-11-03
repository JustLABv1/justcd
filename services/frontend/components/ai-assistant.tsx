"use client"

import Link from "next/link"
import { HugeiconsIcon } from "@hugeicons/react"
import { AiMagicIcon } from "@hugeicons/core-free-icons"
import { useEffect, useId, useRef, useState } from "react"
import { api } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Textarea } from "@/components/ui/textarea"
import { FormSelect } from "@/components/ui/form-select"
import { AppDialog } from "@/components/ui/dialog"
import { FormField } from "@/components/ui-kit"
import { ErrorNotice } from "@/components/workspace-ui"
import type { Operation } from "@/lib/types"

type Feature = "plan" | "diagnose" | "drift" | "summary" | "compare" | "ask"
type Availability = { enabled: boolean; available: boolean; model: string }
type Analysis = {
  summary: string
  findings: { title: string; detail: string; evidenceIds: string[] }[] | null
  nextSteps: string[] | null
  limitations: string[] | null
  sources: { id: string; label: string; url: string }[]
  model: string
  generatedAt: string
}
const labels: Record<Feature, string> = {
  plan: "Explain plan",
  diagnose: "Diagnose health",
  drift: "Explain drift",
  summary: "Summarize deployments",
  compare: "Compare environments",
  ask: "Ask justcd",
}

export function AIAssistant({
  workspaceId,
  applicationId,
  groupId,
  planId,
  features = ["ask"],
  operations,
  contextVersion = "",
  presentation = "dialog",
}: {
  workspaceId: string
  applicationId?: string
  groupId?: string
  planId?: string
  features?: Feature[]
  operations?: Operation[]
  contextVersion?: string
  presentation?: "dialog" | "inline"
}) {
  const id = useId()
  const [open, setOpen] = useState(false)
  const [availability, setAvailability] = useState<Availability | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [result, setResult] = useState<Analysis | null>(null)
  const [question, setQuestion] = useState("")
  const [operationId, setOperationId] = useState("")
  const [includeLogs, setIncludeLogs] = useState(false)
  const [busy, setBusy] = useState<Feature | null>(null)
  const [history, setHistory] = useState<{ role: string; content: string }[]>(
    []
  )
  const [tick, setTick] = useState(0)
  const request = useRef<AbortController | null>(null)
  useEffect(() => {
    const refresh = () => setTick((value) => value + 1)
    window.addEventListener("justcd:ai-policy-updated", refresh)
    return () => window.removeEventListener("justcd:ai-policy-updated", refresh)
  }, [])
  const scope = `${workspaceId}:${applicationId ?? ""}:${groupId ?? ""}:${planId ?? ""}:${contextVersion}`
  useEffect(() => {
    const controller = new AbortController()
    api<Availability>(
      `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/ai`,
      { signal: controller.signal }
    )
      .then(setAvailability)
      .catch((cause) => {
        if (!controller.signal.aborted) setError(cause)
      })
    return () => controller.abort()
  }, [workspaceId, tick])
  useEffect(() => {
    return () => {
      request.current?.abort()
    }
  }, [scope])

  async function analyze(feature: Feature) {
    request.current?.abort()
    const controller = new AbortController()
    request.current = controller
    setError(null)
    setResult(null)
    setBusy(feature)
    try {
      const response = await api<Analysis>(
        `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/ai/analyze`,
        {
          method: "POST",
          signal: controller.signal,
          body: JSON.stringify({
            feature,
            includeLogs: feature === "diagnose" && includeLogs,
            applicationId,
            groupId,
            planId:
              feature === "plan" || feature === "drift" ? planId : undefined,
            operationId: feature === "summary" ? operationId : undefined,
            question: feature === "ask" ? question : undefined,
            history: feature === "ask" ? history : undefined,
          }),
        }
      )
      if (controller.signal.aborted) return
      setResult(response)
      if (feature === "ask") {
        setHistory((items) =>
          [
            ...items,
            { role: "user", content: question.slice(0, 2000) },
            { role: "assistant", content: response.summary.slice(0, 2000) },
          ].slice(-8)
        )
        setQuestion("")
      }
    } catch (cause) {
      if (!controller.signal.aborted) setError(cause)
    } finally {
      if (request.current === controller) setBusy(null)
    }
  }

  const enabled = availability?.available && availability.enabled
  // Optional AI takes no space in delivery workflows until configured.
  if (!enabled && presentation === "dialog") return null
  const content = (
    <div className="space-y-4">
      {error != null && <ErrorNotice error={error} />}
      {!enabled && (
        <p className="text-sm text-muted-foreground">
          AI is not enabled for this workspace. Configure the connection in
          instance settings and enable workspace AI access.
        </p>
      )}
      {availability?.available && availability.enabled && (
        <>
          <p className="text-xs text-muted-foreground">
            Uses {availability.model}. Sends selected deployment metadata and
            filtered configuration to your configured AI endpoint. Raw secrets
            and Helm values are excluded.
          </p>
          {features.includes("diagnose") && (
            <label className="flex items-start gap-2 text-xs leading-5 text-muted-foreground">
              <Checkbox
                checked={includeLogs}
                onCheckedChange={(value) => setIncludeLogs(value === true)}
                disabled={!!busy}
                className="mt-0.5"
              />
              Include limited live container logs in diagnosis. Logs can contain
              sensitive data; redaction is best-effort.
            </label>
          )}
          {operations && (
            <FormField
              label="Deployment to summarize"
              htmlFor={`${id}-operation`}
            >
              <FormSelect
                id={`${id}-operation`}
                value={operationId}
                onValueChange={(value) => {
                  setOperationId(value)
                  setResult(null)
                }}
                disabled={!!busy}
                emptyOption="Recent deployment history"
                items={operations.slice(0, 10).map((operation) => ({
                  value: operation.id,
                  label: `${operation.type} · ${operation.status} · ${new Date(operation.startedAt).toLocaleString()}`,
                }))}
              />
            </FormField>
          )}
          <div className="flex flex-wrap gap-2">
            {features
              .filter((feature) => feature !== "ask")
              .map((feature) => (
                <Button
                  key={feature}
                  variant="outline"
                  size="sm"
                  disabled={
                    !!busy ||
                    ((feature === "plan" || feature === "drift") && !planId)
                  }
                  loading={busy === feature}
                  loadingText="Analyzing…"
                  onClick={() => void analyze(feature)}
                >
                  {labels[feature]}
                </Button>
              ))}
          </div>
          {features.includes("ask") && (
            <form
              className="space-y-3"
              onSubmit={(event) => {
                event.preventDefault()
                void analyze("ask")
              }}
            >
              <FormField
                label={
                  applicationId
                    ? "Ask about this application"
                    : groupId
                      ? "Ask about this deployment group"
                      : "Ask about this workspace"
                }
                htmlFor={`${id}-question`}
              >
                <Textarea
                  id={`${id}-question`}
                  value={question}
                  maxLength={4000}
                  onChange={(event) => setQuestion(event.target.value)}
                  placeholder="Which applications need attention, and what changed recently?"
                  disabled={!!busy}
                  required
                />
              </FormField>
              <div className="flex flex-wrap gap-2">
                <Button
                  type="submit"
                  disabled={!!busy || !question.trim()}
                  loading={busy === "ask"}
                  loadingText="Analyzing…"
                >
                  Ask justcd
                </Button>
                {history.length > 0 && (
                  <Button
                    type="button"
                    variant="ghost"
                    disabled={!!busy}
                    onClick={() => {
                      setHistory([])
                      setResult(null)
                    }}
                  >
                    New conversation
                  </Button>
                )}
              </div>
            </form>
          )}
        </>
      )}
      {busy && (
        <div
          role="status"
          className="flex flex-wrap items-center justify-between gap-3 text-sm text-muted-foreground"
        >
          <span>Analyzing scoped deployment evidence…</span>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              request.current?.abort()
              setBusy(null)
            }}
          >
            Cancel
          </Button>
        </div>
      )}
      {result && (
        <section
          aria-label="AI analysis"
          aria-live="polite"
          className="space-y-4 border-t pt-4"
        >
          <p className="text-sm leading-6 whitespace-pre-wrap">
            {result.summary}
          </p>
          {result.findings?.map((finding, index) => (
            <div key={index} className="space-y-1">
              <h3 className="text-sm font-medium">{finding.title}</h3>
              <p className="text-sm leading-6 whitespace-pre-wrap text-muted-foreground">
                {finding.detail}
              </p>
              <div className="flex flex-wrap gap-x-3 gap-y-1">
                {finding.evidenceIds.map((sourceId) => {
                  const source = result.sources.find(
                    (item) => item.id === sourceId
                  )
                  return source ? (
                    <Link
                      key={sourceId}
                      href={source.url}
                      className="text-xs break-all text-primary underline underline-offset-4"
                    >
                      {source.label}
                    </Link>
                  ) : null
                })}
              </div>
            </div>
          ))}
          {!!result.nextSteps?.length && (
            <div className="space-y-2">
              <h3 className="text-sm font-medium">Suggested next steps</h3>
              <ol className="list-decimal space-y-1 pl-5 text-sm leading-6">
                {result.nextSteps.map((step, index) => (
                  <li key={index}>{step}</li>
                ))}
              </ol>
            </div>
          )}
          {!!result.limitations?.length && (
            <div className="rounded-lg bg-muted/40 p-3">
              <h3 className="text-xs font-medium">Evidence limitations</h3>
              <ul className="mt-1 list-disc space-y-1 pl-4 text-xs leading-5 text-muted-foreground">
                {result.limitations.map((limitation, index) => (
                  <li key={index}>{limitation}</li>
                ))}
              </ul>
            </div>
          )}
          <p className="text-xs text-muted-foreground">
            {result.model} · {new Date(result.generatedAt).toLocaleString()} ·
            Snapshot analysis; regenerate after deployment changes.
          </p>
        </section>
      )}
    </div>
  )
  if (presentation === "inline") return content
  function close(next: boolean) {
    setOpen(next)
    if (!next) {
      request.current?.abort()
      setBusy(null)
    }
  }
  const actionLabel = features.includes("plan")
    ? "Explain with AI"
    : features.includes("summary")
      ? "Summarize with AI"
      : features.includes("compare")
        ? "Compare with AI"
        : features.includes("diagnose")
          ? "Diagnose with AI"
          : "Ask AI"
  return (
    <>
      <Button
        variant="outline"
        onClick={() => setOpen(true)}
        aria-haspopup="dialog"
        aria-expanded={open}
      >
        <HugeiconsIcon
          icon={AiMagicIcon}
          strokeWidth={1.8}
          aria-hidden="true"
          className="text-primary"
        />
        {actionLabel}
      </Button>
      <AppDialog
        open={open}
        onOpenChange={close}
        title="Ask justcd"
        description="Read-only assistance for this view, with links to the source evidence."
        variant="info"
        size="lg"
      >
        {content}
      </AppDialog>
    </>
  )
}
