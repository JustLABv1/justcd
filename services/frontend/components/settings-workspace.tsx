"use client"

import { useCallback, useEffect, useMemo, useState } from "react"
import { useRouter, usePathname } from "next/navigation"
import Link from "next/link"
import { HugeiconsIcon } from "@hugeicons/react"
import { PlusSignIcon } from "@hugeicons/core-free-icons"
import { Button } from "@/components/panelui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { EmptyState } from "@/components/ui-kit"
import { Badge } from "@/components/panelui/badge"
import { ScopeBadge, SettingsLayout, type SettingsNavGroup } from "@/components/settings/layout"
import { useToast } from "@/components/toast-provider"
import { api, errorMessage } from "@/lib/api"
import { workspaceConnectionSections } from "@/components/connections/sections"
import { WorkspaceConnectionShares } from "@/components/workspace-connection-shares"
import { ClusterPanel } from "@/components/settings/cluster-panel"
import { CredentialPanel } from "@/components/settings/credential-panel"
import { GitSourcePanel } from "@/components/settings/git-source-panel"
import { NamespacePanel } from "@/components/settings/namespace-panel"
import { OIDCPanel } from "@/components/settings/oidc-panel"
import { useWorkspaceSelection } from "@/hooks/workspace-selection"
import { PlatformUsersPanel } from "@/components/settings/users-panel"
import { AIPanel } from "@/components/settings/ai-panel"
import { AccessPanel } from "@/components/settings/access-panel"
import { MaintenancePanel } from "@/components/settings/maintenance-panel"
import { LoadErrorNotice, SectionSkeleton, type ActionRunner } from "@/components/settings/shared"
import type {
  Cluster,
  Credential,
  GitSource,
  ListResponse,
  NamespaceBinding,
  OIDCProvider,
  Workspace,
  User,
} from "@/lib/types"

const workspaceSections = ["git-sources", "clusters", "namespaces", "credentials", "shares"]

const navGroups = [
  { title: "Instance", ids: ["maintenance", "ai"] },
  { title: "Delivery", ids: ["git-sources", "clusters", "namespaces"] },
  { title: "Security & access", ids: ["credentials", "shares", "access", "oidc", "users"] },
]

type LoadKind = "base" | "scope" | "bindings" | "providers"

export function SettingsWorkspace({ fixedWorkspaceId, sectionOverride }: { fixedWorkspaceId?: string; sectionOverride?: string }) {
  const router = useRouter()
  const pathname = usePathname()
  const toast = useToast()
  const workspaceScoped = Boolean(fixedWorkspaceId)
  const requestedSection = sectionOverride ?? pathname.split("/")[2] ?? ""
  const section = workspaceScoped && requestedSection === "namespaces" ? "clusters" : requestedSection
  const sections = [
    { id: "git-sources", title: "Git sources", description: "Repositories this workspace deploys from" },
    { id: "clusters", title: "Kubernetes", description: "Cluster connections and namespace access" },
    { id: "namespaces", title: "Namespace access", description: "Namespaces this workspace can deploy to" },
    { id: "credentials", title: "Credentials", description: "Encrypted Git and Kubernetes secrets" },
    { id: "shares", title: "Sharing", description: "Review connection offers and workspace access" },
    { id: "access", title: "Sign-in & access", description: "Sign-in methods, registration and who can create workspaces", adminOnly: true },
    { id: "oidc", title: "OIDC providers", description: "Organization sign-in and group mapping", adminOnly: true },
    { id: "users", title: "Platform users", description: "Accounts and access on this JustCD instance", adminOnly: true },
    { id: "ai", title: "AI", description: "LiteLLM connection and model settings", adminOnly: true },
    { id: "maintenance", title: "Maintenance", description: "Restrict access while administrators work on the instance", adminOnly: true },
  ]
  const visibleSections = sections.filter((item) => workspaceScoped ? workspaceSections.includes(item.id) : !workspaceSections.includes(item.id))
  const current = visibleSections.find((item) => item.id === section)
  const sectionHref = (id: string) => workspaceScoped ? `/workspaces/${fixedWorkspaceId}/connections/${id}` : `/settings/${id}`
  const { pendingAccounts } = useWorkspaceSelection()
  const [user, setUser] = useState<User | null>(null)
  const [workspaces, setWorkspaces] = useState<Workspace[]>([])
  const [workspaceId, setWorkspaceId] = useState("")
  const [clusters, setClusters] = useState<Cluster[]>([])
  const [clusterId, setClusterId] = useState("")
  const [credentials, setCredentials] = useState<Credential[]>([])
  const [loadedWorkspaceId, setLoadedWorkspaceId] = useState("")
  const [loadedBindingsKey, setLoadedBindingsKey] = useState("")
  const [sources, setSources] = useState<GitSource[]>([])
  const [bindings, setBindings] = useState<NamespaceBinding[]>([])
  const [providers, setProviders] = useState<OIDCProvider[]>([])
  const [providersLoaded, setProvidersLoaded] = useState(false)
  const [errors, setErrors] = useState<Partial<Record<LoadKind, unknown>>>({})
  const [ticks, setTicks] = useState<Record<LoadKind, number>>({ base: 0, scope: 0, bindings: 0, providers: 0 })
  const [busy, setBusy] = useState(false)
  const [loading, setLoading] = useState(true)
  const [credentialDialogOpen, setCredentialDialogOpen] = useState(false)
  const [namespaceDialogOpen, setNamespaceDialogOpen] = useState(false)
  const [shareDialogOpen, setShareDialogOpen] = useState(false)

  const setError = useCallback((kind: LoadKind, cause: unknown) => setErrors((current) => ({ ...current, [kind]: cause })), [])
  function retry(kind: LoadKind) {
    setErrors((current) => ({ ...current, [kind]: undefined }))
    if (kind === "base") setLoading(true)
    setTicks((current) => ({ ...current, [kind]: current[kind] + 1 }))
  }

  useEffect(() => {
    async function loadBase() {
      const [session, workspaceList] = await Promise.all([
        api<{ user: User }>("/api/v1/auth/session"),
        api<ListResponse<Workspace>>("/api/v1/workspaces"),
      ])
      setUser(session.user)
      setWorkspaces(workspaceList.items)
      const queryId = new URLSearchParams(window.location.search).get("workspaceId") ?? ""
      const selected =
        workspaceList.items.find((workspace) => workspace.id === (fixedWorkspaceId ?? queryId)) ?? (!workspaceScoped ? workspaceList.items[0] : undefined)
      if (selected) setWorkspaceId(selected.id)
      const clusterList = selected
        ? await api<ListResponse<Cluster>>(`/api/v1/clusters?workspaceId=${encodeURIComponent(selected.id)}`)
        : { items: [] as Cluster[] }
      setClusters(clusterList.items)
      const requestedClusterId = new URLSearchParams(window.location.search).get("clusterId")
      const initialCluster = clusterList.items.find((item) => item.id === requestedClusterId) ?? clusterList.items[0]
      if (initialCluster) setClusterId(initialCluster.id)
    }
    loadBase()
      .catch((cause) => setError("base", cause))
      .finally(() => setLoading(false))
  }, [fixedWorkspaceId, workspaceScoped, ticks.base, setError])

  useEffect(() => {
    if (!workspaceScoped && workspaceSections.includes(section) && !loading) {
      router.replace(workspaceId ? `/workspaces/${workspaceId}/connections/${section}` : "/workspaces")
    }
  }, [workspaceScoped, section, loading, workspaceId, router])

  useEffect(() => {
    if (!workspaceId) return
    let active = true
    Promise.all([
      api<ListResponse<Credential>>(`/api/v1/credentials?workspaceId=${encodeURIComponent(workspaceId)}`),
      api<ListResponse<GitSource>>(`/api/v1/git-sources?workspaceId=${encodeURIComponent(workspaceId)}`),
    ])
      .then(([credentialList, sourceList]) => {
        if (!active) return
        setCredentials(credentialList.items)
        setSources(sourceList.items)
        setLoadedWorkspaceId(workspaceId)
      })
      .catch((cause) => { if (active) setError("scope", cause) })
    return () => { active = false }
  }, [workspaceId, ticks.scope, setError])

  useEffect(() => {
    if (!workspaceId || !clusterId || !["clusters", "namespaces"].includes(section)) return
    let active = true
    api<ListResponse<NamespaceBinding>>(`/api/v1/clusters/${encodeURIComponent(clusterId)}/bindings?workspaceId=${encodeURIComponent(workspaceId)}`)
      .then((result) => {
        if (!active) return
        setBindings(result.items)
        setLoadedBindingsKey(`${workspaceId}:${clusterId}`)
        setError("bindings", undefined)
      })
      .catch((cause) => { if (active) setError("bindings", cause) })
    return () => { active = false }
  }, [workspaceId, clusterId, section, ticks.bindings, setError])

  useEffect(() => {
    if (!user?.isAdmin) return
    let active = true
    api<ListResponse<OIDCProvider>>("/api/v1/admin/oidc-providers")
      .then((result) => {
        if (!active) return
        setProviders(result.items)
        setProvidersLoaded(true)
      })
      .catch((cause) => { if (active) setError("providers", cause) })
    return () => { active = false }
  }, [user, ticks.providers, setError])

  const scopeLoading = !!workspaceId && loadedWorkspaceId !== workspaceId
  const bindingsLoading = !!workspaceId && !!clusterId && loadedBindingsKey !== `${workspaceId}:${clusterId}`

  useEffect(() => {
    if (workspaceScoped && requestedSection === "namespaces" && clusterId && !loading && !scopeLoading && !bindingsLoading) {
      document.getElementById("namespace-access")?.scrollIntoView({ block: "start" })
    }
  }, [workspaceScoped, requestedSection, clusterId, loading, scopeLoading, bindingsLoading])

  const workspace = useMemo(() => workspaces.find((item) => item.id === workspaceId), [workspaces, workspaceId])
  const globalKubeCredentials = credentials.filter((item) => !item.workspaceId && ["kubernetes-token", "kubeconfig"].includes(item.kind))
  const gitCredentials = credentials.filter((item) => ["git-ssh", "git-https"].includes(item.kind))
  const namespaceCredentials = credentials.filter((item) => ["kubernetes-token", "kubeconfig"].includes(item.kind))

  function selectCluster(id: string) {
    if (id === clusterId && errors.bindings !== undefined) retry("bindings")
    else {
      setErrors((current) => ({ ...current, bindings: undefined }))
      setClusterId(id)
    }
    const url = new URL(window.location.href)
    if (id) url.searchParams.set("clusterId", id)
    else {
      url.searchParams.delete("clusterId")
      url.hash = ""
      setNamespaceDialogOpen(false)
    }
    window.history.replaceState(null, "", url)
  }

  const action: ActionRunner = async (work, success, after, loading) => {
    setBusy(true)
    const progress = loading ? toast.loading(loading) : null
    try {
      const result = await work()
      after?.(result)
      const outcome = typeof success === "function" ? success(result) : success
      const { message, kind } = typeof outcome === "string" ? { message: outcome, kind: "success" as const } : outcome
      if (progress) toast.finish(progress, kind, message)
      else toast[kind](message)
      return true
    } catch (cause) {
      if (progress) toast.finish(progress, "error", errorMessage(cause), cause)
      else toast.error(errorMessage(cause), cause)
      return false
    } finally {
      setBusy(false)
    }
  }

  const landingTarget = !workspaceScoped && !section && user?.isAdmin
    ? "maintenance"
    : undefined
  useEffect(() => {
    if (landingTarget) router.replace(`/settings/${landingTarget}`)
  }, [landingTarget, router])

  if (!workspaceScoped && workspaceSections.includes(section)) {
    return (
      <div role="status" aria-label="Opening workspace connections">
        <span className="sr-only">Opening workspace connections</span>
        <Skeleton className="mb-5 h-8 w-64" />
        <SectionSkeleton label="Opening workspace connections" />
      </div>
    )
  }

  const isOwner = workspaceScoped && workspace?.role === "owner" && !scopeLoading
  const headerAction = !isOwner || !workspace ? null
    : section === "git-sources" ? (
      <Button nativeButton={false} render={<Link href={`/workspaces/${workspace.id}/git-sources/new`} />}>
        <HugeiconsIcon icon={PlusSignIcon} strokeWidth={2} aria-hidden="true" />
        Connect Git source
      </Button>
    ) : section === "clusters" ? (
      <Button nativeButton={false} render={<Link href={`/workspaces/${workspace.id}/clusters/new`} />}>
        <HugeiconsIcon icon={PlusSignIcon} strokeWidth={2} aria-hidden="true" />
        Connect cluster
      </Button>
    ) : section === "credentials" ? (
      <Button type="button" onClick={() => setCredentialDialogOpen(true)}>
        <HugeiconsIcon icon={PlusSignIcon} strokeWidth={2} aria-hidden="true" />
        Add credential
      </Button>
    ) : section === "shares" ? (
      <Button type="button" onClick={() => setShareDialogOpen(true)}>
        <HugeiconsIcon icon={PlusSignIcon} strokeWidth={2} aria-hidden="true" />
        Offer connection
      </Button>
    ) : null

  // A failed load must never render as an empty list, so it replaces the section content.
  const sectionError: { kind: LoadKind; title: string } | null = errors.base !== undefined
    ? { kind: "base", title: "Could not load settings" }
    : workspaceScoped && errors.scope !== undefined
      ? { kind: "scope", title: "Could not load workspace connections" }
      : current?.adminOnly && user?.isAdmin && errors.providers !== undefined && section === "oidc"
        ? { kind: "providers", title: "Could not load OIDC providers" }
        : null

  const adminVisibleSections = visibleSections.filter((item) => !item.adminOnly || user?.isAdmin)
  const providersPending = section === "oidc" && !providersLoaded && errors.providers === undefined

  const navGroupsForArea: SettingsNavGroup[] = workspaceScoped && fixedWorkspaceId
    ? [
        { label: "Sources & targets", items: workspaceConnectionSections.filter((item) => item.id === "git-sources" || item.id === "clusters").map((item) => ({ id: item.id, label: item.title, href: `/workspaces/${fixedWorkspaceId}/connections/${item.id}` })) },
        { label: "Security & access", items: workspaceConnectionSections.filter((item) => item.id === "credentials" || item.id === "shares").map((item) => ({ id: item.id, label: item.title, href: `/workspaces/${fixedWorkspaceId}/connections/${item.id}` })) },
      ]
    : navGroups
        .map((group) => ({ label: group.title, items: adminVisibleSections.filter((item) => group.ids.includes(item.id)).map((item) => ({ id: item.id, label: item.title, href: sectionHref(item.id), badge: item.id === "users" && pendingAccounts > 0 ? <Badge variant="warning" >{pendingAccounts}</Badge> : undefined })) }))
        .filter((group) => group.items.length > 0)
  const firstInstanceSection = !workspaceScoped && user?.isAdmin ? adminVisibleSections[0]?.id : undefined

  const body = loading && !sectionError ? (
    <SectionSkeleton rows={3} label="Loading settings" />
  ) : sectionError ? (
    <LoadErrorNotice error={errors[sectionError.kind]} title={sectionError.title} onRetry={() => retry(sectionError.kind)} />
  ) : section && !current ? (
    <EmptyState title="Section not found" description="Choose a section to continue." href={workspaceScoped ? `/workspaces/${fixedWorkspaceId}/connections` : "/settings"} action={workspaceScoped ? "View connections" : "View settings"} />
  ) : (
    <>
      {current?.adminOnly && !user?.isAdmin && (
        <EmptyState title="Administrator access required" description="Only instance administrators can manage maintenance, sign-in providers, and local accounts." href="/settings" action="Back to settings" />
      )}
      {workspaceScoped && !workspace && (
        <EmptyState title="Workspace unavailable" description="You need workspace access to manage its connections." href="/workspaces" action="View workspaces" />
      )}
      {!section && !user?.isAdmin ? (
        <EmptyState title="Administrator access required" description="Instance settings are available to administrators. Workspace connections live on each workspace page." href="/workspaces" action="View workspaces" />
      ) : !section ? (
        <SectionSkeleton rows={2} label="Opening settings" />
      ) : (
        <div className="settings-content min-w-0">
                {section === "ai" && user?.isAdmin && <AIPanel />}
                {section === "maintenance" && user?.isAdmin && <MaintenancePanel />}
                {section === "access" && user?.isAdmin && <AccessPanel />}
                {workspaceScoped && workspace && scopeLoading && <SectionSkeleton label="Loading workspace connections" />}
                {workspaceScoped && workspace && section === "credentials" && !scopeLoading && (
                  <CredentialPanel
                    key={workspaceId}
                    workspace={workspace}
                    user={user}
                    credentials={credentials}
                    busy={busy}
                    action={action}
                    createOpen={credentialDialogOpen}
                    onCreateOpenChange={setCredentialDialogOpen}
                    onCreated={(credential) => setCredentials((items) => [credential, ...items])}
                    onUpdated={(credential) => setCredentials((items) => items.map((item) => (item.id === credential.id ? credential : item)))}
                    onDeleted={(value) => setCredentials((items) => items.filter((item) => item.id !== value))}
                  />
                )}
                {workspaceScoped && workspace && section === "clusters" && !scopeLoading && <div className="space-y-8">
                  <ClusterPanel
                    key={workspace.id}
                    user={user}
                    workspace={workspace}
                    clusters={clusters}
                    globalCredentials={globalKubeCredentials}
                    workspaceCredentials={namespaceCredentials.filter((item) => item.workspaceId === workspaceId)}
                    busy={busy}
                    action={action}
                    activeClusterId={clusterId}
                    onManageNamespaces={(id) => { setNamespaceDialogOpen(false); selectCluster(id) }}
                    renderNamespaceAccess={(cluster) => (
                      <section id="namespace-access" aria-label={`Namespace access for ${cluster.name}`} className="scroll-mt-28">
                        {errors.bindings !== undefined ? <LoadErrorNotice error={errors.bindings} title="Could not load namespace access" onRetry={() => retry("bindings")} /> : bindingsLoading ? <SectionSkeleton label="Loading namespace access" /> : <NamespacePanel
                          key={`${workspaceId}:${cluster.id}`}
                          workspace={workspace}
                          cluster={cluster}
                          credentials={namespaceCredentials}
                          bindings={bindings}
                          busy={busy}
                          action={action}
                          createOpen={namespaceDialogOpen}
                          onCreateOpenChange={setNamespaceDialogOpen}
                          onCreated={(binding) => setBindings((items) => [...items, binding].sort((a, b) => a.namespace.localeCompare(b.namespace)))}
                          onUpdated={(binding) => setBindings((items) => items.map((item) => (item.namespace === binding.namespace ? binding : item)))}
                          onDeleted={(value) => setBindings((items) => items.filter((item) => item.namespace !== value))}
                        />}
                      </section>
                    )}
                    onUpdated={(cluster) => setClusters((items) => items.map((item) => (item.id === cluster.id ? cluster : item)))}
                    onDeleted={(value) => {
                      const remaining = clusters.filter((item) => item.id !== value)
                      setClusters(remaining)
                      if (clusterId === value) {
                        setErrors((current) => ({ ...current, bindings: undefined }))
                        setClusterId(remaining[0]?.id ?? "")
                      }
                    }}
                  />
                </div>}
                {workspaceScoped && workspace && section === "git-sources" && !scopeLoading && (
                  <GitSourcePanel
                    key={workspaceId}
                    workspace={workspace}
                    credentials={gitCredentials}
                    sources={sources}
                    busy={busy}
                    action={action}
                    onUpdated={(source) => setSources((items) => items.map((item) => (item.id === source.id ? source : item)))}
                    onDeleted={(value) => setSources((items) => items.filter((item) => item.id !== value))}
                  />
                )}
                {workspaceScoped && workspace && section === "shares" && !scopeLoading && (
                  <WorkspaceConnectionShares workspace={workspace} workspaces={workspaces} clusters={clusters} sources={sources} credentials={credentials} offerOpen={shareDialogOpen} onOfferOpenChange={setShareDialogOpen} />
                )}
                {section === "oidc" && user?.isAdmin && providersPending && <SectionSkeleton rows={2} label="Loading OIDC providers" />}
                {section === "oidc" && user?.isAdmin && providersLoaded && (
                  <OIDCPanel
                    providers={providers}
                    workspaces={workspaces}
                    busy={busy}
                    action={action}
                    onDeleted={(id) => setProviders((items) => items.filter((item) => item.id !== id))}
                    onCreated={(provider) =>
                      setProviders((items) => items.some((item) => item.id === provider.id) ? items.map((item) => (item.id === provider.id ? provider : item)) : [...items, provider])
                    }
                  />
                )}
                {section === "users" && user?.isAdmin && <PlatformUsersPanel currentUserId={user.id} busy={busy} action={action} />}
        </div>
      )}
    </>
  )

  return (
    <SettingsLayout
      title={workspaceScoped ? "Connections" : "Instance settings"}
      description={workspaceScoped ? "Repositories, Kubernetes access, credentials and sharing for this workspace." : "Maintenance, sign-in, accounts and AI for the whole JustCD instance."}
      scope={workspaceScoped ? undefined : <ScopeBadge scope="instance" />}
      nav={navGroupsForArea}
      activeId={section || firstInstanceSection || ""}
      navLabel={workspaceScoped ? "Connection sections" : "Instance settings sections"}
      actions={headerAction}
    >
      {body}
    </SettingsLayout>
  )
}
