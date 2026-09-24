"use client"

import { useEffect, useMemo, useState } from "react"
import { useRouter } from "next/navigation"
import { usePathname } from "next/navigation"
import Link from "next/link"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { EmptyState, FormField, PageHeading, Panel } from "@/components/ui-kit"
import { api, apiPost, errorMessage } from "@/lib/api"
import type { Cluster, Credential, GitSource, ListResponse, NamespaceBinding, OIDCProvider, Project, User } from "@/lib/types"

const inputClass = "min-h-24 w-full rounded-lg border bg-background px-3 py-2 font-mono text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring"
const selectClass = "h-9 w-full rounded-lg border bg-background px-3 text-sm"

export default function SettingsPage() {
  const router = useRouter()
  const pathname = usePathname()
  const section = pathname.split("/")[2] ?? ""
  const sections = [
    { id: "git-sources", title: "Git sources", description: "Repositories tracked by your projects" },
    { id: "clusters", title: "Kubernetes clusters", description: "Direct API endpoints and cluster credentials" },
    { id: "namespaces", title: "Namespace bindings", description: "Project targets and per-namespace access" },
    { id: "credentials", title: "Credentials", description: "Encrypted Git and Kubernetes secrets" },
    { id: "oidc", title: "OIDC providers", description: "Organization sign-in and group mapping", adminOnly: true },
    { id: "users", title: "Local users", description: "Accounts managed by JustCD", adminOnly: true },
  ]
  const current = sections.find((item) => item.id === section)
  const [user, setUser] = useState<User | null>(null)
  const [projects, setProjects] = useState<Project[]>([])
  const [projectId, setProjectId] = useState("")
  const [clusters, setClusters] = useState<Cluster[]>([])
  const [clusterId, setClusterId] = useState("")
  const [credentials, setCredentials] = useState<Credential[]>([])
  const [sources, setSources] = useState<GitSource[]>([])
  const [bindings, setBindings] = useState<NamespaceBinding[]>([])
  const [providers, setProviders] = useState<OIDCProvider[]>([])
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const [busy, setBusy] = useState(false)
  const [loading, setLoading] = useState(true)

  async function loadBase() {
    const [session, projectList, clusterList] = await Promise.all([
      api<{ user: User }>("/api/v1/auth/session"),
      api<ListResponse<Project>>("/api/v1/projects"),
      api<ListResponse<Cluster>>("/api/v1/clusters"),
    ])
    setUser(session.user)
    const writable = projectList.items.filter((project) => project.role !== "viewer")
    setProjects(writable)
    setClusters(clusterList.items)
    const queryId = new URLSearchParams(window.location.search).get("projectId") ?? ""
    const selected = writable.find((project) => project.id === queryId) ?? writable[0]
    if (selected) setProjectId(selected.id)
    const initialCluster = clusterList.items[0]
    if (initialCluster) setClusterId(initialCluster.id)
  }

  useEffect(() => {
    Promise.resolve().then(loadBase).catch((cause) => setError(errorMessage(cause))).finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    if (!projectId) return
    Promise.all([
      api<ListResponse<Credential>>(`/api/v1/credentials?projectId=${encodeURIComponent(projectId)}`),
      api<ListResponse<GitSource>>(`/api/v1/git-sources?projectId=${encodeURIComponent(projectId)}`),
    ]).then(([credentialList, sourceList]) => { setCredentials(credentialList.items); setSources(sourceList.items) }).catch((cause) => setError(errorMessage(cause)))
  }, [projectId])

  useEffect(() => {
    if (!projectId || !clusterId) { Promise.resolve().then(() => setBindings([])); return }
    api<ListResponse<NamespaceBinding>>(`/api/v1/clusters/${encodeURIComponent(clusterId)}/bindings?projectId=${encodeURIComponent(projectId)}`).then((result) => setBindings(result.items)).catch(() => setBindings([]))
  }, [projectId, clusterId])

  useEffect(() => {
    if (!user?.isAdmin) return
    api<ListResponse<OIDCProvider>>("/api/v1/admin/oidc-providers").then((result) => setProviders(result.items)).catch(() => setProviders([]))
  }, [user])

  const project = useMemo(() => projects.find((item) => item.id === projectId), [projects, projectId])
  const globalKubeCredentials = credentials.filter((item) => !item.projectId && ["kubernetes-token", "kubeconfig"].includes(item.kind))
  const gitCredentials = credentials.filter((item) => ["git-ssh", "git-https"].includes(item.kind))
  const namespaceCredentials = credentials.filter((item) => ["kubernetes-token", "kubeconfig"].includes(item.kind))

  async function action<T>(work: () => Promise<T>, success: string, after?: (value: T) => void) {
    setBusy(true); setError(""); setNotice("")
    try { const result = await work(); after?.(result); setNotice(success) }
    catch (cause) { setError(errorMessage(cause)) }
    finally { setBusy(false) }
  }

  return <>
    <PageHeading title={current?.title ?? "Settings"} description={current?.description ?? "Manage your delivery connections and access in focused sections."} />
    {error && <div role="alert" className="mb-4 rounded-lg border border-destructive/20 bg-destructive/5 px-4 py-3 text-sm text-destructive">{error}</div>}
    {notice && <div role="status" className="mb-4 rounded-lg border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950/40 dark:text-emerald-200">{notice}</div>}
    {loading ? <div role="status" className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">{Array.from({ length: section ? 1 : 6 }, (_, index) => <div key={index} className="h-28 animate-pulse rounded-xl border bg-muted/40" />)}</div> : <>

    {section && !current ? <EmptyState title="Section not found" description="Choose a settings section to continue." href="/settings" action="View settings" /> : <>
    {current && <nav aria-label="Settings sections" className="mb-5 flex flex-wrap gap-2"><Link href="/settings" className="rounded-lg border bg-card px-3 py-2 text-xs font-medium text-muted-foreground hover:bg-muted hover:text-foreground">All settings</Link>{sections.filter((item) => !item.adminOnly || user?.isAdmin).map((item) => <Link key={item.id} href={`/settings/${item.id}${projectId ? `?projectId=${encodeURIComponent(projectId)}` : ""}`} aria-current={section === item.id ? "page" : undefined} className={`rounded-lg border px-3 py-2 text-xs font-medium transition-colors ${section === item.id ? "border-primary/30 bg-primary/8 text-primary" : "bg-card text-muted-foreground hover:bg-muted hover:text-foreground"}`}>{item.title}</Link>)}</nav>}
    {(section === "git-sources" || section === "namespaces" || section === "credentials") && (projects.length > 0 ? <div className="mb-5 flex max-w-4xl flex-wrap items-end gap-3 rounded-xl border bg-card p-4">
      <FormField label="Active project" htmlFor="active-project"><select id="active-project" className={`${selectClass} min-w-[240px]`} value={projectId} onChange={(event) => { setProjectId(event.target.value); router.replace(`${pathname}?projectId=${encodeURIComponent(event.target.value)}`) }}>{projects.map((item) => <option value={item.id} key={item.id}>{item.name} · {item.role}</option>)}</select></FormField>
      {section === "namespaces" && <FormField label="Active cluster" htmlFor="active-cluster"><select id="active-cluster" className={`${selectClass} min-w-[240px]`} value={clusterId} onChange={(event) => setClusterId(event.target.value)}><option value="">Select cluster</option>{clusters.map((item) => <option value={item.id} key={item.id}>{item.name}</option>)}</select></FormField>}
      <span className="pb-2 text-xs text-muted-foreground">Editing <strong className="font-medium text-foreground">{project?.name}</strong></span>
    </div> : <div className="mb-5 rounded-xl border bg-card"><EmptyState title="Create a project first" description="Project-scoped settings need a delivery scope." href="/projects/new" action="Create project" /></div>)}

    {!section ? <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">{sections.filter((item) => !item.adminOnly || user?.isAdmin).map((item) => <Link key={item.id} href={`/settings/${item.id}${projectId ? `?projectId=${encodeURIComponent(projectId)}` : ""}`} className="group flex min-h-28 flex-col justify-between rounded-xl border bg-card p-5 transition-colors hover:border-primary/40 hover:bg-muted/30"><span className="text-sm font-semibold group-hover:text-primary">{item.title}</span><span className="mt-3 flex items-end justify-between gap-3 text-xs text-muted-foreground"><span>{item.description}</span><span aria-hidden="true" className="shrink-0 text-base">→</span></span></Link>)}</div> : <div className="max-w-4xl">
      {section === "credentials" && <CredentialPanel
        project={project}
        user={user}
        credentials={credentials}
        busy={busy}
        action={action}
        onCreated={(credential) => setCredentials((items) => [credential, ...items])}
      />}
      {section === "clusters" && <ClusterPanel
        user={user}
        clusters={clusters}
        globalCredentials={globalKubeCredentials}
        busy={busy}
        action={action}
        onCreated={(cluster) => { setClusters((items) => [...items, cluster]); setClusterId(cluster.id) }}
      />}
      {section === "git-sources" && <GitSourcePanel project={project} credentials={gitCredentials} sources={sources} busy={busy} action={action} onCreated={(source) => setSources((items) => [source, ...items])} />}
      {section === "namespaces" && <NamespacePanel project={project} cluster={clusters.find((item) => item.id === clusterId)} credentials={namespaceCredentials} bindings={bindings} busy={busy} action={action} onCreated={(binding) => setBindings((items) => [...items, binding].sort((a, b) => a.namespace.localeCompare(b.namespace)))} />}
      {section === "oidc" && user?.isAdmin && <OIDCPanel providers={providers} projects={projects} busy={busy} action={action} onCreated={(provider) => setProviders((items) => [...items, provider])} />}
      {section === "users" && user?.isAdmin && <LocalUsersPanel busy={busy} action={action} />}
    </div>}

    {section === "credentials" && <div className="mt-5 max-w-4xl rounded-xl border bg-card px-5 py-4"><div className="flex flex-col justify-between gap-2 sm:flex-row sm:items-center"><div><p className="text-xs font-semibold">Credentials never leave the server</p><p className="mt-1 max-w-3xl text-[11px] leading-5 text-muted-foreground">Secrets are encrypted at rest using JUSTCD_ENCRYPTION_KEY and are not returned to the browser. Git deploy keys require pinned known_hosts; kubeconfig exec plugins and token files are disabled.</p></div><span className="w-fit rounded-full border border-emerald-200 bg-emerald-50 px-2.5 py-1 text-[9px] font-medium uppercase tracking-wide text-emerald-700">Encrypted</span></div></div>}
    </>}
    </>}
  </>
}

function CredentialPanel({ project, user, credentials, busy, action, onCreated }: {
  project?: Project
  user: User | null
  credentials: Credential[]
  busy: boolean
  action: <T>(work: () => Promise<T>, success: string, after?: (value: T) => void) => Promise<void>
  onCreated: (value: Credential) => void
}) {
  const [name, setName] = useState("")
  const [kind, setKind] = useState<Credential["kind"]>("kubernetes-token")
  const [secretOne, setSecretOne] = useState("")
  const [secretTwo, setSecretTwo] = useState("")
  const [global, setGlobal] = useState(false)
  const isAdmin = user?.isAdmin ?? false
  const fields = kind === "git-ssh" ? { first: "Private key", second: "Pinned known_hosts", firstKey: "privateKey", secondKey: "knownHosts" } : kind === "git-https" ? { first: "Access token", second: "", firstKey: "token", secondKey: "" } : kind === "kubeconfig" ? { first: "Static kubeconfig content", second: "", firstKey: "content", secondKey: "" } : { first: "Bearer token", second: "", firstKey: "token", secondKey: "" }
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const projectId = global ? undefined : project?.id
    await action(async () => {
      const result = await apiPost<Credential>("/api/v1/credentials", { projectId, name, kind, secret: { [fields.firstKey]: secretOne, ...(fields.secondKey ? { [fields.secondKey]: secretTwo } : {}) } })
      setName(""); setSecretOne(""); setSecretTwo("")
      return result
    }, "Encrypted credential added.", onCreated)
  }
  return <Panel title="Credentials" description="Add Kubernetes tokens or static kubeconfigs per namespace, and Git deploy credentials." className="self-start">
    <div className="divide-y">
      <form className="space-y-4 p-5" onSubmit={submit}>
        <div className="grid gap-3 sm:grid-cols-2">
          <FormField label="Name" htmlFor="credential-name"><Input id="credential-name" placeholder="production deploy token" value={name} onChange={(event) => setName(event.target.value)} required maxLength={100} /></FormField>
          <FormField label="Credential type" htmlFor="credential-kind"><select id="credential-kind" className={selectClass} value={kind} onChange={(event) => setKind(event.target.value as Credential["kind"])}><option value="kubernetes-token">Kubernetes bearer token</option><option value="kubeconfig">Static kubeconfig</option><option value="git-https">Git over HTTPS</option><option value="git-ssh">Git over SSH</option></select></FormField>
        </div>
        <FormField label={fields.first} htmlFor="credential-secret" hint={kind === "kubeconfig" ? "Exec plugins, auth-provider plugins, and token files are rejected." : undefined}><textarea id="credential-secret" className={inputClass} value={secretOne} onChange={(event) => setSecretOne(event.target.value)} required /></FormField>
        {fields.second && <FormField label={fields.second} htmlFor="credential-secret-two" hint="Host keys must be explicitly pinned; unknown hosts are not accepted."><textarea id="credential-secret-two" className={inputClass} value={secretTwo} onChange={(event) => setSecretTwo(event.target.value)} required /></FormField>}
        {isAdmin && <label className="flex items-center gap-2 text-[11px] text-muted-foreground"><input className="accent-primary" type="checkbox" checked={global} onChange={(event) => setGlobal(event.target.checked)} />Store as an instance-wide credential (admin only)</label>}
        {!global && !project && <p className="text-[11px] text-amber-700">Select a project to add a scoped credential.</p>}
        <Button size="sm" type="submit" disabled={busy || (!global && !project) || (global && !isAdmin)}>Add credential</Button>
      </form>
      <div className="p-5"><div className="mb-3 flex items-center justify-between"><p className="text-[11px] font-medium">Available credentials</p><span className="text-[10px] text-muted-foreground">{credentials.length} total</span></div>{credentials.length ? <div className="space-y-2">{credentials.map((credential) => <div key={credential.id} className="flex items-center gap-3 rounded-lg border px-3 py-2"><span className="grid size-7 place-items-center rounded-md bg-muted text-[10px]">{credential.kind.startsWith("git") ? "G" : "K"}</span><span className="min-w-0 flex-1"><span className="block truncate text-xs font-medium">{credential.name}</span><span className="block text-[9px] capitalize text-muted-foreground">{credential.kind.replaceAll("-", " ")} · {credential.projectId ? "project scoped" : "instance wide"}</span></span>{credential.expiresAt && <span className="text-[9px] text-muted-foreground">expires {new Date(credential.expiresAt).toLocaleDateString()}</span>}</div>)}</div> : <p className="text-[11px] text-muted-foreground">Credentials will be listed here by name only.</p>}</div>
    </div>
  </Panel>
}

function ClusterPanel({ user, clusters, globalCredentials, busy, action, onCreated }: {
  user: User | null
  clusters: Cluster[]
  globalCredentials: Credential[]
  busy: boolean
  action: <T>(work: () => Promise<T>, success: string, after?: (value: T) => void) => Promise<void>
  onCreated: (value: Cluster) => void
}) {
  const [name, setName] = useState("")
  const [apiServer, setApiServer] = useState("")
  const [caDataBase64, setCaDataBase64] = useState("")
  const [defaultCredentialId, setDefaultCredentialId] = useState("")
  const [clusterScopeCredentialId, setClusterScopeCredentialId] = useState("")
  const [insecure, setInsecure] = useState(false)
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    await action(async () => {
      const result = await apiPost<Cluster>("/api/v1/clusters", { name, apiServer, caDataBase64, insecureSkipVerify: insecure, defaultCredentialId: defaultCredentialId || undefined, clusterScopeCredentialId: clusterScopeCredentialId || undefined })
      setName(""); setApiServer(""); setCaDataBase64("")
      return result
    }, "Cluster connected. Add a namespace binding to a project before creating applications.", onCreated)
  }
  return <Panel title="Kubernetes clusters" description="Clusters are direct API connections. JustCD runs its reconciler outside the cluster." className="self-start">
    {clusters.length ? <div className="divide-y border-b">{clusters.map((cluster) => <div key={cluster.id} className="flex items-center gap-3 px-5 py-3"><span className="grid size-8 place-items-center rounded-lg bg-violet-500/10 text-xs text-violet-700">K8s</span><span className="min-w-0 flex-1"><span className="block text-xs font-medium">{cluster.name}</span><span className="block truncate font-mono text-[9px] text-muted-foreground">{cluster.apiServer}</span></span><span className="text-[9px] text-muted-foreground">{cluster.defaultCredentialId ? "default credential set" : "no default credential"}</span></div>)}</div> : null}
    {user?.isAdmin ? <form className="space-y-4 p-5" onSubmit={submit}>
      <div className="grid gap-3 sm:grid-cols-2"><FormField label="Cluster name" htmlFor="cluster-name"><Input id="cluster-name" placeholder="prod-eu-1" value={name} onChange={(event) => setName(event.target.value)} required /></FormField><FormField label="Kubernetes API URL" htmlFor="cluster-api"><Input id="cluster-api" type="url" placeholder="https://api.example.com:6443" value={apiServer} onChange={(event) => setApiServer(event.target.value)} required /></FormField></div>
      <FormField label="CA certificate (base64)" htmlFor="cluster-ca" hint="Optional when the system trust store is sufficient."><textarea id="cluster-ca" className={inputClass} value={caDataBase64} onChange={(event) => setCaDataBase64(event.target.value)} /></FormField>
      <div className="grid gap-3 sm:grid-cols-2"><FormField label="Default cluster credential" htmlFor="cluster-default"><select id="cluster-default" className={selectClass} value={defaultCredentialId} onChange={(event) => setDefaultCredentialId(event.target.value)}><option value="">None yet</option>{globalCredentials.map((credential) => <option key={credential.id} value={credential.id}>{credential.name}</option>)}</select></FormField><FormField label="Cluster-scope credential" htmlFor="cluster-scope"><select id="cluster-scope" className={selectClass} value={clusterScopeCredentialId} onChange={(event) => setClusterScopeCredentialId(event.target.value)}><option value="">Disabled (recommended)</option>{globalCredentials.map((credential) => <option key={credential.id} value={credential.id}>{credential.name}</option>)}</select></FormField></div>
      <label className="flex items-start gap-2 text-[11px] leading-4 text-muted-foreground"><input className="mt-0.5 accent-primary" type="checkbox" checked={insecure} onChange={(event) => setInsecure(event.target.checked)} />Skip TLS certificate verification <span className="text-amber-700">— only for temporary development clusters.</span></label>
      <Button size="sm" type="submit" disabled={busy}>Add cluster</Button>
    </form> : <div className="px-5 py-4 text-[11px] leading-5 text-muted-foreground">Only instance administrators can add cluster API endpoints and global credentials.</div>}
  </Panel>
}

function GitSourcePanel({ project, credentials, sources, busy, action, onCreated }: {
  project?: Project
  credentials: Credential[]
  sources: GitSource[]
  busy: boolean
  action: <T>(work: () => Promise<T>, success: string, after?: (value: T) => void) => Promise<void>
  onCreated: (value: GitSource) => void
}) {
  const [name, setName] = useState("")
  const [repositoryUrl, setRepositoryUrl] = useState("")
  const [credentialId, setCredentialId] = useState("")
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    await action(async () => {
      const source = await apiPost<GitSource>("/api/v1/git-sources", { projectId: project?.id, name, repositoryUrl, credentialId: credentialId || undefined })
      setName(""); setRepositoryUrl("")
      return source
    }, "Git source connected.", onCreated)
  }
  return <Panel title="Git sources" description="Configure repositories used by applications in the selected project." className="self-start">
    {sources.length ? <div className="divide-y border-b">{sources.map((source) => <div key={source.id} className="px-5 py-3"><p className="text-xs font-medium">{source.name}</p><p className="mt-0.5 truncate font-mono text-[10px] text-muted-foreground">{source.repositoryUrl}</p></div>)}</div> : null}
    <form className="space-y-4 p-5" onSubmit={submit}>
      <div className="grid gap-3 sm:grid-cols-2"><FormField label="Source name" htmlFor="source-name"><Input id="source-name" placeholder="platform-config" value={name} onChange={(event) => setName(event.target.value)} required /></FormField><FormField label="Repository URL" htmlFor="source-url"><Input id="source-url" placeholder="https://github.com/org/repo.git" value={repositoryUrl} onChange={(event) => setRepositoryUrl(event.target.value)} required /></FormField></div>
      <FormField label="Git credential" htmlFor="source-credential"><select id="source-credential" className={selectClass} value={credentialId} onChange={(event) => setCredentialId(event.target.value)}><option value="">Public repository</option>{credentials.map((credential) => <option key={credential.id} value={credential.id}>{credential.name} · {credential.kind}</option>)}</select></FormField>
      {!project && <p className="text-[11px] text-amber-700">Select a project to configure Git sources.</p>}
      {!credentials.length && <p className="text-[10px] text-muted-foreground">For private repositories, add a Git HTTPS token or pinned SSH key in the Credentials panel.</p>}
      <Button size="sm" type="submit" disabled={busy || !project || project.role !== "owner"}>Add Git source</Button>
    </form>
  </Panel>
}

function NamespacePanel({ project, cluster, credentials, bindings, busy, action, onCreated }: {
  project?: Project
  cluster?: Cluster
  credentials: Credential[]
  bindings: NamespaceBinding[]
  busy: boolean
  action: <T>(work: () => Promise<T>, success: string, after?: (value: T) => void) => Promise<void>
  onCreated: (value: NamespaceBinding) => void
}) {
  const [namespace, setNamespace] = useState("")
  const [credentialId, setCredentialId] = useState("")
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!project || !cluster) return
    await action(async () => {
      const binding = await apiPost<NamespaceBinding>(`/api/v1/clusters/${encodeURIComponent(cluster.id)}/bindings`, { projectId: project.id, namespace, credentialId: credentialId || undefined })
      setNamespace("")
      return binding
    }, "Namespace access binding added.", onCreated)
  }
  return <Panel title="Namespace bindings" description="Bind a project to specific namespaces and choose credentials per target." className="self-start">
    {bindings.length ? <div className="divide-y border-b">{bindings.map((binding) => <div key={binding.namespace} className="flex items-center justify-between gap-3 px-5 py-2.5"><span className="font-mono text-xs">{binding.namespace}</span><span className="text-[10px] text-muted-foreground">{binding.credentialId ? credentials.find((item) => item.id === binding.credentialId)?.name || "namespace credential" : "cluster default"}</span></div>)}</div> : <div className="px-5 pt-4 text-[11px] text-muted-foreground">{cluster ? "No namespace bindings on this cluster." : "Select a cluster."}</div>}
    <form className="space-y-4 p-5" onSubmit={submit}>
      <div className="grid gap-3 sm:grid-cols-2"><FormField label="Namespace" htmlFor="namespace-name" hint="Kubernetes namespace, up to 63 characters."><Input id="namespace-name" placeholder="payments" value={namespace} onChange={(event) => setNamespace(event.target.value)} required maxLength={63} /></FormField><FormField label="Credential for this namespace" htmlFor="namespace-credential"><select id="namespace-credential" className={selectClass} value={credentialId} onChange={(event) => setCredentialId(event.target.value)}><option value="">Use cluster default</option>{credentials.map((credential) => <option key={credential.id} value={credential.id}>{credential.name}</option>)}</select></FormField></div>
      {!cluster?.defaultCredentialId && !credentialId && <p className="text-[10px] text-amber-700">Choose a namespace credential or configure a cluster default credential.</p>}
      <Button size="sm" type="submit" disabled={busy || !project || project.role !== "owner" || !cluster}>Bind namespace</Button>
    </form>
  </Panel>
}

function OIDCPanel({ providers, projects, busy, action, onCreated }: {
  providers: OIDCProvider[]
  projects: Project[]
  busy: boolean
  action: <T>(work: () => Promise<T>, success: string, after?: (value: T) => void) => Promise<void>
  onCreated: (value: OIDCProvider) => void
}) {
  const [name, setName] = useState("")
  const [issuer, setIssuer] = useState("")
  const [clientId, setClientId] = useState("")
  const [clientSecret, setClientSecret] = useState("")
  const [groupsClaim, setGroupsClaim] = useState("groups")
  const [providerId, setProviderId] = useState("")
  const [projectId, setProjectId] = useState("")
  const [groupName, setGroupName] = useState("")
  const [groupRole, setGroupRole] = useState("viewer")
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    await action(async () => {
      const provider = await apiPost<OIDCProvider>("/api/v1/admin/oidc-providers", { name, issuer, clientId, clientSecret, groupsClaim })
      setName(""); setClientId(""); setClientSecret("")
      return provider
    }, "OIDC provider added. Verify the callback URL in your identity provider.", onCreated)
  }
  async function mapGroup(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const selectedProvider = providerId || providers[0]?.id
    const selectedProject = projectId || projects[0]?.id
    if (!selectedProvider || !selectedProject) return
    await action(() => apiPost(`/api/v1/admin/oidc-providers/${encodeURIComponent(selectedProvider)}/groups`, { group: groupName, projectId: selectedProject, role: groupRole }), "OIDC group mapping saved.")
    setGroupName("")
  }
  return <Panel title="OIDC providers" description="Let users sign in with one or more OpenID Connect providers.">
    {providers.length ? <div className="divide-y border-b">{providers.map((provider) => <div key={provider.id} className="flex items-center justify-between gap-4 px-5 py-3"><div className="min-w-0"><span className="block text-xs font-medium">{provider.name}</span><span className="mt-0.5 block truncate font-mono text-[9px] text-muted-foreground">Callback: {provider.redirectUrl}</span></div><span className={`rounded-full border px-2 py-0.5 text-[9px] ${provider.enabled ? "border-emerald-200 bg-emerald-50 text-emerald-700" : "text-muted-foreground"}`}>{provider.enabled ? "enabled" : "disabled"}</span></div>)}</div> : null}
    <form className="space-y-4 p-5" onSubmit={submit}>
      <div className="grid gap-3 sm:grid-cols-2"><FormField label="Display name" htmlFor="oidc-name"><Input id="oidc-name" placeholder="Company SSO" value={name} onChange={(event) => setName(event.target.value)} required /></FormField><FormField label="Issuer URL" htmlFor="oidc-issuer"><Input id="oidc-issuer" type="url" placeholder="https://id.example.com" value={issuer} onChange={(event) => setIssuer(event.target.value)} required /></FormField><FormField label="Client ID" htmlFor="oidc-client"><Input id="oidc-client" value={clientId} onChange={(event) => setClientId(event.target.value)} required /></FormField><FormField label="Groups claim" htmlFor="oidc-groups"><Input id="oidc-groups" value={groupsClaim} onChange={(event) => setGroupsClaim(event.target.value)} /></FormField></div>
      <FormField label="Client secret" htmlFor="oidc-secret"><Input id="oidc-secret" type="password" autoComplete="new-password" value={clientSecret} onChange={(event) => setClientSecret(event.target.value)} required /></FormField>
      <p className="text-[10px] leading-4 text-muted-foreground">Callback URL: use the JustCD public URL plus <code className="font-mono">/api/v1/auth/oidc/&lt;provider-id&gt;/callback</code>. It is available after the provider is created.</p>
      <Button size="sm" type="submit" disabled={busy}>Add OIDC provider</Button>
    </form>
    {providers.length > 0 && projects.length > 0 && <form className="space-y-4 border-t p-5" onSubmit={mapGroup}>
      <div><p className="text-xs font-medium">Map an identity group</p><p className="mt-1 text-[10px] text-muted-foreground">Verified ID-token groups grant the selected project role at each login.</p></div>
      <div className="grid gap-3 sm:grid-cols-2"><FormField label="Provider" htmlFor="mapping-provider"><select id="mapping-provider" className={selectClass} value={providerId || providers[0]?.id} onChange={(event) => setProviderId(event.target.value)}>{providers.map((provider) => <option value={provider.id} key={provider.id}>{provider.name}</option>)}</select></FormField><FormField label="Project" htmlFor="mapping-project"><select id="mapping-project" className={selectClass} value={projectId || projects[0]?.id} onChange={(event) => setProjectId(event.target.value)}>{projects.map((project) => <option value={project.id} key={project.id}>{project.name}</option>)}</select></FormField><FormField label="Group claim value" htmlFor="mapping-group"><Input id="mapping-group" placeholder="platform-deployers" value={groupName} onChange={(event) => setGroupName(event.target.value)} required /></FormField><FormField label="Granted role" htmlFor="mapping-role"><select id="mapping-role" className={selectClass} value={groupRole} onChange={(event) => setGroupRole(event.target.value)}><option value="viewer">Viewer</option><option value="deployer">Deployer</option><option value="owner">Owner</option></select></FormField></div>
      <Button size="sm" type="submit" disabled={busy || !groupName}>Save group mapping</Button>
    </form>}
  </Panel>
}

function LocalUsersPanel({ busy, action }: {
  busy: boolean
  action: <T>(work: () => Promise<T>, success: string, after?: (value: T) => void) => Promise<void>
}) {
  const [email, setEmail] = useState("")
  const [displayName, setDisplayName] = useState("")
  const [password, setPassword] = useState("")
  const [isAdmin, setIsAdmin] = useState(false)
  const [users, setUsers] = useState<User[]>([])
  useEffect(() => { api<ListResponse<User>>("/api/v1/admin/users").then((result) => setUsers(result.items)).catch(() => undefined) }, [])
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    await action(async () => {
      const user = await apiPost<User>("/api/v1/admin/users", { email, displayName, password, isAdmin })
      setEmail(""); setDisplayName(""); setPassword(""); setIsAdmin(false)
      setUsers((items) => [...items, user])
      return user
    }, "Local user added.")
  }
  return <Panel title="Local users" description="Create accounts managed directly by JustCD.">
    {users.length > 0 && <div className="divide-y border-b">{users.map((user) => <div key={user.id} className="flex items-center gap-3 px-5 py-2.5"><span className="min-w-0 flex-1 truncate text-xs">{user.displayName || user.email} <span className="text-muted-foreground">· {user.email}</span></span><span className="text-[9px] text-muted-foreground">{user.isAdmin ? "admin" : "user"}</span></div>)}</div>}
    <form className="space-y-4 p-5" onSubmit={submit}>
      <div className="grid gap-3 sm:grid-cols-2"><FormField label="Email" htmlFor="user-email"><Input id="user-email" type="email" value={email} onChange={(event) => setEmail(event.target.value)} required /></FormField><FormField label="Display name" htmlFor="user-display"><Input id="user-display" value={displayName} onChange={(event) => setDisplayName(event.target.value)} /></FormField></div>
      <FormField label="Password" htmlFor="user-password" hint="At least 12 characters. Passwords are stored as Argon2id hashes."><Input id="user-password" type="password" autoComplete="new-password" minLength={12} value={password} onChange={(event) => setPassword(event.target.value)} required /></FormField>
      <label className="flex items-center gap-2 text-[11px]"><input type="checkbox" className="accent-primary" checked={isAdmin} onChange={(event) => setIsAdmin(event.target.checked)} />Instance administrator</label>
      <Button size="sm" type="submit" disabled={busy}>Add local user</Button>
    </form>
  </Panel>
}
