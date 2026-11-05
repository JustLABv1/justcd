"use client"

import { useEffect, useRef, useState } from "react"
import Link from "next/link"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { PublishingServicesEditor } from "@/components/settings/publishing-services"
import { ArchiveUpload } from "@/components/applications/archive-upload"
import { FormSelect } from "@/components/ui/form-select"
import { Textarea } from "@/components/ui/textarea"
import { PageHeading } from "@/components/ui-kit"
import {
  SummaryList,
  Wizard,
  WizardFooter,
  useWizard,
} from "@/components/applications/wizard"
import { ErrorNotice } from "@/components/workspace-ui"
import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowLeft01Icon } from "@hugeicons/core-free-icons"
import {
  TemplateCatalog,
  TemplateGlyph,
  layoutSummary,
  type ApplicationTemplate,
} from "@/components/applications/template-catalog"
import {
  PublishingCheckbox,
  PublishingField as FormField,
  PublishingSection as Panel,
} from "@/components/applications/publishing-fields"
import { api } from "@/lib/api"
import { publishProject, publishingSize, selectedWorkspaceId, templatesUrl } from "@/lib/publishing"
import { WorkspaceNote } from "@/components/applications/workspace-note"
import { PackageSourcesCard } from "@/components/settings/package-sources"
import { usePublishingDraft } from "@/hooks/use-publishing-draft"
import { PublishingDraftNotice } from "@/components/applications/publishing-draft-notice"
import { usePublishingLimits } from "@/hooks/use-publishing-limits"
import type {
  Cluster,
  ListResponse,
  NamespaceBinding,
  Workspace,
} from "@/lib/types"
import { useToast } from "@/components/toast-provider"

type Config = {
  services?: string
  createNamespace?: boolean
  templateId?: string
  name: string
  runtime: string
  entryPoint: string
  buildCommand?: string
  outputDir?: string
  webServerImage?: string
  baseImage: string
  builderImage: string
  registryImage: string
  namespace: string
  host: string
  pullSecret: string
  secretMode: string
  vaultAddress: string
  vaultRole: string
  vaultPath: string
  gitImage?: string
  buildEngine?: string
  caVariable: string
  runnerTag: string
  runnerServiceAccount: string
  runnerScc: string
  builderSetup: string
  exposure: string
  exposureLabels: string
  exposureAnnotations: string
  ingressClass: string
  ingressTls: string
  templateParameters: string
  packageSources: string
  packagePolicy: string
}
type Publication = {
  applicationId: string
  projectUrl: string
  repositoryUrl: string
  pipelineUrl: string
}
const initialConfig: Config = {
  services: "",
  name: "",
  runtime: "shiny",
  entryPoint: ".",
  baseImage: "",
  builderImage: "",
  registryImage: "",
  namespace: "",
  host: "",
  pullSecret: "",
  secretMode: "variables",
  vaultAddress: "",
  vaultRole: "",
  vaultPath: "",
  gitImage: "",
  buildEngine: "buildkit",
  caVariable: "",
  runnerTag: "",
  runnerServiceAccount: "",
  runnerScc: "",
  builderSetup: "",
  exposure: "route",
  exposureLabels: "",
  exposureAnnotations: "",
  ingressClass: "",
  ingressTls: "hosts",
  templateParameters: "",
  packageSources: "",
  packagePolicy: "",
}

export default function PublishApplicationPage() {
  const toast = useToast()
  const { limits, error: limitsError } = usePublishingLimits()
  const [selectedTemplate, setSelectedTemplate] =
    useState<ApplicationTemplate | null>(null)
  const wizard = useWizard(6, "publish-app")
  const [workspaces, setWorkspaces] = useState<Workspace[]>([])
  const [workspaceId, setWorkspaceId] = useState("")
  const [clusters, setClusters] = useState<Cluster[]>([])
  const [clusterId, setClusterId] = useState("")
  const [bindings, setBindings] = useState<NamespaceBinding[]>([])
  const [config, setConfig] = useState(initialConfig)
  const [gitlabUrl, setGitlabUrl] = useState("")
  const [group, setGroup] = useState("")
  const [gitlabToken, setGitlabToken] = useState("")
  const [gitReadToken, setGitReadToken] = useState("")
  const [registryUser, setRegistryUser] = useState("")
  const [registryPassword, setRegistryPassword] = useState("")
  // Package credentials are never kept in the browser draft.
  const [packageUser, setPackageUser] = useState("")
  const [packageToken, setPackageToken] = useState("")
  const [file, setFile] = useState<File | null>(null)
  const [archiveValid, setArchiveValid] = useState(false)
  const [acknowledged, setAcknowledged] = useState(false)
  const [busy, setBusy] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<unknown>(null)
  const [result, setResult] = useState<Publication | null>(null)
  const errorRef = useRef<HTMLDivElement>(null)
  const patch = (values: Partial<Config>) =>
    setConfig((current) => ({ ...current, ...values }))

  const draft = usePublishingDraft(
    workspaceId && selectedTemplate
      ? `manual:${workspaceId}:${selectedTemplate.id}`
      : null,
    {
      clusterId,
      gitlabUrl,
      group,
      step: wizard.step,
      config: {
        ...config,
        createNamespace: !!config.createNamespace,
        templateId: config.templateId ?? "",
        buildCommand: config.buildCommand ?? "",
        outputDir: config.outputDir ?? "",
        webServerImage: config.webServerImage ?? "",
      },
    },
    (data) => {
      setClusterId(data.clusterId)
      setGitlabUrl(data.gitlabUrl)
      setGroup(data.group)
      setConfig((current) => ({ ...current, ...data.config }))
      wizard.setStep(Math.max(1, Math.min(6, Math.trunc(data.step))))
    }
  )

  useEffect(() => {
    const id = new URLSearchParams(window.location.search).get("templateId")
    if (!id) return
    let active = true
    api<ListResponse<ApplicationTemplate>>(templatesUrl(new URLSearchParams(window.location.search).get("workspaceId") ?? undefined))
      .then(({ items }) => {
        const template = items.find((item) => item.id === id)
        if (!active || !template) return
        setSelectedTemplate(template)
        setConfig((current) => ({
          ...current,
          templateId: template.id,
          runtime: template.runtime,
          entryPoint: template.entryPoint,
          buildCommand:
            template.runtime === "python-static" ? "python build_html.py" : "",
          outputDir: template.runtime === "python-static" ? "dist" : "",
          webServerImage: "",
        }))
      })
      .catch((cause) => {
        if (active) setError(cause)
      })
    return () => {
      active = false
    }
  }, [])
  useEffect(() => {
    let active = true
    api<ListResponse<Workspace>>("/api/v1/workspaces")
      .then(({ items }) => {
        if (!active) return
        const owners = items.filter((item) => item.role === "owner")
        setWorkspaces(owners)
        const preferred =
          new URLSearchParams(window.location.search).get("workspaceId") ||
          selectedWorkspaceId()
        setWorkspaceId(
          owners.find((item) => item.id === preferred)?.id ??
            owners[0]?.id ??
            ""
        )
      })
      .catch((cause) => active && setError(cause))
      .finally(() => active && setLoading(false))
    return () => {
      active = false
    }
  }, [])
  useEffect(() => {
    if (!workspaceId) return
    let active = true
    api<ListResponse<Cluster>>(
      `/api/v1/clusters?workspaceId=${encodeURIComponent(workspaceId)}`
    )
      .then(({ items }) => {
        if (!active) return
        setClusters(items)
        setClusterId((current) => current || items[0]?.id || "")
      })
      .catch((cause) => active && setError(cause))
    return () => {
      active = false
    }
  }, [workspaceId])
  useEffect(() => {
    if (!workspaceId || !clusterId) return
    let active = true
    api<ListResponse<NamespaceBinding>>(
      `/api/v1/clusters/${encodeURIComponent(clusterId)}/bindings?workspaceId=${encodeURIComponent(workspaceId)}`
    )
      .then(({ items }) => {
        if (!active) return
        setBindings(items)
        setConfig((current) => ({
          ...current,
          namespace: current.createNamespace
            ? current.namespace
            : current.namespace || items[0]?.namespace || "",
        }))
      })
      .catch((cause) => active && setError(cause))
    return () => {
      active = false
    }
  }, [workspaceId, clusterId])
  useEffect(() => {
    if (error) errorRef.current?.focus()
  }, [error])

  const packagesNeedCredentials =
    config.secretMode === "variables" &&
    (() => {
      try {
        const list = JSON.parse(config.packageSources || "[]")
        return Array.isArray(list) && list.some((item) => item?.authenticated)
      } catch {
        return false
      }
    })()
  const checkStep = (step: number) => {
    const problem =
      step === 1 && (!workspaceId || !file || !archiveValid)
        ? "Choose a workspace and upload a ZIP that passes the structure check."
        : step === 3 && packagesNeedCredentials && (!packageUser || packageToken.length < 8)
          ? "Enter the package user and token for the authenticated package sources."
        : step === 5 && (!clusterId || !config.namespace)
          ? "Choose an available cluster and namespace."
          : step === 6 && !acknowledged
            ? "Confirm the publishing actions before continuing."
            : ""
    if (problem) {
      setError(new Error(problem))
      return false
    }
    return true
  }
  async function publish(event: React.FormEvent) {
    event.preventDefault()
    if (!wizard.isLast || !wizard.validateAll()) return
    for (let step = 1; step <= 6; step++)
      if (!checkStep(step)) {
        wizard.setStep(step)
        return
      }
    if (!file || !archiveValid || !limits || busy) return
    setBusy(true)
    setError(null)
    try {
      const publication = await toast.promise(publishProject<Publication>(
        {
          workspaceId,
          clusterId,
          gitlabUrl: gitlabUrl.replace(/\/$/, ""),
          group,
          gitlabToken,
          gitReadToken,
          registryUser: config.secretMode === "variables" ? registryUser : "",
          registryPassword:
            config.secretMode === "variables" ? registryPassword : "",
          ...(packagesNeedCredentials ? { packageUser, packageToken } : {}),
          config,
        },
        file,
        limits
      ), { loading: "Publishing your project…", success: "Project published. The pipeline has started.", error: null })
      draft.clear()
      setResult(publication)
      setGitlabToken("")
      setGitReadToken("")
      setRegistryPassword("")
      setPackageToken("")
      setFile(null)
    } catch (cause) {
      setError(cause)
    } finally {
      setBusy(false)
    }
  }
  const textField = (
    key: Exclude<keyof Config, "createNamespace">,
    label: string,
    hint?: string,
    required = true,
    placeholder?: string
  ) => (
    <FormField label={label} htmlFor={`publish-${key}`} hint={hint}>
      <Input
        id={`publish-${key}`}
        value={config[key]}
        required={required}
        onChange={(event) => patch({ [key]: event.target.value })}
        placeholder={placeholder}
      />
    </FormField>
  )

  if (result)
    return (
      <>
        <PageHeading
          title="Your application is being built"
          description="The private repository and JustCD application are ready. GitLab is building and publishing the first image."
        />
        <Panel
          title={config.name}
          description="Follow the build, then review the deployment in JustCD."
        >
          <div className="space-y-5 rounded-2xl border border-border bg-card p-5 dark:border-transparent dark:bg-muted/45">
            <SummaryList
              items={[
                {
                  label: "Repository",
                  value: (
                    <a
                      href={result.projectUrl}
                      target="_blank"
                      rel="noreferrer"
                      className="text-primary hover:underline"
                    >
                      Open GitLab project
                    </a>
                  ),
                },
                {
                  label: "Build",
                  value: (
                    <a
                      href={result.pipelineUrl}
                      target="_blank"
                      rel="noreferrer"
                      className="text-primary hover:underline"
                    >
                      Follow pipeline progress
                    </a>
                  ),
                },
                {
                  label: "Deployment",
                  value: "Manual · calculate a plan after the build succeeds",
                },
              ]}
            />
            <p className="self-start px-1 text-sm leading-6 text-muted-foreground md:col-span-2 lg:col-span-3">
              The build commits the immutable image digest into
              deploy/application.yaml. JustCD will discover that revision.
              Review and apply the plan to make the app available. Before the
              first successful build, no deployment manifest exists.
            </p>
            <Link
              href={`/applications/${result.applicationId}`}
              className="font-medium text-primary hover:underline"
            >
              Open application →
            </Link>
          </div>
        </Panel>
      </>
    )

  if (!selectedTemplate)
    return (
      <TemplateCatalog
        onSelect={(template) => {
          setFile(null)
          setConfig((current) => ({
            ...current,
            templateId: template.id,
            runtime: template.runtime,
            entryPoint: template.entryPoint,
            buildCommand:
              template.runtime === "python-static"
                ? "python build_html.py"
                : "",
            outputDir: template.runtime === "python-static" ? "dist" : "",
            webServerImage: "",
            baseImage: "",
          }))
          setSelectedTemplate(template)
          wizard.setStep(1)
          setError(null)
        }}
      />
    )

  return (
    <div className="space-y-4 pb-4">
      <Button
        type="button"
        variant="ghost"
        className="-ml-3 gap-2 text-muted-foreground"
        disabled={busy}
        onClick={() => setSelectedTemplate(null)}
      >
        <HugeiconsIcon icon={ArrowLeft01Icon} strokeWidth={1.8} />
        All templates
      </Button>
      <header className="flex items-start gap-4">
        <TemplateGlyph
          template={selectedTemplate}
          className="size-11 bg-muted/50"
        />
        <div className="min-w-0 space-y-1">
          <h1 className="text-xl font-medium tracking-tight">
            {selectedTemplate.name}
          </h1>
          <p className="max-w-3xl text-sm leading-6 text-muted-foreground">
            {selectedTemplate.description}
          </p>
        </div>
      </header>
      <WorkspaceNote
        name={workspaces.find((item) => item.id === workspaceId)?.name}
      />
      <div ref={errorRef} tabIndex={-1}>
        {(error ?? limitsError) != null && (
          <ErrorNotice error={error ?? limitsError} />
        )}
      </div>
      {!loading && !workspaces.length && (
        <p className="mb-5 text-sm text-muted-foreground">
          A workspace owner must set up publishing. Ask your workspace owner to
          configure the repository, build runner, and deployment target.
        </p>
      )}
      <form onSubmit={publish} noValidate className="space-y-5">
        <fieldset
          disabled={
            busy || loading || !workspaces.length || !limits || !draft.ready
          }
          className="min-w-0 space-y-5"
        >
          <Wizard
            step={wizard.step}
            onStepChange={(step) => {
              if (!busy) wizard.setStep(step)
            }}
            idPrefix="publish-app"
            steps={[
              {
                title: "Application",
                content: (
                  <Panel
                    title="Bring your application"
                    description="Upload the contents of your project directory, with dependency files at the archive root."
                  >
                    <div className="grid gap-4 md:grid-cols-2">
                      {textField(
                        "name",
                        "Application name",
                        "Lowercase letters, numbers and hyphens; used for the repository and workload names.",
                        true,
                        "regional-statistics"
                      )}
                      {config.runtime === "python-static" ? (
                        <>
                          {textField(
                            "buildCommand",
                            "Build command",
                            "Runs in CI. requirements.txt is installed when present."
                          )}
                          {textField(
                            "outputDir",
                            "Output directory",
                            "Relative directory containing index.html and generated assets."
                          )}
                        </>
                      ) : (
                        textField(
                          "entryPoint",
                          config.runtime === "static"
                            ? "Website directory"
                            : config.runtime === "shiny"
                              ? "Shiny app directory"
                              : config.runtime === "server"
                                ? "Entry file or directory"
                                : "Python entry point",
                          config.runtime === "static"
                            ? "Use . for index.html at the archive root, or a subdirectory such as public containing index.html and assets."
                            : config.runtime === "shiny"
                              ? "Use . for an app at the archive root, or a path such as src/regionalsteckbrief. Include renv.lock or packages.txt at the root."
                              : config.runtime === "server"
                                ? `Relative path to the file or directory your template expects. ${selectedTemplate?.layout?.length ? "Required files: " + layoutSummary(selectedTemplate, config.entryPoint) + "." : ""}`
                                : "For example app.py. Include requirements.txt at the archive root."
                        )
                      )}
                      <FormField
                        label="Application ZIP"
                        htmlFor="publish-archive"
                        className="md:col-span-2 lg:col-span-3"
                        hint={
                          limits
                            ? `Maximum ${publishingSize(limits.archiveBytes)} compressed, ${publishingSize(limits.expandedBytes)} expanded, 1500 archive entries. Remove credentials, .env files, local Git history and session files.`
                            : "Loading upload limits…"
                        }
                      >
                        <ArchiveUpload
                          id="publish-archive"
                          key={selectedTemplate.id}
                          file={file}
 validation={{ workspaceId, runtime: config.runtime, entryPoint: config.entryPoint, templateId: config.templateId, packageSources: config.packageSources, packagePolicy: config.packagePolicy }}
 onValidationChange={setArchiveValid}
                          maxBytes={limits?.archiveBytes}
                          disabled={
                            busy || loading || !workspaces.length || !limits
                          }
                          onFileChange={(next) => {
                            setFile(next)
                setArchiveValid(false)
                            setError(null)
                          }}
                        />
                      </FormField>
                    </div>
                  </Panel>
                ),
              },
              {
                title: "Repository",
                content: (
                  <Panel
                    title="Where should the Git repository live?"
                    description="JustCD creates a private project in your GitLab group. Existing unrelated repositories are never overwritten."
                  >
                    <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
                      <FormField label="GitLab URL" htmlFor="publish-gitlab">
                        <Input
                          id="publish-gitlab"
                          type="url"
                          required
                          value={gitlabUrl}
                          onChange={(event) => setGitlabUrl(event.target.value)}
                          placeholder="https://gitlab.example.com"
                        />
                      </FormField>
                      <FormField
                        label="Group or subgroup path"
                        htmlFor="publish-group"
                      >
                        <Input
                          id="publish-group"
                          required
                          value={group}
                          onChange={(event) => setGroup(event.target.value)}
                          placeholder="analytics/apps"
                        />
                      </FormField>
                      <FormField
                        label="GitLab provisioning token"
                        htmlFor="publish-git-token"
                        hint="Requires API access and permission to create private projects and configure CI/CD variables in this group. Used for this request; not stored by JustCD."
                      >
                        <Input
                          id="publish-git-token"
                          type="password"
                          autoComplete="off"
                          required
                          minLength={8}
                          value={gitlabToken}
                          onChange={(event) =>
                            setGitlabToken(event.target.value)
                          }
                        />
                      </FormField>
                      <FormField
                        label="Git repository read token"
                        htmlFor="publish-read-token"
                        hint="Use a group access token with read_repository for the new project. JustCD stores this credential encrypted so it can read deployment revisions."
                      >
                        <Input
                          id="publish-read-token"
                          type="password"
                          autoComplete="off"
                          required
                          minLength={8}
                          value={gitReadToken}
                          onChange={(event) =>
                            setGitReadToken(event.target.value)
                          }
                        />
                      </FormField>
                      <p className="self-start px-1 text-sm leading-6 text-muted-foreground md:col-span-2 lg:col-span-3">
                        JustCD retains the API token encrypted to update
                        deployment manifests after builds. It must permit Git
                        repository writes to the protected main branch. GitLab
                        18.4 or newer is recommended. The protected main branch
                        is the publishing branch.
                      </p>
                    </div>
                  </Panel>
                ),
              },
              {
                title: "Build",
                content: (
                  <Panel
                    title="How should the app be built?"
                    description="Your platform team supplies approved image references. JustCD generates the Dockerfile and GitLab pipeline."
                  >
                    <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
                      {config.runtime !== "static" &&
                        textField(
                          "baseImage",
                          config.runtime === "python-static"
                            ? "Python build image"
                            : "Runtime base image",
                          "Must provide R/Shiny or Python and your required system libraries and certificate trust. Pin an approved version or digest.",
                          true,
                          "artifactory.example.com/platform/shiny-geospatial:4.4.2"
                        )}
                      {(config.runtime === "python-static" ||
                        config.runtime === "static") &&
                        textField(
                          "webServerImage",
                          "Static web server image",
                          "Unprivileged NGINX-compatible image: port 8080, /usr/share/nginx/html, UID 1001 and OpenShift arbitrary UID support."
                        )}
                      <FormField
                        label="Build engine"
                        htmlFor="publish-build-engine"
                        hint="Choose the engine installed in your CI builder image."
                      >
                        <FormSelect
                          id="publish-build-engine"
                          value={config.buildEngine || "buildkit"}
                          onValueChange={(buildEngine) =>
                            setConfig((previous) => ({
                              ...previous,
                              buildEngine,
                            }))
                          }
                          items={[
                            { value: "buildkit", label: "BuildKit" },
                            { value: "podman", label: "Podman" },
                          ]}
                        />
                      </FormField>
                      {textField(
                        "builderImage",
                        "CI builder image",
                        "Podman requires podman, grep and shell tools; BuildKit requires rootless BuildKit, jq and a shell. JustCD updates the deployment manifest after the build.",
                        true,
                        "artifactory.example.com/platform/justcd-builder:1"
                      )}
                      {textField(
                        "registryImage",
                        "Artifactory destination image",
                        "Registry hostname and repository/image path, without a tag. Each build gets its source commit tag.",
                        true,
                        "artifactory.example.com/analytics-local/regional-statistics"
                      )}
                      {textField(
                        "caVariable",
                        "Custom CA variable (optional)",
                        "Inherited GitLab variable containing PEM certificates. Configures CI Git and build-engine trust; build stages need their own CA configuration.",
                        false,
                        "PLATFORM_ROOT_CA"
                      )}
                      {textField(
                        "runnerTag",
                        "GitLab runner tag",
                        "Optional. Select a platform runner that can reach GitLab, package repositories, Artifactory and Vault.",
                        false
                      )}
                      {textField(
                        "runnerServiceAccount",
                        "Runner service account (optional)",
                        "Kubernetes service account the build job runs as, for runners that only allow rootless builds under a dedicated account.",
                        false,
                        "gitlab-runner-app-sa"
                      )}
                      {textField(
                        "runnerScc",
                        "Required OpenShift SCC (optional)",
                        "Security context constraint the build pod must use. Rootless Podman usually needs one that allows user namespaces.",
                        false,
                        "gitlab-runner-scc"
                      )}
                      {textField(
                        "builderSetup",
                        "Builder setup script (optional)",
                        "Absolute path of an executable in the builder image that runs before the build. No arguments.",
                        false,
                        "/home/user/entrypoint.sh"
                      )}
                      <FormField
                        label="Template parameters (optional)"
                        htmlFor="publish-template-parameters"
                        hint="Non-secret key=value lines for custom templates, available as .Parameters. For the generic server starter: startCommand=python app.py. For Shiny on a geospatial base image: renvRestore=missing installs only the renv.lock packages the image lacks."
                      >
                        <Textarea
                          id="publish-template-parameters"
                          value={config.templateParameters}
                          onChange={(event) =>
                            patch({ templateParameters: event.target.value })
                          }
                          placeholder="startCommand=python app.py"
                          className="font-mono text-xs"
                        />
                      </FormField>
                      <div className="md:col-span-2 lg:col-span-3">
                        <PackageSourcesCard
                          sources={config.packageSources}
                          policy={config.packagePolicy}
                          onChange={(next) => patch(next)}
                          secretMode={config.secretMode}
                          user={packageUser}
                          token={packageToken}
                          onCredentials={(next) => {
                            if (next.packageUser !== undefined) setPackageUser(next.packageUser)
                            if (next.packageToken !== undefined) setPackageToken(next.packageToken)
                          }}
                          missing={packagesNeedCredentials && (!packageUser || packageToken.length < 8)}
                          credentialsNote="Builds use these mirrors. The token is stored as a masked, protected CI variable in the new GitLab project; JustCD does not keep it."
                        />
                      </div>
                      <p className="self-start px-1 text-sm leading-6 text-muted-foreground md:col-span-2 lg:col-span-3">
                        {config.runtime === "static" ? (
                          "Your website files are copied directly into the approved webserver image. No dependencies are installed and no build command is run."
                        ) : (
                          <>
                            Without package sources above, the base image must
                            configure access to your package mirrors. Application dependencies are installed from
                            packages.txt / renv.lock or requirements.txt. Custom
                            native dependencies must be supplied by the base
                            image.
                          </>
                        )}
                      </p>
                    </div>
                  </Panel>
                ),
              },
              {
                title: "Secrets",
                content: (
                  <>
                  <Panel
                    title="Where should build secrets come from?"
                    description="Registry credentials are used by CI to push images. They are never committed to Git or included in the application image."
                  >
                    <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
                      <FormField
                        label="Secret storage"
                        htmlFor="publish-secret-mode"
                        className="md:col-span-2 lg:col-span-3"
                      >
                        <FormSelect
                          id="publish-secret-mode"
                          value={config.secretMode}
                          onValueChange={(secretMode) => {
                            patch({ secretMode })
                            setRegistryPassword("")
                            setRegistryUser("")
                          }}
                          items={[
                            {
                              value: "variables",
                              label: "GitLab · protected CI/CD variables",
                            },
                            {
                              value: "vault",
                              label:
                                "Vault · existing secrets with GitLab OIDC",
                            },
                          ]}
                        />
                      </FormField>
                      {config.secretMode === "variables" ? (
                        <>
                          <FormField
                            label="Registry username"
                            htmlFor="publish-registry-user"
                          >
                            <Input
                              id="publish-registry-user"
                              required
                              value={registryUser}
                              onChange={(event) =>
                                setRegistryUser(event.target.value)
                              }
                              autoComplete="off"
                            />
                          </FormField>
                          <FormField
                            label="Registry push token"
                            htmlFor="publish-registry-token"
                            hint="Scoped to the destination repository. Stored as a masked, protected GitLab project variable. At least eight characters without whitespace."
                          >
                            <Input
                              id="publish-registry-token"
                              type="password"
                              autoComplete="off"
                              required
                              minLength={8}
                              value={registryPassword}
                              onChange={(event) =>
                                setRegistryPassword(event.target.value)
                              }
                            />
                          </FormField>
                          <p className="self-start px-1 text-sm leading-6 text-muted-foreground md:col-span-2 lg:col-span-3">
                            JustCD sends REGISTRY_USER and REGISTRY_PASSWORD
                            through the GitLab API. GitLab stores the values;
                            JustCD does not retain them.
                          </p>
                        </>
                      ) : (
                        <>
                          {textField(
                            "vaultAddress",
                            "Vault server URL",
                            "Existing Vault integration with GitLab JWT authentication; no Vault administrator token is needed.",
                            true,
                            "https://vault.example.com"
                          )}
                          {textField(
                            "vaultRole",
                            "Vault JWT role",
                            "Must restrict access to this GitLab project or group and its protected main branch."
                          )}
                          {textField(
                            "vaultPath",
                            "Vault KV v2 API path",
                            "Existing secret containing username and token, for example secret/data/analytics/registry. GitLab native Vault secrets require a supported GitLab tier.",
                            true,
                            "secret/data/analytics/registry"
                          )}
                          <p className="self-start px-1 text-sm leading-6 text-muted-foreground md:col-span-2 lg:col-span-3">
                            This workflow references an existing Vault secret.
                            It does not create Vault policies or upload secret
                            values into Vault.
                          </p>
                        </>
                      )}
                    </div>
                  </Panel>
                  <PublishingServicesEditor value={config.services ?? ""} onChange={services => patch({ services })} />
                  <p className="text-xs text-muted-foreground">Every service configured here is included for this application. Use approved publishing profiles to offer end users optional services.</p>
                  </>
                ),
              },
              {
                title: "Deployment",
                content: (
                  <Panel
                    title="Where should the app run?"
                    description="Choose an existing workspace namespace or create one through the deployment flow. Cluster credentials remain managed by JustCD."
                  >
                    <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
                      <div className="md:col-span-2 lg:col-span-3">
                        <PublishingCheckbox
                          id="publish-create-namespace"
                          checked={!!config.createNamespace}
                          onCheckedChange={(checked) =>
                            patch({
                              createNamespace: checked,
                              namespace: checked
                                ? config.name
                                : (bindings[0]?.namespace ?? ""),
                            })
                          }
                          title="Create namespace if missing"
                          description="Registers a workspace binding now. JustCD provisions the namespace or OpenShift project during deployment using the configured cluster identity; cluster permissions and approval rules still apply."
                        />
                      </div>
                      <FormField label="Cluster" htmlFor="publish-cluster">
                        <FormSelect
                          id="publish-cluster"
                          value={clusterId}
                          onValueChange={(value) => {
                            setClusterId(value)
                            setBindings([])
                            patch({ namespace: "", createNamespace: false })
                          }}
                          items={clusters.map((item) => ({
                            value: item.id,
                            label: item.name,
                          }))}
                          placeholder="Select cluster"
                        />
                      </FormField>

                      <FormField label="Namespace" htmlFor="publish-namespace">
                        {config.createNamespace ? (
                          <Input
                            id="publish-namespace"
                            required
                            value={config.namespace}
                            onChange={(event) =>
                              patch({ namespace: event.target.value })
                            }
                            maxLength={63}
                            placeholder="my-app"
                          />
                        ) : (
                          <FormSelect
                            id="publish-namespace"
                            value={config.namespace}
                            onValueChange={(namespace) => patch({ namespace })}
                            items={bindings.map((item) => ({
                              value: item.namespace,
                              label: item.namespace,
                            }))}
                            placeholder="Select namespace"
                          />
                        )}
                      </FormField>
                      {!bindings.length && !config.createNamespace && (
                        <p className="self-start px-1 text-sm leading-6 text-muted-foreground md:col-span-2 lg:col-span-3">
                          Configure namespace access in workspace Connections
                          before publishing.
                        </p>
                      )}
                      {textField(
                        "pullSecret",
                        "Existing image pull secret",
                        config.createNamespace
                          ? "Optional registry pull secret. Your namespace provisioning setup must create it in the new namespace before deployment; secrets are not copied."
                          : "Optional for registries requiring no authentication. Otherwise supply an existing read-only registry secret in the selected namespace.",
                        false
                      )}
                      {textField(
                        "host",
                        "Application hostname",
                        "Optional. Publishes the app directly over HTTPS through a Route or Ingress. This initial template does not add SSO; leave blank for internal Service access only.",
                        false,
                        "statistics.apps.example.com"
                      )}
                      <FormField label="Publish with" htmlFor="publish-exposure">
                        <FormSelect
                          id="publish-exposure"
                          value={config.exposure || "route"}
                          onValueChange={(exposure) =>
                            patch({
                              exposure,
                              ...(exposure === "ingress"
                                ? {}
                                : { ingressClass: "", ingressTls: "hosts" }),
                            })
                          }
                          items={[
                            { value: "route", label: "OpenShift Route" },
                            { value: "ingress", label: "Kubernetes Ingress" },
                          ]}
                        />
                      </FormField>
                      {config.exposure === "ingress" && (
                        <>
                          {textField(
                            "ingressClass",
                            "Ingress class (optional)",
                            "Sets ingressClassName.",
                            false,
                            "openshift-default"
                          )}
                          <FormField
                            label="Ingress TLS"
                            htmlFor="publish-ingress-tls"
                            hint="List the host, or render tls: [] when TLS is handled elsewhere."
                          >
                            <FormSelect
                              id="publish-ingress-tls"
                              value={config.ingressTls || "hosts"}
                              onValueChange={(ingressTls) => patch({ ingressTls })}
                              items={[
                                { value: "hosts", label: "List the host (default)" },
                                { value: "none", label: "None (tls: [])" },
                              ]}
                            />
                          </FormField>
                        </>
                      )}
                      <FormField
                        label="Labels (optional)"
                        htmlFor="publish-exposure-labels"
                        hint="One key=value per line, on the Route or Ingress only."
                      >
                        <Textarea
                          id="publish-exposure-labels"
                          value={config.exposureLabels}
                          onChange={(event) =>
                            patch({ exposureLabels: event.target.value })
                          }
                          placeholder="router=external"
                        />
                      </FormField>
                      <FormField
                        label="Annotations (optional)"
                        htmlFor="publish-exposure-annotations"
                        hint="One key=value per line, on the Route or Ingress only."
                      >
                        <Textarea
                          id="publish-exposure-annotations"
                          value={config.exposureAnnotations}
                          onChange={(event) =>
                            patch({ exposureAnnotations: event.target.value })
                          }
                          placeholder="haproxy.router.openshift.io/timeout=5m"
                        />
                      </FormField>
                      {!config.host && (
                        <p className="self-start px-1 text-sm leading-6 text-muted-foreground md:col-span-2 lg:col-span-3">
                          Route or Ingress settings apply once an application
                          hostname is set.
                        </p>
                      )}
                      <p className="self-start px-1 text-sm leading-6 text-muted-foreground md:col-span-2 lg:col-span-3">
                        Deployment starts in manual mode. After the build
                        succeeds, review and apply the plan under your workspace
                        approval policy.
                      </p>
                    </div>
                  </Panel>
                ),
              },
              {
                title: "Review",
                content: (
                  <Panel
                    title="Ready to create your application?"
                    description="Review the destinations and actions before JustCD writes to GitLab."
                  >
                    <div className="space-y-5 rounded-2xl border border-border bg-card p-5 dark:border-transparent dark:bg-muted/45">
                      <SummaryList
                        items={[
                          {
                            label: "Application",
                            value: `${config.name || "—"} · ${config.runtime}`,
                          },
                          {
                            label: "Source",
                            value: file?.name ?? "No archive selected",
                          },
                          {
                            label: "Repository",
                            value: `${gitlabUrl}/${group}/${config.name}`,
                          },
                          {
                            label: "Image",
                            value: config.registryImage || "—",
                          },
                          {
                            label: "Secrets",
                            value:
                              config.secretMode === "vault"
                                ? "Existing Vault secret · GitLab OIDC"
                                : "Protected GitLab CI/CD variables",
                          },
                          {
                            label: "Target",
                            value: `${clusters.find((item) => item.id === clusterId)?.name ?? "—"} / ${config.namespace || "—"}`,
                          },
                          {
                            label: "Access",
                            value: config.host
                              ? `HTTPS ${config.exposure === "ingress" ? "Ingress" : "Route"} ${config.host} · no SSO`
                              : "Internal Service only",
                          },
                          {
                            label: "Deployment",
                            value: "Manual review and apply after build",
                          },
                        ]}
                      />
                      <PublishingCheckbox
                        id="publish-confirm"
                        checked={acknowledged}
                        onCheckedChange={setAcknowledged}
                        title="Create the repository and start the build"
                        description="JustCD will upload this source to a private GitLab project, configure CI, register the application, and start its pipeline. I have removed credentials from the source and reviewed the access settings."
                      />
                      <p className="self-start px-1 text-sm leading-6 text-muted-foreground md:col-span-2 lg:col-span-3">
                        If setup fails, the private repository is retained.
                        Retry the same archive and settings to resume. For
                        updates, choose Upload new version on the application
                        page. Each upload replaces the complete source snapshot.
                      </p>
                    </div>
                  </Panel>
                ),
              },
            ]}
          />
          <div className="space-y-3 rounded-2xl border border-border bg-card p-3 dark:border-transparent dark:bg-muted/30 [&_button]:h-10 [&_button]:rounded-lg [&_button]:px-4">
            <PublishingDraftNotice draft={draft} inline />
            <div className="border-t pt-3">
            <WizardFooter
              step={wizard.step}
              isLast={wizard.isLast}
              onBack={wizard.back}
              onNext={() => {
                setError(null)
                wizard.next(checkStep)
              }}
              submit={
                <Button
                  type="submit"
                  loading={busy}
                  loadingText="Creating repository and pipeline…"
                  disabled={!acknowledged}
                >
                  Create and build app
                </Button>
              }
            />
            </div>
          </div>
          {busy && (
            <p role="status" className="text-sm text-muted-foreground">
              Creating the private project, configuring secrets, uploading
              source, and starting CI. Keep this page open; setup can take up to
              ten minutes.
            </p>
          )}
        </fieldset>
      </form>
    </div>
  )
}
