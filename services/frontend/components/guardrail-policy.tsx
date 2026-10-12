"use client"

import { useEffect, useId, useState } from "react"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { Disclosure } from "@/components/ui/collapsible"
import { FormField } from "@/components/ui-kit"
import {
  SettingsGroup,
  SettingsSection,
} from "@/components/settings/settings-section"
import { FramePanel } from "@/components/reui/frame"
import { api, errorMessage } from "@/lib/api"
import type { GuardrailPolicy, Plan } from "@/lib/types"

const rules = [
  ["privileged-containers", "Privileged containers and host access"],
  ["mutable-image-tags", "Images without an immutable SHA-256 digest"],
  ["missing-resource-limits", "Missing CPU or memory limits"],
  ["public-services", "Public services"],
  ["forbidden-namespaces", "Forbidden namespaces"],
] as const

export function GuardrailPolicyPanel({ workspaceId }: { workspaceId: string }) {
  const prefix = useId()
  const [config, setConfig] = useState<GuardrailPolicy | null>(null)
  const [namespaces, setNamespaces] = useState("")
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const path = `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/guardrail-policy`
  useEffect(() => {
    let active = true
    api<GuardrailPolicy>(path)
      .then((value) => {
        if (active) {
          setConfig(value)
          setNamespaces(value.forbiddenNamespaces?.join(", ") ?? "")
        }
      })
      .catch((cause) => {
        if (active) setError(errorMessage(cause))
      })
    return () => {
      active = false
    }
  }, [path])
  async function save(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true)
    setError(null)
    setSaved(false)
    try {
      const value = await api<GuardrailPolicy>(path, {
        method: "PUT",
        body: JSON.stringify({
          ...config,
          forbiddenNamespaces: namespaces
            .split(",")
            .map((n) => n.trim())
            .filter(Boolean),
        }),
      })
      setConfig(value)
      setNamespaces(value.forbiddenNamespaces?.join(", ") ?? "")
      setSaved(true)
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setBusy(false)
    }
  }
  return (
    <SettingsSection
      title="Deployment guardrails"
      description="Check desired resources before approval and deployment. Policy changes require existing plans to be reviewed again."
    >
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      {!config && !error && (
        <p role="status" className="text-sm text-muted-foreground">
          Loading deployment guardrails…
        </p>
      )}
      {config && (
        <SettingsGroup>
          <FramePanel>
            <form
              onSubmit={save}
              className="space-y-4"
              onChange={() => setSaved(false)}
            >
              <FormField label="Enforcement mode" htmlFor={`${prefix}-mode`}>
                <FormSelect
                  id={`${prefix}-mode`}
                  value={config.mode}
                  disabled={busy}
                  onValueChange={(mode) => {
                    setConfig({
                      ...config,
                      mode: mode as GuardrailPolicy["mode"],
                    })
                    setSaved(false)
                  }}
                  items={[
                    { value: "audit", label: "Audit — record findings" },
                    { value: "warn", label: "Warn — highlight findings" },
                    {
                      value: "block",
                      label: "Block — prevent deployment on violations",
                    },
                  ]}
                />
              </FormField>
              <fieldset disabled={busy} className="space-y-3">
                <legend className="mb-2 text-sm font-medium">
                  Enabled checks
                </legend>
                {rules.map(([rule, label]) => (
                  <label
                    key={rule}
                    className="flex items-center gap-3 text-sm"
                    htmlFor={`${prefix}-${rule}`}
                  >
                    <Checkbox
                      id={`${prefix}-${rule}`}
                      checked={!config.disabledRules?.includes(rule)}
                      onCheckedChange={(enabled) => {
                        setConfig({
                          ...config,
                          disabledRules: enabled
                            ? config.disabledRules?.filter((r) => r !== rule)
                            : [...(config.disabledRules ?? []), rule],
                        })
                        setSaved(false)
                      }}
                    />
                    {label}
                  </label>
                ))}
              </fieldset>
              <FormField
                label="Forbidden namespaces"
                htmlFor={`${prefix}-namespaces`}
              >
                <Input
                  id={`${prefix}-namespaces`}
                  value={namespaces}
                  disabled={busy}
                  placeholder="kube-system, production"
                  onChange={(event) => setNamespaces(event.target.value)}
                  aria-describedby={`${prefix}-hint`}
                />
                <p
                  id={`${prefix}-hint`}
                  className="text-xs text-muted-foreground"
                >
                  Comma-separated exact namespace names.
                </p>
              </FormField>
              <div className="flex flex-wrap items-center gap-3">
                <Button type="submit" disabled={busy} loading={busy}>
                  Save guardrails
                </Button>
                {saved && (
                  <p role="status" className="text-sm text-muted-foreground">
                    Guardrails saved. Changed policies invalidate existing
                    plans.
                  </p>
                )}
              </div>
            </form>
          </FramePanel>
        </SettingsGroup>
      )}
    </SettingsSection>
  )
}

export function PolicyEvidence({ plan }: { plan: Plan }) {
  const evaluation = plan.policyEvaluation
  if (!evaluation) return null
  const blocked = evaluation.mode === "block" && evaluation.results.length > 0
  return (
    <section
      aria-label="Deployment policy results"
      className={`mb-4 rounded-lg border p-3 ${blocked ? "border-destructive/30 bg-destructive/5" : evaluation.mode === "warn" && evaluation.results.length ? "border-warning/30 bg-warning/10" : "bg-muted/20"}`}
    >
      <h4 className="text-sm font-semibold">
        Deployment policies · {evaluation.mode} · {evaluation.results.length}{" "}
        finding(s)
      </h4>
      <p className="mt-1 text-sm text-muted-foreground">
        {blocked
          ? "Deployment is blocked. Approval cannot override these violations."
          : evaluation.results.length
            ? "These findings allow deployment under the saved enforcement mode."
            : "All enabled policy checks passed."}
      </p>
      {evaluation.results.length > 0 && (
        <Disclosure summary="Review policy findings" className="mt-3">
          <ul className="space-y-3">
            {evaluation.results.map((result, index) => (
              <li key={index} className="rounded-md border p-3 text-sm">
                <p className="font-medium break-words">
                  {result.resource.kind}{" "}
                  {result.resource.namespace
                    ? `${result.resource.namespace}/`
                    : ""}
                  {result.resource.name}
                </p>
                <p className="mt-1 font-mono text-xs break-all">
                  {result.rule}
                </p>
                <p className="mt-1 break-words">{result.evidence}</p>
                <p className="mt-1 break-words text-muted-foreground">
                  {result.remediation}
                </p>
              </li>
            ))}
          </ul>
        </Disclosure>
      )}
    </section>
  )
}
