"use client"

import Link from "next/link"
import { useEffect, useState } from "react"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { HugeiconsIcon } from "@hugeicons/react"
import { Add01Icon, Delete02Icon } from "@hugeicons/core-free-icons"
import { Badge } from "@/components/reui/badge"
import { Button } from "@/components/ui/button"
import { AppDialog, DialogCancel, DialogFooter } from "@/components/ui/dialog"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { CheckboxCard, FormField, SwitchField } from "@/components/ui-kit"
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
  pipelineStatusReporting: false,
  credentialId: "",
  mode: "review-only",
  allowNamespaceCreation: false,
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
type Actor = { providerId: string; userId: string }

function initialSettings(
  repository: RepositoryConfiguration
): RepositoryPRSettings {
  const saved = repository.prSettings
  return {
    ...defaults,
    ...saved,
    mode: saved?.mode || "review-only",
    destinations: saved?.destinations ?? [],
    profile: { ...defaults.profile, ...saved?.profile },
  }
}

/**
 * Pull request discovery policy for one repository branch. Controlled by the
 * parent (opened from the repository row menu); content is mounted only while
 * open so every opening starts from the saved settings with no stale errors.
 */
export function RepositoryPRSettingsDialog({
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
    <SettingsDialogContent
      repository={repository}
      onOpenChange={onOpenChange}
      onSaved={onSaved}
    />
  )
}

function SettingsDialogContent({
  repository,
  onOpenChange,
  onSaved,
}: {
  repository: RepositoryConfiguration
  onOpenChange: (open: boolean) => void
  onSaved: () => Promise<void>
}) {
  const toast = useToast()
  const [tab, setTab] = useState("policy")
  const [busy, setBusy] = useState(false)
  const [settings, setSettings] = useState<RepositoryPRSettings>(() =>
    initialSettings(repository)
  )
  const [credentials, setCredentials] = useState<Credential[]>([])
  const [destinations, setDestinations] = useState<Destination[]>([])
  const [clusterItems, setClusterItems] = useState<
    { value: string; label: string }[]
  >([])
  const [members, setMembers] = useState<WorkspaceMember[]>([])
  const [actors, setActors] = useState<Actor[]>(() =>
    Object.entries(repository.prSettings?.profile.approvalActors ?? {}).map(
      ([providerId, userId]) => ({ providerId, userId })
    )
  )
  const [entries, setEntries] = useState<Entry[]>([])
  const [loaded, setLoaded] = useState(false)
  const [loadAttempt, setLoadAttempt] = useState(0)
  const [loadError, setLoadError] = useState("")
  const [error, setError] = useState("")

  useEffect(() => {
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
          setClusterItems(
            clusters.items.map((cluster) => ({
              value: cluster.id,
              label: cluster.name,
            }))
          )
          setMembers(users.items)
          setEntries(reviews.items)
          setLoadError("")
          setLoaded(true)
        }
      } catch (cause) {
        if (active) {
          setLoadError(errorMessage(cause))
          setLoaded(true)
        }
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
      if (
        settings.enabled &&
        (!settings.destinations.length ||
          settings.destinations.some(
            (d) => !d.clusterId || (!d.namespace && !d.namespacePattern?.trim())
          ))
      ) {
        setTab("destinations")
        throw new Error(
          "Add an allowed destination with an exact namespace or namespace regex."
        )
      }
      if (settings.enabled && settings.mode === "isolated") {
        const { namespacePrefix, hostSuffix, quotaCpu, quotaMemory } =
          settings.profile
        if (
          ![namespacePrefix, hostSuffix, quotaCpu, quotaMemory].every((v) =>
            String(v ?? "").trim()
          )
        ) {
          setTab("limits")
          throw new Error("Fill in every isolated namespace limit.")
        }
      }
      if (
        actors.some((a) => !/^[1-9][0-9]*$/.test(a.providerId) || !a.userId) ||
        new Set(actors.map((a) => a.providerId)).size !== actors.length
      ) {
        setTab("approvals")
        throw new Error(
          "Approval identities need unique numeric provider account IDs and a workspace member."
        )
      }
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
      onOpenChange(false)
      toast.success(
        "Pull request policy saved. Discovery runs every two minutes."
      )
    } catch (cause) {
      const message = errorMessage(cause)
      if (message.includes("namespace regex")) setTab("destinations")
      setError(message)
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
  function addDestination() {
    const next = destinations.find(
      (d) =>
        !settings.destinations.some(
          (s) => s.clusterId === d.clusterId && s.namespace === d.namespace
        )
    )
    if (next)
      setSettings((s) => ({
        ...s,
        destinations: [
          ...s.destinations,
          { clusterId: next.clusterId, namespace: next.namespace },
        ],
      }))
    else if (settings.allowNamespaceCreation && clusterItems[0]) {
      setSettings((s) => ({
        ...s,
        destinations: [
          ...s.destinations,
          { clusterId: clusterItems[0].value, namespace: "" },
        ],
      }))
    }
  }
  function updateDestination(
    index: number,
    patch: Partial<RepositoryPRSettings["destinations"][number]>
  ) {
    setSettings((s) => ({
      ...s,
      destinations: s.destinations.map((d, i) =>
        i === index ? { ...d, ...patch } : d
      ),
    }))
  }
  function addNamespacePattern() {
    const cluster = clusterItems[0]
    if (cluster)
      setSettings((s) => ({
        ...s,
        destinations: [
          ...s.destinations,
          {
            clusterId: cluster.value,
            namespace: "",
            namespacePattern: "^dev-.*$",
          },
        ],
      }))
  }
  function updateActor(index: number, patch: Partial<Actor>) {
    setActors((rows) =>
      rows.map((row, i) => (i === index ? { ...row, ...patch } : row))
    )
  }
  const destinationLabel = (clusterId: string, namespace: string) =>
    destinations.find(
      (d) => d.clusterId === clusterId && d.namespace === namespace
    )?.label ?? namespace
  const disabledNote = (
    <p className="text-sm text-muted-foreground">
      Turn on pull request discovery in the Policy tab to configure this.
    </p>
  )

  return (
    <AppDialog
      open
      onOpenChange={onOpenChange}
      busy={busy}
      size="xl"
      title="Pull request discovery"
      description={`Discover justcd.yaml applications added in pull requests targeting ${repository.revision}, including drafts.`}
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
        {repository.prError && (
          <p role="alert" className="text-sm text-destructive">
            {repository.prError}
          </p>
        )}
        <fieldset disabled={busy} className="min-w-0">
          <Tabs value={tab} onValueChange={(value) => setTab(String(value))}>
            <TabsList aria-label="Pull request discovery sections">
              <TabsTrigger value="policy">Policy</TabsTrigger>
              <TabsTrigger value="destinations">
                Destinations
                <Badge size="xs" variant="secondary">
                  {settings.destinations.length}
                </Badge>
              </TabsTrigger>
              <TabsTrigger value="limits">Limits</TabsTrigger>
              <TabsTrigger value="approvals">Approvals</TabsTrigger>
              <TabsTrigger value="discovered">
                Discovered PRs
                <Badge size="xs" variant="secondary">
                  {entries.length}
                </Badge>
              </TabsTrigger>
            </TabsList>

            <TabsContent value="policy" className="space-y-4 outline-none">
              <SwitchField
                id="repo-pr-enabled"
                label="Discover new applications in pull requests"
                description="Find justcd.yaml definitions added in open pull requests, including drafts."
                checked={settings.enabled}
                onCheckedChange={(enabled) =>
                  setSettings((s) => ({ ...s, enabled }))
                }
              />
              {settings.enabled ? (
                <>
                  <FormField
                    label="Provider API credential"
                    htmlFor="repo-pr-credential"
                    hint="HTTPS token with pull request read and comment read/write access. Commit status write access is needed only when commit statuses are enabled."
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
                  <SwitchField
                    id="repo-pr-pipeline-status-reporting"
                    label="Report commit statuses"
                    description="Off by default; JustCD reports through PR/MR comments. Enabling this can block merges. GitLab adds an external job to a pipeline for the commit, and JustCD failures can fail that pipeline."
                    checked={settings.pipelineStatusReporting ?? false}
                    onCheckedChange={(pipelineStatusReporting) =>
                      setSettings((s) => ({ ...s, pipelineStatusReporting }))
                    }
                  />
                  <FormField label="Deployment" htmlFor="repo-pr-mode">
                    <FormSelect
                      id="repo-pr-mode"
                      value={settings.mode}
                      onValueChange={(mode) =>
                        setSettings((s) => ({
                          ...s,
                          mode: mode as RepositoryPRSettings["mode"],
                          allowNamespaceCreation:
                            mode === "existing" && s.allowNamespaceCreation,
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
                  {settings.mode === "existing" && (
                    <div className="space-y-3">
                      <CheckboxCard
                        id="repo-pr-confirm-shared"
                        checked={settings.profile.confirmShared}
                        onCheckedChange={(checked) =>
                          profile("confirmShared", checked)
                        }
                        title="Authorize changes to approved dev environments and their data"
                        description="On merge, the app keeps its ID and resource ownership. Unmerged pull requests require reviewed cleanup."
                      />
                      <SwitchField
                        id="repo-pr-namespace-creation"
                        label="Allow creation of matching dev namespaces"
                        description="Applications with createNamespaces: true can register matching namespaces during discovery and create them during deployment. Uses the cluster's default workspace credential and normal deployment approvals."
                        checked={settings.allowNamespaceCreation ?? false}
                        onCheckedChange={(allowNamespaceCreation) =>
                          setSettings((s) => ({ ...s, allowNamespaceCreation }))
                        }
                      />
                    </div>
                  )}
                </>
              ) : null}
            </TabsContent>

            <TabsContent
              value="destinations"
              className="space-y-3 outline-none"
            >
              {settings.enabled ? (
                <>
                  <div>
                    <h3 className="text-sm font-semibold">
                      Allowed destinations in justcd.yaml
                    </h3>
                    <p className="mt-1 text-sm text-muted-foreground">
                      The destination in justcd.yaml must match one of these
                      rules. Regex rules match the entire namespace in the
                      selected cluster.{" "}
                      {settings.allowNamespaceCreation
                        ? "Matching namespaces can be registered automatically when justcd.yaml sets createNamespaces: true."
                        : "Every destination requires existing workspace namespace access."}{" "}
                      Forks are excluded.
                    </p>
                  </div>
                  {settings.destinations.length === 0 && (
                    <p className="rounded-lg border border-dashed p-3 text-sm text-muted-foreground">
                      No destinations yet.
                      {loaded &&
                      !destinations.length &&
                      !settings.allowNamespaceCreation
                        ? " Grant this workspace namespace access on a cluster first."
                        : ""}
                    </p>
                  )}
                  {settings.destinations.map((d, index) => (
                    <div
                      key={index}
                      className="flex items-start gap-2 rounded-lg border p-3"
                    >
                      <div className="min-w-0 flex-1 space-y-3">
                        <FormField
                          label="Namespace matching"
                          htmlFor={`repo-pr-match-${index}`}
                        >
                          <FormSelect
                            id={`repo-pr-match-${index}`}
                            value={
                              d.namespacePattern !== undefined
                                ? "regex"
                                : "exact"
                            }
                            items={[
                              { value: "exact", label: "Exact namespace" },
                              { value: "regex", label: "Namespace regex" },
                            ]}
                            onValueChange={(value) => {
                              if (value === "regex")
                                updateDestination(index, {
                                  namespace: "",
                                  namespacePattern: "^dev-.*$",
                                })
                              else {
                                const next = destinations.find(
                                  (item) => item.clusterId === d.clusterId
                                )
                                updateDestination(index, {
                                  namespace: next?.namespace ?? "",
                                  namespacePattern: undefined,
                                })
                              }
                            }}
                          />
                        </FormField>
                        {d.namespacePattern !== undefined ||
                        settings.allowNamespaceCreation ? (
                          <div className="grid gap-3 sm:grid-cols-2">
                            <FormField
                              label="Cluster"
                              htmlFor={`repo-pr-cluster-${index}`}
                            >
                              <FormSelect
                                id={`repo-pr-cluster-${index}`}
                                value={d.clusterId}
                                items={clusterItems}
                                onValueChange={(clusterId) =>
                                  updateDestination(index, { clusterId })
                                }
                              />
                            </FormField>
                            <FormField
                              label={
                                d.namespacePattern !== undefined
                                  ? "Namespace regex"
                                  : "Namespace"
                              }
                              htmlFor={`repo-pr-namespace-${index}`}
                              hint={
                                d.namespacePattern !== undefined
                                  ? "For example: ^dev-.*$ or .*-dev. Uses Go/RE2 syntax; matches the whole name."
                                  : "An existing or new dev namespace, for example dev-shop."
                              }
                            >
                              <Input
                                id={`repo-pr-namespace-${index}`}
                                value={d.namespacePattern ?? d.namespace}
                                maxLength={
                                  d.namespacePattern !== undefined ? 256 : 63
                                }
                                placeholder={
                                  d.namespacePattern !== undefined
                                    ? "^dev-.*$"
                                    : "dev-shop"
                                }
                                spellCheck={false}
                                onChange={(event) =>
                                  updateDestination(index, {
                                    ...(d.namespacePattern !== undefined
                                      ? { namespacePattern: event.target.value }
                                      : { namespace: event.target.value }),
                                  })
                                }
                              />
                            </FormField>
                          </div>
                        ) : (
                          <FormField
                            label="Destination"
                            htmlFor={`repo-pr-destination-${index}`}
                          >
                            <FormSelect
                              id={`repo-pr-destination-${index}`}
                              value={`${d.clusterId}/${d.namespace}`}
                              items={destinations}
                              onValueChange={(value) =>
                                chooseDestination(index, value)
                              }
                            />
                          </FormField>
                        )}
                      </div>
                      <Button
                        type="button"
                        size="icon"
                        variant="ghost"
                        aria-label={`Remove destination ${d.namespacePattern ?? destinationLabel(d.clusterId, d.namespace)}`}
                        onClick={() =>
                          setSettings((s) => ({
                            ...s,
                            destinations: s.destinations.filter(
                              (_, i) => i !== index
                            ),
                          }))
                        }
                      >
                        <HugeiconsIcon
                          icon={Delete02Icon}
                          strokeWidth={1.8}
                          aria-hidden="true"
                        />
                      </Button>
                    </div>
                  ))}
                  <div className="flex flex-wrap gap-2">
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      disabled={
                        (!destinations.length &&
                          !(
                            settings.allowNamespaceCreation &&
                            clusterItems.length
                          )) ||
                        settings.destinations.length >= 32 ||
                        (!settings.allowNamespaceCreation &&
                          destinations.every((d) =>
                            settings.destinations.some(
                              (s) =>
                                s.clusterId === d.clusterId &&
                                s.namespace === d.namespace
                            )
                          ))
                      }
                      onClick={addDestination}
                    >
                      <HugeiconsIcon
                        icon={Add01Icon}
                        strokeWidth={1.8}
                        aria-hidden="true"
                      />
                      Add destination
                    </Button>
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      disabled={
                        !clusterItems.length ||
                        settings.destinations.length >= 32
                      }
                      onClick={addNamespacePattern}
                    >
                      <HugeiconsIcon
                        icon={Add01Icon}
                        strokeWidth={1.8}
                        aria-hidden="true"
                      />
                      Add namespace regex
                    </Button>
                  </div>
                </>
              ) : (
                disabledNote
              )}
            </TabsContent>

            <TabsContent value="limits" className="space-y-4 outline-none">
              {!settings.enabled ? (
                disabledNote
              ) : settings.mode !== "isolated" ? (
                <p className="text-sm text-muted-foreground">
                  Limits apply when Deployment is set to “Deploy in isolated
                  namespaces”.
                </p>
              ) : (
                <>
                  <h3 className="text-sm font-semibold">
                    Isolated namespace limits
                  </h3>
                  <div className="grid gap-4 sm:grid-cols-2">
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
                          onChange={(event) => profile(key, event.target.value)}
                        />
                      </FormField>
                    ))}
                    <FormField
                      label="Lifetime (hours)"
                      htmlFor="repo-pr-lifetime"
                      hint="Between 1 and 168 hours."
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
                </>
              )}
            </TabsContent>

            <TabsContent value="approvals" className="space-y-4 outline-none">
              {settings.enabled ? (
                <>
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
                  <div className="border-t pt-4">
                    <h3 className="text-sm font-semibold">
                      Pull request comment approval identities
                    </h3>
                    <p className="mt-1 text-sm text-muted-foreground">
                      Map provider accounts to workspace members so their
                      approval comments count.
                    </p>
                  </div>
                  {actors.map((actor, index) => (
                    <div
                      key={index}
                      className="grid items-start gap-3 sm:grid-cols-[1fr_1fr_auto]"
                    >
                      <FormField
                        label="Provider account ID"
                        htmlFor={`repo-pr-actor-id-${index}`}
                        hint="Numeric ID, not the username."
                      >
                        <Input
                          id={`repo-pr-actor-id-${index}`}
                          value={actor.providerId}
                          inputMode="numeric"
                          onChange={(event) =>
                            updateActor(index, {
                              providerId: event.target.value,
                            })
                          }
                        />
                      </FormField>
                      <FormField
                        label="JustCD user"
                        htmlFor={`repo-pr-actor-user-${index}`}
                      >
                        <FormSelect
                          id={`repo-pr-actor-user-${index}`}
                          value={actor.userId}
                          placeholder="Select workspace member"
                          items={members
                            .filter((m) => !m.disabled || m.id === actor.userId)
                            .map((m) => ({
                              value: m.id,
                              label: `${m.displayName || m.email} · ${m.role}`,
                            }))}
                          onValueChange={(userId) =>
                            updateActor(index, { userId })
                          }
                        />
                      </FormField>
                      <Button
                        type="button"
                        size="icon"
                        variant="ghost"
                        className="sm:mt-6"
                        aria-label={`Remove approval identity ${actor.providerId || index + 1}`}
                        onClick={() =>
                          setActors((rows) =>
                            rows.filter((_, i) => i !== index)
                          )
                        }
                      >
                        <HugeiconsIcon
                          icon={Delete02Icon}
                          strokeWidth={1.8}
                          aria-hidden="true"
                        />
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
                    <HugeiconsIcon
                      icon={Add01Icon}
                      strokeWidth={1.8}
                      aria-hidden="true"
                    />
                    Add approval identity
                  </Button>
                </>
              ) : (
                disabledNote
              )}
            </TabsContent>

            <TabsContent value="discovered" className="outline-none">
              <h3 className="text-sm font-semibold">
                Discovered pull request applications
              </h3>
              <p className="mt-1 text-sm text-muted-foreground">
                Read-only status. Updated by discovery, not by this form.
              </p>
              {!loaded ? (
                <p role="status" className="mt-3 text-sm text-muted-foreground">
                  Loading…
                </p>
              ) : entries.length === 0 ? (
                <p className="mt-3 rounded-lg border border-dashed p-3 text-sm text-muted-foreground">
                  No pull request applications discovered yet.
                </p>
              ) : (
                <ul className="mt-3 divide-y rounded-lg border">
                  {entries.map((entry) => (
                    <li key={entry.id} className="px-4 py-3 text-sm">
                      <div className="flex flex-wrap items-center justify-between gap-3">
                        {entry.applicationId ? (
                          <Link
                            className="font-medium text-primary hover:underline"
                            href={`/applications/${entry.applicationId}?tab=pull-requests`}
                          >
                            {entry.definitionName} · PR #{entry.number}
                          </Link>
                        ) : (
                          <span className="font-medium">
                            {entry.definitionName} · PR #{entry.number}
                          </span>
                        )}
                        <Badge
                          size="sm"
                          radius="full"
                          variant={
                            entry.error ? "destructive-light" : "outline"
                          }
                          className="capitalize"
                        >
                          {entry.phase.replaceAll("_", " ")}
                        </Badge>
                      </div>
                      {entry.error && (
                        <p className="mt-1 text-sm text-destructive">
                          {entry.error}
                        </p>
                      )}
                    </li>
                  ))}
                </ul>
              )}
            </TabsContent>
          </Tabs>
        </fieldset>
        <DialogFooter>
          <DialogCancel disabled={busy} />
          <Button type="submit" loading={busy} loadingText="Saving…">
            Save changes
          </Button>
        </DialogFooter>
      </form>
    </AppDialog>
  )
}
