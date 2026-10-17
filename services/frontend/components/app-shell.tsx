"use client"

import { DocumentationLink } from "@/components/documentation-link"
import { LogoMark } from "@/components/logo"
import Link from "next/link"
import styles from "./app-shell.module.css"
import { usePathname, useRouter } from "next/navigation"
import { useEffect, useMemo, useState } from "react"
import { AccountMenu } from "@/components/account-menu"
import { Button, buttonVariants } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuGroup, DropdownMenuItem, DropdownMenuLabel, DropdownMenuRadioGroup, DropdownMenuRadioItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { APIError, api, apiPost, errorMessage } from "@/lib/api"
import { WorkspaceSelectionProvider } from "@/hooks/workspace-selection"
import type { Application, ListResponse, OnboardingStatus, Workspace, User } from "@/lib/types"
import { HugeiconsIcon } from "@hugeicons/react"
import { Home01Icon, Folder01Icon, Layers01Icon, Task01Icon, Settings02Icon, Audit01Icon, ArrowDown01Icon, Link01Icon, Add01Icon, Rocket01Icon } from "@hugeicons/core-free-icons"

type NavItem = { href: string; label: string; icon: typeof Home01Icon }

const deliveryNavigation: NavItem[] = [
  { href: "/", label: "Overview", icon: Home01Icon },
  { href: "/applications", label: "Applications", icon: Layers01Icon },
  { href: "/approvals", label: "Approvals", icon: Task01Icon },
]

const adminNavigation: NavItem[] = [
  { href: "/onboarding", label: "Get started", icon: Rocket01Icon },
  { href: "/settings", label: "Instance settings", icon: Settings02Icon },
  { href: "/audit", label: "Audit trail", icon: Audit01Icon },
]

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname()
  const router = useRouter()
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)
  const [sessionError, setSessionError] = useState<unknown>(null)
  const [workspaces, setWorkspaces] = useState<Workspace[]>([])
  const [workspacesLoading, setWorkspacesLoading] = useState(true)
  const [selectedWorkspaceId, setSelectedWorkspaceId] = useState("")
  const [routeApplication, setRouteApplication] = useState<Application | null>(null)
  const [onboardingIncomplete, setOnboardingIncomplete] = useState(false)

  useEffect(() => {
    let active = true
    api<{ user: User }>("/api/v1/auth/session")
      .then((response) => {
        if (active) setUser(response.user)
      })
      .catch((cause) => {
        if (!active) return
        if (cause instanceof APIError && cause.status === 401) router.replace("/login")
        else setSessionError(cause)
      })
      .finally(() => active && setLoading(false))
    return () => {
      active = false
    }
  }, [router])

  useEffect(() => {
    if (!user?.isAdmin) return
    let active = true
    const refresh = () => {
      void api<OnboardingStatus>("/api/v1/onboarding")
        .then((status) => {
          if (active) setOnboardingIncomplete(status.total > 0 && status.completed < status.total)
        })
        .catch(() => {
          // Keep the checklist reachable if its readiness check is unavailable.
          if (active) setOnboardingIncomplete(true)
        })
    }
    refresh()
    window.addEventListener("justcd:onboarding-updated", refresh)
    return () => {
      active = false
      window.removeEventListener("justcd:onboarding-updated", refresh)
    }
  }, [user?.isAdmin, pathname])

  // The workspace list is fetched once per route change and shared through state.
  useEffect(() => {
    if (!user) return
    let active = true
    const segments = pathname.split("/").filter(Boolean)
    const applicationId = segments[0] === "applications" && segments[1] && segments[1] !== "new" ? segments[1] : null
    void Promise.resolve().then(async () => {
      const [list, application] = await Promise.all([
        api<ListResponse<Workspace>>("/api/v1/workspaces").then((response) => response.items, () => null),
        applicationId ? api<Application>(`/api/v1/applications/${encodeURIComponent(applicationId)}`).catch(() => null) : Promise.resolve(null),
      ])
      if (!active) return
      if (list) setWorkspaces(list)
      setRouteApplication(application)
      setWorkspacesLoading(false)
    })
    return () => { active = false }
  }, [user, pathname])

  const routeWorkspace = useMemo(() => {
    const segments = pathname.split("/").filter(Boolean)
    if (segments[0] === "workspaces" && segments[1] && segments[1] !== "new") return workspaces.find((item) => item.id === segments[1]) ?? null
    if (segments[0] === "applications" && routeApplication && routeApplication.id === segments[1]) return workspaces.find((item) => item.id === routeApplication.workspaceId) ?? null
    return null
  }, [pathname, workspaces, routeApplication])

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
  const showAdmin = user?.isAdmin ?? false

  async function signOut() {
    try {
      await apiPost("/api/v1/auth/logout")
    } finally {
      router.replace("/login")
    }
  }

  const workspaceNavigation: NavItem[] = [
    { href: `/workspaces/${selectedWorkspaceId}/connections`, label: "Connections", icon: Link01Icon },
    { href: `/workspaces/${selectedWorkspaceId}`, label: "Workspace settings", icon: Settings02Icon },
    ...(!user?.isAdmin ? [{ href: "/audit", label: "Audit trail", icon: Audit01Icon }] : []),
  ]
  const administration = adminNavigation.filter((item) => item.href !== "/onboarding" || onboardingIncomplete)
  const mobileGroups = [
    { label: "Delivery", items: deliveryNavigation },
    ...(showAdmin ? [{ label: "Administration", items: administration }] : []),
  ]

  if (loading || !user) {
    return (
      <main className="grid min-h-svh place-items-center bg-muted/30">
        <div role={sessionError ? "alert" : "status"} className="flex max-w-md flex-wrap items-center gap-3 rounded-xl border bg-card px-4 py-3 text-sm text-muted-foreground shadow-sm">
          {sessionError ? <><span>{errorMessage(sessionError)}</span><Button onClick={() => window.location.reload()}>Try again</Button></> : <><span className="size-2 animate-pulse rounded-full bg-primary motion-reduce:animate-none" />Loading your JustCD workspace…</>}
        </div>
      </main>
    )
  }

  return (
    <div className="min-h-svh overflow-x-clip bg-muted text-foreground dark:bg-background">
      <aside className="fixed bottom-3 left-3 top-3 z-20 hidden w-[224px] flex-col overflow-hidden rounded-[24px] border border-border/70 bg-card shadow-[0_8px_32px_-16px_rgb(0_0_0_/_0.2)] lg:flex">
        <Link href="/" className="flex shrink-0 items-center gap-3 px-5 pb-7 pt-6">
          <LogoMark className="h-9 w-auto shrink-0 text-foreground" />
          <span>
            <span className="block text-[15px] font-semibold tracking-tight">JustCD</span>
            <span className="block text-xs text-muted-foreground">continuous delivery</span>
          </span>
        </Link>
        <WorkspaceSelector workspaces={workspaces} selectedId={selectedWorkspaceId} onChange={selectWorkspace} loading={workspacesLoading} />
        <div className="min-h-0 flex-1 overflow-y-auto px-3 pb-5">
          <NavGroup label="Delivery" items={deliveryNavigation} pathname={pathname} first />
          {selectedWorkspaceId && <NavGroup label="Workspace" items={workspaceNavigation} pathname={pathname} />}
          {user.isAdmin && <NavGroup label="Administration" items={administration} pathname={pathname} />}
        </div>
        <div className="px-3 pb-2"><DocumentationLink /></div>
        <AccountMenu user={user} onSignOut={signOut} />
      </aside>

      <div className="lg:pl-[248px]">
        <header className="flex h-14 items-center justify-between gap-4 px-5 sm:px-8 lg:hidden">
          <Link href="/" className="flex items-center gap-2">
            <LogoMark className="h-7 w-auto text-foreground" />
            <span className="font-semibold">JustCD</span>
          </Link>
          <div className="flex items-center gap-1"><DocumentationLink compact /><AccountMenu user={user} onSignOut={signOut} compact /></div>
        </header>
        <div className="mx-4 mb-2 lg:hidden">
          <WorkspaceSelector workspaces={workspaces} selectedId={selectedWorkspaceId} onChange={selectWorkspace} compact loading={workspacesLoading} />
          {selectedWorkspaceId && <div className="grid grid-cols-2 gap-2">
            {workspaceNavigation.map((item) => <Link key={item.href} className={buttonVariants({ variant: isNavSelected(item.href, pathname) ? "secondary" : "outline", className: "min-w-0 h-auto justify-start py-2 text-left" })} aria-current={isNavSelected(item.href, pathname) ? "page" : undefined} href={item.href}>
              <HugeiconsIcon icon={item.icon} className="size-4" aria-hidden="true" /><span className="min-w-0 whitespace-normal">{item.label}</span>
            </Link>)}
          </div>}

        </div>
        <nav aria-label="Main navigation" className={`mx-4 mt-3 flex lg:hidden ${styles.mobileNav}`}>
          {mobileGroups.map((group, groupIndex) => <div key={group.label} role="group" aria-label={group.label} className="flex shrink-0 items-center gap-1">
            {groupIndex > 0 && <span aria-hidden="true" className="mx-1 h-5 w-px shrink-0 bg-border" />}
            <span className="shrink-0 px-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">{group.label}</span>
            {group.items.map((item) => <Link key={item.href} href={item.href} aria-current={isNavSelected(item.href, pathname) ? "page" : undefined} className={`${styles.navItem} shrink-0 !px-3 !py-2 !text-xs`}><HugeiconsIcon icon={item.icon} strokeWidth={1.8} className="size-4 shrink-0" aria-hidden="true" />{item.label}</Link>)}
          </div>)}
        </nav>
        <main style={{ paddingBottom: "calc(2rem + var(--toast-clearance, 0px))" }} className={`mx-auto w-full ${/^\/applications\/[^/]+$/.test(pathname) && !pathname.endsWith("/new") ? "max-w-none" : "max-w-[1440px]"} px-5 pb-7 pt-5 sm:px-8 sm:pb-8 sm:pt-6 lg:pt-9 ${pathname === "/audit" ? "lg:flex lg:h-dvh lg:flex-col lg:overflow-hidden" : ""}`}>
          {crumbs.length > 1 && <nav aria-label="Breadcrumb" className="mb-4 shrink-0 text-sm">
            <ol className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
              {crumbs.map((crumb, index) => <li key={`${crumb.label}-${index}`} className="flex min-w-0 max-w-full items-center gap-2">
                {index > 0 && <span aria-hidden="true" className="text-muted-foreground/50">/</span>}
                {crumb.href ? <Link href={crumb.href} className="truncate text-muted-foreground transition-colors hover:text-foreground">{crumb.label}</Link> : <span aria-current="page" className="truncate text-muted-foreground">{crumb.label}</span>}
              </li>)}
            </ol>
          </nav>}
          <WorkspaceSelectionProvider value={{ isAdmin: user.isAdmin, workspaces, workspaceId: selectedWorkspaceId, workspace: workspaces.find((item) => item.id === selectedWorkspaceId) ?? null, selectWorkspace }}>
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
    return <Link href="/workspaces/new" className={`mx-3 mb-4 flex h-10 items-center justify-between rounded-lg border border-dashed px-3 text-xs font-medium text-primary hover:bg-muted/40 ${compact ? "mx-0" : ""}`}>Create a workspace <HugeiconsIcon icon={Add01Icon} strokeWidth={1.8} className="size-4" aria-hidden="true" /></Link>
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
        <DropdownMenuItem onClick={() => router.push(`/workspaces/${selectedId}`)} className="gap-2 py-2"><HugeiconsIcon icon={Settings02Icon} className="size-4" aria-hidden="true" />Workspace settings</DropdownMenuItem>
        <DropdownMenuItem onClick={() => router.push("/workspaces/new")} className="gap-2 py-2"><HugeiconsIcon icon={Add01Icon} className="size-4" aria-hidden="true" />New workspace</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function NavGroup({ label, items, pathname, first = false }: { label: string; items: NavItem[]; pathname: string; first?: boolean }) {
  return <>
    <p className={`px-4 pb-2 text-[11px] font-semibold uppercase tracking-[0.08em] text-muted-foreground ${first ? "" : "pt-7"}`}>{label}</p>
    <nav aria-label={label} className="space-y-1">
      {items.map((item) => <Link key={item.href} href={item.href} aria-current={isNavSelected(item.href, pathname) ? "page" : undefined} className={styles.navItem}><HugeiconsIcon icon={item.icon} strokeWidth={1.8} className="size-4 shrink-0" aria-hidden="true" />{item.label}</Link>)}
    </nav>
  </>
}

function isNavSelected(href: string, pathname: string) {
  if (/^\/workspaces\/[^/]+$/.test(href)) return pathname === href
  if (isSelected(href, pathname)) return true
  const connections = /^\/workspaces\/([^/]+)\/connections$/.exec(href)
  return !!connections && (pathname === `/workspaces/${connections[1]}/clusters/new` || pathname === `/workspaces/${connections[1]}/git-sources/new`)
}

function isSelected(href: string, pathname: string) {
  if (href === "/") return pathname === "/"
  return pathname === href || pathname.startsWith(`${href}/`)
}

function pageCrumbs(pathname: string, workspace: Workspace | null, application: Application | null): { label: string; href?: string }[] {
  const parts = pathname.split("/").filter(Boolean)
  if (!parts.length) return [{ label: "Overview" }]
  if (parts[0] === "workspaces") {
    if (!parts[1]) return [{ label: "Overview" }]
    const parent = { label: workspace?.name ?? "Workspace", href: `/workspaces/${parts[1]}` }
    const connectionsParent = { label: "Connections", href: `/workspaces/${parts[1]}/connections` }
    const connectionsLabels: Record<string, string> = { "git-sources": "Git sources", clusters: "Kubernetes", namespaces: "Kubernetes", credentials: "Credentials", shares: "Sharing" }
    if (parts[2] === "connections") return parts[3] ? [parent, connectionsParent, { label: connectionsLabels[parts[3]] ?? "Connections" }] : [parent, { label: "Connections" }]
    if (parts[2] === "clusters" && parts[3] === "new") return [parent, connectionsParent, { label: "Connect cluster" }]
    if (parts[2] === "git-sources" && parts[3] === "new") return [parent, connectionsParent, { label: "Connect Git source" }]
    return [{ label: parts[1] === "new" ? "New workspace" : workspace?.name ?? "Workspace" }]
  }
  if (parts[0] === "applications") {
    if (!parts[1]) return [{ label: "Applications" }]
    if (parts[1] === "new") return [{ label: "Applications", href: "/applications" }, { label: "New application" }]
    return [{ label: "Applications", href: "/applications" }, { label: application?.name ?? "Application" }]
  }
  if (parts[0] === "settings" && parts[1] === "security") return [{ label: "Account security" }]
  if (parts[0] === "settings") {
    const section = { "git-sources": "Git sources", clusters: "Clusters", namespaces: "Namespace access", credentials: "Credentials", oidc: "OIDC providers", users: "Local users" }[parts[1] as "git-sources"]
    return parts[1] ? [{ label: "Instance settings", href: "/settings" }, { label: section ?? "Section" }] : [{ label: "Instance settings" }]
  }
  if (parts[0] === "onboarding") return [{ label: "Get started" }]
  if (parts[0] === "approvals") return [{ label: "Approvals" }]
  if (parts[0] === "audit") return [{ label: "Audit trail" }]
  return [{ label: "Overview", href: "/" }, { label: "Not found" }]
}
