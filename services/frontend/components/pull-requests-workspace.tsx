"use client"

import Link from "next/link"
import { useParams } from "next/navigation"
import { useCallback, useEffect, useState, type FormEvent } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { ErrorNotice } from "@/components/workspace-ui"
import { EmptyState, FormField, PageHeading, Panel, StatusBadge } from "@/components/ui-kit"
import { APIError, api } from "@/lib/api"
import type { Application, ListResponse, PlanRecord, Workspace } from "@/lib/types"

type PreviewProfile = {
  enabled: boolean
  manifestPath: string
  namespacePrefix: string
  hostSuffix: string
  helmValuesYaml: string
  helmValuesFiles: string[]
  allowedSecrets: string[]
  databaseStrategy: string
  maxActive: number
  maxLifetimeHours: number
  allowForks: boolean
  quotaCpu: string
  quotaMemory: string
}

type Connection = { id: string; provider: "github" | "gitlab"; apiUrl: string; repository: string; previewProfile: PreviewProfile }
type SourceDetails = { repositoryUrl: string; provider: "github" | "gitlab"; apiUrl: string; repository: string; selfHosted: boolean }
type ConnectionResponse = { source: SourceDetails; connection: Connection | null; webhookUrl: string }
type Review = { id: string; number: number; headSha: string; sourceUrl: string; phase: string; error?: string; reportError?: string; plan?: PlanRecord; previewApplicationId?: string; expiresAt?: string; updatedAt: string }

const defaultProfile: PreviewProfile = {
  enabled: false, manifestPath: "", namespacePrefix: "preview", hostSuffix: "preview.example.com", helmValuesYaml: "", helmValuesFiles: [],
  allowedSecrets: [], databaseStrategy: "none", maxActive: 5, maxLifetimeHours: 72, allowForks: false, quotaCpu: "2", quotaMemory: "2Gi",
}

export function PullRequestsWorkspace({ embedded = false, canConfigure }: { embedded?: boolean; canConfigure?: boolean }) {
  const { applicationID } = useParams<{ applicationID: string }>()
  const [application, setApplication] = useState<Application | null>(null)
  const [source, setSource] = useState<SourceDetails | null>(null)
  const [ownerAccess, setOwnerAccess] = useState(false)
  const [connection, setConnection] = useState<Connection | null>(null)
  const [webhookUrl, setWebhookUrl] = useState("")
  const [reviews, setReviews] = useState<Review[]>([])
  const [apiUrl, setApiUrl] = useState("")
  const [confirmedSelfHosted, setConfirmedSelfHosted] = useState(false)
  const [webhookSecret, setWebhookSecret] = useState("")
  const [statusToken, setStatusToken] = useState("")
  const [profile, setProfile] = useState<PreviewProfile>(defaultProfile)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [showSettings, setShowSettings] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [savedMessage, setSavedMessage] = useState("")

  const load = useCallback(async () => {
    setError(null)
    setSource(null)
    const app = await api<Application>(`/api/v1/applications/${encodeURIComponent(applicationID)}`)
    setApplication(app)
    if (canConfigure === undefined) {
      const workspaces = await api<ListResponse<Workspace>>("/api/v1/workspaces")
      setOwnerAccess(workspaces.items.some((workspace) => workspace.id === app.workspaceId && workspace.role === "owner"))
    }
    let result: ConnectionResponse
    try {
      result = await api<ConnectionResponse>(`/api/v1/applications/${encodeURIComponent(applicationID)}/source-control`)
    } catch (cause) {
      if (cause instanceof APIError && cause.status === 404) {
        throw new Error("PR reporting requires the matching JustCD backend. Deploy the backend changes, then reload this page.")
      }
      throw cause
    }
    setSource(result.source)
    setConnection(result.connection)
    setWebhookUrl(result.webhookUrl)
    setApiUrl(result.connection?.apiUrl ?? result.source.apiUrl)
    setConfirmedSelfHosted(Boolean(result.connection && result.source.selfHosted))
    setProfile({ ...defaultProfile, ...result.connection?.previewProfile })
    if (result.connection) {
      const list = await api<ListResponse<Review>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/pull-requests`)
      setReviews(list.items)
    } else {
      setReviews([])
    }
  }, [applicationID, canConfigure])

  useEffect(() => {
    let active = true
    void Promise.resolve().then(load).catch((cause) => { if (active) setError(cause) }).finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [load])

  useEffect(() => {
    if (!loading && reviews.length > 0 && window.location.hash.startsWith("#review-")) {
      document.getElementById(window.location.hash.slice(1))?.scrollIntoView({ block: "start" })
    }
  }, [loading, reviews])

  function updateProfile<K extends keyof PreviewProfile>(key: K, value: PreviewProfile[K]) {
    setProfile((current) => ({ ...current, [key]: value }))
  }

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setSaving(true); setError(null); setSavedMessage("")
    try {
      const result = await api<ConnectionResponse>(`/api/v1/applications/${encodeURIComponent(applicationID)}/source-control`, {
        method: "PUT", body: JSON.stringify({ provider: source?.selfHosted ? (confirmedSelfHosted ? "gitlab" : "") : "", apiUrl: source?.selfHosted ? apiUrl : "", webhookSecret, statusToken, previewProfile: profile }),
      })
      setSource(result.source)
      setConnection(result.connection)
      setWebhookUrl(result.webhookUrl)
      setWebhookSecret(""); setStatusToken("")
      setSavedMessage("Source control connection saved. Add the webhook URL and secret to your repository settings.")
      setShowSettings(false)
    } catch (cause) { setError(cause) }
    finally { setSaving(false) }
  }

  return <div className="space-y-5">
    {embedded ? <header className="flex flex-wrap items-start justify-between gap-4"><div><h2 className="text-lg font-semibold tracking-tight">Pull requests</h2><p className="mt-1 text-sm text-muted-foreground">{application ? `Review plans and previews for ${application.name}.` : "Review plans and optional previews for this application."}</p></div><div className="flex flex-wrap items-center gap-2">
        {source && (canConfigure ?? ownerAccess) && (showSettings
          ? <Button variant="outline" size="sm" onClick={() => setShowSettings(false)}>Back to reviews</Button>
          : <Button size="sm" variant={connection ? "outline" : "default"} onClick={() => setShowSettings(true)}>{connection ? "Edit settings" : "Enable PR reporting"}</Button>)}
      </div></header> : <PageHeading title="Pull requests" description={application ? `Review plans and previews for ${application.name}.` : "Review plans and optional previews for this application."} actions={<>
        {!embedded && <Link href={`/applications/${applicationID}`}><Button variant="outline" size="sm">Back to application</Button></Link>}
        {source && (canConfigure ?? ownerAccess) && (showSettings
          ? <Button variant="outline" size="sm" onClick={() => setShowSettings(false)}>Back to reviews</Button>
          : <Button size="sm" variant={connection ? "outline" : "default"} onClick={() => setShowSettings(true)}>{connection ? "Edit settings" : "Enable PR reporting"}</Button>)}
      </>} />}
    {error != null && <ErrorNotice error={error} />}
    {savedMessage && <p role="status" className="rounded-lg border border-emerald-300/50 bg-emerald-50 p-3 text-sm text-emerald-900 dark:bg-emerald-950/30 dark:text-emerald-200">{savedMessage}</p>}
    {loading ? <p role="status" className="text-sm text-muted-foreground">Loading pull requests…</p> : source ? <>
      {showSettings ? <>
      <Panel title="PR reporting settings" description="Use this application’s Git source for webhook reporting and optional previews.">
        <form onSubmit={(event) => void save(event)} className="grid gap-4 p-5 md:grid-cols-2">
          <div className="md:col-span-2 rounded-lg border bg-muted/30 p-3 text-sm"><p className="font-medium">{source?.provider === "github" ? "GitHub" : source?.selfHosted ? "GitLab (self-hosted candidate)" : "GitLab"} · {connection?.repository ?? source?.repository}</p><p className="mt-1 break-all font-mono text-xs text-muted-foreground">{source?.repositoryUrl}</p><p className="mt-2 text-xs text-muted-foreground">To change this repository, edit the application’s Git source.</p></div>
          {source?.selfHosted && <>
            <FormField label="GitLab API URL" htmlFor="api-url" hint="Use your instance's HTTPS API URL, including any installation subpath and /api/v4."><Input id="api-url" required value={apiUrl} onChange={(event) => setApiUrl(event.target.value)} /></FormField>
            <label className="flex items-center gap-2 self-end text-sm"><input type="checkbox" checked={confirmedSelfHosted} onChange={(event) => setConfirmedSelfHosted(event.target.checked)} />This Git host runs self-hosted GitLab</label>
          </>}
          <div className="md:col-span-2 border-t pt-4"><p className="text-sm font-medium">PR/MR reporting access</p><p className="mt-1 text-xs text-muted-foreground">The existing Git credential fetches code. These separate credentials verify webhooks and post status back to the provider.</p></div>
          <FormField label="Webhook secret" htmlFor="webhook-secret" hint={connection?"Leave blank to keep the existing secret.":"At least 16 characters. Use this in the repository webhook settings."}><Input id="webhook-secret" type="password" required={!connection} value={webhookSecret} onChange={(event) => setWebhookSecret(event.target.value)} autoComplete="new-password" /></FormField>
          <FormField label="Status API token" htmlFor="status-token" hint={connection?"Leave blank to keep the existing token.":"Requires permission to write commit statuses in this repository."}><Input id="status-token" type="password" required={!connection} value={statusToken} onChange={(event) => setStatusToken(event.target.value)} autoComplete="new-password" /></FormField>
          {connection && <div className="md:col-span-2 rounded-lg border bg-muted/30 p-3 text-sm"><p className="font-medium">Webhook URL</p><code className="mt-1 block break-all text-xs">{webhookUrl}</code><p className="mt-2 text-xs text-muted-foreground">Enable pull request and push events on GitHub, or merge request and push events on GitLab. Pushes trigger plans for applications tracking the changed branch. GitLab supports a signing token or legacy secret token.</p></div>}
          <div className="md:col-span-2 border-t pt-4"><label className="flex items-center gap-2 text-sm font-medium"><input type="checkbox" checked={profile.enabled} onChange={(event) => updateProfile("enabled",event.target.checked)} />Deploy isolated previews</label><p className="mt-1 text-xs text-muted-foreground">A preview profile is required. Without it, JustCD reports a review-only plan and cannot change production.</p></div>
          {profile.enabled && <>
            <FormField label="Preview manifest or overlay path" htmlFor="manifest-path" hint="Required for YAML and Kustomize; optional for Helm"><Input id="manifest-path" value={profile.manifestPath} onChange={(event) => updateProfile("manifestPath",event.target.value)} placeholder="deploy/preview" /></FormField>
            <FormField label="Database strategy" htmlFor="database-strategy"><select id="database-strategy" className="h-8 w-full rounded-lg border bg-background px-2 text-sm" value={profile.databaseStrategy} onChange={(event) => updateProfile("databaseStrategy",event.target.value)}>{["none","shared-preview","schema-per-pr","ephemeral","sanitized-snapshot"].map((value)=><option key={value} value={value}>{value}</option>)}</select></FormField>
            <FormField label="Namespace prefix" htmlFor="namespace-prefix"><Input id="namespace-prefix" value={profile.namespacePrefix} onChange={(event) => updateProfile("namespacePrefix",event.target.value)} /></FormField>
            <FormField label="Ingress host suffix" htmlFor="host-suffix" hint="All preview ingress hosts must end in the generated namespace and this suffix."><Input id="host-suffix" value={profile.hostSuffix} onChange={(event) => updateProfile("hostSuffix",event.target.value)} /></FormField>
            <FormField label="Maximum active previews" htmlFor="max-active"><Input id="max-active" type="number" min="1" max="100" value={profile.maxActive} onChange={(event) => updateProfile("maxActive",Number(event.target.value))} /></FormField>
            <FormField label="Maximum lifetime (hours)" htmlFor="max-lifetime"><Input id="max-lifetime" type="number" min="1" max="168" value={profile.maxLifetimeHours} onChange={(event) => updateProfile("maxLifetimeHours",Number(event.target.value))} /></FormField>
            <FormField label="CPU quota" htmlFor="quota-cpu"><Input id="quota-cpu" value={profile.quotaCpu} onChange={(event) => updateProfile("quotaCpu",event.target.value)} /></FormField>
            <FormField label="Memory quota" htmlFor="quota-memory"><Input id="quota-memory" value={profile.quotaMemory} onChange={(event) => updateProfile("quotaMemory",event.target.value)} /></FormField>
            <FormField label="Allowed preview secrets" htmlFor="allowed-secrets" hint="Comma separated names of secrets already provisioned in the preview namespace."><Input id="allowed-secrets" value={profile.allowedSecrets.join(", ")} onChange={(event) => updateProfile("allowedSecrets",event.target.value.split(",").map((item)=>item.trim()).filter(Boolean))} /></FormField>
            <FormField label="Helm values files" htmlFor="helm-values-files" hint="Comma separated, repository-relative preview values files."><Input id="helm-values-files" value={profile.helmValuesFiles.join(", ")} onChange={(event) => updateProfile("helmValuesFiles",event.target.value.split(",").map((item)=>item.trim()).filter(Boolean))} /></FormField>
            <div className="md:col-span-2"><FormField label="Preview Helm values YAML" htmlFor="helm-values-yaml" hint="Supports {{namespace}}, {{number}}, and {{sha}} placeholders. Quote placeholders in YAML strings."><Textarea id="helm-values-yaml" value={profile.helmValuesYaml} onChange={(event) => updateProfile("helmValuesYaml",event.target.value)} className="min-h-32 font-mono" /></FormField></div>
            <label className="md:col-span-2 flex items-center gap-2 text-sm"><input type="checkbox" checked={profile.allowForks} onChange={(event) => updateProfile("allowForks",event.target.checked)} />Allow fork PRs with owner approval and no secrets</label>
          </>}
          <div className="md:col-span-2 flex justify-end"><Button type="submit" loading={saving} loadingText="Saving…" disabled={Boolean(source?.selfHosted && !confirmedSelfHosted)}>Save PR reporting</Button></div>
        </form>
      </Panel>
      </> : <>
      {connection ? <>
      <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground"><span className="font-medium text-foreground">{connection.repository}</span><span aria-hidden="true">·</span><span>{connection.provider === "github" ? "GitHub" : "GitLab"}</span><span aria-hidden="true">·</span><span>{connection.previewProfile.enabled ? "Isolated previews enabled" : "Review-only plans"}</span></div>
      <Panel title="Recent pull requests" description="Each PR/MR status links back to its plan and preview details.">
        <div className="divide-y">{reviews.length===0 ? <EmptyState title="No pull requests received yet" description="New pull requests will appear here after the repository webhook sends an event." /> : reviews.map((review)=><div key={review.id} id={`review-${review.number}`} className="space-y-2 p-5">
          <div className="flex flex-wrap items-center gap-3"><a href={review.sourceUrl} target="_blank" rel="noreferrer" className="font-medium text-primary hover:underline">PR/MR #{review.number} ↗</a><StatusBadge status={review.phase} /><code className="text-xs text-muted-foreground">{review.headSha.slice(0,12)}</code>{review.previewApplicationId && <Link href={`/applications/${review.previewApplicationId}`} className="text-xs text-primary hover:underline">Preview application →</Link>}</div>
          {review.error && <p className="text-xs text-destructive">{review.error}</p>}
          {review.reportError && <p className="text-xs text-destructive">{review.reportError}</p>}
          {review.expiresAt && <p className="text-xs text-muted-foreground">Expires {new Date(review.expiresAt).toLocaleString()}</p>}
          {review.plan && <details className="text-xs"><summary className="cursor-pointer font-medium">Review plan · {review.plan.plan.changes.length} changes {review.plan.status==="review-only"?"· cannot be applied":""}</summary><ul className="mt-2 space-y-1 pl-4">{review.plan.plan.changes.map((change,index)=><li key={`${change.identity.kind}-${change.identity.namespace}-${change.identity.name}-${index}`}>{change.kind}: {change.identity.kind} {change.identity.namespace}/{change.identity.name}</li>)}</ul></details>}
        </div>)}</div>
      </Panel>
      </> : <div className="rounded-xl border border-dashed bg-card">
        <EmptyState title="PR reporting isn’t enabled yet" description={`This application already uses ${source.repository}. Enable reporting to receive review-only plans and commit statuses for its pull requests.${canConfigure ?? ownerAccess ? "" : " A workspace owner can enable reporting."}`} />
      </div>}
      </>}
    </> : null}
  </div>
}
