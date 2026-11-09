"use client"

import { useEffect, useState } from "react"
import { useParams, useRouter } from "next/navigation"
import Link from "next/link"
import { Button } from "@/components/panelui/button"
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert"
import { Spinner } from "@/components/ui/spinner"
import { Input } from "@/components/ui/input"
import { ArchiveUpload } from "@/components/applications/archive-upload"
import {
  CheckboxCard,
  FormField,
  PageHeading,
  Panel,
} from "@/components/ui-kit"
import { ErrorNotice } from "@/components/workspace-ui"
import { api } from "@/lib/api"
import { publishProject, publishingSize } from "@/lib/publishing"
import { usePublishingLimits } from "@/hooks/use-publishing-limits"
import { useToast } from "@/components/toast-provider"

type Publication = {
  applicationId: string
  completionStatus?: string
  completionError?: string
  profileId?: string
  profileName?: string
  profileEnabled?: boolean
  profileRevision?: number
  latestProfileRevision?: number
  profileUpdateAvailable?: boolean
  config: { name: string; runtime: string; [key: string]: string }
  gitlabUrl: string
  group: string
  pipelineUrl: string
}
type Setup = {
  publication: Publication
  workspaceId: string
  clusterId: string
}

export default function PublishVersionPage() {
  const toast = useToast()
  const router = useRouter()
  const [uploadPercent, setUploadPercent] = useState(0)
  const { limits, error: limitsError } = usePublishingLimits()
  const { applicationID } = useParams<{ applicationID: string }>()
  const [setup, setSetup] = useState<Setup | null>(null)
  const [file, setFile] = useState<File | null>(null)
  const [archiveValid, setArchiveValid] = useState(false)
  const [token, setToken] = useState("")
  const [confirmed, setConfirmed] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [pipeline, setPipeline] = useState("")
  useEffect(() => {
    let active = true
    api<Setup>(
      `/api/v1/applications/${encodeURIComponent(applicationID)}/publication`
    )
      .then((value) => {
        if (active) setSetup(value)
      })
      .catch((cause) => {
        if (active) setError(cause)
      })
    return () => {
      active = false
    }
  }, [applicationID])
  async function submit(event: React.FormEvent) {
    event.preventDefault()
    if (
      !setup ||
      !file ||
      !archiveValid ||
      !limits ||
      !confirmed ||
      busy ||
      setup.publication.profileEnabled === false
    )
      return
    setBusy(true)
    setUploadPercent(0)
    setError(null)
    try {
      const result = await toast.promise(publishProject<Publication>(
        setup.publication.profileId
          ? {
              workspaceId: setup.workspaceId,
              profileId: setup.publication.profileId,
              updateApplicationId: applicationID,
            }
          : {
              workspaceId: setup.workspaceId,
              clusterId: setup.clusterId,
              updateApplicationId: applicationID,
              gitlabUrl: setup.publication.gitlabUrl,
              group: setup.publication.group,
              gitlabToken: token,
              gitReadToken: "",
              registryUser: "",
              registryPassword: "",
              config: setup.publication.config,
            },
        file,
        limits,
        setUploadPercent
      ), { loading: "Publishing the new version…", success: "New version published. The pipeline has started.", error: null })
      setPipeline(result.pipelineUrl)
      setToken("")
      setFile(null)
      router.push(`/applications/${applicationID}?tab=publishing`)
    } catch (cause) {
      setError(cause)
    } finally {
      setBusy(false)
    }
  }
  return (
    <>
      <PageHeading
        title="Publish a new version"
        description={
          setup
            ? `${setup.publication.config.name} · ${setup.publication.config.runtime}. Reuse the existing repository, build configuration, secrets and deployment target.`
            : "Loading application publishing settings…"
        }
      />
      {setup?.publication.completionError && (
        <Panel
          title="Deployment update needs attention"
          description={setup.publication.completionError}
        >
          <p className="px-5 pb-4 text-sm text-muted-foreground">
            JustCD retries automatically. Check the publishing token permissions
            and backend GitLab connectivity.
          </p>
        </Panel>
      )}
      {(error ?? limitsError) != null && (
        <ErrorNotice error={error ?? limitsError} />
      )}
      {busy && (
        <Alert
          role="status"
          className="mb-5 border-primary/25 bg-primary/5 p-4"
        >
          <Spinner className="text-primary" />
          <AlertTitle>
            {uploadPercent < 100
              ? `Uploading new version · ${uploadPercent}%`
              : "Upload complete · Preparing build"}
          </AlertTitle>
          <AlertDescription>
            {uploadPercent < 100
              ? "Your source archive is being transferred. Keep this page open."
              : "JustCD is validating the source, committing it to GitLab, and requesting a build. This may take a little while. Keep this page open; publishing progress will open automatically once the pipeline starts."}
          </AlertDescription>
        </Alert>
      )}
      {pipeline ? (
        <Panel
          title="Build started"
          description="The running application keeps its current version until the new release is built and applied."
        >
          <div className="space-y-4 p-5">
            <Button variant="outline" nativeButton={false} render={<a href={pipeline} target="_blank" rel="noreferrer" />}>
              Follow GitLab pipeline
            </Button>
            <p className="text-sm text-muted-foreground">
              After the build succeeds, review and apply the new deployment plan
              in JustCD.
            </p>
            <Button nativeButton={false} render={<Link href={`/applications/${applicationID}`} />}>
              Open application
            </Button>
          </div>
        </Panel>
      ) : (
        <form onSubmit={submit} className="space-y-5">
          <Panel
            title="Upload application source"
            description="Each upload is a complete source snapshot. Files absent from this archive are removed from source/ in Git."
          >
            <fieldset
              disabled={!setup || !limits || busy}
              className="space-y-4 p-5"
            >
              <FormField
                label="Application ZIP"
                htmlFor="version-archive"
                hint={
                  limits
                    ? `Keep the same layout and dependencies as the first upload. Maximum ${publishingSize(limits.archiveBytes)} compressed and ${publishingSize(limits.expandedBytes)} expanded. Remove local credentials and Git history.`
                    : "Loading upload limits…"
                }
              >
                <ArchiveUpload
                  id="version-archive"
                  file={file}
 validation={{ workspaceId: setup?.workspaceId ?? "", runtime: setup?.publication.config.runtime ?? "", entryPoint: setup?.publication.config.entryPoint ?? "." }}
 onValidationChange={setArchiveValid}
                  maxBytes={limits?.archiveBytes}
                  disabled={!setup || !limits || busy}
                  onFileChange={(next) => {
                    setFile(next)
                setArchiveValid(false)
                    setError(null)
                  }}
                />
              </FormField>
              {setup?.publication.profileId ? (
                <div className="rounded-xl border bg-muted/30 p-4 text-sm">
                  <p className="font-medium">{setup.publication.profileName}</p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    Using v{setup.publication.profileRevision}
                    {setup.publication.profileUpdateAvailable
                      ? ` · v${setup.publication.latestProfileRevision} available`
                      : ""}
                  </p>
                  {setup.publication.profileUpdateAvailable && (
                    <Link
                      className="mt-2 inline-block text-sm text-primary underline"
                      href={`/applications/${applicationID}/profile-update`}
                    >
                      Review profile update
                    </Link>
                  )}
                  <p className="mt-1 text-muted-foreground">
                    {setup.publication.profileEnabled === false
                      ? "This profile is disabled. Ask your workspace administrator before publishing another version."
                      : "This source upload keeps the application’s pinned profile version and uses current credentials. Use the profile update action to review newer images and rebuild without uploading a ZIP."}
                  </p>
                </div>
              ) : (
                <>
                  <FormField
                    label="GitLab provisioning token"
                    htmlFor="version-token"
                    hint="Requires API access and permission to commit to the protected main branch and start pipelines. Used for this request and not stored."
                  >
                    <Input
                      id="version-token"
                      type="password"
                      autoComplete="off"
                      minLength={8}
                      required
                      value={token}
                      onChange={(event) => setToken(event.target.value)}
                    />
                  </FormField>
                </>
              )}
              <CheckboxCard
                id="version-confirm"
                checked={confirmed}
                onCheckedChange={setConfirmed}
                title="Replace source and build a new release"
                description="The complete source snapshot will be committed to GitLab. Existing CI secret settings are reused. Deployment still follows JustCD review and approval controls."
              />
              <Button
                type="submit"
                loading={busy}
                disabled={!file || !archiveValid || !confirmed}
              >
                {busy ? (uploadPercent < 100 ? `Uploading source · ${uploadPercent}%` : "Preparing build…") : "Upload and build version"}
              </Button>
            </fieldset>
          </Panel>
        </form>
      )}
    </>
  )
}
