"use client"

import { useEffect, useState } from "react"
import { api, errorMessage } from "@/lib/api"
import { Button } from "@/components/panelui/button"
import { Input } from "@/components/panelui/input"
import { Switch } from "@/components/panelui/switch"
import { Checkbox } from "@/components/panelui/checkbox"
import { CheckboxField, FormField } from "@/components/ui-kit"
import { ErrorNotice } from "@/components/workspace-ui"
import { useToast } from "@/components/toast-provider"
import { SectionSkeleton } from "./shared"
import { SettingsPanel } from "@/components/settings/settings-section"

type Settings = {
  enabled: boolean
  baseUrl: string
  model: string
  maxTokens: number
  hasApiKey: boolean
}
export function AIPanel() {
  const toast = useToast()
  const [settings, setSettings] = useState<Settings | null>(null)
  const [key, setKey] = useState("")
  const [clearKey, setClearKey] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)
  const [dirty, setDirty] = useState(false)
  const [tick, setTick] = useState(0)
  useEffect(() => {
    const controller = new AbortController()
    api<Settings>("/api/v1/admin/ai", { signal: controller.signal })
      .then(setSettings)
      .catch((cause) => {
        if (!controller.signal.aborted) setError(cause)
      })
    return () => controller.abort()
  }, [tick])
  async function save() {
    if (!settings) return
    setBusy(true)
    setError(null)
    try {
      const result = await api<Settings>("/api/v1/admin/ai", {
        method: "PUT",
        body: JSON.stringify({
          ...settings,
          apiKey: key || undefined,
          clearApiKey: clearKey,
        }),
      })
      setSettings(result)
      setKey("")
      setClearKey(false)
      setDirty(false)
      toast.success("AI connection saved")
    } catch (cause) {
      setError(cause)
    } finally {
      setBusy(false)
    }
  }
  async function test() {
    setBusy(true)
    setError(null)
    try {
      await api("/api/v1/admin/ai/test", { method: "POST", body: "{}" })
      toast.success("AI connection and structured analysis verified")
    } catch (cause) {
      setError(cause)
    } finally {
      setBusy(false)
    }
  }
  if (!settings)
    return error ? (
      <div className="space-y-3">
        <ErrorNotice error={error} />
        <Button
          variant="outline"
          onClick={() => {
            setError(null)
            setTick((value) => value + 1)
          }}
        >
          Retry
        </Button>
      </div>
    ) : (
      <SectionSkeleton label="Loading AI settings" />
    )
  const update = (change: Partial<Settings>) => {
    setSettings({ ...settings, ...change })
    setDirty(true)
  }
  return (
    <SettingsPanel
      title="AI connection"
      description="Connect your own LiteLLM instance or an OpenAI-compatible gateway."
    >
      <form
        className="space-y-5 p-5"
        onSubmit={(event) => {
          event.preventDefault()
          void save()
        }}
      >
        {error != null && <ErrorNotice inline error={error} />}
        <CheckboxField id="ai-enabled" label="Enable AI features" checked={settings.enabled} onCheckedChange={(enabled) => update({ enabled })} disabled={busy} />
        <FormField
          label="API base URL"
          htmlFor="ai-url"
          hint="For example https://litellm.example.com or https://litellm.example.com/v1. Private instance URLs are supported; requests originate from the backend."
        >
          <Input
            id="ai-url"
            type="url"
            value={settings.baseUrl}
            placeholder="https://litellm.example.com/v1"
            onChange={(event) => update({ baseUrl: event.target.value })}
            disabled={busy}
            required={settings.enabled}
          />
        </FormField>
        <FormField
          label="Model alias"
          htmlFor="ai-model"
          hint="Use a model alias configured on your LiteLLM instance. The model must return structured JSON."
        >
          <Input
            id="ai-model"
            value={settings.model}
            maxLength={200}
            onChange={(event) => update({ model: event.target.value })}
            disabled={busy}
            required={settings.enabled}
          />
        </FormField>
        <FormField
          label="API key"
          htmlFor="ai-key"
          hint={
            settings.hasApiKey
              ? "A key is stored securely. Leave blank to retain it. Changing the endpoint clears the previous key unless you supply a replacement."
              : "Optional for gateways without authentication. Prefer a dedicated LiteLLM virtual key with a budget."
          }
        >
          <Input
            id="ai-key"
            type="password"
            autoComplete="new-password"
            value={key}
            onChange={(event) => {
              setKey(event.target.value)
              setDirty(true)
            }}
            disabled={busy || clearKey}
          />
        </FormField>
        {settings.hasApiKey && (
          <label className="flex items-center gap-2 text-sm">
            <Checkbox
              checked={clearKey}
              onCheckedChange={(value) => {
                setClearKey(value === true)
                setDirty(true)
              }}
              disabled={busy}
            />
            Remove stored API key
          </label>
        )}
        <FormField label="Maximum output tokens" htmlFor="ai-max-tokens">
          <Input
            id="ai-max-tokens"
            type="number"
            min={256}
            max={8192}
            value={settings.maxTokens}
            onChange={(event) =>
              update({ maxTokens: Number(event.target.value) })
            }
            disabled={busy}
            required
          />
        </FormField>
        <p className="text-sm leading-6 text-muted-foreground">
          Workspace owners must opt in separately. AI receives filtered
          deployment context and user questions, and cannot apply changes or
          grant approvals. Configure provider retention and spending limits on
          your gateway.
        </p>
        <div className="flex flex-wrap gap-2">
          <Button
            type="submit"
            loading={busy}
            loadingText="Working…"
            disabled={!dirty}
          >
            Save connection
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={busy || dirty || !settings.baseUrl || !settings.model}
            onClick={() => void test()}
          >
            Test saved connection
          </Button>
        </div>
      </form>
    </SettingsPanel>
  )
}

type Policy = { enabled: boolean; available: boolean; model: string; autoTriage?: boolean }
export function WorkspaceAIPolicy({ workspaceId }: { workspaceId: string }) {
  const toast = useToast()
  const [policy, setPolicy] = useState<Policy | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)
  const [tick, setTick] = useState(0)
  const path = `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/ai`
  useEffect(() => {
    const controller = new AbortController()
    api<Policy>(path, { signal: controller.signal })
      .then(setPolicy)
      .catch((cause) => {
        if (!controller.signal.aborted) setError(cause)
      })
    return () => controller.abort()
  }, [path, tick])
  async function change(update: { enabled?: boolean; autoTriage?: boolean }) {
    setBusy(true)
    setError(null)
    try {
      setPolicy(
        await api<Policy>(path, {
          method: "PUT",
          body: JSON.stringify(update),
        })
      )
      window.dispatchEvent(new Event("justcd:ai-policy-updated"))
      toast.success(
        update.autoTriage !== undefined
          ? update.autoTriage ? "Automatic AI features enabled" : "Automatic AI features disabled"
          : update.enabled ? "Workspace AI enabled" : "Workspace AI disabled"
      )
    } catch (cause) {
      setError(cause)
      toast.error(errorMessage(cause), cause)
    } finally {
      setBusy(false)
    }
  }
  return (
    <SettingsPanel
      title="Workspace AI access"
      description="Choose whether workspace members can send deployment context to the instance AI connection."
    >
      <div className="space-y-4 p-5">
        {error != null && <ErrorNotice inline error={error} />}
        {!policy && !error && (
          <SectionSkeleton label="Loading workspace AI policy" />
        )}
        {!policy && error != null && (
          <Button
            variant="outline"
            onClick={() => {
              setError(null)
              setTick((value) => value + 1)
            }}
          >
            Retry
          </Button>
        )}
        {policy && (
          <>
            <div className="flex items-center justify-between gap-5">
              <label
                htmlFor="workspace-ai-enabled"
                className="text-sm font-medium"
              >
                Allow AI analysis in this workspace
              </label>
              <Switch
                id="workspace-ai-enabled"
                checked={policy.enabled}
                disabled={busy}
                onCheckedChange={(enabled) => void change({ enabled })}
              />
            </div>
            <div className="flex items-start justify-between gap-5">
              <div className="space-y-1">
                <label htmlFor="workspace-ai-auto-triage" className="text-sm font-medium">
                  Run AI features automatically
                </label>
                <p className="text-sm leading-6 text-muted-foreground">
                  Ranks the applications that need attention, writes approval briefings
                  for new plans that need approval, and refreshes the daily and weekly
                  digest. Runs after changes settle (rankings at most every 10 minutes).
                  Sends compact summaries, not logs. Uses AI tokens in the background.
                </p>
              </div>
              <Switch
                id="workspace-ai-auto-triage"
                checked={Boolean(policy.autoTriage)}
                disabled={busy || !policy.enabled}
                onCheckedChange={(autoTriage) => void change({ autoTriage })}
              />
            </div>
            <p className="text-sm leading-6 text-muted-foreground">
              Members can analyze plans, drift, health, deployment history, and
              environment differences. Context is limited to this workspace. Raw
              Secret/ConfigMap contents, Helm values, environment values, and
              commands are excluded. Questions and diagnostic messages may
              contain sensitive information; review your gateway’s data policy
              before enabling.
            </p>
            <p className="text-sm text-muted-foreground">
              {policy.available
                ? `Instance connection is enabled · ${policy.model}`
                : "The instance AI connection is not enabled. An administrator can configure it under Settings → AI."}
            </p>
          </>
        )}
      </div>
    </SettingsPanel>
  )
}
