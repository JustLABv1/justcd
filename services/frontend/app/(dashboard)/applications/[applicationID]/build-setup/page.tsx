"use client"

import { useEffect, useState } from "react"
import { useParams } from "next/navigation"
import Link from "next/link"
import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowLeft01Icon } from "@hugeicons/core-free-icons"
import { api } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { Badge } from "@/components/reui/badge"
import { FormField } from "@/components/ui-kit"
import { PublishingCard } from "@/components/applications/publishing-fields"
import {
  RecipeReview,
  type RecipeFileChange,
  type RecipeSettingChange,
} from "@/components/applications/recipe-review"
import { PackageSourcesCard } from "@/components/settings/package-sources"
import { ErrorNotice } from "@/components/workspace-ui"
import { useToast } from "@/components/toast-provider"

type Publication = {
  profileId?: string
  profileName?: string
  config: {
    name: string
    packageSources?: string
    packagePolicy?: string
    templateParameters?: string
    secretMode?: string
  }
}
type Plan = {
  hash: string
  sourceSha: string
  upToDate: boolean
  images: RecipeSettingChange[]
  changes: RecipeFileChange[]
}

/**
 * Change how a manually published application gets its packages. JustCD
 * regenerates the Dockerfile, pipeline and package script from the app's stored
 * settings, shows the exact changes and commits them only after approval.
 */
export default function BuildSetupPage() {
  const { applicationID } = useParams<{ applicationID: string }>()
  const base = `/api/v1/applications/${encodeURIComponent(applicationID)}`
  const [publication, setPublication] = useState<Publication | null>(null)
  const [sources, setSources] = useState("")
  const [policy, setPolicy] = useState("")
  const [parameters, setParameters] = useState("")
  const [packageUser, setPackageUser] = useState("")
  const [packageToken, setPackageToken] = useState("")
  const [gitlabToken, setGitlabToken] = useState("")
  const [tokenError, setTokenError] = useState("")
  const [plan, setPlan] = useState<Plan | null>(null)
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [result, setResult] = useState<{ pipelineUrl?: string } | null>(null)

  useEffect(() => {
    let active = true
    api<{ publication: Publication }>(`${base}/publication`)
      .then(({ publication: value }) => {
        if (!active) return
        setPublication(value)
        setSources(value.config.packageSources ?? "")
        setPolicy(value.config.packagePolicy ?? "")
        setParameters(value.config.templateParameters ?? "")
      })
      .catch((cause) => active && setError(cause))
    return () => {
      active = false
    }
  }, [base])

  const authenticated = (() => {
    try {
      const list = JSON.parse(sources || "[]")
      return Array.isArray(list) && list.some((s) => s?.authenticated)
    } catch {
      return false
    }
  })()
  const dirty =
    !!publication &&
    ((publication.config.packageSources ?? "") !== sources ||
      (publication.config.packagePolicy ?? "") !== policy ||
      (publication.config.templateParameters ?? "") !== parameters)
  const variables =
    (publication?.config.secretMode ?? "variables") === "variables"
  const needsCredentials = authenticated && variables
  const body = {
    gitlabToken,
    packageSources: sources,
    packagePolicy: policy,
    templateParameters: parameters,
    ...(needsCredentials ? { packageUser, packageToken } : {}),
  }
  const edit = (change: () => void) => {
    change()
    setPlan(null)
    setResult(null)
  }

  async function review() {
    if (busy) return
    if (!gitlabToken.trim()) {
      setTokenError(
        "Enter a GitLab token with access to this app's project. JustCD needs it to commit the files and set the CI variables."
      )
      document.getElementById("build-setup-token")?.focus()
      return
    }
    setTokenError("")
    setBusy(true)
    setError(null)
    try {
      setPlan(
        await api<Plan>(`${base}/build-setup/preview`, {
          method: "POST",
          body: JSON.stringify(body),
        })
      )
    } catch (cause) {
      setError(cause)
    } finally {
      setBusy(false)
    }
  }
  async function apply() {
    if (!plan || busy) return
    if (needsCredentials && (!packageUser || packageToken.length < 8)) {
      setError(
        new Error(
          "Enter the package user and token for the authenticated sources."
        )
      )
      return
    }
    setBusy(true)
    setError(null)
    try {
      setResult(
        await toast.promise(
          api(`${base}/build-setup`, {
            method: "POST",
            body: JSON.stringify({ ...body, hash: plan.hash }),
          }),
          { loading: "Committing build setup…", success: "Build setup committed.", error: null }
        )
      )
      setGitlabToken("")
      setPackageToken("")
    } catch (cause) {
      setError(cause)
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
          <h1 className="text-xl font-medium">Update build setup</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            Change where this app&apos;s builds get packages. JustCD regenerates
            the Dockerfile and pipeline, shows you every change, and commits
            only after you approve.
          </p>
        </div>
      </header>
      {error != null && <ErrorNotice error={error} />}
      {publication?.profileId ? (
        <PublishingCard contentClassName="space-y-2">
          <p className="text-sm font-medium">
            This app uses a publishing profile
          </p>
          <p className="text-sm text-muted-foreground">
            Build settings come from {publication.profileName ?? "the profile"}.
            Ask a workspace owner to change the profile&apos;s package sources,
            then review the profile update.
          </p>
        </PublishingCard>
      ) : result ? (
        <PublishingCard contentClassName="space-y-3">
          <h2 className="text-base font-medium">
            Build setup updated · build started
          </h2>
          <p className="text-sm text-muted-foreground">
            The running release stays in place until the new build succeeds.
            Then review and apply the deployment in JustCD.
          </p>
          <div className="flex flex-wrap gap-3">
            {result.pipelineUrl && (
              <Button
                nativeButton={false}
                render={
                  <a
                    href={result.pipelineUrl}
                    target="_blank"
                    rel="noreferrer"
                  />
                }
              >
                Follow build
              </Button>
            )}
            <Button
              variant="outline"
              nativeButton={false}
              render={<Link href={`/applications/${applicationID}`} />}
            >
              Open application
            </Button>
          </div>
        </PublishingCard>
      ) : publication ? (
        <>
          <PackageSourcesCard
            sources={sources}
            policy={policy}
            onChange={(next) =>
              edit(() => {
                setSources(next.packageSources)
                setPolicy(next.packagePolicy)
              })
            }
            secretMode={publication.config.secretMode ?? "variables"}
            user={packageUser}
            token={packageToken}
            onCredentials={(next) =>
              edit(() => {
                if (next.packageUser !== undefined)
                  setPackageUser(next.packageUser)
                if (next.packageToken !== undefined)
                  setPackageToken(next.packageToken)
              })
            }
            credentialsNote="Builds use these mirrors. The token is stored as a masked, protected CI variable in this app's GitLab project; JustCD does not keep it. Closing this panel does not apply anything: review and apply on the page."
          />
          <PublishingCard contentClassName="space-y-3">
            <FormField
              label="Template parameters (optional)"
              htmlFor="build-setup-parameters"
              hint="Non-secret key=value lines for the template. Shiny: renvRestore=missing keeps the packages your base image already has (for example sf and terra) and installs only what renv.lock adds, instead of rebuilding the whole lockfile from source."
            >
              <Textarea
                id="build-setup-parameters"
                className="font-mono text-xs"
                value={parameters}
                placeholder="renvRestore=missing"
                onChange={(event) =>
                  edit(() => setParameters(event.target.value))
                }
              />
            </FormField>
          </PublishingCard>
          {dirty && (
            <p
              role="status"
              className="px-1 text-sm leading-6 text-muted-foreground"
            >
              These build settings are{" "}
              <span className="font-medium text-foreground">
                not applied yet
              </span>
              . Enter your GitLab token and review the changes below. Nothing is
              saved or committed until you apply them.
            </p>
          )}
          <PublishingCard contentClassName="space-y-3">
            <FormField
              label="GitLab token"
              htmlFor="build-setup-token"
              error={tokenError || undefined}
              hint="Needed to read and change this app's project (commit files, set CI variables): a token with API access and the Maintainer role. Used for this request only and never stored."
            >
              <Input
                id="build-setup-token"
                type="password"
                autoComplete="off"
                value={gitlabToken}
                onChange={(event) =>
                  edit(() => {
                    setGitlabToken(event.target.value)
                    setTokenError("")
                  })
                }
              />
            </FormField>
            <Button
              type="button"
              disabled={busy}
              loading={busy && !plan}
              loadingText="Checking the repository…"
              onClick={() => void review()}
            >
              {dirty ? "Review changes to apply" : "Review changes"}
            </Button>
          </PublishingCard>
          {plan &&
            (plan.upToDate ? (
              <PublishingCard>
                <p className="text-sm">
                  The repository already matches these settings. Nothing to
                  change.
                </p>
              </PublishingCard>
            ) : (
              <>
                <section
                  aria-labelledby="build-setup-changes"
                  className="space-y-3"
                >
                  <div className="flex flex-wrap items-center gap-2">
                    <h2
                      id="build-setup-changes"
                      className="text-base font-medium"
                    >
                      What will change
                    </h2>
                    <Badge variant="outline" size="sm">
                      Source commit {plan.sourceSha.slice(0, 12)}
                    </Badge>
                  </div>
                  <p className="text-sm text-muted-foreground">
                    {plan.images.length} setting changes · {plan.changes.length}{" "}
                    generated file changes. Your source files and deployment
                    destination stay as they are. Files you edited in GitLab
                    since publishing show as changes here.
                  </p>
                  <RecipeReview
                    images={plan.images}
                    changes={plan.changes}
                    emptyText="Only the generated files change."
                  />
                </section>
                <PublishingCard contentClassName="space-y-3">
                  <p className="text-sm text-muted-foreground">
                    Applying commits these files, sets the package credentials
                    in GitLab and starts a build. The running app keeps its
                    current version until you review and apply the new release.
                  </p>
                  <Button
                    type="button"
                    disabled={busy}
                    loading={busy}
                    loadingText="Updating build setup and starting build…"
                    onClick={() => void apply()}
                  >
                    Apply and rebuild
                  </Button>
                </PublishingCard>
              </>
            ))}
        </>
      ) : (
        error == null && (
          <p role="status" className="text-sm text-muted-foreground">
            Loading application…
          </p>
        )
      )}
    </div>
  )
}
