"use client"

import { useEffect, useState } from "react"
import { useParams } from "next/navigation"
import Link from "next/link"
import { Button } from "@/components/ui/button"
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

type Publication = {
  applicationId: string
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
  const { limits, error: limitsError } = usePublishingLimits()
  const { applicationID } = useParams<{ applicationID: string }>()
  const [setup, setSetup] = useState<Setup | null>(null)
  const [file, setFile] = useState<File | null>(null)
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
    if (!setup || !file || !limits || !confirmed || busy) return
    setBusy(true)
    setError(null)
    try {
      const result = await publishProject<Publication>(
        {
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
        limits
      )
      setPipeline(result.pipelineUrl)
      setToken("")
      setFile(null)
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
      {(error ?? limitsError) != null && (
        <ErrorNotice error={error ?? limitsError} />
      )}
      {pipeline ? (
        <Panel
          title="Build started"
          description="The running application keeps its current version until the new release is built and applied."
        >
          <div className="space-y-4 p-5">
            <a
              className="font-medium text-primary hover:underline"
              href={pipeline}
              target="_blank"
              rel="noreferrer"
            >
              Follow GitLab pipeline →
            </a>
            <p className="text-sm text-muted-foreground">
              After the build succeeds, review and apply the new deployment plan
              in JustCD.
            </p>
            <Link
              className="font-medium text-primary hover:underline"
              href={`/applications/${applicationID}`}
            >
              Open application →
            </Link>
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
                  maxBytes={limits?.archiveBytes}
                  disabled={!setup || !limits || busy}
                  onFileChange={(next) => {
                    setFile(next)
                    setError(null)
                  }}
                />
              </FormField>
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
                loadingText="Uploading source…"
                disabled={!file || !confirmed}
              >
                Upload and build version
              </Button>
            </fieldset>
          </Panel>
        </form>
      )}
    </>
  )
}
