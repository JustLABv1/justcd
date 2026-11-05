"use client"

import { DocumentationLink } from "@/components/documentation-link"
import { LogoMark } from "@/components/logo"
import Link from "next/link"
import styles from "./app-shell.module.css"
import { usePathname, useRouter } from "next/navigation"
import { useEffect, useMemo, useState } from "react"
import { AccountMenu } from "@/components/account-menu"
import { CommandPalette, CommandPaletteButton } from "@/components/command-palette"
import { Select } from "@/components/panelui/select"
import { Button, buttonVariants } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { APIError, api, apiPost, errorMessage } from "@/lib/api"
import { WorkspaceSelectionProvider } from "@/hooks/workspace-selection"
import type { Application, ListResponse, OnboardingStatus, Workspace, User } from "@/lib/types"
import { HugeiconsIcon } from "@hugeicons/react"
import { Home01Icon, Folder01Icon, Layers01Icon, Task01Icon, Settings02Icon, Audit01Icon, Link01Icon, Add01Icon, Rocket01Icon, SidebarLeftIcon, SidebarRightIcon } from "@hugeicons/core-free-icons"

type NavItem = { href: string; label: string; icon: typeof Home01Icon; badge?: number; dot?: boolean }

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
  const [collapsed, setCollapsed] = useState(false)
  const [paletteOpen, setPaletteOpen] = useState(false)
  const [pendingApprovals, setPendingApprovals] = useState(0)

  useEffect(() => {
    // Restore after mount so server and client markup match.
    void Promise.resolve().then(() => {
      try { if (window.localStorage.getItem("justcd.sidebarCollapsed") === "1") setCollapsed(true) } catch { /* storage unavailable */ }
    })
  }, [])
  function toggleSidebar() {
    setCollapsed((previous) => {
      const next = !previous
      try { window.localStorage.setItem("justcd.sidebarCollapsed", next ? "1" : "0") } catch { /* storage unavailable */ }
      return next
    })
  }
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (!(event.metaKey || event.ctrlKey) || event.altKey || event.shiftKey || event.key.toLowerCase() !== "b") return
      const target = event.target as HTMLElement | null
      if (target && (target.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName))) return
      event.preventDefault()
      toggleSidebar()
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [])

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
    const applicationId = segments[0] === "applications" && segments[1] && !["new", "publish"].includes(segments[1]) ? segments[1] : null
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

  // Pending approvals drive the badge on the Approvals nav item.
  useEffect(() => {
    if (!selectedWorkspaceId) return
    let active = true
    const load = () => {
      if (document.visibilityState === "hidden") return
      void api<{ total: number }>(`/api/v1/approval-inbox?workspaceId=${encodeURIComponent(selectedWorkspaceId)}&page=1&limit=1`)
        .then((response) => { if (active) setPendingApprovals(response.total) }, () => { if (active) setPendingApprovals(0) })
    }
    load()
    const timer = window.setInterval(load, 30000)
    return () => { active = false; window.clearInterval(timer) }
  }, [selectedWorkspaceId, pathname])

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
  const administration = adminNavigation.filter((item) => item.href !== "/onboarding" || onboardingIncomplete).map((item) => item.href === "/onboarding" ? { ...item, dot: true } : item)
  const delivery = deliveryNavigation.map((item) => item.href === "/approvals" ? { ...item, badge: pendingApprovals } : item)
  const mobileGroups = [
    { label: "Delivery", items: delivery },
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
      <aside aria-label="Sidebar" className={`fixed bottom-3 left-3 top-3 z-20 hidden ${collapsed ? "w-[72px]" : "w-[224px]"} flex-col overflow-hidden transition-[width] duration-200 motion-reduce:transition-none rounded-[24px] border border-border/70 bg-card shadow-[0_8px_32px_-16px_rgb(0_0_0_/_0.2)] lg:flex`}>
        <div className={`flex shrink-0 items-center ${collapsed ? "justify-center pb-4 pt-6" : "justify-between gap-2 py-6 pl-5 pr-3"}`}>
                  <Link href="/" aria-label="JustCD home" className={`flex min-w-0 items-center gap-3 ${collapsed ? "justify-center" : ""}`}>
          <LogoMark className="h-9 w-auto shrink-0 text-foreground" />
          {!collapsed && <span className="text-[15px] font-semibold tracking-tight">JustCD</span>}
        </Link>
          {!collapsed && <CommandPaletteButton onClick={() => setPaletteOpen(true)} />}
        </div>
        <WorkspaceSelector workspaces={workspaces} selectedId={selectedWorkspaceId} onChange={selectWorkspace} loading={workspacesLoading} collapsed={collapsed} />
        {collapsed && <div className="mb-3 flex justify-center"><CommandPaletteButton collapsed onClick={() => setPaletteOpen(true)} /></div>}
        <div className="min-h-0 flex-1 overflow-y-auto px-3 pb-5">
          <NavGroup label="Delivery" items={delivery} pathname={pathname} first collapsed={collapsed} />
          {selectedWorkspaceId && <NavGroup label="Workspace" items={workspaceNavigation} pathname={pathname} collapsed={collapsed} />}
          {user.isAdmin && <NavGroup label="Administration" items={administration} pathname={pathname} collapsed={collapsed} />}
        </div>
        <div className="space-y-1 px-3 pb-2">
          <DocumentationLink collapsed={collapsed} />
          {collapsed
            ? <Tooltip>
              <TooltipTrigger render={<button type="button" aria-label="Expand sidebar" aria-expanded={false} aria-keyshortcuts="Control+B Meta+B" onClick={toggleSidebar} className={`${styles.navItem} ${styles.navItemCollapsed} cursor-pointer`} />}>
                <HugeiconsIcon icon={SidebarRightIcon} strokeWidth={1.8} className="size-4 shrink-0" aria-hidden="true" />
              </TooltipTrigger>
              <TooltipContent side="right">Expand sidebar · Ctrl/⌘ B</TooltipContent>
            </Tooltip>
            : <button type="button" aria-expanded aria-keyshortcuts="Control+B Meta+B" onClick={toggleSidebar} className={`${styles.navItem} w-full cursor-pointer`}>
              <HugeiconsIcon icon={SidebarLeftIcon} strokeWidth={1.8} className="size-4 shrink-0" aria-hidden="true" />
              Collapse
              <kbd className="ml-auto text-[11px] font-medium text-muted-foreground/70">⌘B</kbd>
            </button>}
        </div>
        <AccountMenu user={user} onSignOut={signOut} collapsed={collapsed} />
      </aside>

      <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} isAdmin={user.isAdmin} workspaces={workspaces} workspaceId={selectedWorkspaceId} onSelectWorkspace={selectWorkspace} onToggleSidebar={toggleSidebar} onboardingIncomplete={onboardingIncomplete} />

      <div className={`${collapsed ? "lg:pl-[96px]" : "lg:pl-[248px]"} transition-[padding] duration-200 motion-reduce:transition-none`}>
        <header className="flex h-14 items-center justify-between gap-4 px-5 sm:px-8 lg:hidden">
          <Link href="/" className="flex items-center gap-2">
            <LogoMark className="h-7 w-auto text-foreground" />
            <span className="font-semibold">JustCD</span>
          </Link>
          <div className="flex items-center gap-1"><CommandPaletteButton compact onClick={() => setPaletteOpen(true)} /><DocumentationLink compact /><AccountMenu user={user} onSignOut={signOut} compact /></div>
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
            {group.items.map((item) => <Link key={item.href} href={item.href} aria-current={isNavSelected(item.href, pathname) ? "page" : undefined} className={`${styles.navItem} shrink-0 !py-1.5 !text-xs`}><HugeiconsIcon icon={item.icon} strokeWidth={1.8} className="size-4 shrink-0" aria-hidden="true" />{item.label}<NavBadge item={item} /></Link>)}
          </div>)}
        </nav>
        <main style={{ paddingBottom: "calc(2rem + var(--toast-clearance, 0px))" }} className={`mx-auto w-full ${/^\/applications\/[^/]+$/.test(pathname) && !pathname.endsWith("/new") && !pathname.endsWith("/publish") ? "max-w-none" : "max-w-[1440px]"} px-5 pb-7 pt-5 sm:px-8 sm:pb-8 sm:pt-6 lg:pt-9 ${pathname === "/audit" ? "lg:flex lg:h-dvh lg:flex-col lg:overflow-hidden" : ""}`}>
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
  collapsed = false,
}: {
  workspaces: Workspace[]
  selectedId: string
  onChange: (id: string) => void
  loading?: boolean
  compact?: boolean
  collapsed?: boolean
}) {
  const router = useRouter()
  if (loading && collapsed) return <div role="status" className="mx-auto mb-4 size-10 animate-pulse rounded-lg border bg-muted/40 motion-reduce:animate-none" />
  if (!loading && !workspaces.length && collapsed) return <Link href="/workspaces/new" aria-label="Create a workspace" title="Create a workspace" className="mx-auto mb-4 grid size-10 place-items-center rounded-lg border border-dashed text-primary hover:bg-muted/40"><HugeiconsIcon icon={Add01Icon} strokeWidth={1.8} className="size-4" aria-hidden="true" /></Link>
  if (loading) {
    return <div role="status" className={`mx-3 mb-4 h-10 animate-pulse rounded-lg border bg-muted/40 motion-reduce:animate-none ${compact ? "mx-0" : ""}`} />
  }
  if (!workspaces.length) {
    return <Link href="/workspaces/new" className={`mx-3 mb-4 flex h-10 items-center justify-between rounded-lg border border-dashed px-3 text-xs font-medium text-primary hover:bg-muted/40 ${compact ? "mx-0" : ""}`}>Create a workspace <HugeiconsIcon icon={Add01Icon} strokeWidth={1.8} className="size-4" aria-hidden="true" /></Link>
  }
  const selected = workspaces.find((workspace) => workspace.id === selectedId)
  const items = workspaces.map((workspace) => ({ value: workspace.id, label: workspace.name }))
  // The two shortcut entries are not workspaces, so they navigate instead of changing the selection.
  function choose(value: string | null) {
    if (value === "__settings") router.push(`/workspaces/${selectedId}`)
    else if (value === "__new") router.push("/workspaces/new")
    else if (value) onChange(value)
  }
  return (
    <Select items={items} value={selectedId} onValueChange={choose}>
      {collapsed
        ? <Select.Trigger variant="ghost" icon={null} aria-label={`Select workspace · ${selected?.name ?? ""}`} title={selected?.name} className="mx-auto mb-3 size-11 justify-center rounded-xl border bg-background p-0">
          <HugeiconsIcon icon={Folder01Icon} className="size-4 text-muted-foreground" aria-hidden="true" />
        </Select.Trigger>
        : <Select.Trigger variant="default" size="sm" aria-label="Select workspace" className={`mb-4 h-9 rounded-xl px-2.5 ${compact ? "" : "mx-3 !w-[calc(100%-1.5rem)]"}`}>
          <HugeiconsIcon icon={Folder01Icon} className="size-4 text-muted-foreground" aria-hidden="true" />
          <Select.Value placeholder="Select workspace" className="text-[13px] font-medium" />
        </Select.Trigger>}
      <Select.Content side={collapsed ? "right" : "bottom"} alignItemWithTrigger={false} className="min-w-52">
        <Select.Group>
          <Select.GroupLabel>Switch workspace</Select.GroupLabel>
          {workspaces.map((workspace) => <Select.Item key={workspace.id} value={workspace.id} icon={<HugeiconsIcon icon={Folder01Icon} aria-hidden="true" />}>{workspace.name}</Select.Item>)}
        </Select.Group>
        <Select.Separator />
        <Select.Item value="__settings" icon={<HugeiconsIcon icon={Settings02Icon} aria-hidden="true" />}>Workspace settings</Select.Item>
        <Select.Item value="__new" icon={<HugeiconsIcon icon={Add01Icon} aria-hidden="true" />}>New workspace</Select.Item>
      </Select.Content>
    </Select>
  )
}

function NavGroup({ label, items, pathname, first = false, collapsed = false }: { label: string; items: NavItem[]; pathname: string; first?: boolean; collapsed?: boolean }) {
  return <>
    {collapsed
      ? !first && <div aria-hidden="true" className="mx-3 my-3 border-t" />
      : <p className={`px-4 pb-2 text-[11px] font-semibold uppercase tracking-[0.08em] text-muted-foreground ${first ? "" : "pt-7"}`}>{label}</p>}
    <nav aria-label={label} className="space-y-1">
      {items.map((item) => {
        const current = isNavSelected(item.href, pathname) ? "page" : undefined
        const selected = isNavSelected(item.href, pathname)
        const icon = <HugeiconsIcon icon={item.icon} strokeWidth={1.8} className={`size-4 shrink-0 ${selected ? "text-primary" : ""}`} aria-hidden="true" />
        return collapsed
          ? <Tooltip key={item.href}>
            <TooltipTrigger render={<Link href={item.href} aria-label={item.badge ? `${item.label} (${item.badge} pending)` : item.label} aria-current={current} className={`${styles.navItem} ${styles.navItemCollapsed} relative`} />}>
              {icon}
              {(item.badge || item.dot) ? <span aria-hidden="true" className={`absolute right-1 top-1 size-2.5 rounded-full border-2 border-card ${item.badge ? "bg-rose-500" : "bg-emerald-500"}`} /> : null}
            </TooltipTrigger>
            <TooltipContent side="right">{item.label}</TooltipContent>
          </Tooltip>
          : <Link key={item.href} href={item.href} aria-current={current} className={styles.navItem}>{icon}{item.label}<NavBadge item={item} /></Link>
      })}
    </nav>
  </>
}

function NavBadge({ item }: { item: NavItem }) {
  if (item.badge) return <span className="ml-auto min-w-5 rounded-full bg-rose-500 px-1.5 py-0.5 text-center text-[11px] font-semibold leading-none text-white" aria-label={`${item.badge} pending`}>{item.badge > 99 ? "99+" : item.badge}</span>
  if (item.dot) return <span className="ml-auto size-2 rounded-full bg-emerald-500" role="img" aria-label="Setup incomplete" />
  return null
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
    if (parts[1] === "publish" && parts[2] === "manual") return [{ label: "Applications", href: "/applications" }, { label: "Templates", href: "/applications/publish" }, { label: "Manual publishing" }]
    if (parts[1] === "publish") return [{ label: "Applications", href: "/applications" }, { label: "Templates" }]
    if (parts[2] === "publish") return [{ label: "Applications", href: "/applications" }, { label: application?.name ?? "Application", href: `/applications/${parts[1]}` }, { label: "Publish a new version" }]
    return [{ label: "Applications", href: "/applications" }, { label: application?.name ?? "Application" }]
  }
  if (parts[0] === "settings" && parts[1] === "security") return [{ label: "Account security" }]
  if (parts[0] === "settings") {
    const section = { "git-sources": "Git sources", clusters: "Clusters", namespaces: "Namespace access", credentials: "Credentials", ai: "AI", oidc: "OIDC providers", users: "Local users" }[parts[1] as "git-sources"]
    return parts[1] ? [{ label: "Instance settings", href: "/settings" }, { label: section ?? "Section" }] : [{ label: "Instance settings" }]
  }
  if (parts[0] === "onboarding") return [{ label: "Get started" }]
  if (parts[0] === "approvals") return [{ label: "Approvals" }]
  if (parts[0] === "audit") return [{ label: "Audit trail" }]
  return [{ label: "Overview", href: "/" }, { label: "Not found" }]
}
