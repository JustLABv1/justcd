"use client"

import { useEffect, useState } from "react"
import Link from "next/link"
import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowLeft01Icon } from "@hugeicons/core-free-icons"
import {
  TemplateCatalog,
  TemplateGlyph,
  type ApplicationTemplate,
} from "@/components/applications/template-catalog"
import { ArchiveUpload } from "@/components/applications/archive-upload"
import {
  PublishingCard,
  PublishingField,
} from "@/components/applications/publishing-fields"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { FormSelect } from "@/components/ui/form-select"
import { Disclosure } from "@/components/ui/collapsible"
import { ErrorNotice } from "@/components/workspace-ui"
import { usePublishingLimits } from "@/hooks/use-publishing-limits"
import { api } from "@/lib/api"
import {
  publishProject,
  publishingSize,
  type ApprovedPublishingProfile,
  type PublicationResult,
} from "@/lib/publishing"
import type { ListResponse, Workspace } from "@/lib/types"

export default function PublishApplicationPage() {
  const { limits, error: limitsError } = usePublishingLimits()
  const [method, setMethod] = useState<"profile" | null>(null)
  const [template, setTemplate] = useState<ApplicationTemplate | null>(null)
  const [workspaces, setWorkspaces] = useState<Workspace[]>([])
  const [workspaceId, setWorkspaceId] = useState("")
  const [profiles, setProfiles] = useState<ApprovedPublishingProfile[]>([])
  const [profileId, setProfileId] = useState("")
  const [name, setName] = useState("")
  const [buildCommand, setBuildCommand] = useState("python build_html.py")
  const [outputDir, setOutputDir] = useState("dist")
  const [entryPoint, setEntryPoint] = useState("")
  const [file, setFile] = useState<File | null>(null)
  const [busy, setBusy] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<unknown>(null)
  const [result, setResult] = useState<PublicationResult | null>(null)
  const workspace = workspaces.find((item) => item.id === workspaceId)
  const choices = profiles.filter(
    (profile) => profile.templateId === template?.id
  )
  const profile = choices.find((item) => item.id === profileId) ?? choices[0]
  useEffect(() => {
    let active = true
    api<ListResponse<Workspace>>("/api/v1/workspaces")
      .then(({ items }) => {
        if (!active) return
        const allowed = items.filter(
          (item) => item.role === "owner" || item.role === "deployer"
        )
        const preferred = new URLSearchParams(window.location.search).get(
          "workspaceId"
        )
        setWorkspaces(allowed)
        setWorkspaceId(
          allowed.find((item) => item.id === preferred)?.id ??
            allowed[0]?.id ??
            ""
        )
      })
      .catch((cause) => {
        if (active) setError(cause)
      })
    return () => {
      active = false
    }
  }, [])
  useEffect(() => {
    if (!workspaceId) return
    let active = true
    api<ListResponse<ApprovedPublishingProfile>>(
      `/api/v1/publishing/profiles?workspaceId=${encodeURIComponent(workspaceId)}`
    )
      .then((result) => {
        if (active) {
          setProfiles(result.items)
          setError(null)
        }
      })
      .catch((cause) => {
        if (active) setError(cause)
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [workspaceId])
  async function publish(event: React.FormEvent) {
    event.preventDefault()
    if (!file || !limits || !profile || !template || busy) return
    setBusy(true)
    setError(null)
    try {
      setResult(
        await publishProject<PublicationResult>(
          {
            workspaceId,
            profileId: profile.id,
            name,
            entryPoint,
            ...(template?.runtime === "python-static"
              ? { buildCommand, outputDir }
              : {}),
          },
          file,
          limits
        )
      )
    } catch (cause) {
      setError(cause)
    } finally {
      setBusy(false)
    }
  }
  const management =
    workspace?.role === "owner" ? (
      <Link
        className="text-sm font-medium text-primary hover:underline"
        href={`/workspaces/${workspaceId}?tab=publishing`}
      >
        Manage publishing profiles →
      </Link>
    ) : null
  if (result)
    return (
      <div className="max-w-3xl space-y-5">
        <h1 className="text-2xl font-medium tracking-tight">
          Your app is being built
        </h1>
        <p className="text-sm leading-6 text-muted-foreground">
          JustCD has created the repository and started the build using{" "}
          {profile?.name}.{" "}
          {result.syncPolicy === "auto-safe"
            ? "After a successful build, deployment follows your workspace approval rules automatically."
            : "After a successful build, an authorized user must review and apply the deployment plan."}
        </p>
        <div className="space-y-4 rounded-2xl border bg-card p-5">
          {result.applicationUrl && (
            <div>
              <p className="text-xs text-muted-foreground">
                Application URL · available after deployment
              </p>
              <a
                className="mt-1 block font-medium break-all text-primary hover:underline"
                href={result.applicationUrl}
                target="_blank"
                rel="noreferrer"
              >
                {result.applicationUrl}
              </a>
            </div>
          )}
          <div className="flex flex-wrap gap-4">
            <Link
              className="text-sm font-medium text-primary hover:underline"
              href={`/applications/${result.applicationId}`}
            >
              Open application in JustCD →
            </Link>
            <a
              className="text-sm text-primary hover:underline"
              href={result.pipelineUrl}
              target="_blank"
              rel="noreferrer"
            >
              Follow build progress →
            </a>
          </div>
        </div>
      </div>
    )
  if (!template)
    return (
      <div className="space-y-5">
        <TemplateCatalog
          onSelect={(next) => {
            setMethod(null)
            setTemplate(next)
            setEntryPoint(next.entryPoint)
            setBuildCommand("python build_html.py")
            setOutputDir("dist")
            setFile(null)
            setProfileId("")
            setError(null)
          }}
        />
        {management}
      </div>
    )
  const manualURL = `/applications/publish/manual?workspaceId=${encodeURIComponent(workspaceId)}&templateId=${encodeURIComponent(template.id)}`
  if (!method)
    return (
      <div className="space-y-5 pb-5">
        <Button
          type="button"
          variant="ghost"
          className="-ml-3 gap-2 text-muted-foreground"
          onClick={() => setTemplate(null)}
        >
          <HugeiconsIcon icon={ArrowLeft01Icon} />
          All templates
        </Button>
        <header className="flex items-start gap-4">
          <TemplateGlyph template={template} className="size-11 bg-muted/50" />
          <div>
            <h1 className="text-xl font-medium tracking-tight">
              How would you like to publish {template.name}?
            </h1>
            <p className="mt-1 text-sm leading-6 text-muted-foreground">
              Choose a publishing profile or configure the setup yourself.
            </p>
          </div>
        </header>
        {error != null && <ErrorNotice error={error} />}
        <div className="max-w-lg">
          <PublishingField label="Workspace" htmlFor="setup-workspace">
            <FormSelect
              id="setup-workspace"
              value={workspaceId}
              onValueChange={(value) => {
                setWorkspaceId(value)
                setProfileId("")
                setProfiles([])
                setLoading(true)
              }}
              items={workspaces.map((item) => ({
                value: item.id,
                label: item.name,
              }))}
            />
          </PublishingField>
        </div>
        <div className="grid gap-4 md:grid-cols-2">
          <PublishingCard contentClassName="flex h-full flex-col items-start gap-3">
            <h2 className="text-base font-medium">Use a publishing profile</h2>
            <p className="flex-1 text-sm leading-6 text-muted-foreground">
              Use an administrator-approved runtime and destination. Repository,
              build settings, and credentials are already configured.
            </p>
            <p className="text-xs text-muted-foreground">
              {loading
                ? "Checking available profiles…"
                : choices.length
                  ? `${choices.length} ${choices.length === 1 ? "profile available" : "profiles available"} for ${template.name}`
                  : `No profile configured for ${template.name} in this workspace.`}
            </p>
            <div className="flex flex-wrap items-center gap-4">
              <Button
                type="button"
                disabled={loading || !choices.length}
                onClick={() => setMethod("profile")}
              >
                Use publishing profile
              </Button>
              {management}
            </div>
          </PublishingCard>
          <PublishingCard contentClassName="flex h-full flex-col items-start gap-3">
            <h2 className="text-base font-medium">Manual setup</h2>
            <p className="flex-1 text-sm leading-6 text-muted-foreground">
              Set up GitLab, build images, registry access, secrets, and the
              deployment destination with a guided wizard.
            </p>
            <p className="text-xs text-muted-foreground">
              {workspace?.role === "owner"
                ? "Available with or without publishing profiles."
                : "Manual setup requires a workspace owner."}
            </p>
            {workspace?.role === "owner" ? (
              <Button variant="outline" render={<Link href={manualURL} />}>
                Set up manually
              </Button>
            ) : (
              <Button variant="outline" disabled>
                Set up manually
              </Button>
            )}
          </PublishingCard>
        </div>
      </div>
    )
  return (
    <div className="space-y-5 pb-5">
      <Button
        type="button"
        variant="ghost"
        disabled={busy}
        onClick={() => setMethod(null)}
        className="-ml-3 gap-2 text-muted-foreground"
      >
        <HugeiconsIcon icon={ArrowLeft01Icon} />
        Publishing options
      </Button>
      <header className="flex items-start gap-4">
        <TemplateGlyph template={template} className="size-11 bg-muted/50" />
        <div>
          <h1 className="text-xl font-medium tracking-tight">
            Publish {template.name}
          </h1>
          <p className="mt-1 text-sm leading-6 text-muted-foreground">
            Bring your application. Your publishing profile handles the
            repository, build, and deployment.
          </p>
        </div>
      </header>
      {(error ?? limitsError) != null && (
        <ErrorNotice error={error ?? limitsError} />
      )}
      {!workspaces.length && (
        <p className="text-sm text-muted-foreground">
          Publishing requires the owner or deployer role in a workspace. Ask
          your administrator for access.
        </p>
      )}
      <form onSubmit={publish} className="space-y-4">
        <fieldset disabled={busy || !limits} className="min-w-0 space-y-4">
          <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
            <PublishingField label="Workspace" htmlFor="publish-workspace">
              <FormSelect
                id="publish-workspace"
                value={workspaceId}
                onValueChange={(value) => {
                  setWorkspaceId(value)
                  setProfileId("")
                  setProfiles([])
                  setLoading(true)
                }}
                items={workspaces.map((item) => ({
                  value: item.id,
                  label: item.name,
                }))}
              />
            </PublishingField>
            <PublishingField
              label="Application name"
              htmlFor="publish-name"
              hint="Lowercase letters, numbers, and hyphens."
            >
              <Input
                id="publish-name"
                value={name}
                onChange={(event) => setName(event.target.value)}
                required
                maxLength={63}
                pattern="[a-z](([a-z0-9]|-)*[a-z0-9])?"
                placeholder="regional-statistics"
              />
            </PublishingField>
            <PublishingField
              label="Where to publish"
              htmlFor="publish-profile"
              hint={
                profile
                  ? `${profile.clusterName} / ${profile.namespace}`
                  : "An administrator must configure an approved destination."
              }
            >
              <FormSelect
                id="publish-profile"
                value={profile?.id ?? ""}
                onValueChange={setProfileId}
                items={choices.map((item) => ({
                  value: item.id,
                  label: item.name,
                }))}
                placeholder={
                  loading
                    ? "Loading destinations…"
                    : "No destinations available"
                }
                disabled={loading || !choices.length}
              />
            </PublishingField>
          </div>
          {!loading && !choices.length && (
            <PublishingCard contentClassName="space-y-2">
              <p className="text-sm font-medium">
                No publishing profile for {template.name} in this workspace
              </p>
              <p className="text-sm text-muted-foreground">
                {workspace?.role === "owner"
                  ? "Configure an approved runtime, credentials, and destination once to enable publishing."
                  : "Ask your workspace administrator to enable a publishing profile for this template."}
              </p>
              <div className="flex flex-wrap items-center gap-x-4 gap-y-2 pt-1">
                {management}
                {workspace?.role === "owner" && (
                  <Button variant="outline" render={<Link href={manualURL} />}>
                    Set up manually
                  </Button>
                )}
              </div>
            </PublishingCard>
          )}
          <PublishingField
            label="Application ZIP"
            htmlFor="publish-archive"
            hint={
              limits
                ? `Up to ${publishingSize(limits.archiveBytes)} compressed and ${publishingSize(limits.expandedBytes)} expanded. ${template.runtime === "python-static" ? "Optionally include requirements.txt" : "Include " + (template.runtime === "shiny" ? "renv.lock or packages.txt" : "requirements.txt")} at the archive root. Remove local credentials before uploading.`
                : "Loading upload limits…"
            }
          >
            <ArchiveUpload
              id="publish-archive"
              file={file}
              onFileChange={(next) => {
                setFile(next)
                setError(null)
              }}
              maxBytes={limits?.archiveBytes}
              disabled={busy || !limits || !profile}
            />
          </PublishingField>
          {template.runtime === "python-static" ? (
            <div className="grid gap-4 md:grid-cols-2">
              <PublishingField
                label="Build command"
                htmlFor="publish-build-command"
                hint="Runs in CI after installing requirements.txt, when present."
              >
                <Input
                  id="publish-build-command"
                  required
                  value={buildCommand}
                  onChange={(event) => setBuildCommand(event.target.value)}
                />
              </PublishingField>
              <PublishingField
                label="Output directory"
                htmlFor="publish-output-dir"
                hint="Relative folder containing the generated index.html and assets, for example dist."
              >
                <Input
                  id="publish-output-dir"
                  required
                  value={outputDir}
                  onChange={(event) => setOutputDir(event.target.value)}
                />
              </PublishingField>
            </div>
          ) : (
            <PublishingCard>
              <Disclosure summary="Application entry point">
                <div className="max-w-lg">
                  <label
                    htmlFor="publish-entry"
                    className="mb-2 block text-sm font-medium"
                  >
                    {template.runtime === "shiny"
                      ? "Shiny app directory"
                      : "Python entry file"}
                  </label>
                  <Input
                    id="publish-entry"
                    required
                    value={entryPoint}
                    onChange={(event) => setEntryPoint(event.target.value)}
                  />
                  <p className="mt-2 text-xs leading-5 text-muted-foreground">
                    {template.runtime === "shiny"
                      ? "Use . for an app at the archive root, or a folder such as src/regionalsteckbrief."
                      : "Relative path to your Streamlit app, for example app.py."}
                  </p>
                </div>
              </Disclosure>
            </PublishingCard>
          )}
          {profile && (
            <div className="space-y-1 px-1 text-sm leading-6 text-muted-foreground">
              {profile.description && <p>{profile.description}</p>}
              <p>
                {profile.syncPolicy === "auto-safe"
                  ? "Successful builds deploy automatically when workspace approval rules allow."
                  : "Successful builds wait for a deployment review and apply."}{" "}
                {profile.hostSuffix
                  ? `Your app will use https://${name || "application-name"}.${profile.hostSuffix}.`
                  : "This destination provides internal Service access."}
              </p>
            </div>
          )}
          <div className="flex flex-wrap items-center justify-between gap-3 rounded-2xl border bg-card p-3">
            <p className="text-xs leading-5 text-muted-foreground">
              {busy
                ? "Creating the repository, configuring the build, and starting CI…"
                : "Your administrator’s settings and credentials are applied securely."}
            </p>
            <Button
              type="submit"
              loading={busy}
              loadingText="Preparing your app…"
              disabled={!file || !profile || !workspaceId || loading}
              className="h-10 px-5"
            >
              Publish app
            </Button>
          </div>
        </fieldset>
      </form>
    </div>
  )
}
