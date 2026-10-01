"use client"

import Link from "next/link"
import { useParams } from "next/navigation"
import { useCallback, useEffect, useState, type FormEvent } from "react"
import { Checkbox } from "@/components/ui/checkbox"
import { Button } from "@/components/ui/button"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { ConfirmDisclosure } from "@/components/confirm-disclosure"
import { ErrorNotice } from "@/components/workspace-ui"
import { EmptyState, FormField, PageHeading, Panel, StatusBadge } from "@/components/ui-kit"
import { APIError, api } from "@/lib/api"
import type { Application, ListResponse, PlanRecord, Workspace, WorkspaceMember, User } from "@/lib/types"

type PreviewProfile = {
  deploymentMode: "isolated" | "existing"
  confirmShared: boolean
  approvalActors: Record<string, string>
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

type Connection = { repositoryPrId?: string; managedByGit?: boolean; id: string; enabled: boolean; provider: "github" | "gitlab"; apiUrl: string; repository: string; previewProfile: PreviewProfile; statusCredentialId?: string }
type SourceDetails = { repositoryUrl: string; provider: "github" | "gitlab"; apiUrl: string; repository: string; selfHosted: boolean }
type ConnectionResponse = { source: SourceDetails; connection: Connection | null; webhookUrl: string; webhookConfigured?: boolean }
type Review = { scopeNote?: string; id: string; number: number; headSha: string; sourceUrl: string; phase: string; error?: string; reportError?: string; plan?: PlanRecord; previewApplicationId?: string; expiresAt?: string; updatedAt: string; headBranch?: string; adoptedBranchPreview?: boolean; sharedEnvironment?: boolean; branchPreviews?: Application[] }

const defaultProfile: PreviewProfile = {
  deploymentMode: "isolated", confirmShared: false, approvalActors: {}, enabled: false, manifestPath: "", namespacePrefix: "preview", hostSuffix: "preview.example.com", helmValuesYaml: "", helmValuesFiles: [],
  allowedSecrets: [], databaseStrategy: "none", maxActive: 5, maxLifetimeHours: 72, allowForks: false, quotaCpu: "2", quotaMemory: "2Gi",
}

export function PullRequestsWorkspace({ embedded = false, canConfigure }: { embedded?: boolean; canConfigure?: boolean }) {
  const { applicationID } = useParams<{ applicationID: string }>()
  const [application, setApplication] = useState<Application | null>(null)
  const [source, setSource] = useState<SourceDetails | null>(null)
  const [ownerAccess, setOwnerAccess] = useState(false)
  const [connection, setConnection] = useState<Connection | null>(null)
  const [webhookUrl, setWebhookUrl] = useState("")
  const [webhookConfigured, setWebhookConfigured] = useState(false)
  const [reviews, setReviews] = useState<Review[]>([])
  const [apiUrl, setApiUrl] = useState("")
  const [webhookSecret, setWebhookSecret] = useState("")
  const [statusToken, setStatusToken] = useState("")
  const [statusCredentialId, setStatusCredentialId] = useState("")
  const [credentials, setCredentials] = useState<{ id: string; name: string; kind: string }[]>([])
  const [approvalActors, setApprovalActors] = useState<{ key: string; providerId: string; userId: string }[]>([])
  const [members, setMembers] = useState<WorkspaceMember[]>([])
  const [profile, setProfile] = useState<PreviewProfile>(defaultProfile)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [changingState, setChangingState] = useState(false)
  const [showSettings, setShowSettings] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [savedMessage, setSavedMessage] = useState("")

  const load = useCallback(async () => {
    setError(null)
    setSource(null)
    const app = await api<Application>(`/api/v1/applications/${encodeURIComponent(applicationID)}`)
    setApplication(app)
    if (canConfigure !== false) {
      const list = await api<ListResponse<{ id: string; name: string; kind: string }>>(`/api/v1/credentials?workspaceId=${encodeURIComponent(app.workspaceId)}`)
      setCredentials(list.items.filter(item => item.kind === "git-https"))
      const memberList = await api<ListResponse<WorkspaceMember>>(`/api/v1/workspaces/${encodeURIComponent(app.workspaceId)}/members`)
      const session = await api<{ user: User }>("/api/v1/auth/session")
      const selectableMembers = [...memberList.items]
      if (session.user.isAdmin) {
        const users = await api<ListResponse<User>>("/api/v1/admin/users")
        for (const user of users.items.filter(user => user.isAdmin && !user.disabled && !user.deletedAt)) {
          if (!selectableMembers.some(member => member.id === user.id)) selectableMembers.push({ ...user, role: "administrator", managedBySSO: false, editable: false })
        }
      }
      setMembers(selectableMembers)
    }
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
    setConnection(result.connection ? { ...result.connection, enabled: result.connection.enabled ?? true } : null)
    setWebhookUrl(result.webhookUrl)
    setWebhookConfigured(result.webhookConfigured ?? Boolean(result.connection && result.webhookUrl))
    setApiUrl(result.connection?.apiUrl ?? result.source.apiUrl)
    setProfile({ ...defaultProfile, ...result.connection?.previewProfile })
    setStatusCredentialId(result.connection?.statusCredentialId ?? "")
    setApprovalActors(Object.entries(result.connection?.previewProfile.approvalActors ?? {}).map(([providerId, userId]) => ({ key: crypto.randomUUID(), providerId, userId })))
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

  useEffect(() => {
    if (!connection || showSettings) return
    const timer = window.setInterval(() => {
      void api<ListResponse<Review>>(`/api/v1/applications/${encodeURIComponent(applicationID)}/pull-requests`).then(list => setReviews(list.items)).catch(setError)
    }, 10000)
    return () => window.clearInterval(timer)
  }, [applicationID, connection, showSettings])

  function updateProfile<K extends keyof PreviewProfile>(key: K, value: PreviewProfile[K]) {
    setProfile((current) => ({ ...current, [key]: value }))
  }

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setSaving(true); setError(null); setSavedMessage("")
    try {
      const actors: Record<string, string> = {}
      for (const row of approvalActors) {
        const providerID = row.providerId.trim()
        if (!/^[1-9][0-9]*$/.test(providerID) || !row.userId || actors[providerID]) throw new Error("Each approval mapping needs a unique numeric provider account ID and a JustCD user.")
        actors[providerID] = row.userId
      }
      const result = await api<ConnectionResponse>(`/api/v1/applications/${encodeURIComponent(applicationID)}/source-control`, {
        method: "PUT", body: JSON.stringify({ provider: source?.selfHosted ? "gitlab" : "", apiUrl: source?.selfHosted ? apiUrl : "", webhookSecret, statusToken: statusCredentialId ? "" : statusToken, statusCredentialId, previewProfile: { ...profile, approvalActors: actors } }),
      })
      setSource(result.source)
      setConnection(result.connection)
      setWebhookUrl(result.webhookUrl)
      setWebhookConfigured(result.webhookConfigured ?? Boolean(result.connection && result.webhookUrl))
      setWebhookSecret(""); setStatusToken("")
      setSavedMessage("PR reporting saved. JustCD will check for pull requests about every two minutes.")
      setShowSettings(false)
    } catch (cause) { setError(cause) }
    finally { setSaving(false) }
  }

  async function setReportingEnabled(enabled: boolean) {
    setChangingState(true); setError(null); setSavedMessage("")
    try {
      await api<void>(`/api/v1/applications/${encodeURIComponent(applicationID)}/source-control`, { method: "PATCH", body: JSON.stringify({ enabled }) })
      setConnection((current) => current ? { ...current, enabled } : null)
      setSavedMessage(enabled ? "PR reporting enabled." : "PR reporting disabled. Saved settings and review history are preserved.")
    } catch (cause) { setError(cause) }
    finally { setChangingState(false) }
  }

  async function deleteReporting() {
    await api<void>(`/api/v1/applications/${encodeURIComponent(applicationID)}/source-control`, { method: "DELETE" })
    setConnection(null); setReviews([]); setWebhookUrl(""); setWebhookConfigured(false); setWebhookSecret(""); setStatusToken("")
    setShowSettings(false)
    setSavedMessage("PR reporting settings and review history deleted. Remove any optional repository webhook from your Git provider as well.")
  }

  async function removeWebhook() {
    await api<void>(`/api/v1/applications/${encodeURIComponent(applicationID)}/source-control/webhook`, { method: "DELETE" })
    setWebhookConfigured(false)
    setWebhookSecret("")
    setSavedMessage("Webhook removed from JustCD. PR reporting continues by polling. Remove the repository webhook at your Git provider too.")
  }

  const reportingActions = connection?.managedByGit ? <span className="text-sm text-muted-foreground">{connection.repositoryPrId ? "Managed by repository PR discovery settings" : "Managed by justcd.yaml · spec.pullRequests"}</span> : source && (canConfigure ?? ownerAccess) && (showSettings
    ? <Button variant="outline" size="sm" onClick={() => setShowSettings(false)}>Back to reviews</Button>
    : <><Button size="sm" variant={connection ? "outline" : "default"} onClick={() => setShowSettings(true)}>{connection ? "Edit settings" : "Enable PR reporting"}</Button>
      {connection && <Button type="button" size="sm" variant={connection.enabled ? "destructive" : "outline"} className={connection.enabled ? undefined : "border-emerald-300 bg-emerald-50 text-emerald-800 hover:bg-emerald-100 hover:text-emerald-900 dark:border-emerald-800 dark:bg-emerald-950/50 dark:text-emerald-300 dark:hover:bg-emerald-950"} loading={changingState} onClick={() => void setReportingEnabled(!connection.enabled)}>{connection.enabled ? "Disable reporting" : "Enable reporting"}</Button>}</>)

  return <div className="space-y-5">
    {embedded ? <header className="flex flex-wrap items-start justify-between gap-4"><div><h2 className="text-lg font-semibold tracking-tight">Pull requests</h2><p className="mt-1 text-sm text-muted-foreground">{application ? `Review plans and previews for ${application.name}.` : "Review plans and optional previews for this application."}</p></div><div className="flex flex-wrap items-center gap-2">{reportingActions}</div></header> : <PageHeading title="Pull requests" description={application ? `Review plans and previews for ${application.name}.` : "Review plans and optional previews for this application."} actions={<>
        {!embedded && <Link href={`/applications/${applicationID}`}><Button variant="outline" size="sm">Back to application</Button></Link>}
        {reportingActions}
      </>} />}
    {error != null && <ErrorNotice error={error} />}
    {savedMessage && <p role="status" className="rounded-lg border border-emerald-300/50 bg-emerald-50 p-3 text-sm text-emerald-900 dark:bg-emerald-950/30 dark:text-emerald-200">{savedMessage}</p>}
    {loading ? <p role="status" className="text-sm text-muted-foreground">Loading pull requests…</p> : source ? <>
      {showSettings ? <>
      <Panel title="PR reporting settings" description="Report plans for pull requests, including drafts." className="max-w-4xl">
        <form onSubmit={(event) => void save(event)} className="grid gap-4 p-5 md:grid-cols-2">
          <div className="md:col-span-2 rounded-lg border bg-muted/30 p-3 text-sm"><p className="font-medium">{source?.provider === "github" ? "GitHub" : source?.selfHosted ? connection ? "GitLab (self-managed)" : "Custom Git host · GitLab required" : "GitLab"} · {connection?.repository ?? source?.repository}</p><p className="mt-1 break-all font-mono text-sm text-muted-foreground">{source?.repositoryUrl}</p></div>

          {source?.selfHosted && <>
            {!connection && <div className="md:col-span-2 text-xs leading-5 text-muted-foreground">This custom host will connect as GitLab. Verify its API URL.</div>}
            <div className="md:col-span-2"><FormField label="GitLab API URL" htmlFor="api-url" hint="Use the HTTPS API URL for this host, including any installation subpath and /api/v4."><Input id="api-url" required type="url" value={apiUrl} onChange={(event) => setApiUrl(event.target.value)} /></FormField></div>
          </>}

          <div className="md:col-span-2"><FormField label="Provider API credential" htmlFor="provider-credential" hint="HTTPS token with PR read, comment read/write and commit status write access."><FormSelect id="provider-credential" value={statusCredentialId} onValueChange={setStatusCredentialId} emptyOption="Separate API token" items={credentials.map(credential => ({ value: credential.id, label: credential.name }))} /></FormField></div>
          {!statusCredentialId && <div className="md:col-span-2"><FormField label={source?.provider === "github" ? "GitHub API token" : "GitLab API token"} htmlFor="status-token" hint={`Minimum 16 characters. Needs PR/MR read, comments read/write and commit status write access.${connection ? " Leave blank to keep the saved value." : ""}`}><Input id="status-token" type="password" minLength={16} required={!connection || Boolean(connection.statusCredentialId)} value={statusToken} onChange={(event) => setStatusToken(event.target.value)} autoComplete="new-password" /></FormField></div>}
          <details className="md:col-span-2 rounded-lg border p-3"><summary className="cursor-pointer text-sm font-medium">PR comment approvals</summary><div className="mt-4 grid gap-4">          <div className="space-y-3">
            <p className="text-sm text-muted-foreground">Link provider accounts to workspace members. Their current permissions and the plan’s approval policy apply.</p>
            {members.length === 0 && <p className="text-sm text-muted-foreground">Add workspace members to make users available here.</p>}
            {approvalActors.length === 0 && <p className="text-sm text-muted-foreground">No accounts linked. Approvals remain available in JustCD.</p>}
            {approvalActors.map((row, index) => <div key={row.key} className="grid items-end gap-3 sm:grid-cols-[1fr_1fr_auto]">
              <div className="flex flex-col gap-1.5"><label className="text-sm font-medium" htmlFor={`approval-provider-${row.key}`}>{source.provider === "github" ? "GitHub" : "GitLab"} account ID</label><Input id={`approval-provider-${row.key}`} inputMode="numeric" pattern="[1-9][0-9]*" required placeholder="123456" value={row.providerId} onChange={(event) => setApprovalActors(current => current.map(item => item.key === row.key ? { ...item, providerId: event.target.value } : item))} /></div>
              <div className="flex flex-col gap-1.5"><label className="text-sm font-medium" htmlFor={`approval-user-${row.key}`}>JustCD user</label><FormSelect id={`approval-user-${row.key}`} value={row.userId} placeholder="Select workspace member" required onValueChange={(userId) => setApprovalActors(current => current.map(item => item.key === row.key ? { ...item, userId } : item))} items={[
                ...members.filter(member => !member.disabled || member.id === row.userId).map(member => ({ value: member.id, label: `${member.displayName || member.email} · ${member.role}${member.disabled ? " · disabled" : ""}` })),
                ...(row.userId && !members.some(member => member.id === row.userId) ? [{ value: row.userId, label: "Previously linked user · unavailable" }] : []),
              ]} /></div>
              <Button type="button" variant="destructive" size="sm" aria-label={`Remove approval mapping ${index + 1}`} onClick={() => setApprovalActors(current => current.filter(item => item.key !== row.key))}>Remove</Button>
            </div>)}
            <Button type="button" variant="outline" size="sm" onClick={() => setApprovalActors(current => [...current, { key: crypto.randomUUID(), providerId: "", userId: "" }])}>Add account mapping</Button>
          </div>
          </div></details>
          <details className="md:col-span-2 rounded-lg border p-3"><summary className="cursor-pointer text-sm font-medium">Webhook updates <span className="font-normal text-muted-foreground">· optional</span></summary><div className="mt-4 grid gap-4"><p className="text-sm text-muted-foreground">Without a webhook, JustCD checks every two minutes.</p>          <div className="md:col-span-2"><FormField label={source?.provider === "github" ? "GitHub webhook secret (optional)" : "GitLab webhook signing token or secret token (optional)"} htmlFor="webhook-secret" hint={`At least 16 characters when used.${webhookConfigured ? " Leave blank to keep the saved value, or use Remove webhook below." : ""}`}><Input id="webhook-secret" type="password" minLength={16} value={webhookSecret} onChange={(event) => setWebhookSecret(event.target.value)} autoComplete="new-password" /></FormField></div>
          {connection && (webhookConfigured ? <div className="md:col-span-2 rounded-lg border bg-muted/30 p-3 text-sm"><div className="flex flex-wrap items-start justify-between gap-3"><div className="min-w-0"><p className="font-medium">Webhook configured{!connection.enabled && " · reporting disabled"}</p><code className="mt-1 block break-all text-xs">{webhookUrl}</code></div><ConfirmDisclosure trigger="Remove webhook" title="Remove webhook from JustCD?" description="JustCD will stop accepting this webhook and continue checking pull requests by polling. Remove the webhook in your Git provider as well." confirmLabel="Remove webhook" onConfirm={removeWebhook} /></div><p className="mt-2 text-sm text-muted-foreground">Enable pull request and push events on GitHub, or merge request and push events on GitLab. Pushes also trigger plans for applications tracking the changed branch.</p></div> : <p className="md:col-span-2 text-sm text-muted-foreground">No webhook configured. JustCD checks for pull request changes by polling.</p>)}
</div></details>
          <div className="md:col-span-2 border-t pt-4"><FormField label="PR deployment" htmlFor="pr-deployment-mode" hint="Deploy automatically when policy permits; otherwise wait for approval."><FormSelect id="pr-deployment-mode" value={profile.enabled ? profile.deploymentMode : "review-only"} onValueChange={(value) => { updateProfile("enabled", value !== "review-only"); if (value !== "review-only") updateProfile("deploymentMode", value as PreviewProfile["deploymentMode"]); if (value === "existing") updateProfile("allowForks", false) }} items={[{ value: "review-only", label: "Review plans only" }, { value: "isolated", label: "Isolated namespace per PR" }, { value: "existing", label: "Existing application environment" }]} /></FormField></div>
          {profile.enabled && <>
            {profile.deploymentMode === "existing" ? <div className="md:col-span-2 space-y-3 rounded-lg border p-4 text-sm"><p>One PR at a time temporarily replaces this application’s source. Shared credentials and data remain available. On close or merge, review the return plan before resuming reconciliation. Migrations and data changes are not undone. Forks cannot deploy here.</p><label className="flex items-start gap-2"><Checkbox aria-label="Authorize shared environment PR deployments" className="mt-0.5" checked={profile.confirmShared} onCheckedChange={(checked) => updateProfile("confirmShared", checked)} />I authorize PR deployments to change this shared environment and its data.</label></div> : <>
            <FormField label="Preview manifest or overlay path" htmlFor="manifest-path" hint="Required for YAML and Kustomize; optional for Helm"><Input id="manifest-path" value={profile.manifestPath} onChange={(event) => updateProfile("manifestPath",event.target.value)} placeholder="deploy/preview" /></FormField>
            <FormField label="Database strategy" htmlFor="database-strategy"><FormSelect id="database-strategy" value={profile.databaseStrategy} onValueChange={(value) => updateProfile("databaseStrategy", value)} items={["none","shared-preview","schema-per-pr","ephemeral","sanitized-snapshot"].map(value => ({ value, label: value }))} /></FormField>
            <FormField label="Ingress host suffix" htmlFor="host-suffix" hint="All preview ingress hosts must end in the generated namespace and this suffix."><Input id="host-suffix" value={profile.hostSuffix} onChange={(event) => updateProfile("hostSuffix",event.target.value)} /></FormField>
            <details className="md:col-span-2 rounded-lg border p-3"><summary className="cursor-pointer text-sm font-medium">Preview limits and access</summary><div className="mt-4 grid gap-4 md:grid-cols-2">            <FormField label="Namespace prefix" htmlFor="namespace-prefix"><Input id="namespace-prefix" value={profile.namespacePrefix} onChange={(event) => updateProfile("namespacePrefix",event.target.value)} /></FormField>
            <FormField label="Maximum active previews" htmlFor="max-active"><Input id="max-active" type="number" min="1" max="100" value={profile.maxActive} onChange={(event) => updateProfile("maxActive",Number(event.target.value))} /></FormField>
            <FormField label="Maximum lifetime (hours)" htmlFor="max-lifetime"><Input id="max-lifetime" type="number" min="1" max="168" value={profile.maxLifetimeHours} onChange={(event) => updateProfile("maxLifetimeHours",Number(event.target.value))} /></FormField>
            <FormField label="CPU quota" htmlFor="quota-cpu"><Input id="quota-cpu" value={profile.quotaCpu} onChange={(event) => updateProfile("quotaCpu",event.target.value)} /></FormField>
            <FormField label="Memory quota" htmlFor="quota-memory"><Input id="quota-memory" value={profile.quotaMemory} onChange={(event) => updateProfile("quotaMemory",event.target.value)} /></FormField>
            <FormField label="Allowed preview secrets" htmlFor="allowed-secrets" hint="Comma separated names of secrets already provisioned in the preview namespace."><Input id="allowed-secrets" value={profile.allowedSecrets.join(", ")} onChange={(event) => updateProfile("allowedSecrets",event.target.value.split(",").map((item)=>item.trim()).filter(Boolean))} /></FormField>
            <label className="md:col-span-2 flex items-center gap-2 text-sm"><Checkbox aria-label="Allow fork PRs with owner approval and no secrets" checked={profile.allowForks} onCheckedChange={(checked) => updateProfile("allowForks", checked)} />Allow fork PRs with owner approval and no secrets</label>
</div></details>
            <details className="md:col-span-2 rounded-lg border p-3"><summary className="cursor-pointer text-sm font-medium">Helm values <span className="font-normal text-muted-foreground">· optional</span></summary><div className="mt-4 grid gap-4">            <FormField label="Helm values files" htmlFor="helm-values-files" hint="Comma separated, repository-relative preview values files."><Input id="helm-values-files" value={profile.helmValuesFiles.join(", ")} onChange={(event) => updateProfile("helmValuesFiles",event.target.value.split(",").map((item)=>item.trim()).filter(Boolean))} /></FormField>
            <div className="md:col-span-2"><FormField label="Preview Helm values YAML" htmlFor="helm-values-yaml" hint="Supports {{namespace}}, {{number}}, and {{sha}} placeholders. Quote placeholders in YAML strings."><Textarea id="helm-values-yaml" value={profile.helmValuesYaml} onChange={(event) => updateProfile("helmValuesYaml",event.target.value)} className="min-h-32 font-mono" /></FormField></div>
</div></details>
            </>}
          </>}
          <div className="md:col-span-2 flex justify-end"><Button type="submit" loading={saving} loadingText="Saving…">Save PR reporting</Button></div>
          {connection && <div className="md:col-span-2 flex flex-wrap items-center justify-between gap-3 border-t pt-4"><p className="text-sm text-muted-foreground">Deleting also removes stored review history. Remove the webhook from your Git provider separately. Close active previews first.</p><ConfirmDisclosure trigger="Delete PR reporting" title="Delete PR reporting?" description="This permanently removes the webhook connection and its review history. Existing repository webhooks must be removed separately. Close active previews before deleting." confirmLabel="Delete PR reporting" onConfirm={deleteReporting} /></div>}
        </form>
      </Panel>
      </> : <>
      {connection ? <>
      <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground"><span className={`inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 font-medium ${connection.enabled ? "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900 dark:bg-emerald-950/50 dark:text-emerald-300" : "border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-900 dark:bg-rose-950/50 dark:text-rose-300"}`}><span className="size-1.5 rounded-full bg-current" />{connection.enabled ? "Reporting enabled" : "Reporting disabled"}</span><span className="font-medium text-foreground">{connection.repository}</span><span aria-hidden="true">·</span><span>{connection.provider === "github" ? "GitHub" : "GitLab"}</span><span aria-hidden="true">·</span><span>{connection.previewProfile.enabled ? connection.previewProfile.deploymentMode === "existing" ? "Shared environment deployments" : "Isolated previews enabled" : "Review-only plans"}</span></div>
      <Panel title="Recent pull requests" description="Each PR/MR status links back to its plan and preview details.">
        <div className="divide-y">{reviews.length===0 ? <EmptyState title="No pull requests found yet" description="Open pull requests will appear after the next provider check, usually within two minutes." /> : reviews.map((review)=><div key={review.id} id={`review-${review.number}`} className="space-y-2 p-5">
          <div className="flex flex-wrap items-center gap-3"><a href={review.sourceUrl} target="_blank" rel="noreferrer" className="font-medium text-primary hover:underline">PR/MR #{review.number} ↗</a><StatusBadge status={review.phase} /><code className="text-xs text-muted-foreground">{review.headSha.slice(0,12)}</code>{review.previewApplicationId && <Link href={`/applications/${review.previewApplicationId}`} className="text-xs text-primary hover:underline">Preview application →</Link>}</div>
          {review.phase === "adoption_available" && <div className="space-y-3 rounded-lg border bg-muted/20 p-4"><p className="text-sm">An isolated branch preview already exists for {review.headBranch}. Reuse it to keep its resources and avoid creating another environment.</p><div className="flex flex-wrap gap-2">{(canConfigure ?? ownerAccess) && <>{review.branchPreviews?.map(preview => <ConfirmDisclosure key={preview.id} trigger={`Reuse ${preview.name}`} triggerVariant="outline" confirmVariant="default" title="Transfer this preview to the PR?" description="Existing resources remain. JustCD validates the preview against the PR profile and creates a fresh plan. Future commits and reviewed resource cleanup follow the PR; the namespace and untracked resources remain." confirmLabel="Reuse preview" onConfirm={async () => { await api(`/api/v1/applications/${encodeURIComponent(applicationID)}/pull-requests/${review.id}/branch-preview`, { method: "POST", body: JSON.stringify({ applicationId: preview.id }) }); await load() }} />)}<Button size="sm" variant="outline" onClick={() => { void api(`/api/v1/applications/${encodeURIComponent(applicationID)}/pull-requests/${review.id}/branch-preview`, { method: "POST", body: JSON.stringify({ applicationId: "" }) }).then(load).catch(setError) }}>Create a separate PR preview</Button></>}</div></div>}
          {review.adoptedBranchPreview && <p className="text-sm text-muted-foreground">Reused branch preview · cleanup retains its namespace and untracked resources.</p>}
          {review.sharedEnvironment && <p className="text-sm text-muted-foreground">Deploys to the existing application environment · reconciliation remains paused.</p>}
          {review.phase === "approval_required" && review.plan && <p className="text-sm text-muted-foreground">Approve in JustCD or post <code className="break-all">/justcd approve {review.plan.id} {review.plan.plan.digest}</code> in the PR using a mapped account.</p>}
          {review.scopeNote && <p className="text-sm text-muted-foreground">{review.scopeNote}</p>}
          {review.error && <p className="text-xs text-destructive">{review.error}</p>}
          {review.reportError && <p className="text-xs text-destructive">{review.reportError}</p>}
          {review.expiresAt && <p className="text-sm text-muted-foreground">Expires {new Date(review.expiresAt).toLocaleString()}</p>}
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
