"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import { useEffect, useRef, useState } from "react"
import { Button } from "@/components/ui/button"
import { ServiceStatePage } from "@/components/service-state"

type Availability = {
  status: "online" | "maintenance" | "offline"
  maintenanceEnabled?: boolean
  adminBypass?: boolean
}

export function ServiceAvailability({
  children,
}: {
  children: React.ReactNode
}) {
  const pathname = usePathname()
  const [availability, setAvailability] = useState<Availability | null>(null)
  const previousStatus = useRef<Availability["status"] | null>(null)
  useEffect(() => {
    const controller = new AbortController()
    let pending = false
    const check = async () => {
      if (pending || controller.signal.aborted) return
      pending = true
      try {
        const response = await fetch("/api/service-status", {
          cache: "no-store",
          signal: controller.signal,
        })
        const result = (await response.json()) as Availability
        if (!controller.signal.aborted) {
          if (
            previousStatus.current &&
            previousStatus.current !== "online" &&
            result.status === "online" &&
            !result.adminBypass
          ) {
            recover()
            return
          }
          previousStatus.current = result.status
          setAvailability(result)
        }
      } catch {
        if (!controller.signal.aborted) {
          previousStatus.current = "offline"
          setAvailability({ status: "offline" })
        }
      } finally {
        pending = false
      }
    }
    void check()
    const timer = window.setInterval(() => {
      if (document.visibilityState === "visible") void check()
    }, 15000)
    window.addEventListener("justcd:service-unavailable", check)
    window.addEventListener("justcd:maintenance-updated", check)
    return () => {
      controller.abort()
      window.clearInterval(timer)
      window.removeEventListener("justcd:service-unavailable", check)
      window.removeEventListener("justcd:maintenance-updated", check)
    }
  }, [pathname])

  if (!availability)
    return (
      <main className="grid min-h-svh place-items-center" role="status">
        <p className="text-sm text-muted-foreground">Connecting to JustCD…</p>
      </main>
    )
  // Keep sign-in reachable so an administrator without a session can recover.
  if (availability.status === "maintenance" && pathname !== "/login")
    return <ServiceStatePage state="maintenance" onRecover={recover} />
  return (
    <>
      {availability.status === "offline" && (
        <ServiceStatePage state="offline" onRecover={recover} />
      )}
      <div hidden={availability.status === "offline"}>
        {availability.maintenanceEnabled && availability.adminBypass && (
          <aside
            role="status"
            aria-label="Maintenance mode active"
            className="flex flex-wrap items-center justify-center gap-3 border-b border-warning/30 bg-warning/10 px-4 py-2 text-sm"
          >
            <span>
              Maintenance mode is active. You have administrator access.
            </span>
            <Button
              variant="outline"
              size="sm"
              nativeButton={false}
              render={<Link href="/settings/maintenance" />}
            >
              Manage maintenance
            </Button>
          </aside>
        )}
        {children}
      </div>
    </>
  )
}

function recover() {
  window.location.reload()
}
