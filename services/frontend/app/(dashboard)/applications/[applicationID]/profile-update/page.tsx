"use client"

import { useEffect, useState } from "react"
import { useParams } from "next/navigation"
import Link from "next/link"
import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowLeft01Icon, ArrowRight01Icon } from "@hugeicons/core-free-icons"
import { api } from "@/lib/api"
import { RecipeReview } from "@/components/applications/recipe-review"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/reui/badge"
import { PublishingCard } from "@/components/applications/publishing-fields"
import { ErrorNotice } from "@/components/workspace-ui"
import { useToast } from "@/components/toast-provider"

type UpgradePlan = {
  applicationId: string
  profileName: string
  fromRevision: number
  toRevision: number
  sourceSha: string
  hash: string
  syncPolicy: string
  resume: boolean
  adopting?: boolean
  upToDate: boolean
  images: { name: string; before: string; after: string }[]
  changes: { path: string; before: string; after: string; exists: boolean }[]
}
export default function ProfileUpdatePage() {
  const toast = useToast()
  const { applicationID } = useParams<{ applicationID: string }>()
  const [plan, setPlan] = useState<UpgradePlan | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)
  const [reload, setReload] = useState(0)
  const [result, setResult] = useState<{
    profileRevision: number
    pipelineUrl: string
  } | null>(null)
  useEffect(() => {
    let active = true
    api<UpgradePlan>(
      `/api/v1/applications/${encodeURIComponent(applicationID)}/profile-upgrade`
    )
      .then((value) => {
        if (active) {
          setPlan(value)
          setError(null)
        }
      })
      .catch((e) => active && setError(e))
    return () => {
      active = false
    }
  }, [applicationID, reload])
  async function upgrade() {
    if (!plan || busy) return
    setBusy(true)
    setError(null)
    try {
      setResult(
        await toast.promise(
          api(
            `/api/v1/applications/${encodeURIComponent(applicationID)}/profile-upgrade`,
            {
              method: "POST",
              body: JSON.stringify({
                revision: plan.toRevision,
                hash: plan.hash,
              }),
            }
          ),
          { loading: "Applying the profile update…", success: "Profile update applied.", error: null }
        )
      )
    } catch (e) {
      setError(e)
      setPlan(null)
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="space-y-4 pb-5">
      <header className="space-y-2">
        <Button
          variant="ghost"
          nativeButton={false}
          className="-ml-2.5 gap-2 text-muted-foreground"
          render={<Link href={`/applications/${applicationID}`} />}
        >
          <HugeiconsIcon
            icon={ArrowLeft01Icon}
            strokeWidth={1.8}
            aria-hidden="true"
          />
          Back to application
        </Button>
        <div>
          <h1 className="text-xl font-medium">
            {plan?.adopting
              ? "Convert to publishing profile"
              : "Update publishing profile"}
          </h1>
          <p className="mt-1 text-sm text-muted-foreground">
            Review the build settings and generated files before rebuilding the
            source already in Git.
          </p>
        </div>
      </header>
      {error != null && (
        <>
          <ErrorNotice error={error} />
          <Button
            type="button"
            variant="outline"
            disabled={busy}
            onClick={() => {
              setPlan(null)
              setReload((n) => n + 1)
            }}
          >
            Refresh upgrade review
          </Button>
        </>
      )}
      {result ? (
        <PublishingCard contentClassName="space-y-3">
          <h2 className="text-base font-medium">
            Using profile v{result.profileRevision} · build started
          </h2>
          <p className="text-sm text-muted-foreground">
            The running release stays in place until the new build succeeds and
            deployment follows your existing approval policy.
          </p>
          <div className="flex flex-wrap gap-3">
            <Button
              nativeButton={false}
              render={
                <a href={result.pipelineUrl} target="_blank" rel="noreferrer" />
              }
            >
              Follow build
            </Button>
            <Button
              variant="outline"
              nativeButton={false}
              render={<Link href={`/applications/${applicationID}`} />}
            >
              Open application
            </Button>
          </div>
        </PublishingCard>
      ) : plan ? (
        plan.upToDate ? (
          <PublishingCard>
            <p className="text-sm">
              This application already uses the latest profile version.
            </p>
          </PublishingCard>
        ) : (
          <>
            <PublishingCard contentClassName="space-y-3">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <h2 className="text-base font-medium">{plan.profileName}</h2>
                <div
                  className="flex flex-wrap items-center gap-2"
                  aria-label="Profile version change"
                >
                  {plan.adopting ? (
                    <>
                      <Badge variant="warning-light">Converting</Badge>
                      <Badge variant="primary-light">
                        Profile v{plan.toRevision}
                      </Badge>
                    </>
                  ) : plan.resume ? (
                    <>
                      <Badge variant="primary-light">v{plan.toRevision}</Badge>
                      <Badge variant="warning-light">Build pending</Badge>
                    </>
                  ) : (
                    <>
                      <Badge variant="secondary">
                        Current · v{plan.fromRevision}
                      </Badge>
                      <HugeiconsIcon
                        icon={ArrowRight01Icon}
                        className="size-4 text-muted-foreground"
                        aria-hidden="true"
                      />
                      <Badge variant="primary-light">
                        Update · v{plan.toRevision}
                      </Badge>
                    </>
                  )}
                </div>
              </div>
              <p className="text-xs text-muted-foreground">
                Source commit {plan.sourceSha.slice(0, 12)}. No ZIP upload is
                required.
              </p>
            </PublishingCard>
            <section
              aria-labelledby="profile-changes-heading"
              className="space-y-3"
            >
              <div>
                <h2
                  id="profile-changes-heading"
                  className="text-base font-medium"
                >
                  {plan.resume ? "Pending build" : "What will change"}
                </h2>
                <p className="mt-1 text-sm text-muted-foreground">
                  {plan.resume
                    ? "The profile update is saved. Retry the build to continue publishing."
                    : plan.adopting
                      ? `The application keeps its destination and source. It now takes the profile's build settings: ${plan.images.length} setting changes · ${plan.changes.length} generated file changes. Review them before converting.`
                      : `${plan.images.length} setting changes · ${plan.changes.length} generated file changes. Review the details below before updating.`}
                </p>
              </div>
              <RecipeReview
                images={plan.images}
                changes={plan.changes}
                emptyText={
                  plan.resume
                    ? "Retry the pending build using the pinned profile version."
                    : plan.adopting
                      ? "The profile's build settings match what the application already uses."
                      : "The profile version changes the approved recipe snapshot; image references are unchanged."
                }
              />
            </section>
            <PublishingCard contentClassName="space-y-3">
              <p className="text-sm text-muted-foreground">
                The update changes generated build files and uses the current
                profile credentials. Your source files and deployment
                destination are retained.{" "}
                {plan.syncPolicy === "manual"
                  ? "After the build, review and apply the deployment in JustCD."
                  : "After a successful build, automatic deployment follows your existing workspace approval rules."}
              </p>
              <Button
                type="button"
                disabled={busy}
                loading={busy}
                loadingText="Updating profile and starting build…"
                onClick={() => void upgrade()}
              >
                {plan.resume
                  ? "Retry build"
                  : plan.adopting
                    ? `Adopt profile v${plan.toRevision} and rebuild`
                    : `Update to v${plan.toRevision} and rebuild`}
              </Button>
            </PublishingCard>
          </>
        )
      ) : (
        error == null && (
          <p role="status" className="text-sm text-muted-foreground">
            Loading profile upgrade review…
          </p>
        )
      )}
    </div>
  )
}
