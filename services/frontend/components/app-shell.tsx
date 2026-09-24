"use client"

import Link from "next/link"
import { usePathname, useRouter } from "next/navigation"
import { useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { api, apiPost } from "@/lib/api"
import type { Application, ListResponse, Project, User } from "@/lib/types"
import { HugeiconsIcon } from "@hugeicons/react"
import { Home01Icon, Folder01Icon, PlusSignIcon, Settings02Icon, Audit01Icon, Logout01Icon } from "@hugeicons/core-free-icons"

const navigation = [
  { href: "/", label: "Overview", icon: Home01Icon },
  { href: "/projects", label: "Projects", icon: Folder01Icon },
  { href: "/applications/new", label: "New application", icon: PlusSignIcon },
  { href: "/settings", label: "Settings", icon: Settings02Icon },
  { href: "/audit", label: "Audit trail", icon: Audit01Icon, adminOnly: true },
]

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname()
  const router = useRouter()
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)
  const [routeProject, setRouteProject] = useState<Project | null>(null)
  const [routeApplication, setRouteApplication] = useState<Application | null>(null)
  const [apiOnline, setApiOnline] = useState<boolean | null>(null)

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
    let active = true
    const segments = pathname.split("/").filter(Boolean)
    Promise.resolve().then(async () => {
      if (segments[0] === "projects" && segments[1] && segments[1] !== "new") {
        const projects = await api<ListResponse<Project>>("/api/v1/projects")
        if (active) { setRouteProject(projects.items.find((item) => item.id === segments[1]) ?? null); setRouteApplication(null) }
      } else if (segments[0] === "applications" && segments[1] && segments[1] !== "new") {
        const [application, projects] = await Promise.all([
          api<Application>(`/api/v1/applications/${encodeURIComponent(segments[1])}`),
          api<ListResponse<Project>>("/api/v1/projects"),
        ])
        if (active) { setRouteApplication(application); setRouteProject(projects.items.find((item) => item.id === application.projectId) ?? null) }
      } else if (active) { setRouteProject(null); setRouteApplication(null) }
    }).catch(() => { if (active) { setRouteProject(null); setRouteApplication(null) } })
    return () => { active = false }
  }, [pathname])

  useEffect(() => {
    let active = true
    const check = () => { void api("/api/v1/health").then(() => { if (active) setApiOnline(true) }).catch(() => { if (active) setApiOnline(false) }) }
    check()
    const timer = window.setInterval(check, 30000)
    return () => { active = false; window.clearInterval(timer) }
  }, [])

  const crumbs = pageCrumbs(pathname, routeProject, routeApplication)

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
    <div className="min-h-svh bg-muted/30 text-foreground">
      <aside className="fixed inset-y-0 left-0 z-20 hidden w-[248px] flex-col border-r bg-background lg:flex">
        <Link href="/" className="flex h-[72px] items-center gap-3 border-b px-6">
          <span className="grid size-9 place-items-center rounded-xl bg-primary text-sm font-bold text-primary-foreground">J</span>
          <span>
            <span className="block text-[15px] font-semibold tracking-tight">JustCD</span>
            <span className="block text-[11px] text-muted-foreground">continuous delivery</span>
          </span>
        </Link>
        <div className="px-4 pt-6">
          <p className="px-3 pb-2 text-[10px] font-semibold uppercase tracking-[0.15em] text-muted-foreground">Workspace</p>
          <nav className="space-y-1">
            {navigation.filter((item) => !item.adminOnly || user.isAdmin).map((item) => {
              const selected = isSelected(item.href, pathname)
              return (
                <Link
                  key={item.href}
                  href={item.href}
                  className={`flex items-center gap-3 rounded-lg px-3 py-2.5 text-[13px] transition-colors ${selected ? "bg-primary/8 font-medium text-primary" : "text-muted-foreground hover:bg-muted hover:text-foreground"}`}
                >
                  <HugeiconsIcon icon={item.icon} strokeWidth={1.8} className="size-4 shrink-0" aria-hidden="true" />
                  {item.label}
                </Link>
              )
            })}
          </nav>
        </div>
        <div className="mt-auto border-t p-4">
          <div className="flex items-center gap-3 rounded-lg px-2 py-2">
            <span className="grid size-8 shrink-0 place-items-center rounded-full bg-muted text-xs font-semibold uppercase">
              {(user.displayName || user.email).slice(0, 2)}
            </span>
            <span className="min-w-0 flex-1">
              <span className="block truncate text-xs font-medium">{user.displayName || user.email}</span>
              <span className="block truncate text-[11px] text-muted-foreground">{user.isAdmin ? "Instance administrator" : user.email}</span>
            </span>
          </div>
          <Button variant="ghost" size="sm" className="mt-1 w-full justify-start text-muted-foreground" onClick={signOut}>
            <HugeiconsIcon icon={Logout01Icon} strokeWidth={1.8} className="size-4" aria-hidden="true" /> Sign out
          </Button>
        </div>
      </aside>

      <div className="lg:pl-[248px]">
        <header className="sticky top-0 z-10 flex h-[64px] items-center justify-between border-b bg-background/90 px-5 backdrop-blur-md sm:px-8">
          <div className="flex min-w-0 items-center gap-3">
            <Link href="/" className="flex items-center gap-2 lg:hidden">
              <span className="grid size-8 place-items-center rounded-lg bg-primary font-bold text-primary-foreground">J</span>
              <span className="font-semibold">JustCD</span>
            </Link>
            <nav aria-label="Breadcrumb" className="flex min-w-0 items-center gap-2 overflow-hidden text-xs">
              {crumbs.map((crumb, index) => <span key={`${crumb.label}-${index}`} className="flex min-w-0 items-center gap-2">
                {index > 0 && <span aria-hidden="true" className="hidden text-muted-foreground/60 sm:block">/</span>}
                {crumb.href ? <Link href={crumb.href} className="hidden truncate text-muted-foreground transition-colors hover:text-foreground sm:block">{crumb.label}</Link> : <span aria-current="page" className="truncate font-medium text-foreground">{crumb.label}</span>}
              </span>)}
            </nav>
          </div>
          <div className="flex items-center gap-3">
            <Link href="/applications/new" className="hidden shrink-0 items-center gap-1.5 rounded-md border bg-background px-2.5 py-1.5 text-xs font-medium transition-colors hover:bg-muted md:inline-flex">
              <HugeiconsIcon icon={PlusSignIcon} className="size-3.5" aria-hidden="true" /> New application
            </Link>
            <span role="status" className="hidden shrink-0 items-center gap-1.5 text-[11px] text-muted-foreground sm:flex" title="Backend API health">
              <span className={`size-1.5 rounded-full ${apiOnline === null ? "bg-muted-foreground" : apiOnline ? "bg-emerald-500" : "bg-destructive"}`} /> {apiOnline === null ? "Checking API" : apiOnline ? "API online" : "API unavailable"}
            </span>
            <Link href="/settings" className="grid size-8 place-items-center rounded-full bg-muted text-xs font-semibold uppercase lg:hidden">
              {(user.displayName || user.email).slice(0, 2)}
            </Link>
            <Button variant="ghost" size="sm" className="hidden text-muted-foreground sm:inline-flex lg:hidden" onClick={signOut}>Sign out</Button>
          </div>
        </header>
        <nav className="flex gap-1 overflow-x-auto border-b bg-background px-4 py-2 lg:hidden">
          {navigation.filter((item) => !item.adminOnly || user.isAdmin).map((item) => {
            const selected = isSelected(item.href, pathname)
            return <Link key={item.href} href={item.href} className={`shrink-0 rounded-md px-3 py-1.5 text-[11px] ${selected ? "bg-primary/8 font-medium text-primary" : "text-muted-foreground hover:bg-muted"}`}>{item.label}</Link>
          })}
        </nav>
        <main className="mx-auto w-full max-w-[1440px] px-5 py-7 sm:px-8 sm:py-9">{children}</main>
        <footer className="mx-auto flex max-w-[1440px] justify-between px-5 pb-8 text-[11px] text-muted-foreground sm:px-8">
          <span>JustCD · no cluster-side operator</span>
          <span>Plans expire after 15 minutes</span>
        </footer>
      </div>
    </div>
  )
}

function isSelected(href: string, pathname: string) {
  if (href === "/") return pathname === "/"
  if (href === "/projects") return pathname.startsWith("/projects") || (pathname.startsWith("/applications/") && pathname !== "/applications/new")
  return pathname === href || pathname.startsWith(`${href}/`)
}

function pageCrumbs(pathname: string, project: Project | null, application: Application | null) {
  const parts = pathname.split("/").filter(Boolean)
  if (!parts.length) return [{ label: "Overview" }]
  if (parts[0] === "projects") {
    if (!parts[1]) return [{ label: "Projects" }]
    return [{ label: "Projects", href: "/projects" }, { label: parts[1] === "new" ? "New project" : project?.name ?? "Project" }]
  }
  if (parts[0] === "applications") {
    if (parts[1] === "new") return [{ label: "Projects", href: "/projects" }, { label: "New application" }]
    return [{ label: "Projects", href: "/projects" }, { label: project?.name ?? "Project", href: application ? `/projects/${application.projectId}` : "/projects" }, { label: application?.name ?? "Application" }]
  }
  if (parts[0] === "settings") {
    const section = { "git-sources": "Git sources", clusters: "Kubernetes clusters", namespaces: "Namespace bindings", credentials: "Credentials", oidc: "OIDC providers", users: "Local users" }[parts[1] as "git-sources"]
    return parts[1] ? [{ label: "Settings", href: "/settings" }, { label: section ?? "Section" }] : [{ label: "Settings" }]
  }
  if (parts[0] === "audit") return [{ label: "Audit trail" }]
  return [{ label: "Overview", href: "/" }, { label: "Not found" }]
}
