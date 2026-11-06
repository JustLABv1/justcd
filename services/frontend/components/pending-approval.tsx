"use client"

import { useCallback } from "react"
import { ServiceStatePage } from "@/components/service-state"
import { api } from "@/lib/api"
import type { User } from "@/lib/types"

type Session = { user: User; canCreateWorkspace?: boolean }

/**
 * Shown instead of the app to a self-registered account that an administrator has not approved yet.
 * It keeps checking, so the person lands in the app as soon as they are approved.
 */
export function PendingApproval({ user, onApproved, onSignOut }: { user: User; onApproved: (session: Session) => void; onSignOut: () => void }) {
  const recheck = useCallback(async () => {
    try {
      const session = await api<Session>("/api/v1/auth/session")
      if (session.user.emailVerified === false) return false
      onApproved(session)
      return true
    } catch {
      // A failed check is not an answer; the next poll tries again.
      return false
    }
  }, [onApproved])
  return <ServiceStatePage state="pending-approval" account={{ name: user.displayName || user.email, email: user.email }} recheck={recheck} onSignOut={onSignOut} />
}
