"use client"

import { useEffect, useMemo, useState } from "react"
import { useRouter } from "next/navigation"
import { usePathname } from "next/navigation"
import Link from "next/link"
import { WorkspaceIcon } from "@/components/workspace-ui"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { EmptyState, FormField, PageHeading } from "@/components/ui-kit"
import { api, apiPost, errorMessage } from "@/lib/api"
import type {
  Cluster,
  Credential,
  GitSource,
  ListResponse,
  NamespaceBinding,
  OIDCProvider,
  Project,
  User,
} from "@/lib/types"

const inputClass = "min-h-24 font-mono text-xs"

export default function SettingsPage() {
  const router = useRouter()
  const pathname = usePathname()
  const section = pathname.split("/")[2] ?? ""
  const sections = [
    {
      id: "git-sources",
      title: "Git sources",
      description: "Repositories tracked by your projects",
    },
    {
      id: "clusters",
      title: "Kubernetes clusters",
      description: "Direct API endpoints and cluster credentials",
    },
    {
      id: "namespaces",
      title: "Namespace bindings",
      description: "Project targets and per-namespace access",
    },
    {
      id: "credentials",
      title: "Credentials",
      description: "Encrypted Git and Kubernetes secrets",
    },
    {
      id: "oidc",
      title: "OIDC providers",
      description: "Organization sign-in and group mapping",
      adminOnly: true,
    },
    {
      id: "users",
      title: "Local users",
      description: "Accounts managed by JustCD",
      adminOnly: true,
    },
  ]
  const current = sections.find((item) => item.id === section)
  const [user, setUser] = useState<User | null>(null)
  const [projects, setProjects] = useState<Project[]>([])
  const [projectId, setProjectId] = useState("")
  const [clusters, setClusters] = useState<Cluster[]>([])
  const [clusterId, setClusterId] = useState("")
  const [credentials, setCredentials] = useState<Credential[]>([])
  const [loadedProjectId, setLoadedProjectId] = useState("")
  const [loadedBindingsKey, setLoadedBindingsKey] = useState("")
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
    const writable = projectList.items.filter(
      (project) => project.role !== "viewer"
    )
    setProjects(writable)
    setClusters(clusterList.items)
    const queryId =
      new URLSearchParams(window.location.search).get("projectId") ?? ""
    const selected =
      writable.find((project) => project.id === queryId) ?? writable[0]
    if (selected) setProjectId(selected.id)
    const initialCluster = clusterList.items[0]
    if (initialCluster) setClusterId(initialCluster.id)
  }

  useEffect(() => {
    Promise.resolve()
      .then(loadBase)
      .catch((cause) => setError(errorMessage(cause)))
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    if (!projectId) return
    let active = true
    Promise.all([
      api<ListResponse<Credential>>(
        `/api/v1/credentials?projectId=${encodeURIComponent(projectId)}`
      ),
      api<ListResponse<GitSource>>(
        `/api/v1/git-sources?projectId=${encodeURIComponent(projectId)}`
      ),
    ])
      .then(([credentialList, sourceList]) => {
        if (active) {
          setCredentials(credentialList.items)
          setSources(sourceList.items)
          setLoadedProjectId(projectId)
        }
      })
      .catch((cause) => {
        if (active) setError(errorMessage(cause))
      })
    return () => {
      active = false
    }
  }, [projectId])

  useEffect(() => {
    if (!projectId || !clusterId) return
    let active = true
    api<ListResponse<NamespaceBinding>>(
      `/api/v1/clusters/${encodeURIComponent(clusterId)}/bindings?projectId=${encodeURIComponent(projectId)}`
    )
      .then((result) => {
        if (active) {
          setBindings(result.items)
          setLoadedBindingsKey(`${projectId}:${clusterId}`)
        }
      })
      .catch((cause) => {
        if (active) setError(errorMessage(cause))
      })
    return () => {
      active = false
    }
  }, [projectId, clusterId])

  useEffect(() => {
    if (!user?.isAdmin) return
    let active = true
    api<ListResponse<OIDCProvider>>("/api/v1/admin/oidc-providers")
      .then((result) => {
        if (active) setProviders(result.items)
      })
      .catch((cause) => {
        if (active) setError(errorMessage(cause))
      })
    return () => {
      active = false
    }
  }, [user])
  const scopeLoading = !!projectId && loadedProjectId !== projectId
  const bindingsLoading =
    !!projectId &&
    !!clusterId &&
    loadedBindingsKey !== `${projectId}:${clusterId}`

  const project = useMemo(
    () => projects.find((item) => item.id === projectId),
    [projects, projectId]
  )
  const globalKubeCredentials = credentials.filter(
    (item) =>
      !item.projectId && ["kubernetes-token", "kubeconfig"].includes(item.kind)
  )
  const gitCredentials = credentials.filter((item) =>
    ["git-ssh", "git-https"].includes(item.kind)
  )
  const namespaceCredentials = credentials.filter((item) =>
    ["kubernetes-token", "kubeconfig"].includes(item.kind)
  )

  async function action<T>(
    work: () => Promise<T>,
    success: string,
    after?: (value: T) => void
  ) {
    setBusy(true)
    setError("")
    setNotice("")
    try {
      const result = await work()
      after?.(result)
      setNotice(success)
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <PageHeading
        title={current?.title ?? "Settings"}
        description={
          current?.description ??
          "The connections and people behind your deployments."
        }
      />
      {error && (
        <div
          role="alert"
          className="mb-4 rounded-lg border border-destructive/20 bg-destructive/5 px-4 py-3 text-sm text-destructive"
        >
          {error}
        </div>
      )}
      {notice && (
        <div
          role="status"
          className="mb-4 rounded-lg border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950/40 dark:text-emerald-200"
        >
          {notice}
        </div>
      )}
      {loading ? (
        <div role="status" className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {Array.from({ length: section ? 1 : 6 }, (_, index) => (
            <div
              key={index}
              className="h-28 animate-pulse rounded-xl border bg-muted/40"
            />
          ))}
        </div>
      ) : (
        <>
          {section && !current ? (
            <EmptyState
              title="Section not found"
              description="Choose a settings section to continue."
              href="/settings"
              action="View settings"
            />
          ) : (
            <>
              <div
                className={
                  current
                    ? "grid items-start gap-8 lg:grid-cols-[200px_minmax(0,1fr)]"
                    : ""
                }
              >
                {current && (
                  <nav
                    aria-label="Settings sections"
                    className="flex min-w-0 gap-2 overflow-x-auto rounded-xl border bg-card p-2 lg:sticky lg:top-24 lg:block"
                  >
                    <Link
                      href={`/settings${projectId ? `?projectId=${encodeURIComponent(projectId)}` : ""}`}
                      className="block shrink-0 rounded-lg px-3 py-3 text-xs font-medium whitespace-nowrap text-muted-foreground hover:bg-muted lg:mb-2 lg:border-b"
                    >
                      ← All settings
                    </Link>
                    {[
                      {
                        title: "Delivery",
                        ids: ["git-sources", "clusters", "namespaces"],
                      },
                      {
                        title: "Security & access",
                        ids: ["credentials", "oidc", "users"],
                      },
                    ].map((group) => (
                      <div
                        key={group.title}
                        className="flex shrink-0 gap-2 lg:mb-2 lg:block"
                      >
                        <p className="hidden px-3 py-2 text-[10px] font-semibold tracking-wider text-muted-foreground uppercase lg:block">
                          {group.title}
                        </p>
                        {sections
                          .filter(
                            (item) =>
                              group.ids.includes(item.id) &&
                              (!item.adminOnly || user?.isAdmin)
                          )
                          .map((item) => (
                            <Link
                              key={item.id}
                              href={`/settings/${item.id}${projectId ? `?projectId=${encodeURIComponent(projectId)}` : ""}`}
                              aria-current={
                                section === item.id ? "page" : undefined
                              }
                              className={`block shrink-0 rounded-lg px-3 py-2.5 text-xs font-medium whitespace-nowrap transition-colors lg:mb-1 ${section === item.id ? "bg-primary/10 text-primary" : "bg-muted/40 text-muted-foreground hover:bg-muted hover:text-foreground"}`}
                            >
                              {item.title}
                            </Link>
                          ))}
                      </div>
                    ))}
                  </nav>
                )}
                <div className="min-w-0">
                  {current?.adminOnly && (
                    <div className="mb-6 flex items-center gap-3 rounded-xl border bg-card p-4">
                      <WorkspaceIcon
                        name="shield"
                        className="size-5 text-primary"
                      />
                      <div>
                        <p className="text-xs font-semibold">
                          Instance settings
                        </p>
                        <p className="mt-1 text-xs text-muted-foreground">
                          These settings apply to the whole workspace, across
                          all projects.
                        </p>
                      </div>
                    </div>
                  )}
                  {current?.adminOnly && !user?.isAdmin && (
                    <EmptyState
                      title="Administrator access required"
                      description="Only instance administrators can manage sign-in providers and local accounts."
                      href="/settings"
                      action="Back to settings"
                    />
                  )}

                  {(section === "git-sources" ||
                    section === "clusters" ||
                    section === "namespaces" ||
                    section === "credentials") &&
                    (projects.length > 0 ? (
                      <div className="mb-6 flex flex-wrap items-end gap-4 rounded-xl border bg-muted/40 p-5">
                        <FormField
                          label="Active project"
                          htmlFor="active-project"
                        >
                          <FormSelect
                            id="active-project"
                            className="w-full sm:min-w-[220px]"
                            value={projectId}
                            onValueChange={(value) => {
                              setError("")
                              setNotice("")
                              setProjectId(value)
                              router.replace(
                                `${pathname}?projectId=${encodeURIComponent(value)}`
                              )
                            }}
                            items={projects.map((item) => ({
                              value: item.id,
                              label: `${item.name} · ${item.role}`,
                            }))}
                          />
                        </FormField>
                        {section === "namespaces" && (
                          <FormField
                            label="Active cluster"
                            htmlFor="active-cluster"
                          >
                            <FormSelect
                              id="active-cluster"
                              className="w-full sm:min-w-[220px]"
                              value={clusterId}
                              onValueChange={(value) => {
                                setError("")
                                setNotice("")
                                setClusterId(value)
                              }}
                              emptyOption="Select cluster"
                              items={clusters.map((item) => ({
                                value: item.id,
                                label: item.name,
                              }))}
                            />
                          </FormField>
                        )}
                        <span className="pb-2 text-xs text-muted-foreground">
                          Project scope:{" "}
                          <strong className="font-medium text-foreground">
                            {project?.name}
                          </strong>
                        </span>
                      </div>
                    ) : (
                      <div className="mb-5 rounded-xl border bg-card">
                        <EmptyState
                          title="Create a project first"
                          description="Project-scoped settings need a delivery scope."
                          href="/projects/new"
                          action="Create project"
                        />
                      </div>
                    ))}

                  {!section ? (
                    <div className="space-y-8">
                      <div className="flex flex-col justify-between gap-4 rounded-2xl border bg-card p-6 sm:flex-row sm:items-center">
                        <div className="flex items-start gap-4">
                          <span className="grid size-11 shrink-0 place-items-center rounded-xl bg-primary/7 text-primary">
                            <WorkspaceIcon name="settings" />
                          </span>
                          <div>
                            <h2 className="text-sm font-semibold">
                              Workspace configuration
                            </h2>
                            <p className="mt-1 text-xs leading-5 text-muted-foreground">
                              Choose a project to manage its repositories,
                              targets, and credentials.
                            </p>
                          </div>
                        </div>
                        {projects.length > 0 && (
                          <div className="w-full sm:w-64">
                            <FormSelect
                              ariaLabel="Settings project"
                              value={projectId}
                              onValueChange={(value) => {
                                setError("")
                                setNotice("")
                                setProjectId(value)
                                router.replace(
                                  `/settings?projectId=${encodeURIComponent(value)}`
                                )
                              }}
                              items={projects.map((item) => ({
                                value: item.id,
                                label: item.name,
                              }))}
                            />
                          </div>
                        )}
                      </div>
                      {[
                        {
                          title: "Delivery connections",
                          description:
                            "From your source repository to your Kubernetes target.",
                          ids: ["git-sources", "clusters", "namespaces"],
                        },
                        {
                          title: "Security & access",
                          description:
                            "Control how people and applications connect.",
                          ids: ["credentials", "oidc", "users"],
                        },
                      ].map((group) => (
                        <section key={group.title}>
                          <div className="mb-4">
                            <h2 className="text-base font-semibold tracking-tight">
                              {group.title}
                            </h2>
                            <p className="mt-1 text-xs text-muted-foreground">
                              {group.description}
                            </p>
                          </div>
                          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
                            {sections
                              .filter(
                                (item) =>
                                  group.ids.includes(item.id) &&
                                  (!item.adminOnly || user?.isAdmin)
                              )
                              .map((item) => (
                                <Link
                                  key={item.id}
                                  href={`/settings/${item.id}${projectId ? `?projectId=${encodeURIComponent(projectId)}` : ""}`}
                                  className="workspace-card group flex min-h-48 flex-col rounded-2xl border bg-card p-5"
                                >
                                  <div className="flex items-center justify-between">
                                    <span className="grid size-10 place-items-center rounded-xl border bg-muted/30 text-muted-foreground">
                                      <WorkspaceIcon
                                        name={
                                          item.id === "git-sources"
                                            ? "branch"
                                            : item.id === "clusters" ||
                                                item.id === "namespaces"
                                              ? "server"
                                              : "shield"
                                        }
                                      />
                                    </span>
                                    <span className="text-[10px] text-muted-foreground">
                                      {item.adminOnly
                                        ? "Instance admin"
                                        : item.id === "clusters"
                                          ? "Infrastructure"
                                          : "Project scope"}
                                    </span>
                                  </div>
                                  <h3 className="mt-5 text-sm font-semibold group-hover:text-primary">
                                    {item.title}
                                  </h3>
                                  <p className="mt-1.5 text-xs leading-5 text-muted-foreground">
                                    {item.description}
                                  </p>
                                  <div className="mt-5 flex items-center justify-between border-t pt-3 text-xs">
                                    <span className="text-muted-foreground">
                                      Configure
                                    </span>
                                    <WorkspaceIcon
                                      name="arrow"
                                      className="size-4 text-muted-foreground group-hover:text-primary"
                                    />
                                  </div>
                                </Link>
                              ))}
                          </div>
                        </section>
                      ))}
                    </div>
                  ) : (
                    <div className="settings-content min-w-0">
                      {scopeLoading && !current?.adminOnly && (
                        <p
                          role="status"
                          className="rounded-xl border bg-card p-6 text-sm text-muted-foreground"
                        >
                          Loading project connections…
                        </p>
                      )}
                      {section === "credentials" && !scopeLoading && (
                        <CredentialPanel
                          key={projectId}
                          project={project}
                          user={user}
                          credentials={credentials}
                          busy={busy}
                          action={action}
                          onCreated={(credential) =>
                            setCredentials((items) => [credential, ...items])
                          }
                          onUpdated={(credential) =>
                            setCredentials((items) =>
                              items.map((item) =>
                                item.id === credential.id ? credential : item
                              )
                            )
                          }
                        />
                      )}
                      {section === "clusters" && !scopeLoading && (
                        <ClusterPanel
                          key={project?.id}
                          user={user}
                          project={project}
                          clusters={clusters}
                          globalCredentials={globalKubeCredentials}
                          projectCredentials={namespaceCredentials.filter(
                            (item) => item.projectId === projectId
                          )}
                          busy={busy}
                          action={action}
                          onCreated={(cluster) => {
                            setClusters((items) => [...items, cluster])
                            setClusterId(cluster.id)
                          }}
                          onUpdated={(cluster) =>
                            setClusters((items) =>
                              items.map((item) =>
                                item.id === cluster.id ? cluster : item
                              )
                            )
                          }
                        />
                      )}
                      {section === "git-sources" && !scopeLoading && (
                        <GitSourcePanel
                          key={projectId}
                          project={project}
                          credentials={gitCredentials}
                          sources={sources}
                          busy={busy}
                          action={action}
                          onCreated={(source) =>
                            setSources((items) => [source, ...items])
                          }
                          onUpdated={(source) =>
                            setSources((items) =>
                              items.map((item) =>
                                item.id === source.id ? source : item
                              )
                            )
                          }
                        />
                      )}
                      {section === "namespaces" && bindingsLoading && (
                        <p
                          role="status"
                          className="rounded-xl border bg-card p-6 text-sm text-muted-foreground"
                        >
                          Loading namespace access…
                        </p>
                      )}
                      {section === "namespaces" &&
                        !scopeLoading &&
                        !bindingsLoading && (
                          <NamespacePanel
                            key={`${projectId}:${clusterId}`}
                            project={project}
                            cluster={clusters.find(
                              (item) => item.id === clusterId
                            )}
                            credentials={namespaceCredentials}
                            bindings={bindings}
                            busy={busy}
                            action={action}
                            onCreated={(binding) =>
                              setBindings((items) =>
                                [...items, binding].sort((a, b) =>
                                  a.namespace.localeCompare(b.namespace)
                                )
                              )
                            }
                            onUpdated={(binding) =>
                              setBindings((items) =>
                                items.map((item) =>
                                  item.namespace === binding.namespace
                                    ? binding
                                    : item
                                )
                              )
                            }
                          />
                        )}
                      {section === "oidc" && user?.isAdmin && (
                        <OIDCPanel
                          providers={providers}
                          projects={projects}
                          busy={busy}
                          action={action}
                          onCreated={(provider) =>
                            setProviders((items) => [...items, provider])
                          }
                        />
                      )}
                      {section === "users" && user?.isAdmin && (
                        <LocalUsersPanel busy={busy} action={action} />
                      )}
                    </div>
                  )}

                  {section === "credentials" && (
                    <div className="mt-5 rounded-xl border bg-card px-5 py-4">
                      <div className="flex flex-col justify-between gap-2 sm:flex-row sm:items-center">
                        <div>
                          <p className="text-xs font-semibold">
                            Credentials never leave the server
                          </p>
                          <p className="mt-1 max-w-3xl text-[11px] leading-5 text-muted-foreground">
                            Secrets are encrypted at rest using
                            JUSTCD_ENCRYPTION_KEY and are not returned to the
                            browser. Git deploy keys require pinned known_hosts;
                            kubeconfig exec plugins and token files are
                            disabled.
                          </p>
                        </div>
                        <span className="w-fit rounded-full border border-emerald-200 bg-emerald-50 px-2.5 py-1 text-[9px] font-medium tracking-wide text-emerald-700 uppercase">
                          Encrypted
                        </span>
                      </div>
                    </div>
                  )}
                </div>
              </div>
            </>
          )}
        </>
      )}
    </>
  )
}

function CredentialPanel({
  project,
  user,
  credentials,
  busy,
  action,
  onCreated,
  onUpdated,
}: {
  project?: Project
  user: User | null
  credentials: Credential[]
  busy: boolean
  action: <T>(
    work: () => Promise<T>,
    success: string,
    after?: (value: T) => void
  ) => Promise<void>
  onCreated: (value: Credential) => void
  onUpdated: (value: Credential) => void
}) {
  const [name, setName] = useState("")
  const [kind, setKind] = useState<Credential["kind"]>("kubernetes-token")
  const [secretOne, setSecretOne] = useState("")
  const [secretTwo, setSecretTwo] = useState("")
  const [global, setGlobal] = useState(false)
  const [username, setUsername] = useState("")
  const [editing, setEditing] = useState<Credential | null>(null)
  const isAdmin = user?.isAdmin ?? false
  const fields =
    kind === "git-ssh"
      ? {
          first: "Private key",
          second: "Pinned known_hosts",
          firstKey: "privateKey",
          secondKey: "knownHosts",
        }
      : kind === "git-https"
        ? {
            first: "Access token",
            second: "",
            firstKey: "token",
            secondKey: "",
          }
        : kind === "kubeconfig"
          ? {
              first: "Static kubeconfig content",
              second: "",
              firstKey: "content",
              secondKey: "",
            }
          : {
              first: "Bearer token",
              second: "",
              firstKey: "token",
              secondKey: "",
            }
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const projectId = global ? undefined : project?.id
    await action(
      async () => {
        const secret = {
          [fields.firstKey]: secretOne,
          ...(fields.secondKey ? { [fields.secondKey]: secretTwo } : {}),
          ...(kind === "git-https" ? { username } : {}),
        }
        const result = editing
          ? await api<Credential>(
              `/api/v1/credentials/${encodeURIComponent(editing.id)}`,
              {
                method: "PUT",
                body: JSON.stringify({
                  name,
                  ...(kind === "git-https" ? { username } : {}),
                  ...(secretOne ? { secret } : {}),
                }),
              }
            )
          : await apiPost<Credential>("/api/v1/credentials", {
              projectId,
              name,
              kind,
              secret,
            })
        setName("")
        setSecretOne("")
        setSecretTwo("")
        setUsername("")
        setEditing(null)
        return result
      },
      editing
        ? "Credential updated. Existing token was kept unless you entered a replacement."
        : "Encrypted credential added.",
      editing ? onUpdated : onCreated
    )
  }
  function edit(credential: Credential) {
    setEditing(credential)
    setName(credential.name)
    setKind(credential.kind)
    setGlobal(!credential.projectId)
    setSecretOne("")
    setSecretTwo("")
    setUsername(credential.username ?? "")
    focusSettingsEditor("credential-name")
  }
  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-6">
        <div className="rounded-2xl border bg-card p-6">
          <div className="mb-3 flex items-center justify-between">
            <p className="text-[11px] font-medium">Available credentials</p>
            <span className="text-[10px] text-muted-foreground">
              {credentials.length} total
            </span>
          </div>
          {credentials.length ? (
            <div className="space-y-2">
              {credentials.map((credential) => (
                <div
                  key={credential.id}
                  className="flex items-center gap-3 rounded-lg border px-3 py-2"
                >
                  <span className="grid size-7 place-items-center rounded-md bg-muted text-[10px]">
                    {credential.kind.startsWith("git") ? "G" : "K"}
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-xs font-medium">
                      {credential.name}
                    </span>
                    <span className="block text-[9px] text-muted-foreground capitalize">
                      {credential.kind.replaceAll("-", " ")} ·{" "}
                      {credential.projectId
                        ? "project scoped"
                        : "instance wide"}
                    </span>
                  </span>
                  {credential.expiresAt && (
                    <span className="text-[9px] text-muted-foreground">
                      expires{" "}
                      {new Date(credential.expiresAt).toLocaleDateString()}
                    </span>
                  )}
                  <Button
                    size="sm"
                    variant="outline"
                    type="button"
                    disabled={
                      busy ||
                      (!!credential.projectId && project?.role !== "owner") ||
                      (!credential.projectId && !isAdmin)
                    }
                    onClick={() => edit(credential)}
                  >
                    Edit
                  </Button>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-[11px] text-muted-foreground">
              Credentials will be listed here by name only.
            </p>
          )}
        </div>
        <form
          className="settings-editor space-y-5 rounded-2xl border bg-card p-6"
          onSubmit={submit}
        >
          <div className="border-b pb-4">
            <h2 className="text-base font-semibold">
              {editing ? "Edit credential" : "Add credential"}
            </h2>
            <p className="mt-1 text-sm leading-6 text-muted-foreground">
              Choose a type and scope, then provide the connection secret.
            </p>
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <FormField label="Name" htmlFor="credential-name">
              <Input
                id="credential-name"
                placeholder="production deploy token"
                value={name}
                onChange={(event) => setName(event.target.value)}
                required
                maxLength={100}
              />
            </FormField>
            <FormField label="Credential type" htmlFor="credential-kind">
              <FormSelect
                id="credential-kind"
                value={kind}
                onValueChange={(value) => setKind(value as Credential["kind"])}
                disabled={!!editing}
                items={[
                  {
                    value: "kubernetes-token",
                    label: "Kubernetes bearer token",
                  },
                  { value: "kubeconfig", label: "Static kubeconfig" },
                  { value: "git-https", label: "Git over HTTPS" },
                  { value: "git-ssh", label: "Git over SSH" },
                ]}
              />
            </FormField>
          </div>
          {kind === "git-https" && (
            <FormField
              label="Git username"
              htmlFor="git-username"
              hint="Use the username required by your Git provider; the token is sent as the password."
            >
              <Input
                id="git-username"
                value={username}
                onChange={(event) => setUsername(event.target.value)}
                required
              />
            </FormField>
          )}
          <FormField
            label={fields.first}
            htmlFor="credential-secret"
            hint={
              editing
                ? "Leave blank to keep the encrypted secret. To rotate it, enter all secret fields again."
                : kind === "kubeconfig"
                  ? "Exec plugins, auth-provider plugins, and token files are rejected."
                  : undefined
            }
          >
            <Textarea
              id="credential-secret"
              className={inputClass}
              value={secretOne}
              onChange={(event) => setSecretOne(event.target.value)}
              required={!editing}
            />
          </FormField>
          {fields.second && (
            <FormField
              label={fields.second}
              htmlFor="credential-secret-two"
              hint="Host keys must be explicitly pinned; unknown hosts are not accepted."
            >
              <Textarea
                id="credential-secret-two"
                className={inputClass}
                value={secretTwo}
                onChange={(event) => setSecretTwo(event.target.value)}
                required={!editing || !!secretOne}
              />
            </FormField>
          )}
          {isAdmin && !editing && (
            <label className="flex items-center gap-2 text-[11px] text-muted-foreground">
              <Checkbox
                checked={global}
                onCheckedChange={(checked) => setGlobal(Boolean(checked))}
              />
              Store as an instance-wide credential (admin only)
            </label>
          )}
          {!global && !project && (
            <p className="text-[11px] text-amber-700">
              Select a project to add a scoped credential.
            </p>
          )}
          <div className="flex gap-2">
            <Button
              size="sm"
              type="submit"
              disabled={
                busy ||
                (!global && project?.role !== "owner") ||
                (global && !isAdmin)
              }
            >
              {editing ? "Save credential" : "Add credential"}
            </Button>
            {editing && (
              <Button
                size="sm"
                type="button"
                variant="outline"
                onClick={() => {
                  setEditing(null)
                  setName("")
                  setSecretOne("")
                  setSecretTwo("")
                  setUsername("")
                }}
              >
                Cancel
              </Button>
            )}
          </div>
        </form>
      </div>
    </div>
  )
}

function ClusterPanel({
  user,
  project,
  clusters,
  globalCredentials,
  projectCredentials,
  busy,
  action,
  onCreated,
  onUpdated,
}: {
  user: User | null
  project?: Project
  clusters: Cluster[]
  globalCredentials: Credential[]
  projectCredentials: Credential[]
  busy: boolean
  action: <T>(
    work: () => Promise<T>,
    success: string,
    after?: (value: T) => void
  ) => Promise<void>
  onCreated: (value: Cluster) => void
  onUpdated: (value: Cluster) => void
}) {
  const [name, setName] = useState("")
  const [apiServer, setApiServer] = useState("")
  const [caDataBase64, setCaDataBase64] = useState("")
  const [defaultCredentialId, setDefaultCredentialId] = useState("")
  const [clusterScopeCredentialId, setClusterScopeCredentialId] = useState("")
  const [projectCredentialId, setProjectCredentialId] = useState("")
  const [newProjectCredentialId, setNewProjectCredentialId] = useState("")
  const [selectedClusterId, setSelectedClusterId] = useState("")
  const [credentialLoadedFor, setCredentialLoadedFor] = useState("")
  const [credentialLoadError, setCredentialLoadError] = useState("")
  const [insecure, setInsecure] = useState(false)
  const [editing, setEditing] = useState<Cluster | null>(null)
  const [testResult, setTestResult] = useState("")
  useEffect(() => {
    if (!project || !selectedClusterId) return
    let active = true
    api<{ credentialId: string | null }>(
      `/api/v1/clusters/${encodeURIComponent(selectedClusterId)}/project-credential?projectId=${encodeURIComponent(project.id)}`
    )
      .then((result) => {
        if (active) {
          setProjectCredentialId(result.credentialId ?? "")
          setCredentialLoadedFor(selectedClusterId)
          setCredentialLoadError("")
        }
      })
      .catch((cause) => {
        if (active) setCredentialLoadError(errorMessage(cause))
      })
    return () => {
      active = false
    }
  }, [project, selectedClusterId])
  async function saveProjectCredential() {
    if (!project || !selectedClusterId) return
    await action(
      () =>
        api<{ credentialId: string | null }>(
          `/api/v1/clusters/${encodeURIComponent(selectedClusterId)}/project-credential`,
          {
            method: "PUT",
            body: JSON.stringify({
              projectId: project.id,
              credentialId: projectCredentialId || null,
            }),
          }
        ),
      "Project credential saved for this cluster."
    )
  }
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    await action(
      async () => {
        const result = editing
          ? await api<Cluster>(
              `/api/v1/clusters/${encodeURIComponent(editing.id)}`,
              {
                method: "PUT",
                body: JSON.stringify({
                  name,
                  apiServer,
                  ...(caDataBase64 ? { caDataBase64 } : {}),
                  insecureSkipVerify: insecure,
                  defaultCredentialId: defaultCredentialId || null,
                  clusterScopeCredentialId: clusterScopeCredentialId || null,
                }),
              }
            )
          : await apiPost<Cluster>("/api/v1/clusters", {
              name,
              apiServer,
              caDataBase64,
              insecureSkipVerify: insecure,
              defaultCredentialId: defaultCredentialId || undefined,
              clusterScopeCredentialId: clusterScopeCredentialId || undefined,
              projectId: project?.id,
              projectCredentialId: newProjectCredentialId || undefined,
            })
        setName("")
        setApiServer("")
        setCaDataBase64("")
        setNewProjectCredentialId("")
        setDefaultCredentialId("")
        setClusterScopeCredentialId("")
        setInsecure(false)
        setEditing(null)
        return result
      },
      editing
        ? "Cluster updated. Existing CA certificate was kept unless replaced."
        : "Cluster connected. Add a namespace binding to a project before creating applications.",
      editing ? onUpdated : onCreated
    )
  }
  function edit(cluster: Cluster) {
    setEditing(cluster)
    setName(cluster.name)
    setApiServer(cluster.apiServer)
    setCaDataBase64("")
    setInsecure(cluster.insecureSkipVerify)
    setDefaultCredentialId(cluster.defaultCredentialId ?? "")
    setClusterScopeCredentialId(cluster.clusterScopeCredentialId ?? "")
    focusSettingsEditor("cluster-name")
  }
  async function test(cluster: Cluster) {
    if (!project) return
    setTestResult("")
    await action(
      () =>
        apiPost<{
          status: string
          serverVersion: string
          namespace: string
          canReadPods: boolean
        }>(`/api/v1/clusters/${encodeURIComponent(cluster.id)}/test`, {
          projectId: project.id,
        }).then((result) => {
          setTestResult(
            `${cluster.name}: Kubernetes ${result.serverVersion} authenticated; ${result.canReadPods ? "can" : "cannot"} read Pods in ${result.namespace}`
          )
          return result
        }),
      "Cluster authentication verified."
    )
  }
  return (
    <div className="space-y-6">
      <SettingsInventory title="Connected clusters" count={clusters.length}>
        {clusters.length ? (
          <div className="divide-y border-b">
            {clusters.map((cluster) => (
              <div
                key={cluster.id}
                className="flex flex-wrap items-center gap-3 px-5 py-3"
              >
                <span className="grid size-8 place-items-center rounded-lg bg-violet-500/10 text-xs text-violet-700">
                  K8s
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block text-xs font-medium">
                    {cluster.name}
                  </span>
                  <span className="block truncate font-mono text-[9px] text-muted-foreground">
                    {cluster.apiServer}
                  </span>
                </span>
                <Button
                  size="sm"
                  variant="outline"
                  type="button"
                  disabled={busy || !project}
                  onClick={() => test(cluster)}
                >
                  Test
                </Button>
                {user?.isAdmin && (
                  <Button
                    size="sm"
                    variant="outline"
                    type="button"
                    disabled={busy}
                    onClick={() => edit(cluster)}
                  >
                    Edit
                  </Button>
                )}
              </div>
            ))}
          </div>
        ) : null}
        {!clusters.length && (
          <p className="px-6 py-5 text-sm text-muted-foreground">
            No connected clusters yet. Use the form below to get started.
          </p>
        )}
      </SettingsInventory>
      {testResult && (
        <p role="status" className="px-5 py-2 text-xs text-emerald-700">
          {testResult}
        </p>
      )}
      {project && clusters.length > 0 && (
        <div className="space-y-4 rounded-2xl border bg-card p-6">
          <h2 className="text-base font-semibold">Project authentication</h2>
          <p className="text-xs font-medium text-primary">{project.name}</p>
          <p className="text-[11px] text-muted-foreground">
            This project credential is used for its namespace bindings on the
            selected cluster. It is never shared with other projects.
          </p>
          <div className="grid gap-3 sm:grid-cols-2">
            <FormField label="Cluster" htmlFor="project-cluster">
              <FormSelect
                id="project-cluster"
                value={selectedClusterId}
                onValueChange={(value) => {
                  setSelectedClusterId(value)
                  setCredentialLoadedFor("")
                  setCredentialLoadError("")
                }}
                emptyOption="Select cluster"
                items={clusters.map((cluster) => ({
                  value: cluster.id,
                  label: cluster.name,
                }))}
              />
            </FormField>
            <FormField
              label="Project Kubernetes credential"
              htmlFor="project-cluster-credential"
            >
              <FormSelect
                id="project-cluster-credential"
                disabled={
                  !selectedClusterId ||
                  credentialLoadedFor !== selectedClusterId
                }
                value={projectCredentialId}
                onValueChange={setProjectCredentialId}
                emptyOption="Use global default"
                items={projectCredentials.map((credential) => ({
                  value: credential.id,
                  label: credential.name,
                }))}
              />
            </FormField>
          </div>
          <Button
            size="sm"
            type="button"
            disabled={
              busy ||
              !selectedClusterId ||
              credentialLoadedFor !== selectedClusterId ||
              project.role !== "owner"
            }
            onClick={saveProjectCredential}
          >
            Save project credential
          </Button>
          {credentialLoadError && (
            <p role="alert" className="text-sm text-destructive">
              {credentialLoadError}
            </p>
          )}
        </div>
      )}
      {user?.isAdmin ? (
        <form
          className="settings-editor space-y-5 rounded-2xl border bg-card p-6"
          onSubmit={submit}
        >
          <div className="border-b pb-4">
            <h2 className="text-base font-semibold">
              {editing ? "Edit cluster connection" : "Connect a cluster"}
            </h2>
            <p className="mt-1 text-sm leading-6 text-muted-foreground">
              Instance administrators manage the API endpoint and shared
              defaults.
            </p>
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <FormField label="Cluster name" htmlFor="cluster-name">
              <Input
                id="cluster-name"
                placeholder="prod-eu-1"
                value={name}
                onChange={(event) => setName(event.target.value)}
                required
              />
            </FormField>
            <FormField label="Kubernetes API URL" htmlFor="cluster-api">
              <Input
                id="cluster-api"
                type="url"
                placeholder="https://api.example.com:6443"
                value={apiServer}
                onChange={(event) => setApiServer(event.target.value)}
                required
              />
            </FormField>
          </div>
          <details
            className="rounded-xl border bg-muted/20 p-4"
            open={editing ? true : undefined}
          >
            <summary className="cursor-pointer text-sm font-medium">
              Authentication & TLS settings
            </summary>
            <div className="mt-5 space-y-5">
              {" "}
              <FormField
                label="CA certificate (base64)"
                htmlFor="cluster-ca"
                hint={
                  editing
                    ? "Leave blank to keep the current CA certificate."
                    : "Optional when the system trust store is sufficient."
                }
              >
                <Textarea
                  id="cluster-ca"
                  className={inputClass}
                  value={caDataBase64}
                  onChange={(event) => setCaDataBase64(event.target.value)}
                />
              </FormField>
              <div className="grid gap-3 sm:grid-cols-2">
                <FormField
                  label="Default cluster credential"
                  htmlFor="cluster-default"
                >
                  <FormSelect
                    id="cluster-default"
                    value={defaultCredentialId}
                    onValueChange={setDefaultCredentialId}
                    emptyOption="None yet"
                    items={globalCredentials.map((credential) => ({
                      value: credential.id,
                      label: credential.name,
                    }))}
                  />
                </FormField>
                <FormField
                  label="Cluster-scope credential"
                  htmlFor="cluster-scope"
                >
                  <FormSelect
                    id="cluster-scope"
                    value={clusterScopeCredentialId}
                    onValueChange={setClusterScopeCredentialId}
                    emptyOption="Disabled (recommended)"
                    items={globalCredentials.map((credential) => ({
                      value: credential.id,
                      label: credential.name,
                    }))}
                  />
                </FormField>
              </div>
              {project && !editing && (
                <FormField
                  label={`Credential for ${project.name}`}
                  htmlFor="new-project-cluster-credential"
                  hint="Only this project can use this token. Namespace credentials override it."
                >
                  <FormSelect
                    id="new-project-cluster-credential"
                    value={newProjectCredentialId}
                    onValueChange={setNewProjectCredentialId}
                    emptyOption="None yet"
                    items={projectCredentials.map((credential) => ({
                      value: credential.id,
                      label: credential.name,
                    }))}
                  />
                </FormField>
              )}
              <p className="text-[11px] text-muted-foreground">
                Instance-wide credentials are optional. Project credentials are
                kept separate and are never shared across projects.
              </p>
              <label className="flex items-start gap-2 text-[11px] leading-4 text-muted-foreground">
                <Checkbox
                  checked={insecure}
                  onCheckedChange={(checked) => setInsecure(Boolean(checked))}
                  className="mt-0.5"
                />
                Skip TLS certificate verification{" "}
                <span className="text-amber-700">
                  — only for temporary development clusters.
                </span>
              </label>
            </div>
          </details>
          <div className="flex gap-2">
            <Button size="sm" type="submit" disabled={busy}>
              {editing ? "Save cluster" : "Add cluster"}
            </Button>
            {editing && (
              <Button
                size="sm"
                variant="outline"
                type="button"
                onClick={() => {
                  setEditing(null)
                  setName("")
                  setApiServer("")
                  setCaDataBase64("")
                  setDefaultCredentialId("")
                  setClusterScopeCredentialId("")
                  setInsecure(false)
                }}
              >
                Cancel
              </Button>
            )}
          </div>
        </form>
      ) : (
        <div className="px-5 py-4 text-[11px] leading-5 text-muted-foreground">
          Only instance administrators can add cluster API endpoints and global
          credentials.
        </div>
      )}
    </div>
  )
}

function GitSourcePanel({
  project,
  credentials,
  sources,
  busy,
  action,
  onCreated,
  onUpdated,
}: {
  project?: Project
  credentials: Credential[]
  sources: GitSource[]
  busy: boolean
  action: <T>(
    work: () => Promise<T>,
    success: string,
    after?: (value: T) => void
  ) => Promise<void>
  onCreated: (value: GitSource) => void
  onUpdated: (value: GitSource) => void
}) {
  const [name, setName] = useState("")
  const [repositoryUrl, setRepositoryUrl] = useState("")
  const [credentialId, setCredentialId] = useState("")
  const [editing, setEditing] = useState<GitSource | null>(null)
  const [testResult, setTestResult] = useState("")
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    await action(
      async () => {
        const source = editing
          ? await api<GitSource>(
              `/api/v1/git-sources/${encodeURIComponent(editing.id)}`,
              {
                method: "PUT",
                body: JSON.stringify({
                  name,
                  repositoryUrl,
                  credentialId: credentialId || null,
                }),
              }
            )
          : await apiPost<GitSource>("/api/v1/git-sources", {
              projectId: project?.id,
              name,
              repositoryUrl,
              credentialId: credentialId || undefined,
            })
        setName("")
        setRepositoryUrl("")
        setCredentialId("")
        setEditing(null)
        return source
      },
      editing ? "Git source updated." : "Git source connected.",
      editing ? onUpdated : onCreated
    )
  }
  function edit(source: GitSource) {
    setEditing(source)
    setName(source.name)
    setRepositoryUrl(source.repositoryUrl)
    setCredentialId(source.credentialId ?? "")
    focusSettingsEditor("source-name")
  }
  async function test(source: GitSource) {
    setTestResult("")
    await action(
      () =>
        apiPost<{ status: string; commit: string }>(
          `/api/v1/git-sources/${encodeURIComponent(source.id)}/test`
        ).then((result) => {
          setTestResult(
            `${source.name}: HEAD ${result.commit.slice(0, 12)} reachable`
          )
          return result
        }),
      "Git source connection verified."
    )
  }
  return (
    <div className="space-y-6">
      <SettingsInventory title="Connected repositories" count={sources.length}>
        {sources.length ? (
          <div className="divide-y border-b">
            {sources.map((source) => (
              <div
                key={source.id}
                className="flex flex-wrap items-center gap-3 px-6 py-3"
              >
                <div className="min-w-0 flex-1">
                  <p className="text-xs font-medium">{source.name}</p>
                  <p className="mt-0.5 truncate font-mono text-[10px] text-muted-foreground">
                    {source.repositoryUrl}
                  </p>
                </div>
                <Button
                  size="sm"
                  variant="outline"
                  type="button"
                  disabled={busy}
                  onClick={() => test(source)}
                >
                  Test
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  type="button"
                  disabled={busy || project?.role !== "owner"}
                  onClick={() => edit(source)}
                >
                  Edit
                </Button>
              </div>
            ))}
          </div>
        ) : null}
        {!sources.length && (
          <p className="px-6 py-5 text-sm text-muted-foreground">
            No connected repositories yet. Use the form below to get started.
          </p>
        )}
      </SettingsInventory>
      {testResult && (
        <p role="status" className="px-5 py-2 text-xs text-emerald-700">
          {testResult}
        </p>
      )}
      <form
        className="settings-editor space-y-5 rounded-2xl border bg-card p-6"
        onSubmit={submit}
      >
        <div className="border-b pb-4">
          <h2 className="text-base font-semibold">
            {editing ? "Edit repository" : "Connect a repository"}
          </h2>
          <p className="mt-1 text-sm leading-6 text-muted-foreground">
            Choose a repository and how JustCD authenticates to it.
          </p>
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <FormField label="Source name" htmlFor="source-name">
            <Input
              id="source-name"
              placeholder="platform-config"
              value={name}
              onChange={(event) => setName(event.target.value)}
              required
            />
          </FormField>
          <FormField label="Repository URL" htmlFor="source-url">
            <Input
              id="source-url"
              placeholder="https://github.com/org/repo.git"
              value={repositoryUrl}
              onChange={(event) => setRepositoryUrl(event.target.value)}
              required
            />
          </FormField>
        </div>
        <FormField label="Git credential" htmlFor="source-credential">
          <FormSelect
            id="source-credential"
            value={credentialId}
            onValueChange={setCredentialId}
            emptyOption="Public repository"
            items={credentials.map((credential) => ({
              value: credential.id,
              label: `${credential.name} · ${credential.kind}`,
            }))}
          />
        </FormField>
        {!project && (
          <p className="text-[11px] text-amber-700">
            Select a project to configure Git sources.
          </p>
        )}
        {!credentials.length && (
          <Link
            href={`/settings/credentials?projectId=${project?.id ?? ""}`}
            className="inline-flex text-xs font-medium text-primary hover:underline"
          >
            Add credentials for a private repository →
          </Link>
        )}
        <div className="flex gap-2">
          <Button
            size="sm"
            type="submit"
            disabled={busy || !project || project.role !== "owner"}
          >
            {editing ? "Save Git source" : "Add Git source"}
          </Button>
          {editing && (
            <Button
              size="sm"
              variant="outline"
              type="button"
              onClick={() => {
                setEditing(null)
                setName("")
                setRepositoryUrl("")
                setCredentialId("")
              }}
            >
              Cancel
            </Button>
          )}
        </div>
      </form>
    </div>
  )
}

function NamespacePanel({
  project,
  cluster,
  credentials,
  bindings,
  busy,
  action,
  onCreated,
  onUpdated,
}: {
  project?: Project
  cluster?: Cluster
  credentials: Credential[]
  bindings: NamespaceBinding[]
  busy: boolean
  action: <T>(
    work: () => Promise<T>,
    success: string,
    after?: (value: T) => void
  ) => Promise<void>
  onCreated: (value: NamespaceBinding) => void
  onUpdated: (value: NamespaceBinding) => void
}) {
  const [namespace, setNamespace] = useState("")
  const [credentialId, setCredentialId] = useState("")
  const [editing, setEditing] = useState<NamespaceBinding | null>(null)
  const [projectDefault, setProjectDefault] = useState(false)
  useEffect(() => {
    if (!project || !cluster) return
    api<{ credentialId: string | null }>(
      `/api/v1/clusters/${encodeURIComponent(cluster.id)}/project-credential?projectId=${encodeURIComponent(project.id)}`
    )
      .then((result) => setProjectDefault(!!result.credentialId))
      .catch(() => setProjectDefault(false))
  }, [project, cluster])
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!project || !cluster) return
    await action(
      async () => {
        const binding = editing
          ? await api<NamespaceBinding>(
              `/api/v1/clusters/${encodeURIComponent(cluster.id)}/bindings/${encodeURIComponent(editing.namespace)}`,
              {
                method: "PUT",
                body: JSON.stringify({
                  projectId: project.id,
                  credentialId: credentialId || null,
                }),
              }
            )
          : await apiPost<NamespaceBinding>(
              `/api/v1/clusters/${encodeURIComponent(cluster.id)}/bindings`,
              {
                projectId: project.id,
                namespace,
                credentialId: credentialId || undefined,
              }
            )
        setNamespace("")
        setCredentialId("")
        setEditing(null)
        return binding
      },
      editing
        ? "Namespace credential updated."
        : "Namespace access binding added.",
      editing ? onUpdated : onCreated
    )
  }
  return (
    <div className="space-y-6">
      <SettingsInventory title="Allowed namespaces" count={bindings.length}>
        {bindings.length ? (
          <div className="divide-y border-b">
            {bindings.map((binding) => (
              <div
                key={binding.namespace}
                className="flex flex-wrap items-center justify-between gap-3 px-6 py-2.5"
              >
                <span className="font-mono text-xs">{binding.namespace}</span>
                <span className="text-[10px] text-muted-foreground">
                  {binding.credentialId
                    ? credentials.find(
                        (item) => item.id === binding.credentialId
                      )?.name || "namespace credential"
                    : projectDefault
                      ? "Project credential"
                      : "Cluster default"}
                </span>
                <Button
                  size="sm"
                  variant="outline"
                  type="button"
                  disabled={busy || project?.role !== "owner"}
                  onClick={() => {
                    setEditing(binding)
                    setNamespace(binding.namespace)
                    setCredentialId(binding.credentialId ?? "")
                    focusSettingsEditor("namespace-credential")
                  }}
                >
                  Edit
                </Button>
              </div>
            ))}
          </div>
        ) : null}
        {!bindings.length && (
          <p className="px-6 py-5 text-sm text-muted-foreground">
            No allowed namespaces yet. Use the form below to get started.
          </p>
        )}
      </SettingsInventory>
      <form
        className="settings-editor space-y-5 rounded-2xl border bg-card p-6"
        onSubmit={submit}
      >
        <div className="border-b pb-4">
          <h2 className="text-base font-semibold">
            {editing ? "Edit namespace access" : "Grant namespace access"}
          </h2>
          <p className="mt-1 text-sm leading-6 text-muted-foreground">
            Allow this project to deploy to a namespace on the selected cluster.
          </p>
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <FormField
            label="Namespace"
            htmlFor="namespace-name"
            hint="Kubernetes namespace, up to 63 characters."
          >
            <Input
              id="namespace-name"
              placeholder="payments"
              value={namespace}
              onChange={(event) => setNamespace(event.target.value)}
              required
              maxLength={63}
              readOnly={!!editing}
            />
          </FormField>
          <FormField
            label="Credential for this namespace"
            htmlFor="namespace-credential"
          >
            <FormSelect
              id="namespace-credential"
              value={credentialId}
              onValueChange={setCredentialId}
              emptyOption={
                projectDefault
                  ? "Use project credential"
                  : "Use cluster default"
              }
              items={credentials.map((credential) => ({
                value: credential.id,
                label: credential.name,
              }))}
            />
          </FormField>
        </div>
        {!cluster?.defaultCredentialId && !projectDefault && !credentialId && (
          <p className="text-[10px] text-amber-700">
            Choose a namespace credential or configure a cluster default
            credential.
          </p>
        )}
        <div className="flex gap-2">
          <Button
            size="sm"
            type="submit"
            disabled={busy || !project || project.role !== "owner" || !cluster}
          >
            {editing ? "Save binding" : "Bind namespace"}
          </Button>
          {editing && (
            <Button
              size="sm"
              variant="outline"
              type="button"
              onClick={() => {
                setEditing(null)
                setNamespace("")
                setCredentialId("")
              }}
            >
              Cancel
            </Button>
          )}
        </div>
      </form>
    </div>
  )
}

function OIDCPanel({
  providers,
  projects,
  busy,
  action,
  onCreated,
}: {
  providers: OIDCProvider[]
  projects: Project[]
  busy: boolean
  action: <T>(
    work: () => Promise<T>,
    success: string,
    after?: (value: T) => void
  ) => Promise<void>
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
    await action(
      async () => {
        const provider = await apiPost<OIDCProvider>(
          "/api/v1/admin/oidc-providers",
          { name, issuer, clientId, clientSecret, groupsClaim }
        )
        setName("")
        setClientId("")
        setClientSecret("")
        return provider
      },
      "OIDC provider added. Verify the callback URL in your identity provider.",
      onCreated
    )
  }
  async function mapGroup(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const selectedProvider = providerId || providers[0]?.id
    const selectedProject = projectId || projects[0]?.id
    if (!selectedProvider || !selectedProject) return
    await action(
      () =>
        apiPost(
          `/api/v1/admin/oidc-providers/${encodeURIComponent(selectedProvider)}/groups`,
          { group: groupName, projectId: selectedProject, role: groupRole }
        ),
      "OIDC group mapping saved.",
      () => setGroupName("")
    )
  }
  return (
    <div className="space-y-6">
      <SettingsInventory title="Identity providers" count={providers.length}>
        {providers.length ? (
          <div className="divide-y border-b">
            {providers.map((provider) => (
              <div
                key={provider.id}
                className="flex items-center justify-between gap-4 px-5 py-3"
              >
                <div className="min-w-0">
                  <span className="block text-xs font-medium">
                    {provider.name}
                  </span>
                  <span className="mt-1 block font-mono text-xs break-all text-muted-foreground">
                    Callback: {provider.redirectUrl}
                  </span>
                </div>
                <span
                  className={`rounded-full border px-2 py-0.5 text-[9px] ${provider.enabled ? "border-emerald-200 bg-emerald-50 text-emerald-700" : "text-muted-foreground"}`}
                >
                  {provider.enabled ? "enabled" : "disabled"}
                </span>
              </div>
            ))}
          </div>
        ) : null}
        {!providers.length && (
          <p className="px-6 py-5 text-sm text-muted-foreground">
            No identity providers yet. Use the form below to get started.
          </p>
        )}
      </SettingsInventory>
      <form
        className="settings-editor space-y-5 rounded-2xl border bg-card p-6"
        onSubmit={submit}
      >
        <div className="border-b pb-4">
          <h2 className="text-base font-semibold">
            Connect an identity provider
          </h2>
          <p className="mt-1 text-sm leading-6 text-muted-foreground">
            Set up organization sign-in before assigning groups to project
            roles.
          </p>
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <FormField label="Display name" htmlFor="oidc-name">
            <Input
              id="oidc-name"
              placeholder="Company SSO"
              value={name}
              onChange={(event) => setName(event.target.value)}
              required
            />
          </FormField>
          <FormField label="Issuer URL" htmlFor="oidc-issuer">
            <Input
              id="oidc-issuer"
              type="url"
              placeholder="https://id.example.com"
              value={issuer}
              onChange={(event) => setIssuer(event.target.value)}
              required
            />
          </FormField>
          <FormField label="Client ID" htmlFor="oidc-client">
            <Input
              id="oidc-client"
              value={clientId}
              onChange={(event) => setClientId(event.target.value)}
              required
            />
          </FormField>
          <FormField label="Groups claim" htmlFor="oidc-groups">
            <Input
              id="oidc-groups"
              value={groupsClaim}
              onChange={(event) => setGroupsClaim(event.target.value)}
            />
          </FormField>
        </div>
        <FormField label="Client secret" htmlFor="oidc-secret">
          <Input
            id="oidc-secret"
            type="password"
            autoComplete="new-password"
            value={clientSecret}
            onChange={(event) => setClientSecret(event.target.value)}
            required
          />
        </FormField>
        <p className="text-[10px] leading-4 text-muted-foreground">
          Callback URL: use the JustCD public URL plus{" "}
          <code className="font-mono">
            /api/v1/auth/oidc/&lt;provider-id&gt;/callback
          </code>
          . It is available after the provider is created.
        </p>
        <Button size="sm" type="submit" disabled={busy}>
          Add OIDC provider
        </Button>
      </form>
      {providers.length > 0 && projects.length > 0 && (
        <form
          className="settings-editor space-y-5 rounded-2xl border bg-card p-6"
          onSubmit={mapGroup}
        >
          <div>
            <h2 className="text-base font-semibold">Assign group access</h2>
            <p className="mt-1 text-[10px] text-muted-foreground">
              Verified ID-token groups grant the selected project role at each
              login.
            </p>
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <FormField label="Provider" htmlFor="mapping-provider">
              <FormSelect
                id="mapping-provider"
                value={providerId || providers[0]?.id || ""}
                onValueChange={setProviderId}
                placeholder="Choose provider"
                items={providers.map((provider) => ({
                  value: provider.id,
                  label: provider.name,
                }))}
              />
            </FormField>
            <FormField label="Project" htmlFor="mapping-project">
              <FormSelect
                id="mapping-project"
                value={projectId || projects[0]?.id || ""}
                onValueChange={setProjectId}
                placeholder="Choose project"
                items={projects.map((project) => ({
                  value: project.id,
                  label: project.name,
                }))}
              />
            </FormField>
            <FormField label="Group claim value" htmlFor="mapping-group">
              <Input
                id="mapping-group"
                placeholder="platform-deployers"
                value={groupName}
                onChange={(event) => setGroupName(event.target.value)}
                required
              />
            </FormField>
            <FormField label="Granted role" htmlFor="mapping-role">
              <FormSelect
                id="mapping-role"
                value={groupRole}
                onValueChange={setGroupRole}
                items={[
                  { value: "viewer", label: "Viewer" },
                  { value: "deployer", label: "Deployer" },
                  { value: "owner", label: "Owner" },
                ]}
              />
            </FormField>
          </div>
          <Button size="sm" type="submit" disabled={busy || !groupName}>
            Save group mapping
          </Button>
        </form>
      )}
    </div>
  )
}

function LocalUsersPanel({
  busy,
  action,
}: {
  busy: boolean
  action: <T>(
    work: () => Promise<T>,
    success: string,
    after?: (value: T) => void
  ) => Promise<void>
}) {
  const [email, setEmail] = useState("")
  const [displayName, setDisplayName] = useState("")
  const [password, setPassword] = useState("")
  const [isAdmin, setIsAdmin] = useState(false)
  const [users, setUsers] = useState<User[]>([])
  useEffect(() => {
    api<ListResponse<User>>("/api/v1/admin/users")
      .then((result) => setUsers(result.items))
      .catch(() => undefined)
  }, [])
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    await action(async () => {
      const user = await apiPost<User>("/api/v1/admin/users", {
        email,
        displayName,
        password,
        isAdmin,
      })
      setEmail("")
      setDisplayName("")
      setPassword("")
      setIsAdmin(false)
      setUsers((items) => [...items, user])
      return user
    }, "Local user added.")
  }
  return (
    <div className="space-y-6">
      <SettingsInventory title="Local accounts" count={users.length}>
        {users.length > 0 && (
          <div className="divide-y border-b">
            {users.map((user) => (
              <div
                key={user.id}
                className="flex flex-wrap items-center gap-3 px-6 py-2.5"
              >
                <span className="min-w-0 flex-1 truncate text-xs">
                  {user.displayName || user.email}{" "}
                  <span className="text-muted-foreground">· {user.email}</span>
                </span>
                <span className="text-[9px] text-muted-foreground">
                  {user.isAdmin ? "admin" : "user"}
                </span>
              </div>
            ))}
          </div>
        )}
        {!users.length && (
          <p className="px-6 py-5 text-sm text-muted-foreground">
            No local accounts yet. Use the form below to get started.
          </p>
        )}
      </SettingsInventory>
      <form
        className="settings-editor space-y-5 rounded-2xl border bg-card p-6"
        onSubmit={submit}
      >
        <div className="border-b pb-4">
          <h2 className="text-base font-semibold">Create a local account</h2>
          <p className="mt-1 text-sm leading-6 text-muted-foreground">
            Add a person who signs in directly with an email address and
            password.
          </p>
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <FormField label="Email" htmlFor="user-email">
            <Input
              id="user-email"
              type="email"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
              required
            />
          </FormField>
          <FormField label="Display name" htmlFor="user-display">
            <Input
              id="user-display"
              value={displayName}
              onChange={(event) => setDisplayName(event.target.value)}
            />
          </FormField>
        </div>
        <FormField
          label="Password"
          htmlFor="user-password"
          hint="At least 12 characters. Share the initial password securely with the account owner."
        >
          <Input
            id="user-password"
            type="password"
            autoComplete="new-password"
            minLength={12}
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            required
          />
        </FormField>
        <label className="flex items-center gap-2 text-[11px]">
          <Checkbox
            checked={isAdmin}
            onCheckedChange={(checked) => setIsAdmin(Boolean(checked))}
          />
          Instance administrator
        </label>
        <Button size="sm" type="submit" disabled={busy}>
          Add local user
        </Button>
      </form>
    </div>
  )
}

function SettingsInventory({
  title,
  count,
  children,
}: {
  title: string
  count: number
  children: React.ReactNode
}) {
  return (
    <section className="overflow-hidden rounded-2xl border bg-card">
      <div className="flex items-center justify-between border-b px-6 py-4">
        <h2 className="text-sm font-semibold">{title}</h2>
        <span className="rounded-md bg-muted px-2 py-1 text-xs text-muted-foreground tabular-nums">
          {count}
        </span>
      </div>
      {children}
    </section>
  )
}

function focusSettingsEditor(id: string) {
  window.requestAnimationFrame(() => {
    const input = document.getElementById(id)
    input?.focus({ preventScroll: true })
    input
      ?.closest("form")
      ?.scrollIntoView({ block: "nearest", behavior: "instant" })
  })
}
