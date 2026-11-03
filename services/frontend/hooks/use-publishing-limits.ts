"use client"

import { useEffect, useState } from "react"
import { api } from "@/lib/api"
import type { PublishingLimits } from "@/lib/publishing"

export function usePublishingLimits() {
  const [limits, setLimits] = useState<PublishingLimits | null>(null)
  const [error, setError] = useState<unknown>(null)
  useEffect(() => {
    let active = true
    api<PublishingLimits>("/api/v1/publishing/limits")
      .then((value) => {
        if (active) setLimits(value)
      })
      .catch((cause) => {
        if (active) setError(cause)
      })
    return () => {
      active = false
    }
  }, [])
  return { limits, error }
}
