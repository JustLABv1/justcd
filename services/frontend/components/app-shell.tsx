"use client"

import Link from "next/link"
import { usePathname, useRouter } from "next/navigation"
import { useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { api, apiPost } from "@/lib/api"
import type { User } from "@/lib/types"

const navigation: { href: string; label: string; icon: string; adminOnly?: boolean }[] = [
  { href: "/", label: "Overview", icon: "⌂" },
  { href: "/projects", label: "Projects", icon: "▦" },
  { href: "/applications/new", label: "New application", icon: "+" },
  { href: "/settings", label: "Connections & access", icon: "⚙" },
  { href: "/audit", label: "Audit trail", icon: "≋", adminOnly: true },
]

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname()
  const router = useRouter()
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)

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
              const selected = item.href === "/" ? pathname === "/" : pathname.startsWith(item.href)
              return (
                <Link
                  key={item.href}
                  href={item.href}
                  className={`flex items-center gap-3 rounded-lg px-3 py-2.5 text-[13px] transition-colors ${selected ? "bg-primary/8 font-medium text-primary" : "text-muted-foreground hover:bg-muted hover:text-foreground"}`}
                >
                  <span className="grid size-5 place-items-center text-base leading-none">{item.icon}</span>
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
            <span aria-hidden="true">↪</span> Sign out
          </Button>
        </div>
      </aside>

      <div className="lg:pl-[248px]">
        <header className="sticky top-0 z-10 flex h-[64px] items-center justify-between border-b bg-background/90 px-5 backdrop-blur-md sm:px-8">
          <div className="flex items-center gap-3">
            <Link href="/" className="flex items-center gap-2 lg:hidden">
              <span className="grid size-8 place-items-center rounded-lg bg-primary font-bold text-primary-foreground">J</span>
              <span className="font-semibold">JustCD</span>
            </Link>
            <span className="hidden text-xs text-muted-foreground sm:block">Delivery workspace <span className="px-1.5">/</span> <span className="text-foreground">{pageLabel(pathname)}</span></span>
          </div>
          <div className="flex items-center gap-3">
            <span className="hidden items-center gap-1.5 rounded-full border bg-background px-2.5 py-1 text-[11px] text-muted-foreground sm:flex">
              <span className="size-1.5 rounded-full bg-emerald-500" /> API connected
            </span>
            <Link href="/settings" className="grid size-8 place-items-center rounded-full bg-muted text-xs font-semibold uppercase lg:hidden">
              {(user.displayName || user.email).slice(0, 2)}
            </Link>
            <Button variant="ghost" size="sm" className="hidden text-muted-foreground sm:inline-flex lg:hidden" onClick={signOut}>Sign out</Button>
          </div>
        </header>
        <nav className="flex gap-1 overflow-x-auto border-b bg-background px-4 py-2 lg:hidden">
          {navigation.filter((item) => !item.adminOnly || user.isAdmin).map((item) => {
            const selected = item.href === "/" ? pathname === "/" : pathname.startsWith(item.href)
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

function pageLabel(pathname: string) {
  if (pathname.startsWith("/applications/")) return "Applications"
  if (pathname.startsWith("/projects/")) return "Projects"
  if (pathname.startsWith("/settings")) return "Connections & access"
  if (pathname.startsWith("/audit")) return "Audit trail"
  return "Overview"
}
