"use client"

import { useEffect, useState } from "react"
import { ServiceStatePage } from "@/components/service-state"

export function ServiceAvailability({
  children,
}: {
  children: React.ReactNode
}) {
  const [offline, setOffline] = useState(false)
  useEffect(() => {
    const controller = new AbortController()
    let pending = false
    const check = async () => {
      if (pending) return
      pending = true
      try {
        const response = await fetch("/api/service-status", {
          cache: "no-store",
          signal: controller.signal,
        })
        const result = (await response.json()) as { status: string }
        if (!controller.signal.aborted) setOffline(result.status === "offline")
      } catch {
        if (!controller.signal.aborted) setOffline(true)
      } finally {
        pending = false
      }
    }
    window.addEventListener("justcd:service-unavailable", check)
    return () => {
      controller.abort()
      window.removeEventListener("justcd:service-unavailable", check)
    }
  }, [])

  return (
    <>
      {offline && <ServiceStatePage state="offline" onRecover={recover} />}
      <div hidden={offline}>{children}</div>
    </>
  )
}

function recover() {
  window.location.reload()
}
