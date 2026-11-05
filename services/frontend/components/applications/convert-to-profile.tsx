"use client"

import { useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import { Button } from "@/components/ui/button"
import { AppDialog, DialogCancel, DialogFooter } from "@/components/ui/dialog"
import { FormSelect } from "@/components/ui/form-select"
import { FormField } from "@/components/ui-kit"
import { ErrorNotice } from "@/components/workspace-ui"
import { api } from "@/lib/api"
import type { ApprovedPublishingProfile } from "@/lib/publishing"
import type { ListResponse } from "@/lib/types"
import { useToast } from "@/components/toast-provider"

type Difference = { field: string; app: string; profile: string }
type Check = {
  profileName: string
  compatible: boolean
  applied: boolean
  differences: Difference[]
}

/**
 * Binds a manually published application to a publishing profile. It only works
 * when the app already sits where the profile would put it, because profile
 * updates keep the destination locked. The profile's build settings are adopted
 * afterwards through the reviewed profile update.
 */
export function ConvertToProfileDialog({
  open,
  onOpenChange,
  applicationId,
  workspaceId,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  applicationId: string
  workspaceId: string
}) {
  const router = useRouter()
  const toast = useToast()
  const base = `/api/v1/applications/${encodeURIComponent(applicationId)}`
  const [profiles, setProfiles] = useState<ApprovedPublishingProfile[]>([])
  const [templateId, setTemplateId] = useState("")
  const [profileId, setProfileId] = useState("")
  const [check, setCheck] = useState<Check | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)

  useEffect(() => {
    if (!open) return
    let active = true
    Promise.all([
      api<ListResponse<ApprovedPublishingProfile>>(
        `/api/v1/publishing/profiles?workspaceId=${encodeURIComponent(workspaceId)}`
      ),
      api<{ publication: { config: { templateId?: string } } }>(`${base}/publication`),
    ])
      .then(([list, publication]) => {
        if (!active) return
        setProfiles(list.items)
        setTemplateId(publication.publication.config.templateId ?? "")
      })
      .catch((cause) => active && setError(cause))
    return () => {
      active = false
    }
  }, [open, workspaceId, base])

  const choices = profiles.filter(
    (p) => !templateId || p.templateId === templateId
  )

  async function run(dryRun: boolean) {
    if (!profileId || busy) return
    setBusy(true)
    setError(null)
    try {
      const result = await api<Check>(`${base}/adopt-profile`, {
        method: "POST",
        body: JSON.stringify({ profileId, dryRun }),
      })
      setCheck(result)
      if (result.applied) {
        onOpenChange(false)
        toast.success("Publishing profile adopted.")
        router.push(`/applications/${applicationId}/profile-update`)
      }
    } catch (cause) {
      setError(cause)
    } finally {
      setBusy(false)
    }
  }

  return (
    <AppDialog
      open={open}
      onOpenChange={(next) => {
        onOpenChange(next)
        if (!next) {
          setCheck(null)
          setError(null)
        }
      }}
      busy={busy}
      size="lg"
      title="Convert to a publishing profile"
      description="The app keeps its source and destination. It then takes the profile's build settings, so profile updates reach it like any profile app."
    >
      <div className="space-y-4">
        <FormField
          label="Publishing profile"
          htmlFor="convert-profile"
          hint="Only profiles for this app's template are listed."
        >
          <FormSelect
            id="convert-profile"
            value={profileId}
            placeholder={
              choices.length ? "Choose a profile" : "No matching profile"
            }
            items={choices.map((p) => ({ value: p.id, label: p.name }))}
            onValueChange={(next) => {
              setProfileId(next)
              setCheck(null)
            }}
          />
        </FormField>
        {check &&
          (check.compatible ? (
            <p role="status" className="text-sm text-muted-foreground">
              {check.profileName} matches this app&apos;s destination.
              Converting starts a reviewed update of the build files; nothing is
              committed until you approve it.
            </p>
          ) : (
            <div
              role="alert"
              className="space-y-2 rounded-xl border border-destructive/30 bg-destructive/5 p-4"
            >
              <p className="text-sm font-medium text-destructive">
                This profile does not match the app yet
              </p>
              <p className="text-xs leading-5 text-muted-foreground">
                A profile app keeps its destination locked, so these settings
                must match. Change the profile (or create one) so they agree.
              </p>
              <dl className="space-y-2 text-xs">
                {check.differences.map((difference) => (
                  <div key={difference.field}>
                    <dt className="font-medium">{difference.field}</dt>
                    <dd className="break-all text-muted-foreground">
                      App: {difference.app || "empty"} · Profile:{" "}
                      {difference.profile || "empty"}
                    </dd>
                  </div>
                ))}
              </dl>
            </div>
          ))}
        {error != null && <ErrorNotice error={error} />}
        <DialogFooter>
          <DialogCancel disabled={busy} />
          <Button
            type="button"
            variant="outline"
            disabled={!profileId || busy}
            onClick={() => void run(true)}
          >
            Check compatibility
          </Button>
          <Button
            type="button"
            disabled={!profileId || busy || !check?.compatible}
            loading={busy}
            loadingText="Converting…"
            onClick={() => void run(false)}
          >
            Convert
          </Button>
        </DialogFooter>
      </div>
    </AppDialog>
  )
}
