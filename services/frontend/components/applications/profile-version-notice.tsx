"use client"
import Link from "next/link"
import type { Application } from "@/lib/types"
import { Badge } from "@/components/reui/badge"
import { Button } from "@/components/ui/button"
import { PublishingCard } from "./publishing-fields"
import type { ReactNode } from "react"
export function ProfileVersionBadge({
  application,
}: {
  application: Application
}) {
  const profile = application.publishingProfile
  if (!profile) return null
  return (
    <Badge
      variant={
        profile.updateAvailable || profile.needsBuild ? "info-light" : "outline"
      }
    >
      Profile v{profile.revision}
      {profile.updateAvailable
        ? ` · v${profile.latestRevision} available`
        : profile.needsBuild
          ? " · build pending"
          : ""}
    </Badge>
  )
}
export function ProfileVersionNotice({
  application,
  canUpgrade,
  compact = false,
}: {
  application: Application
  canUpgrade: boolean
  compact?: boolean
}) {
  const profile = application.publishingProfile
  if (!profile) return null
  return (
    <ProfileContainer compact={compact}>
      <div className="min-w-0">
        <p className="text-sm font-medium">
          {profile.name} · using v{profile.revision}
          {profile.updateAvailable
            ? ` · v${profile.latestRevision} available`
            : ""}
        </p>
        <p className="mt-1 text-xs leading-5 text-muted-foreground">
          {!profile.enabled
            ? "This profile is disabled. Ask an administrator to enable it before upgrading."
            : profile.needsBuild
              ? "The profile was updated but its build has not started. Review the pending update to retry."
              : profile.updateAvailable
                ? "Review the new build settings and rebuild using the source already in Git. Source uploads keep your current profile version."
                : "Your application is pinned to the latest published profile version."}
        </p>
      </div>
      {(profile.updateAvailable || profile.needsBuild) && (
        <Button
          nativeButton={false}
          render={
            <Link href={`/applications/${application.id}/profile-update`} />
          }
          disabled={
            !canUpgrade || !profile.enabled || application.decommissioning
          }
        >
          Review profile update
        </Button>
      )}
    </ProfileContainer>
  )
}

function ProfileContainer({ compact, children }: { compact: boolean; children: ReactNode }) {
  return compact ? <div className="space-y-3">{children}</div> : <PublishingCard contentClassName="flex flex-wrap items-center justify-between gap-3">{children}</PublishingCard>
}
