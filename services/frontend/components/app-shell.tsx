"use client"

import Link from "next/link"
import styles from "./app-shell.module.css"
import { usePathname, useRouter } from "next/navigation"
import { useEffect, useState } from "react"
import { AccountMenu } from "@/components/account-menu"
import { api, apiPost } from "@/lib/api"
import type { Application, ListResponse, Project, User } from "@/lib/types"
import { HugeiconsIcon } from "@hugeicons/react"
import { Home01Icon, Folder01Icon, Layers01Icon, Task01Icon, Settings02Icon, Audit01Icon } from "@hugeicons/core-free-icons"

const navigation = [
  { href: "/", label: "Overview", icon: Home01Icon },
  { href: "/projects", label: "Projects", icon: Folder01Icon },
  { href: "/applications", label: "Applications", icon: Layers01Icon },
  { href: "/approvals", label: "Approvals", icon: Task01Icon },
  { href: "/settings", label: "Instance settings", icon: Settings02Icon, adminOnly: true },
  { href: "/audit", label: "Audit trail", icon: Audit01Icon, adminOnly: true },
]

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname()
  const router = useRouter()
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)
  const [routeProject, setRouteProject] = useState<Project | null>(null)
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
    <div className="min-h-svh bg-[#f7f8f8] text-foreground dark:bg-background">
      <aside className="fixed bottom-3 left-3 top-3 z-20 hidden w-[224px] flex-col overflow-hidden rounded-[24px] border border-border/70 bg-card shadow-[0_8px_32px_-16px_rgb(0_0_0_/_0.2)] lg:flex">
        <Link href="/" className="flex shrink-0 items-center gap-3 px-5 pb-7 pt-6">
          <span className="grid size-9 place-items-center rounded-xl bg-primary text-sm font-bold text-primary-foreground">J</span>
          <span>
            <span className="block text-[15px] font-semibold tracking-tight">JustCD</span>
            <span className="block text-[11px] text-muted-foreground">continuous delivery</span>
          </span>
        </Link>
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
        <nav aria-label="Main navigation" className="mx-4 mt-3 flex gap-1 overflow-x-auto rounded-2xl border border-border/70 bg-card p-1.5 shadow-sm lg:hidden">
          {navigation.filter((item) => !item.adminOnly || user.isAdmin).map((item) => {
            const selected = isSelected(item.href, pathname)
            return <Link key={item.href} href={item.href} aria-current={selected ? "page" : undefined} className={`${styles.navItem} shrink-0 !px-3 !py-2 !text-xs`}>{item.label}</Link>
          })}
        </nav>
        <main className={`mx-auto w-full ${/^\/applications\/[^/]+$/.test(pathname) && !pathname.endsWith("/new") ? "max-w-none" : "max-w-[1440px]"} px-5 pb-7 pt-5 sm:px-8 sm:pb-8 sm:pt-6 lg:pt-9 ${pathname === "/audit" ? "lg:flex lg:h-dvh lg:flex-col lg:overflow-hidden" : ""}`}>
          {crumbs.length > 1 && <nav aria-label="Breadcrumb" className="mb-4 shrink-0 text-xs">
            <ol className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
              {crumbs.map((crumb, index) => <li key={`${crumb.label}-${index}`} className="flex min-w-0 max-w-full items-center gap-2">
                {index > 0 && <span aria-hidden="true" className="text-muted-foreground/50">/</span>}
                {crumb.href ? <Link href={crumb.href} className="truncate text-muted-foreground transition-colors hover:text-foreground">{crumb.label}</Link> : <span aria-current="page" className="truncate text-muted-foreground">{crumb.label}</span>}
              </li>)}
            </ol>
          </nav>}
          {children}
        </main>
      </div>
    </div>
  )
}

function isSelected(href: string, pathname: string) {
  if (href === "/") return pathname === "/"
  if (href === "/projects") return pathname.startsWith("/projects")
  return pathname === href || pathname.startsWith(`${href}/`)
}

function pageCrumbs(pathname: string, project: Project | null, application: Application | null) {
  const parts = pathname.split("/").filter(Boolean)
  if (!parts.length) return [{ label: "Overview" }]
  if (parts[0] === "projects") {
    if (!parts[1]) return [{ label: "Projects" }]
    if (parts[2] === "connections") {
      const connection = { "git-sources": "Git sources", clusters: "Kubernetes clusters", namespaces: "Namespace bindings", credentials: "Credentials" }[parts[3] as "git-sources"]
      return [{ label: "Projects", href: "/projects" }, { label: project?.name ?? "Project", href: `/projects/${parts[1]}?tab=connections` }, { label: connection ?? "Connections" }]
    }
    return [{ label: "Projects", href: "/projects" }, { label: parts[1] === "new" ? "New project" : project?.name ?? "Project" }]
  }
  if (parts[0] === "applications") {
    if (!parts[1]) return [{ label: "Applications" }]
    if (parts[1] === "new") return [{ label: "Applications", href: "/applications" }, { label: "New application" }]
    return [{ label: "Applications", href: "/applications" }, { label: project?.name ?? "Project", href: application ? `/projects/${application.projectId}` : "/projects" }, { label: application?.name ?? "Application" }]
  }
  if (parts[0] === "settings") {
    const section = { "git-sources": "Git sources", clusters: "Kubernetes clusters", namespaces: "Namespace bindings", credentials: "Credentials", oidc: "OIDC providers", users: "Local users" }[parts[1] as "git-sources"]
    return parts[1] ? [{ label: "Instance settings", href: "/settings" }, { label: section ?? "Section" }] : [{ label: "Instance settings" }]
  }
  if (parts[0] === "approvals") return [{ label: "Approvals" }]
  if (parts[0] === "audit") return [{ label: "Audit trail" }]
  return [{ label: "Overview", href: "/" }, { label: "Not found" }]
}
