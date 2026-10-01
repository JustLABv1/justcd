"use client"

import { useCallback, useEffect, useMemo, useState } from "react"
import { useRouter, usePathname } from "next/navigation"
import Link from "next/link"
import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowLeft01Icon, ArrowRight01Icon, PlusSignIcon } from "@hugeicons/core-free-icons"
import { WorkspaceIcon } from "@/components/workspace-ui"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { EmptyState, PageHeading, Panel } from "@/components/ui-kit"
import { useToast } from "@/components/toast-provider"
import { api, errorMessage } from "@/lib/api"
import { WorkspaceConnectionShares } from "@/components/workspace-connection-shares"
import { ClusterPanel } from "@/components/settings/cluster-panel"
import { CredentialPanel } from "@/components/settings/credential-panel"
import { GitSourcePanel } from "@/components/settings/git-source-panel"
import { NamespacePanel } from "@/components/settings/namespace-panel"
import { OIDCPanel } from "@/components/settings/oidc-panel"
import { PlatformUsersPanel } from "@/components/settings/users-panel"
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
  { title: "Delivery", ids: ["git-sources", "clusters", "namespaces"] },
  { title: "Security & access", ids: ["credentials", "shares", "oidc", "users"] },
]

type LoadKind = "base" | "scope" | "bindings" | "providers"

/** One notice used on the instance settings landing and on every instance section. */
function InstanceNotice() {
  return (
    <Panel className="mb-6">
      <div className="flex items-start gap-3 p-4">
        <WorkspaceIcon name="shield" className="mt-0.5 size-5 shrink-0 text-primary" />
        <div>
          <p className="text-sm font-semibold">Instance settings</p>
          <p className="mt-1 text-sm text-muted-foreground">These settings apply to the whole instance and every workspace on it.</p>
        </div>
      </div>
    </Panel>
  )
}

export function SettingsWorkspace({ fixedWorkspaceId, sectionOverride }: { fixedWorkspaceId?: string; sectionOverride?: string }) {
  const router = useRouter()
  const pathname = usePathname()
  const toast = useToast()
  const workspaceScoped = Boolean(fixedWorkspaceId)
  const section = sectionOverride ?? pathname.split("/")[2] ?? ""
  const sections = [
    { id: "git-sources", title: "Git sources", description: "Repositories this workspace deploys from" },
    { id: "clusters", title: "Kubernetes clusters", description: "Direct API connections and outbound cluster agents" },
    { id: "namespaces", title: "Namespace access", description: "Namespaces this workspace can deploy to" },
    { id: "credentials", title: "Credentials", description: "Encrypted Git and Kubernetes secrets" },
    { id: "shares", title: "Shared connections", description: "Review connection offers and workspace access" },
    { id: "oidc", title: "OIDC providers", description: "Organization sign-in and group mapping", adminOnly: true },
    { id: "users", title: "Platform users", description: "Accounts and access on this JustCD instance", adminOnly: true },
  ]
  const visibleSections = sections.filter((item) => workspaceScoped ? workspaceSections.includes(item.id) : !workspaceSections.includes(item.id))
  const current = visibleSections.find((item) => item.id === section)
  const sectionHref = (id: string) => workspaceScoped ? `/workspaces/${fixedWorkspaceId}/connections/${id}` : `/settings/${id}`
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
      const initialCluster = clusterList.items[0]
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
    if (!workspaceId || !clusterId) return
    let active = true
    api<ListResponse<NamespaceBinding>>(`/api/v1/clusters/${encodeURIComponent(clusterId)}/bindings?workspaceId=${encodeURIComponent(workspaceId)}`)
      .then((result) => {
        if (!active) return
        setBindings(result.items)
        setLoadedBindingsKey(`${workspaceId}:${clusterId}`)
      })
      .catch((cause) => { if (active) setError("bindings", cause) })
    return () => { active = false }
  }, [workspaceId, clusterId, ticks.bindings, setError])

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

  const workspace = useMemo(() => workspaces.find((item) => item.id === workspaceId), [workspaces, workspaceId])
  const globalKubeCredentials = credentials.filter((item) => !item.workspaceId && ["kubernetes-token", "kubeconfig"].includes(item.kind))
  const gitCredentials = credentials.filter((item) => ["git-ssh", "git-https"].includes(item.kind))
  const namespaceCredentials = credentials.filter((item) => ["kubernetes-token", "kubeconfig"].includes(item.kind))

  const action: ActionRunner = async (work, success, after) => {
    setBusy(true)
    try {
      const result = await work()
      after?.(result)
      toast.success(typeof success === "function" ? success(result) : success)
      return true
    } catch (cause) {
      toast.error(errorMessage(cause), cause)
      return false
    } finally {
      setBusy(false)
    }
  }

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
    ) : section === "namespaces" && clusterId ? (
      <Button type="button" onClick={() => setNamespaceDialogOpen(true)}>
        <HugeiconsIcon icon={PlusSignIcon} strokeWidth={2} aria-hidden="true" />
        Grant namespace access
      </Button>
    ) : null

  // A failed load must never render as an empty list, so it replaces the section content.
  const sectionError: { kind: LoadKind; title: string } | null = errors.base !== undefined
    ? { kind: "base", title: "Could not load settings" }
    : workspaceScoped && errors.scope !== undefined
      ? { kind: "scope", title: "Could not load workspace connections" }
      : section === "namespaces" && errors.bindings !== undefined
        ? { kind: "bindings", title: "Could not load namespace access" }
        : current?.adminOnly && user?.isAdmin && errors.providers !== undefined && section === "oidc"
          ? { kind: "providers", title: "Could not load OIDC providers" }
          : null

  const adminVisibleSections = visibleSections.filter((item) => !item.adminOnly || user?.isAdmin)
  const providersPending = section === "oidc" && !providersLoaded && errors.providers === undefined

  return (
    <>
      <PageHeading
        title={current?.title ?? (workspaceScoped ? "Workspace connections" : "Instance settings")}
        description={
          current?.description ??
          (workspaceScoped ? "Repositories, Kubernetes access, and credentials for this workspace." : "Sign-in providers and accounts for the JustCD instance.")
        }
        actions={headerAction}
      />
      {loading && !sectionError ? (
        <SectionSkeleton rows={section ? 3 : 2} label="Loading settings" />
      ) : sectionError ? (
        <LoadErrorNotice error={errors[sectionError.kind]} title={sectionError.title} onRetry={() => retry(sectionError.kind)} />
      ) : section && !current ? (
        <EmptyState title="Section not found" description="Choose a settings section to continue." href="/settings" action="View settings" />
      ) : (
        <div className={current && !workspaceScoped ? "grid items-start gap-8 lg:grid-cols-[200px_minmax(0,1fr)]" : ""}>
          {current && !workspaceScoped && (
            <nav
              aria-label="Settings sections"
              className="flex min-w-0 items-center gap-2 overflow-x-auto rounded-xl border bg-card p-2 pr-10 [mask-image:linear-gradient(to_right,black_calc(100%-2.5rem),transparent)] lg:sticky lg:top-24 lg:block lg:overflow-visible lg:pr-2 lg:[mask-image:none]"
            >
              <Link
                href="/settings"
                className="flex shrink-0 items-center gap-1.5 rounded-lg px-3 py-3 text-sm font-medium whitespace-nowrap text-muted-foreground hover:bg-muted lg:mb-2 lg:border-b"
              >
                <HugeiconsIcon icon={ArrowLeft01Icon} strokeWidth={2} className="size-4" aria-hidden="true" />
                All settings
              </Link>
              {navGroups
                .filter((group) => group.ids.some((id) => adminVisibleSections.some((item) => item.id === id)))
                .map((group) => (
                  <div key={group.title} className="flex shrink-0 items-center gap-2 lg:mb-2 lg:block">
                    <p className="px-2 text-sm font-semibold whitespace-nowrap text-muted-foreground lg:px-3 lg:py-2 lg:tracking-wider lg:uppercase">{group.title}</p>
                    {adminVisibleSections
                      .filter((item) => group.ids.includes(item.id))
                      .map((item) => (
                        <Link
                          key={item.id}
                          href={sectionHref(item.id)}
                          aria-current={section === item.id ? "page" : undefined}
                          className={`block shrink-0 rounded-lg px-3 py-2.5 text-sm font-medium whitespace-nowrap transition-colors lg:mb-1 ${section === item.id ? "bg-primary/10 text-primary" : "bg-muted/40 text-muted-foreground hover:bg-muted hover:text-foreground"}`}
                        >
                          {item.title}
                        </Link>
                      ))}
                  </div>
                ))}
            </nav>
          )}
          <div className="min-w-0">
            {current?.adminOnly && user?.isAdmin && <InstanceNotice />}
            {current?.adminOnly && !user?.isAdmin && (
              <EmptyState
                title="Administrator access required"
                description="Only instance administrators can manage sign-in providers and local accounts."
                href="/settings"
                action="Back to settings"
              />
            )}
            {workspaceScoped && !workspace && (
              <EmptyState title="Workspace unavailable" description="You need workspace access to manage its connections." href="/workspaces" action="View workspaces" />
            )}
            {!section && !user?.isAdmin ? (
              <EmptyState title="Administrator access required" description="Instance settings are available to administrators. Workspace connections live on each workspace page." href="/workspaces" action="View workspaces" />
            ) : !section ? (
              <>
                <InstanceNotice />
                <Panel title="Authentication & users" description="Control how people sign in to JustCD." className="overflow-hidden">
                  <ul className="divide-y">
                    {adminVisibleSections.map((item) => (
                      <li key={item.id}>
                        <Link href={sectionHref(item.id)} className="group flex items-center gap-4 px-5 py-4 outline-none transition-colors hover:bg-muted/40 focus-visible:bg-muted/40 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset">
                          <span aria-hidden="true" className="grid size-9 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground">
                            <WorkspaceIcon name="shield" className="size-4" />
                          </span>
                          <span className="min-w-0 flex-1">
                            <h3 className="text-sm font-medium group-hover:text-primary">{item.title}</h3>
                            <p className="mt-0.5 text-sm text-muted-foreground">{item.description}</p>
                          </span>
                          <HugeiconsIcon icon={ArrowRight01Icon} strokeWidth={2} aria-hidden="true" className="size-4 shrink-0 text-muted-foreground group-hover:text-primary" />
                        </Link>
                      </li>
                    ))}
                  </ul>
                </Panel>
              </>
            ) : (
              <div className="settings-content min-w-0">
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
                {workspaceScoped && workspace && section === "clusters" && !scopeLoading && (
                  <ClusterPanel
                    key={workspace.id}
                    user={user}
                    workspace={workspace}
                    clusters={clusters}
                    globalCredentials={globalKubeCredentials}
                    workspaceCredentials={namespaceCredentials.filter((item) => item.workspaceId === workspaceId)}
                    busy={busy}
                    action={action}
                    onUpdated={(cluster) => setClusters((items) => items.map((item) => (item.id === cluster.id ? cluster : item)))}
                    onDeleted={(value) => setClusters((items) => items.filter((item) => item.id !== value))}
                  />
                )}
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
                  <WorkspaceConnectionShares workspace={workspace} workspaces={workspaces} clusters={clusters} sources={sources} credentials={credentials} />
                )}
                {workspaceScoped && workspace && section === "namespaces" && !scopeLoading && bindingsLoading && (
                  <SectionSkeleton label="Loading namespace access" />
                )}
                {workspaceScoped && workspace && section === "namespaces" && !scopeLoading && !bindingsLoading && (
                  <NamespacePanel
                    key={`${workspaceId}:${clusterId}`}
                    workspace={workspace}
                    cluster={clusters.find((item) => item.id === clusterId)}
                    credentials={namespaceCredentials}
                    bindings={bindings}
                    clusters={clusters}
                    activeClusterId={clusterId}
                    onClusterChange={(value) => {
                      setErrors((current) => ({ ...current, bindings: undefined }))
                      setClusterId(value)
                    }}
                    busy={busy}
                    action={action}
                    createOpen={namespaceDialogOpen}
                    onCreateOpenChange={setNamespaceDialogOpen}
                    onCreated={(binding) => setBindings((items) => [...items, binding].sort((a, b) => a.namespace.localeCompare(b.namespace)))}
                    onUpdated={(binding) => setBindings((items) => items.map((item) => (item.namespace === binding.namespace ? binding : item)))}
                    onDeleted={(value) => setBindings((items) => items.filter((item) => item.namespace !== value))}
                  />
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
          </div>
        </div>
      )}
    </>
  )
}
