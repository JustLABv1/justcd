"use client"

import Link from "next/link"
import { useEffect, useState } from "react"
import { ConnectionDialog } from "@/components/connection-dialog"
import { Checkbox } from "@/components/ui/checkbox"
import { Button } from "@/components/ui/button"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { FormField } from "@/components/ui-kit"
import { useToast } from "@/components/toast-provider"
import { api, errorMessage } from "@/lib/api"
import type {
  Cluster,
  Credential,
  ListResponse,
  NamespaceBinding,
  RepositoryConfiguration,
  RepositoryPRSettings,
  WorkspaceMember,
} from "@/lib/types"

const defaults: RepositoryPRSettings = {
  enabled: false,
  credentialId: "",
  mode: "review-only",
  destinations: [],
  profile: {
    enabled: false,
    confirmShared: false,
    namespacePrefix: "justcd",
    hostSuffix: "",
    maxActive: 10,
    maxLifetimeHours: 24,
    quotaCpu: "2",
    quotaMemory: "4Gi",
    databaseStrategy: "none",
    approvalActors: {},
  },
}
type Entry = {
  id: string
  number: number
  definitionName: string
  applicationId?: string
  phase: string
  error: string
}
type Destination = {
  value: string
  label: string
  clusterId: string
  namespace: string
}

export function RepositoryPRSettingsControl({
  repository,
  onSaved,
}: {
  repository: RepositoryConfiguration
  onSaved: () => Promise<void>
}) {
  const toast = useToast()
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [settings, setSettings] = useState<RepositoryPRSettings>(defaults)
  const [credentials, setCredentials] = useState<Credential[]>([])
  const [destinations, setDestinations] = useState<Destination[]>([])
  const [members, setMembers] = useState<WorkspaceMember[]>([])
  const [actors, setActors] = useState<
    { providerId: string; userId: string }[]
  >([])
  const [entries, setEntries] = useState<Entry[]>([])
  const [error, setError] = useState("")
  useEffect(() => {
    if (!open) return
    let active = true
    async function load() {
      try {
        const [tokens, clusters, users, reviews] = await Promise.all([
          api<ListResponse<Credential>>(
            `/api/v1/credentials?workspaceId=${encodeURIComponent(repository.workspaceId)}`
          ),
          api<ListResponse<Cluster>>(
            `/api/v1/clusters?workspaceId=${encodeURIComponent(repository.workspaceId)}`
          ),
          api<ListResponse<WorkspaceMember>>(
            `/api/v1/workspaces/${encodeURIComponent(repository.workspaceId)}/members`
          ),
          api<ListResponse<Entry>>(
            `/api/v1/repository-configurations/${repository.id}/pull-requests`
          ),
        ])
        const bindings = await Promise.all(
          clusters.items.map(async (cluster) => {
            const result = await api<ListResponse<NamespaceBinding>>(
              `/api/v1/clusters/${cluster.id}/bindings?workspaceId=${encodeURIComponent(repository.workspaceId)}`
            )
            return result.items.map((binding) => ({
              value: `${cluster.id}/${binding.namespace}`,
              label: `${cluster.name} · ${binding.namespace}`,
              clusterId: cluster.id,
              namespace: binding.namespace,
            }))
          })
        )
        if (active) {
          setCredentials(
            tokens.items.filter(
              (c) =>
                c.kind === "git-https" &&
                (!c.expiresAt || Date.parse(c.expiresAt) > Date.now())
            )
          )
          setDestinations(bindings.flat())
          setMembers(users.items)
          setEntries(reviews.items)
          setError("")
        }
      } catch (cause) {
        if (active) setError(errorMessage(cause))
      }
    }
    void load()
    return () => {
      active = false
    }
  }, [open, repository.id, repository.workspaceId])
  function show() {
    const saved = repository.prSettings
    setSettings({
      ...defaults,
      ...saved,
      mode: saved?.mode || "review-only",
      destinations: saved?.destinations ?? [],
      profile: { ...defaults.profile, ...saved?.profile },
    })
    setActors(
      Object.entries(saved?.profile.approvalActors ?? {}).map(
        ([providerId, userId]) => ({ providerId, userId })
      )
    )
    setOpen(true)
  }
  async function save() {
    setBusy(true)
    try {
      if (
        actors.some((a) => !/^[1-9][0-9]*$/.test(a.providerId) || !a.userId) ||
        new Set(actors.map((a) => a.providerId)).size !== actors.length
      )
        throw new Error(
          "Approval mappings need unique numeric provider IDs and a workspace member."
        )
      await api(
        `/api/v1/repository-configurations/${repository.id}/pull-requests/settings`,
        {
          method: "PUT",
          body: JSON.stringify({
            ...settings,
            profile: {
              ...settings.profile,
              approvalActors: Object.fromEntries(
                actors.map((a) => [a.providerId, a.userId])
              ),
            },
          }),
        }
      )
      await onSaved()
      setOpen(false)
      toast.success(
        "Repository PR policy saved. Discovery runs every two minutes."
      )
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setBusy(false)
    }
  }
  function profile(key: string, value: string | number | boolean) {
    setSettings((s) => ({ ...s, profile: { ...s.profile, [key]: value } }))
  }
  function chooseDestination(index: number, value: string) {
    const next = destinations.find((d) => d.value === value)
    if (next)
      setSettings((s) => ({
        ...s,
        destinations: s.destinations.map((d, i) =>
          i === index
            ? { clusterId: next.clusterId, namespace: next.namespace }
            : d
        ),
      }))
  }
  return (
    <>
      <Button size="sm" variant="outline" onClick={show}>
        PR discovery{repository.prSettings?.enabled ? " · enabled" : ""}
      </Button>
      <ConnectionDialog
        open={open}
        onOpenChange={setOpen}
        busy={busy}
        title="New applications from pull requests"
        description={`Discover justcd.yaml applications added in PRs targeting ${repository.revision}, including drafts.`}
      >
        <form
          className="space-y-5"
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
          {repository.prError && (
            <p className="text-sm text-destructive">{repository.prError}</p>
          )}
          <fieldset disabled={busy} className="space-y-4">
            <label className="flex items-center gap-2 text-sm">
              <Checkbox
                aria-label="Discover new applications in PRs"
                checked={settings.enabled}
                disabled={busy}
                onCheckedChange={(enabled) =>
                  setSettings((s) => ({ ...s, enabled }))
                }
              />
              Discover new applications in PRs
            </label>
            {settings.enabled && (
              <>
                <FormField
                  label="Provider API credential"
                  htmlFor="repo-pr-credential"
                  hint="HTTPS token with PR read, comment read/write and commit status write access."
                >
                  <FormSelect
                    id="repo-pr-credential"
                    value={settings.credentialId}
                    onValueChange={(credentialId) =>
                      setSettings((s) => ({ ...s, credentialId }))
                    }
                    placeholder="Select Git credential"
                    items={credentials.map((c) => ({
                      value: c.id,
                      label: c.name,
                    }))}
                  />
                </FormField>
                <FormField label="Deployment" htmlFor="repo-pr-mode">
                  <FormSelect
                    id="repo-pr-mode"
                    value={settings.mode}
                    onValueChange={(mode) =>
                      setSettings((s) => ({
                        ...s,
                        mode: mode as RepositoryPRSettings["mode"],
                      }))
                    }
                    items={[
                      { value: "review-only", label: "Review plans only" },
                      {
                        value: "isolated",
                        label: "Deploy in isolated namespaces",
                      },
                      {
                        value: "existing",
                        label: "Deploy to approved dev namespaces",
                      },
                    ]}
                  />
                </FormField>
                <div className="space-y-2">
                  <p className="text-sm font-medium">
                    Allowed destinations in justcd.yaml
                  </p>
                  <p className="text-xs text-muted-foreground">
                    Branch definitions must match a destination selected here.
                    Forks are excluded.
                  </p>
                  {settings.destinations.map((d, index) => (
                    <div key={index} className="flex gap-2">
                      <FormSelect
                        ariaLabel={`Allowed destination ${index + 1}`}
                        value={`${d.clusterId}/${d.namespace}`}
                        items={destinations}
                        onValueChange={(value) =>
                          chooseDestination(index, value)
                        }
                      />
                      <Button
                        type="button"
                        size="sm"
                        variant="destructive"
                        onClick={() =>
                          setSettings((s) => ({
                            ...s,
                            destinations: s.destinations.filter(
                              (_, i) => i !== index
                            ),
                          }))
                        }
                      >
                        Remove
                      </Button>
                    </div>
                  ))}
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    disabled={!destinations.length}
                    onClick={() => {
                      const next = destinations.find(
                        (d) =>
                          !settings.destinations.some(
                            (s) =>
                              s.clusterId === d.clusterId &&
                              s.namespace === d.namespace
                          )
                      )
                      if (next)
                        setSettings((s) => ({
                          ...s,
                          destinations: [
                            ...s.destinations,
                            {
                              clusterId: next.clusterId,
                              namespace: next.namespace,
                            },
                          ],
                        }))
                    }}
                  >
                    Add destination
                  </Button>
                </div>
                {settings.mode === "existing" && (
                  <label className="flex items-start gap-2 rounded-lg border p-3 text-sm">
                    <Checkbox
                      aria-label="Authorize changes to approved dev environments and their data"
                      className="mt-0.5"
                      checked={settings.profile.confirmShared}
                      disabled={busy}
                      onCheckedChange={(checked) =>
                        profile("confirmShared", checked)
                      }
                    />
                    I authorize changes to these dev environments and their
                    data. On merge, the app keeps its ID and resource ownership.
                    Unmerged PRs require reviewed cleanup.
                  </label>
                )}
                {settings.mode === "isolated" && (
                  <details className="rounded-lg border p-3" open>
                    <summary className="cursor-pointer text-sm font-medium">
                      Isolated namespace limits
                    </summary>
                    <div className="mt-4 grid gap-4 sm:grid-cols-2">
                      {(
                        [
                          ["namespacePrefix", "Namespace prefix"],
                          ["hostSuffix", "Preview host suffix"],
                          ["quotaCpu", "CPU quota"],
                          ["quotaMemory", "Memory quota"],
                        ] as const
                      ).map(([key, label]) => (
                        <FormField
                          key={key}
                          label={label}
                          htmlFor={`repo-pr-${key}`}
                        >
                          <Input
                            id={`repo-pr-${key}`}
                            value={settings.profile[key] ?? ""}
                            onChange={(event) =>
                              profile(key, event.target.value)
                            }
                            required
                          />
                        </FormField>
                      ))}
                      <FormField
                        label="Lifetime (hours)"
                        htmlFor="repo-pr-lifetime"
                      >
                        <Input
                          id="repo-pr-lifetime"
                          type="number"
                          min={1}
                          max={168}
                          value={settings.profile.maxLifetimeHours}
                          onChange={(event) =>
                            profile(
                              "maxLifetimeHours",
                              Number(event.target.value)
                            )
                          }
                        />
                      </FormField>
                    </div>
                  </details>
                )}
                <details className="rounded-lg border p-3">
                  <summary className="cursor-pointer text-sm font-medium">
                    Provider and approval settings
                  </summary>
                  <div className="mt-4 space-y-4">
                    <FormField label="Provider" htmlFor="repo-pr-provider">
                      <FormSelect
                        id="repo-pr-provider"
                        value={settings.provider ?? ""}
                        emptyOption="Detect from Git source"
                        items={[
                          { value: "github", label: "GitHub" },
                          { value: "gitlab", label: "GitLab" },
                        ]}
                        onValueChange={(provider) =>
                          setSettings((s) => ({ ...s, provider }))
                        }
                      />
                    </FormField>
                    <FormField
                      label="Provider API URL"
                      htmlFor="repo-pr-api"
                      hint="For a custom host, for example https://git.example.com/api/v4."
                    >
                      <Input
                        id="repo-pr-api"
                        value={settings.apiUrl ?? ""}
                        onChange={(event) =>
                          setSettings((s) => ({
                            ...s,
                            apiUrl: event.target.value,
                          }))
                        }
                      />
                    </FormField>
                    <p className="text-sm font-medium">
                      PR comment approval identities
                    </p>
                    {actors.map((actor, index) => (
                      <div
                        key={index}
                        className="grid items-end gap-3 sm:grid-cols-[1fr_1fr_auto]"
                      >
                        <label className="flex flex-col gap-2 text-sm">
                          Provider account ID
                          <Input
                            value={actor.providerId}
                            inputMode="numeric"
                            onChange={(event) =>
                              setActors((rows) =>
                                rows.map((row, i) =>
                                  i === index
                                    ? { ...row, providerId: event.target.value }
                                    : row
                                )
                              )
                            }
                          />
                        </label>
                        <label className="flex flex-col gap-2 text-sm">
                          JustCD user
                          <FormSelect
                            ariaLabel={`Approval user ${index + 1}`}
                            value={actor.userId}
                            placeholder="Select workspace member"
                            items={members
                              .filter(
                                (m) => !m.disabled || m.id === actor.userId
                              )
                              .map((m) => ({
                                value: m.id,
                                label: `${m.displayName || m.email} · ${m.role}`,
                              }))}
                            onValueChange={(userId) =>
                              setActors((rows) =>
                                rows.map((row, i) =>
                                  i === index ? { ...row, userId } : row
                                )
                              )
                            }
                          />
                        </label>
                        <Button
                          type="button"
                          size="sm"
                          variant="destructive"
                          onClick={() =>
                            setActors((rows) =>
                              rows.filter((_, i) => i !== index)
                            )
                          }
                        >
                          Remove
                        </Button>
                      </div>
                    ))}
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      onClick={() =>
                        setActors((rows) => [
                          ...rows,
                          { providerId: "", userId: "" },
                        ])
                      }
                    >
                      Add approval identity
                    </Button>
                  </div>
                </details>
              </>
            )}
            <Button type="submit" size="sm" loading={busy}>
              Save PR policy
            </Button>
          </fieldset>
        </form>
        {entries.length > 0 && (
          <div className="mt-6 border-t pt-4">
            <h4 className="mb-3 text-sm font-medium">
              Discovered PR applications
            </h4>
            <div className="divide-y">
              {entries.map((entry) => (
                <div key={entry.id} className="py-3 text-sm">
                  <div className="flex justify-between gap-3">
                    {entry.applicationId ? (
                      <Link
                        className="text-primary hover:underline"
                        href={`/applications/${entry.applicationId}?tab=pull-requests`}
                      >
                        {entry.definitionName} · PR #{entry.number}
                      </Link>
                    ) : (
                      <span>
                        {entry.definitionName} · PR #{entry.number}
                      </span>
                    )}
                    <span className="text-muted-foreground">
                      {entry.phase.replaceAll("_", " ")}
                    </span>
                  </div>
                  {entry.error && (
                    <p className="mt-1 text-xs text-destructive">
                      {entry.error}
                    </p>
                  )}
                </div>
              ))}
            </div>
          </div>
        )}
      </ConnectionDialog>
    </>
  )
}
