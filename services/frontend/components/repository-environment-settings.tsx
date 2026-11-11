"use client"

import { useEffect, useState } from "react"
import { Button } from "@/components/panelui/button"
import { AppDialog, DialogCancel, DialogFooter } from "@/components/app-dialog"
import { FormSelect } from "@/components/form-select"
import { FormField, CheckboxField } from "@/components/ui-kit"
import { useToast } from "@/components/toast-provider"
import { api, errorMessage } from "@/lib/api"
import type {
  Credential,
  EnvironmentSettings,
  ListResponse,
  RepositoryConfiguration,
} from "@/lib/types"

/**
 * GitLab environment reporting for one repository branch. Mounted only while
 * open so each opening starts from the saved settings.
 */
export function RepositoryEnvironmentSettingsDialog({
  repository,
  open,
  onOpenChange,
  onSaved,
}: {
  repository: RepositoryConfiguration
  open: boolean
  onOpenChange: (open: boolean) => void
  onSaved: () => Promise<void>
}) {
  if (!open) return null
  return (
    <DialogContent
      repository={repository}
      onOpenChange={onOpenChange}
      onSaved={onSaved}
    />
  )
}

function DialogContent({
  repository,
  onOpenChange,
  onSaved,
}: {
  repository: RepositoryConfiguration
  onOpenChange: (open: boolean) => void
  onSaved: () => Promise<void>
}) {
  const toast = useToast()
  const [settings, setSettings] = useState<EnvironmentSettings>({
    enabled: false,
    credentialId: "",
  })
  const [credentials, setCredentials] = useState<Credential[]>([])
  const [loaded, setLoaded] = useState(false)
  const [loadAttempt, setLoadAttempt] = useState(0)
  const [loadError, setLoadError] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    let active = true
    async function load() {
      try {
        const [saved, tokens] = await Promise.all([
          api<EnvironmentSettings>(
            `/api/v1/repository-configurations/${encodeURIComponent(repository.id)}/environment-settings`
          ),
          api<ListResponse<Credential>>(
            `/api/v1/credentials?workspaceId=${encodeURIComponent(repository.workspaceId)}`
          ),
        ])
        if (!active) return
        setSettings(saved)
        setCredentials(
          tokens.items.filter(
            (c) =>
              c.kind === "git-https" &&
              (!c.expiresAt || Date.parse(c.expiresAt) > Date.now())
          )
        )
        setLoadError("")
      } catch (cause) {
        if (active) setLoadError(errorMessage(cause))
      } finally {
        if (active) setLoaded(true)
      }
    }
    void load()
    return () => {
      active = false
    }
  }, [repository.id, repository.workspaceId, loadAttempt])

  async function save() {
    setBusy(true)
    setError("")
    try {
      if (settings.enabled && !settings.credentialId) {
        throw new Error("Select a Git credential with API access.")
      }
      await api(
        `/api/v1/repository-configurations/${encodeURIComponent(repository.id)}/environment-settings`,
        { method: "PUT", body: JSON.stringify(settings) }
      )
      await onSaved()
      onOpenChange(false)
      toast.success(
        settings.enabled
          ? "GitLab environment reporting enabled."
          : "GitLab environment reporting disabled."
      )
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setBusy(false)
    }
  }

  return (
    <AppDialog
      open
      onOpenChange={onOpenChange}
      busy={busy}
      title="GitLab environments"
      description={`Record a GitLab deployment each time an application discovered on ${repository.revision} finishes a sync.`}
    >
      <form
        className="space-y-4"
        onSubmit={(event) => {
          event.preventDefault()
          void save()
        }}
      >
        {error && (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        )}
        {loadError && (
          <div
            role="alert"
            className="flex flex-wrap items-center justify-between gap-3 text-sm text-destructive"
          >
            <span>{loadError}</span>
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={() => {
                setLoaded(false)
                setLoadAttempt((n) => n + 1)
              }}
            >
              Retry
            </Button>
          </div>
        )}
        <fieldset disabled={busy || !loaded || Boolean(loadError)} className="min-w-0 space-y-4">
          <CheckboxField
            id="repo-environments-enabled"
            label="Report deployments to GitLab environments"
            description="Each application appears as an environment named cluster/namespace, with its latest successful or failed deployment. Override the name, tier or URL with spec.environment in justcd.yaml."
            checked={settings.enabled}
            onCheckedChange={(enabled) =>
              setSettings((s) => ({ ...s, enabled }))
            }
          />
          {settings.enabled && (
            <FormField
              label="Provider API credential"
              htmlFor="repo-environments-credential"
              hint="HTTPS token with the api scope and the Developer role or higher on the project, so it can create environments and deployments."
            >
              <FormSelect
                id="repo-environments-credential"
                value={settings.credentialId}
                onValueChange={(credentialId) =>
                  setSettings((s) => ({ ...s, credentialId }))
                }
                placeholder="Select Git credential"
                items={credentials.map((c) => ({ value: c.id, label: c.name }))}
              />
            </FormField>
          )}
        </fieldset>
        <DialogFooter>
          <DialogCancel disabled={busy} />
          <Button
            type="submit"
            loading={busy}
            loadingText="Saving…"
            disabled={!loaded || Boolean(loadError)}
          >
            Save changes
          </Button>
        </DialogFooter>
      </form>
    </AppDialog>
  )
}
