"use client"

import Link from "next/link"
import styles from "./app-shell.module.css"
import { usePathname, useRouter } from "next/navigation"
import { useEffect, useState } from "react"
import { AccountMenu } from "@/components/account-menu"
import { DropdownMenu, DropdownMenuContent, DropdownMenuGroup, DropdownMenuItem, DropdownMenuLabel, DropdownMenuRadioGroup, DropdownMenuRadioItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { api, apiPost } from "@/lib/api"
import { WorkspaceSelectionProvider } from "@/hooks/workspace-selection"
import type { Application, ListResponse, Workspace, User } from "@/lib/types"
import { HugeiconsIcon } from "@hugeicons/react"
import { Home01Icon, Folder01Icon, Layers01Icon, Task01Icon, Settings02Icon, Audit01Icon, ArrowDown01Icon, GitBranchIcon, ServerStack01Icon, Key01Icon, Share01Icon, Route01Icon } from "@hugeicons/core-free-icons"

const navigation = [
  { href: "/", label: "Overview", icon: Home01Icon },
  { href: "/onboarding", label: "Get started", icon: Task01Icon, adminOnly: true },
  { href: "/applications", label: "Applications", icon: Layers01Icon },
  { href: "/approvals", label: "Approvals", icon: Task01Icon },
  { href: "/settings", label: "Instance settings", icon: Settings02Icon, adminOnly: true },
  { href: "/audit", label: "Audit trail", icon: Audit01Icon, adminOnly: true },
]

const connectionNavigation = [
  { section: "git-sources", label: "Git sources", icon: GitBranchIcon },
  { section: "clusters", label: "Clusters", icon: ServerStack01Icon },
  { section: "namespaces", label: "Namespaces", icon: Route01Icon },
  { section: "credentials", label: "Credentials", icon: Key01Icon },
  { section: "shares", label: "Shared connections", icon: Share01Icon },
]

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname()
  const router = useRouter()
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)
  const [workspaces, setWorkspaces] = useState<Workspace[]>([])
  const [workspacesLoading, setWorkspacesLoading] = useState(true)
  const [selectedWorkspaceId, setSelectedWorkspaceId] = useState("")
  const [routeWorkspace, setRouteWorkspace] = useState<Workspace | null>(null)
  const [routeApplication, setRouteApplication] = useState<Application | null>(null)

  useEffect(() => {
    let active = true
    api<{ user: User }>("/api/v1/auth/session")
      .then((response) => {
        if (active) setUser(response.user)
      })
      .catch(() => {
        if (active) router.replace("/login")
      })
      .finally(() => active && setLoading(false))
    return () => {
      active = false
    }
  }, [router])

  useEffect(() => {
    if (!user) return
    let active = true
    api<ListResponse<Workspace>>("/api/v1/workspaces")
      .then((response) => active && setWorkspaces(response.items))
      .catch(() => active && setWorkspaces([]))
      .finally(() => active && setWorkspacesLoading(false))
    return () => { active = false }
  }, [user])

  useEffect(() => {
    let active = true
    const segments = pathname.split("/").filter(Boolean)
    Promise.resolve().then(async () => {
      if (segments[0] === "workspaces" && segments[1] && segments[1] !== "new") {
        const workspaces = await api<ListResponse<Workspace>>("/api/v1/workspaces")
        if (active) { setRouteWorkspace(workspaces.items.find((item) => item.id === segments[1]) ?? null); setRouteApplication(null) }
      } else if (segments[0] === "applications" && segments[1] && segments[1] !== "new") {
        const [application, workspaces] = await Promise.all([
          api<Application>(`/api/v1/applications/${encodeURIComponent(segments[1])}`),
          api<ListResponse<Workspace>>("/api/v1/workspaces"),
        ])
        if (active) { setRouteApplication(application); setRouteWorkspace(workspaces.items.find((item) => item.id === application.workspaceId) ?? null) }
      } else if (active) { setRouteWorkspace(null); setRouteApplication(null) }
    }).catch(() => { if (active) { setRouteWorkspace(null); setRouteApplication(null) } })
    return () => { active = false }
  }, [pathname])

  useEffect(() => {
    let active = true
    void Promise.resolve().then(() => {
      if (!active) return
      if (!workspaces.length) {
        setSelectedWorkspaceId("")
        return
      }
      const segments = pathname.split("/").filter(Boolean)
      const routeWorkspaceId = segments[0] === "workspaces" && segments[1] !== "new"
        ? segments[1]
        : routeWorkspace?.id
      const stored = window.localStorage.getItem("justcd.selectedWorkspaceId")
      const preferred = routeWorkspaceId || stored
      const selected = workspaces.find((item) => item.id === preferred) ?? workspaces[0]
      setSelectedWorkspaceId(selected.id)
      window.localStorage.setItem("justcd.selectedWorkspaceId", selected.id)
    })
    return () => { active = false }
  }, [workspaces, pathname, routeWorkspace?.id])

  function selectWorkspace(id: string) {
    if (!workspaces.some((workspace) => workspace.id === id)) return
    setSelectedWorkspaceId(id)
    window.localStorage.setItem("justcd.selectedWorkspaceId", id)
    router.push(`/workspaces/${encodeURIComponent(id)}`)
  }

  const crumbs = pageCrumbs(pathname, routeWorkspace, routeApplication)

  async function signOut() {
    try {
      await apiPost("/api/v1/auth/logout")
    } finally {
      router.replace("/login")
    }
  }

  if (loading || !user) {
    return (
      <main className="grid min-h-svh place-items-center bg-muted/30">
        <div className="flex items-center gap-3 rounded-xl border bg-card px-4 py-3 text-sm text-muted-foreground shadow-sm">
          <span className="size-2 animate-pulse rounded-full bg-primary" />
          Loading your JustCD workspace…
        </div>
      </main>
    )
  }

  return (
    <div className="min-h-svh bg-[#f7f8f8] text-foreground dark:bg-background">
      <aside className="fixed bottom-3 left-3 top-3 z-20 hidden w-[224px] flex-col overflow-hidden rounded-[24px] border border-border/70 bg-card shadow-[0_8px_32px_-16px_rgb(0_0_0_/_0.2)] lg:flex">
        <Link href="/" className="flex shrink-0 items-center gap-3 px-5 pb-7 pt-6">
          <span className="grid size-9 place-items-center rounded-xl bg-primary text-sm font-bold text-primary-foreground">J</span>
          <span>
            <span className="block text-[15px] font-semibold tracking-tight">JustCD</span>
            <span className="block text-[11px] text-muted-foreground">continuous delivery</span>
          </span>
        </Link>
        <WorkspaceSelector workspaces={workspaces} selectedId={selectedWorkspaceId} onChange={selectWorkspace} loading={workspacesLoading} />
        <div className="min-h-0 flex-1 overflow-y-auto px-3 pb-5">
          <p className="px-4 pb-2 text-[10px] font-medium uppercase tracking-[0.12em] text-muted-foreground">Workspace</p>
          <nav aria-label="Workspace" className="space-y-1">
            {navigation.filter((item) => !item.adminOnly).map((item) => {
              const selected = isSelected(item.href, pathname)
              return (
                <Link
                  key={item.href}
                  href={item.href}
                  aria-current={selected ? "page" : undefined}
                  className={styles.navItem}
                >
                  <HugeiconsIcon icon={item.icon} strokeWidth={1.8} className="size-4 shrink-0" aria-hidden="true" />
                  {item.label}
                </Link>
              )
            })}
          </nav>
          {selectedWorkspaceId && <>
            <p className="px-4 pb-2 pt-7 text-[10px] font-medium uppercase tracking-[0.12em] text-muted-foreground">Connections</p>
            <nav aria-label="Workspace connections" className="space-y-1">
              {connectionNavigation.map((item) => {
                const href = `/workspaces/${selectedWorkspaceId}/connections/${item.section}`
                const selected = isSelected(href, pathname) || (item.section === "clusters" && pathname === `/workspaces/${selectedWorkspaceId}/clusters/new`) || (item.section === "git-sources" && pathname === `/workspaces/${selectedWorkspaceId}/git-sources/new`)
                return <Link key={item.section} href={href} aria-current={selected ? "page" : undefined} className={styles.navItem}><HugeiconsIcon icon={item.icon} strokeWidth={1.8} className="size-4 shrink-0" aria-hidden="true" />{item.label}</Link>
              })}
            </nav>
          </>}
          {user.isAdmin && <>
            <p className="px-4 pb-2 pt-7 text-[10px] font-medium uppercase tracking-[0.12em] text-muted-foreground">Administration</p>
            <nav aria-label="Administration" className="space-y-1">
              {navigation.filter((item) => item.adminOnly).map((item) => {
                const selected = isSelected(item.href, pathname)
                return <Link key={item.href} href={item.href} aria-current={selected ? "page" : undefined} className={styles.navItem}><HugeiconsIcon icon={item.icon} strokeWidth={1.8} className="size-4 shrink-0" aria-hidden="true" />{item.label}</Link>
              })}
            </nav>
          </>}
        </div>
        <AccountMenu user={user} onSignOut={signOut} />
      </aside>

      <div className="lg:pl-[248px]">
        <header className="flex h-14 items-center justify-between gap-4 px-5 sm:px-8 lg:hidden">
          <Link href="/" className="flex items-center gap-2">
            <span className="grid size-8 place-items-center rounded-lg bg-primary font-bold text-primary-foreground">J</span>
            <span className="font-semibold">JustCD</span>
          </Link>
          <AccountMenu user={user} onSignOut={signOut} compact />
        </header>
        <div className="mx-4 mb-2 lg:hidden">
          <WorkspaceSelector workspaces={workspaces} selectedId={selectedWorkspaceId} onChange={selectWorkspace} compact loading={workspacesLoading} />
        </div>
        <nav aria-label="Main navigation" className="mx-4 mt-3 flex gap-1 overflow-x-auto rounded-2xl border border-border/70 bg-card p-1.5 shadow-sm lg:hidden">
          {navigation.filter((item) => !item.adminOnly || user.isAdmin).map((item) => {
            const selected = isSelected(item.href, pathname)
            return <Link key={item.href} href={item.href} aria-current={selected ? "page" : undefined} className={`${styles.navItem} shrink-0 !px-3 !py-2 !text-xs`}>{item.label}</Link>
          })}
          {selectedWorkspaceId && connectionNavigation.map((item) => {
            const href = `/workspaces/${selectedWorkspaceId}/connections/${item.section}`
            const selected = isSelected(href, pathname)
            return <Link key={item.section} href={href} aria-current={selected ? "page" : undefined} className={`${styles.navItem} shrink-0 !px-3 !py-2 !text-xs`}>{item.label}</Link>
          })}
        </nav>
        <main style={{ paddingBottom: "calc(2rem + var(--toast-clearance, 0px))" }} className={`mx-auto w-full ${/^\/applications\/[^/]+$/.test(pathname) && !pathname.endsWith("/new") ? "max-w-none" : "max-w-[1440px]"} px-5 pb-7 pt-5 sm:px-8 sm:pb-8 sm:pt-6 lg:pt-9 ${pathname === "/audit" ? "lg:flex lg:h-dvh lg:flex-col lg:overflow-hidden" : ""}`}>
          {crumbs.length > 1 && <nav aria-label="Breadcrumb" className="mb-4 shrink-0 text-xs">
            <ol className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
              {crumbs.map((crumb, index) => <li key={`${crumb.label}-${index}`} className="flex min-w-0 max-w-full items-center gap-2">
                {index > 0 && <span aria-hidden="true" className="text-muted-foreground/50">/</span>}
                {crumb.href ? <Link href={crumb.href} className="truncate text-muted-foreground transition-colors hover:text-foreground">{crumb.label}</Link> : <span aria-current="page" className="truncate text-muted-foreground">{crumb.label}</span>}
              </li>)}
            </ol>
          </nav>}
          <WorkspaceSelectionProvider value={{ workspaces, workspaceId: selectedWorkspaceId, workspace: workspaces.find((item) => item.id === selectedWorkspaceId) ?? null, selectWorkspace }}>
            {children}
          </WorkspaceSelectionProvider>
        </main>
      </div>
    </div>
  )
}

function WorkspaceSelector({
  workspaces,
  selectedId,
  onChange,
  loading = false,
  compact = false,
}: {
  workspaces: Workspace[]
  selectedId: string
  onChange: (id: string) => void
  loading?: boolean
  compact?: boolean
}) {
  const router = useRouter()
  if (loading) {
    return <div role="status" className={`mx-3 mb-4 h-10 animate-pulse rounded-lg border bg-muted/40 motion-reduce:animate-none ${compact ? "mx-0" : ""}`} />
  }
  if (!workspaces.length) {
    return <Link href="/workspaces/new" className={`mx-3 mb-4 flex h-10 items-center justify-between rounded-lg border border-dashed px-3 text-xs font-medium text-primary hover:bg-muted/40 ${compact ? "mx-0" : ""}`}>Create a workspace <span aria-hidden="true">＋</span></Link>
  }
  return (
    <DropdownMenu>
      <DropdownMenuTrigger aria-label="Select workspace" className={`mx-3 mb-4 flex h-10 min-w-0 items-center gap-2 rounded-lg border bg-background px-3 text-left text-xs font-medium outline-none transition-colors hover:bg-muted/50 focus-visible:ring-2 focus-visible:ring-ring ${compact ? "mx-0 w-full" : "w-[calc(100%-1.5rem)]"}`}>
        <HugeiconsIcon icon={Folder01Icon} className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
        <span className="min-w-0 flex-1 truncate">{workspaces.find((workspace) => workspace.id === selectedId)?.name ?? "Select workspace"}</span>
        <HugeiconsIcon icon={ArrowDown01Icon} className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
      </DropdownMenuTrigger>
      <DropdownMenuContent sideOffset={6} className="min-w-48 rounded-lg p-1.5">
        <DropdownMenuGroup><DropdownMenuLabel>Switch workspace</DropdownMenuLabel></DropdownMenuGroup>
        <DropdownMenuRadioGroup value={selectedId} onValueChange={onChange} aria-label="Workspaces">
          {workspaces.map((workspace) => <DropdownMenuRadioItem key={workspace.id} value={workspace.id} className="min-w-0 py-2"><span className="truncate">{workspace.name}</span></DropdownMenuRadioItem>)}
        </DropdownMenuRadioGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={() => router.push(`/workspaces/${selectedId}${workspaces.find((workspace) => workspace.id === selectedId)?.role === "owner" ? "?tab=settings" : "?tab=members"}`)} className="gap-2 py-2"><HugeiconsIcon icon={Settings02Icon} className="size-4" aria-hidden="true" />Manage current workspace</DropdownMenuItem>
        <DropdownMenuItem onClick={() => router.push("/workspaces/new")} className="gap-2 py-2"><span aria-hidden="true" className="grid size-4 place-items-center text-base leading-none">+</span>Create workspace</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function isSelected(href: string, pathname: string) {
  if (href === "/") return pathname === "/"
  return pathname === href || pathname.startsWith(`${href}/`)
}

function pageCrumbs(pathname: string, workspace: Workspace | null, application: Application | null) {
  const parts = pathname.split("/").filter(Boolean)
  if (!parts.length) return [{ label: "Overview" }]
  if (parts[0] === "workspaces") {
    if (!parts[1]) return [{ label: "Overview" }]
    if (parts[2] === "connections") {
      const connection = { "git-sources": "Git sources", clusters: "Kubernetes clusters", namespaces: "Namespace bindings", credentials: "Credentials", shares: "Shared connections" }[parts[3]]
      return [{ label: connection ?? "Connections" }]
    }
    if (parts[2] === "clusters" && parts[3] === "new") return [{ label: "Connect cluster" }]
    if (parts[2] === "git-sources" && parts[3] === "new") return [{ label: "Connect Git source" }]
    return [{ label: parts[1] === "new" ? "New workspace" : workspace?.name ?? "Workspace" }]
  }
  if (parts[0] === "applications") {
    if (!parts[1]) return [{ label: "Applications" }]
    if (parts[1] === "new") return [{ label: "Applications", href: "/applications" }, { label: "New application" }]
    return [{ label: "Applications", href: "/applications" }, { label: application?.name ?? "Application" }]
  }
  if (parts[0] === "settings") {
    const section = { "git-sources": "Git sources", clusters: "Kubernetes clusters", namespaces: "Namespace bindings", credentials: "Credentials", oidc: "OIDC providers", users: "Local users" }[parts[1] as "git-sources"]
    return parts[1] ? [{ label: "Instance settings", href: "/settings" }, { label: section ?? "Section" }] : [{ label: "Instance settings" }]
  }
  if (parts[0] === "onboarding") return [{ label: "Get started" }]
  if (parts[0] === "approvals") return [{ label: "Approvals" }]
  if (parts[0] === "audit") return [{ label: "Audit trail" }]
  return [{ label: "Overview", href: "/" }, { label: "Not found" }]
}
