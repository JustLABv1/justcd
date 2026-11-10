"use client"
import Link from "next/link"
import type { Application } from "@/lib/types"
import { Badge } from "@/components/panelui/badge"
import { Button } from "@/components/panelui/button"
import { Alert } from "@/components/panelui/alert"
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
        profile.updateAvailable || profile.needsBuild ? "info" : "outline"
      }
    >
      Profile v{profile.revision}
      {profile.adoptionPending
        ? " · conversion pending"
        : profile.updateAvailable
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
  if (profile.updateAvailable || profile.needsBuild) {
    return (
      <Alert tone="info" role="status">
        <Alert.Icon />
        <Alert.Title>
          {profile.adoptionPending
            ? "Finish converting to the profile"
            : profile.needsBuild
              ? "Profile build pending"
              : "Profile update available"}
        </Alert.Title>
        <Alert.Description className="space-y-2">
          <p className="font-medium break-words text-foreground">
            {profile.name}
          </p>
          <div
            className="flex flex-wrap items-center gap-2"
            aria-label={`Current version ${profile.revision}${profile.updateAvailable ? `, available version ${profile.latestRevision}` : ""}`}
          >
            <Badge variant="outline" size="sm">
              Current v{profile.revision}
            </Badge>
            {profile.updateAvailable && !profile.adoptionPending && (
              <>
                <span aria-hidden="true">→</span>
                <Badge variant="info" size="sm">
                  Available v{profile.latestRevision}
                </Badge>
              </>
            )}
          </div>
          <p>
            {!profile.enabled
              ? "This profile is disabled. Ask an administrator to enable it before upgrading."
              : profile.adoptionPending
                ? "The application is bound to this profile. Review the profile's build settings, then rebuild to adopt them. Uploading a new ZIP waits until then."
                : profile.needsBuild
                  ? "The profile build has not started. Review the pending update to retry."
                  : "Review the updated build settings before rebuilding your application."}
          </p>
        </Alert.Description>
        <Alert.Actions>
        <Button
          size="sm"
          className="w-fit max-w-full"
          nativeButton={false}
          render={
            <Link href={`/applications/${application.id}/profile-update`} />
          }
          disabled={
            !canUpgrade || !profile.enabled || application.decommissioning
          }
        >
          {profile.adoptionPending
            ? "Review conversion"
            : "Review profile update"}
        </Button>
        </Alert.Actions>
      </Alert>
    )
  }
  return (
    <ProfileContainer compact={compact}>
      <div className="min-w-0">
        <p className="text-sm font-medium">
          {profile.name} · using v{profile.revision}
        </p>
        <p className="mt-1 text-xs leading-5 text-muted-foreground">
          {!profile.enabled
            ? "This profile is disabled. Ask an administrator to enable it before upgrading."
            : "Your application is pinned to the latest published profile version."}
        </p>
      </div>
    </ProfileContainer>
  )
}

function ProfileContainer({
  compact,
  children,
}: {
  compact: boolean
  children: ReactNode
}) {
  return compact ? (
    <div className="space-y-3">{children}</div>
  ) : (
    <PublishingCard contentClassName="flex flex-wrap items-center justify-between gap-3">
      {children}
    </PublishingCard>
  )
}
