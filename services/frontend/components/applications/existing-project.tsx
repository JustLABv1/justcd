"use client"

import { useEffect, useState } from "react"
import Link from "next/link"
import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowLeft01Icon } from "@hugeicons/core-free-icons"
import { api } from "@/lib/api"
import type {
  Cluster,
  GitSource,
  ListResponse,
  NamespaceBinding,
} from "@/lib/types"
import type { ApprovedPublishingProfile } from "@/lib/publishing"
import type { ApplicationTemplate } from "./template-catalog"
import { PublishingCard, PublishingField } from "./publishing-fields"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { FormSelect } from "@/components/ui/form-select"
import { Disclosure } from "@/components/ui/collapsible"
import { ErrorNotice } from "@/components/workspace-ui"

type Inspection = {
  sha: string
  hasDockerfile: boolean
  hasPipeline: boolean
  manifestPaths: string[]
}
type Plan = Inspection & {
  hash: string
  needsImage: boolean
  files: { file_path: string; content: string }[]
  manifestPath: string
  notes: string[]
}
type Result = {
  applicationId: string
  mergeRequestUrl: string
  notes: string[]
}

export function ExistingProject({
  workspaceId,
  owner,
  template,
  profiles,
  onBack,
}: {
  workspaceId: string
  owner: boolean
  template: ApplicationTemplate
  profiles: ApprovedPublishingProfile[]
  onBack: () => void
}) {
  const [sources, setSources] = useState<GitSource[]>([])
  const [sourceId, setSourceId] = useState("")
  const [profileId, setProfileId] = useState(
    profiles[0]?.id ?? (owner ? "manual" : "")
  )
  const [revision, setRevision] = useState("main")
  const [appDir, setAppDir] = useState(".")
  const [name, setName] = useState("")
  const [entryPoint, setEntryPoint] = useState(template.entryPoint)
  const [buildCommand, setBuildCommand] = useState("python build_html.py")
  const [outputDir, setOutputDir] = useState("dist")
  const [gitlabToken, setGitlabToken] = useState("")
  const [clusters, setClusters] = useState<Cluster[]>([])
  const [clusterId, setClusterId] = useState("")
  const [bindings, setBindings] = useState<NamespaceBinding[]>([])
  const [namespace, setNamespace] = useState("")
  const [baseImage, setBaseImage] = useState("")
  const [builderImage, setBuilderImage] = useState("")
  const [registryImage, setRegistryImage] = useState("")
  const [webServerImage, setWebServerImage] = useState("")
  const [host, setHost] = useState("")
  const [pullSecret, setPullSecret] = useState("")
  const [runnerTag, setRunnerTag] = useState("")
  const [image, setImage] = useState("")
  const [manifestPath, setManifestPath] = useState("deploy")
  const [inspection, setInspection] = useState<Inspection | null>(null)
  const [plan, setPlan] = useState<Plan | null>(null)
  const [result, setResult] = useState<Result | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const manual = profileId === "manual"

  useEffect(() => {
    let active = true
    api<ListResponse<GitSource>>(
      `/api/v1/git-sources?workspaceId=${encodeURIComponent(workspaceId)}`
    )
      .then(({ items }) => {
        if (active) {
          const owned = items.filter(
            (s) => s.workspaceId === workspaceId && !s.shared
          )
          setSources(owned)
          setSourceId(owned[0]?.id ?? "")
        }
      })
      .catch((e) => active && setError(e))
    if (owner)
      api<ListResponse<Cluster>>(
        `/api/v1/clusters?workspaceId=${encodeURIComponent(workspaceId)}`
      )
        .then(({ items }) => {
          if (active) {
            setClusters(items)
            setClusterId(items[0]?.id ?? "")
          }
        })
        .catch((e) => active && setError(e))
    return () => {
      active = false
    }
  }, [workspaceId, owner])
  useEffect(() => {
    if (!clusterId || !manual) return
    let active = true
    api<ListResponse<NamespaceBinding>>(
      `/api/v1/clusters/${encodeURIComponent(clusterId)}/bindings?workspaceId=${encodeURIComponent(workspaceId)}`
    )
      .then(({ items }) => {
        if (active) {
          setBindings(items)
          setNamespace(items[0]?.namespace ?? "")
        }
      })
      .catch((e) => active && setError(e))
    return () => {
      active = false
    }
  }, [workspaceId, clusterId, manual])

  function metadata() {
    return {
      workspaceId,
      sourceId,
      revision,
      appDir,
      image,
      ...(manual
        ? {
            gitlabToken,
            clusterId,
            config: {
              templateId: template.id,
              runtime: template.runtime,
              name,
              entryPoint,
              baseImage,
              builderImage,
              registryImage,
              namespace,
              host,
              pullSecret,
              runnerTag,
              secretMode: "variables",
              ...(template.runtime === "python-static"
                ? { buildCommand, outputDir, webServerImage }
                : {}),
            },
          }
        : {
            profileId,
            name,
            entryPoint,
            ...(template.runtime === "python-static"
              ? { buildCommand, outputDir }
              : {}),
          }),
    }
  }
  async function run(action: "inspect" | "plan" | "publish") {
    setBusy(true)
    setError(null)
    try {
      if (action === "inspect") {
        setPlan(null)
        setInspection(
          await api<Inspection>("/api/v1/publishing/repository/inspect", {
            method: "POST",
            body: JSON.stringify(metadata()),
          })
        )
      } else if (action === "plan") {
        setPlan(
          await api<Plan>("/api/v1/publishing/repository/plan", {
            method: "POST",
            body: JSON.stringify(metadata()),
          })
        )
      } else {
        setResult(
          await api<Result>("/api/v1/publishing/repository", {
            method: "POST",
            body: JSON.stringify({ ...metadata(), planHash: plan?.hash }),
          })
        )
        setGitlabToken("")
      }
    } catch (e) {
      setError(e)
    } finally {
      setBusy(false)
    }
  }
  function text(
    label: string,
    value: string,
    change: (s: string) => void,
    hint?: string,
    required = true,
    type = "text"
  ) {
    const id = `existing-${label.toLowerCase().replaceAll(/[^a-z0-9]+/g, "-")}`
    return (
      <PublishingField label={label} htmlFor={id} hint={hint}>
        <Input
          id={id}
          value={value}
          onChange={(e) => {
            change(e.target.value)
            setPlan(null)
          }}
          required={required}
          type={type}
        />
      </PublishingField>
    )
  }
  const importURL = `/applications/new?${new URLSearchParams({ workspaceId, sourceId, revision, manifestPath, name })}`
  if (result)
    return (
      <div className="space-y-4">
        <h1 className="text-xl font-medium">Merge request ready</h1>
        <PublishingCard contentClassName="space-y-3">
          <p className="text-sm leading-6">
            Your existing Git source is connected to a new JustCD application
            with manual deployment. Review and merge the proposal, complete the
            CI prerequisites, and let the first build finish before planning a
            deployment.
          </p>
          {result.notes.map((note) => (
            <p key={note} className="text-sm text-muted-foreground">
              {note}
            </p>
          ))}
          <div className="flex flex-wrap gap-3">
            <Button
              nativeButton={false}
              render={
                <a
                  href={result.mergeRequestUrl}
                  target="_blank"
                  rel="noreferrer"
                />
              }
            >
              Review merge request
            </Button>
            <Button
              variant="outline"
              nativeButton={false}
              render={<Link href={`/applications/${result.applicationId}`} />}
            >
              Open application
            </Button>
          </div>
        </PublishingCard>
      </div>
    )
  return (
    <form
      className="space-y-4 pb-5"
      onSubmit={(e) => {
        e.preventDefault()
        void run(inspection ? "plan" : "inspect")
      }}
    >
      <Button
        type="button"
        variant="ghost"
        onClick={onBack}
        className="-ml-3 gap-2 text-muted-foreground"
      >
        <HugeiconsIcon icon={ArrowLeft01Icon} />
        Publishing options
      </Button>
      <header>
        <h1 className="text-xl font-medium">
          Use an existing {template.name} project
        </h1>
        <p className="mt-1 text-sm leading-6 text-muted-foreground">
          Inspect your GitLab repository, reuse what is there, and review
          missing files before creating a merge request.
        </p>
      </header>
      {error != null && <ErrorNotice error={error} />}
      <fieldset
        disabled={busy}
        className="space-y-4 disabled:opacity-70"
        onChange={() => setPlan(null)}
      >
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          <PublishingField
            label="Git source"
            htmlFor="existing-source"
            hint="Use a connection owned by this workspace. Shared repositories are available through the existing manifest import."
          >
            <FormSelect
              id="existing-source"
              value={sourceId}
              onValueChange={(v) => {
                setSourceId(v)
                setInspection(null)
                setPlan(null)
              }}
              items={sources.map((s) => ({
                value: s.id,
                label: `${s.name} · ${s.repositoryUrl}`,
              }))}
              placeholder="Select repository"
            />
            {owner && (
              <Link
                className="mt-2 inline-block text-xs text-primary underline"
                href={`/workspaces/${workspaceId}/git-sources/new`}
              >
                Connect a repository
              </Link>
            )}
          </PublishingField>
          {text(
            "Branch",
            revision,
            (v) => {
              setRevision(v)
              setInspection(null)
            },
            "Existing branch to inspect and target with the merge request."
          )}
          {text(
            "Application directory",
            appDir,
            (v) => {
              setAppDir(v)
              setInspection(null)
            },
            "Build context relative to the repository root, for example . or apps/report."
          )}
          {text(
            "Application name",
            name,
            setName,
            "Lowercase letters, numbers and hyphens."
          )}
          <PublishingField
            label="Publishing settings"
            htmlFor="existing-profile"
            hint="Profiles supply approved defaults. Existing Git projects retain ownership of their CI settings and secrets."
          >
            <FormSelect
              id="existing-profile"
              value={profileId}
              onValueChange={(v) => {
                setProfileId(v)
                setInspection(null)
                setPlan(null)
              }}
              items={[
                ...profiles.map((p) => ({ value: p.id, label: p.name })),
                ...(owner
                  ? [{ value: "manual", label: "Configure manually" }]
                  : []),
              ]}
              placeholder="Select profile"
            />
          </PublishingField>
          {manual &&
            text(
              "GitLab API token",
              gitlabToken,
              setGitlabToken,
              "Used for inspection and the merge request only. Your Git source keeps its existing read credential.",
              true,
              "password"
            )}
        </div>
        {!owner && !profiles.length && (
          <PublishingCard>
            <p className="text-sm text-muted-foreground">
              Ask your workspace administrator to configure a publishing profile
              for this template, or connect existing deployment manifests on
              your behalf.
            </p>
          </PublishingCard>
        )}
        {!sources.length && (
          <PublishingCard>
            <p className="text-sm text-muted-foreground">
              Connect your existing GitLab repository first. No repository will
              be created by this workflow.
            </p>
          </PublishingCard>
        )}
        {owner && (
          <PublishingCard contentClassName="space-y-3">
            <h2 className="text-sm font-medium">
              Already have deployment manifests?
            </h2>
            <p className="text-sm text-muted-foreground">
              Connect them directly using the existing application wizard. No
              merge request or build changes are needed.
            </p>
            <div className="flex flex-wrap items-end gap-3">
              <div>
                <label
                  htmlFor="existing-manifests"
                  className="mb-2 block text-sm"
                >
                  Manifest directory
                </label>
                <Input
                  id="existing-manifests"
                  value={manifestPath}
                  onChange={(e) => setManifestPath(e.target.value)}
                />
              </div>
              <Button
                type="button"
                disabled={!sourceId || !manifestPath}
                variant="outline"
                nativeButton={false}
                render={<Link href={importURL} />}
              >
                Use existing manifests
              </Button>
            </div>
            {inspection && inspection.manifestPaths.length > 0 && (
              <p className="text-xs break-words text-muted-foreground">
                Found: {inspection.manifestPaths.slice(0, 8).join(", ")}
              </p>
            )}
          </PublishingCard>
        )}
        <Button
          type="button"
          variant="outline"
          disabled={
            !sourceId ||
            !name ||
            (!manual && !profiles.length) ||
            (manual && !gitlabToken)
          }
          onClick={() => void run("inspect")}
        >
          Inspect repository
        </Button>
        {inspection && (
          <>
            <div className="grid gap-4 md:grid-cols-3">
              <PublishingCard>
                <p className="text-sm font-medium">Dockerfile</p>
                <p className="mt-1 text-sm text-muted-foreground">
                  {inspection.hasDockerfile
                    ? "Reuse existing Dockerfile"
                    : inspection.hasPipeline
                      ? "Owned by your existing build"
                      : "Generate from the selected template"}
                </p>
              </PublishingCard>
              <PublishingCard>
                <p className="text-sm font-medium">GitLab pipeline</p>
                <p className="mt-1 text-sm text-muted-foreground">
                  {inspection.hasPipeline
                    ? "Keep existing pipeline"
                    : "Add a build and publish pipeline"}
                </p>
              </PublishingCard>
              <PublishingCard>
                <p className="text-sm font-medium">Deployment files</p>
                <p className="mt-1 text-sm text-muted-foreground">
                  {inspection.manifestPaths.length
                    ? `${inspection.manifestPaths.length} possible manifest files found`
                    : "Generate deployment files"}
                </p>
              </PublishingCard>
            </div>
            <h2 className="pt-2 text-base font-medium">
              Prepare missing deployment files
            </h2>
            <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
              {!inspection.hasDockerfile &&
                !inspection.hasPipeline &&
                (template.runtime === "python-static" ? (
                  <>
                    {text("Build command", buildCommand, setBuildCommand)}
                    {text(
                      "Output directory",
                      outputDir,
                      setOutputDir,
                      "Must contain index.html after the build."
                    )}
                  </>
                ) : (
                  text(
                    template.runtime === "shiny"
                      ? "Shiny app directory"
                      : "Python entry file",
                    entryPoint,
                    setEntryPoint,
                    "Relative to the application directory."
                  )
                ))}
              {inspection.hasPipeline &&
                text(
                  "Published image digest",
                  image,
                  setImage,
                  "Full immutable image reference from your pipeline: registry/repository/image@sha256:…"
                )}
              {manual && (
                <>
                  <PublishingField label="Cluster" htmlFor="existing-cluster">
                    <FormSelect
                      id="existing-cluster"
                      value={clusterId}
                      onValueChange={(v) => {
                        setClusterId(v)
                        setNamespace("")
                        setBindings([])
                        setPlan(null)
                      }}
                      items={clusters.map((c) => ({
                        value: c.id,
                        label: c.name,
                      }))}
                    />
                  </PublishingField>
                  <PublishingField
                    label="Namespace"
                    htmlFor="existing-namespace"
                  >
                    <FormSelect
                      id="existing-namespace"
                      value={namespace}
                      onValueChange={(v) => {
                        setNamespace(v)
                        setPlan(null)
                      }}
                      items={bindings.map((b) => ({
                        value: b.namespace,
                        label: b.namespace,
                      }))}
                    />
                  </PublishingField>
                  {!inspection.hasDockerfile &&
                    !inspection.hasPipeline &&
                    text(
                      "Base image",
                      baseImage,
                      setBaseImage,
                      "Approved R/Shiny or Python image."
                    )}
                  {!inspection.hasPipeline &&
                    text(
                      "CI builder image",
                      builderImage,
                      setBuilderImage,
                      "Rootless BuildKit, git, jq and a POSIX shell."
                    )}
                  {!inspection.hasPipeline &&
                    text(
                      "Registry destination",
                      registryImage,
                      setRegistryImage,
                      "Registry/repository/image without tag."
                    )}
                  {template.runtime === "python-static" &&
                    !inspection.hasDockerfile &&
                    !inspection.hasPipeline &&
                    text(
                      "Static web server image",
                      webServerImage,
                      setWebServerImage,
                      "Unprivileged NGINX-compatible server, port 8080, /usr/share/nginx/html."
                    )}
                  {text(
                    "Hostname",
                    host,
                    setHost,
                    "Optional OpenShift route hostname.",
                    false
                  )}
                  {text(
                    "Image pull secret",
                    pullSecret,
                    setPullSecret,
                    "Optional existing namespace secret.",
                    false
                  )}
                  {text(
                    "Runner tag",
                    runnerTag,
                    setRunnerTag,
                    "Optional GitLab runner tag.",
                    false
                  )}
                </>
              )}
            </div>
            <p className="text-sm text-muted-foreground">
              The proposal adds files only. Existing secrets, repository
              settings, Dockerfiles and pipelines stay under your control.
            </p>
            <Button
              type="submit"
              disabled={!sourceId || (!manual && !profiles.length)}
            >
              Review proposed files
            </Button>
          </>
        )}
      </fieldset>
      {plan && (
        <PublishingCard contentClassName="space-y-4">
          <h2 className="text-base font-medium">Review additions</h2>
          <p className="text-xs text-muted-foreground">
            Based on commit {plan.sha.slice(0, 12)}. Changes to the repository
            or settings require a fresh review.
          </p>
          {plan.notes.map((note) => (
            <p key={note} className="text-sm leading-6 text-muted-foreground">
              {note}
            </p>
          ))}
          {plan.files.map((file) => (
            <Disclosure key={file.file_path} summary={`Add ${file.file_path}`}>
              <pre className="max-h-80 overflow-auto rounded-lg bg-muted p-3 text-xs">
                <code>{file.content}</code>
              </pre>
            </Disclosure>
          ))}
          <p className="text-sm text-muted-foreground">
            Creates a merge request and registers an application with manual
            deployment. Merge and finish the first build before deploying.
          </p>
          <Button
            type="button"
            disabled={busy || plan.needsImage}
            onClick={() => void run("publish")}
          >
            {busy ? "Creating…" : "Create merge request"}
          </Button>
        </PublishingCard>
      )}
    </form>
  )
}
