"use client"

import { useEffect, useState } from "react"
import Link from "next/link"
import { HugeiconsIcon } from "@hugeicons/react"
import { PlusSignIcon } from "@hugeicons/core-free-icons"
import { Badge } from "@/components/reui/badge"
import { Button } from "@/components/ui/button"
import { AppDialog, DialogCancel, DialogFooter } from "@/components/ui/dialog"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { RowActions } from "@/components/action-menu"
import { ConnectionRow, FormField, InlineLink } from "@/components/ui-kit"
import { WorkspaceIcon } from "@/components/workspace-ui"
import { RepositoryConfigurations } from "@/components/repository-configurations"
import { useToast } from "@/components/toast-provider"
import { api, apiPost } from "@/lib/api"
import type { Credential, GitSource, Workspace } from "@/lib/types"
import {
  credentialKindLabels,
  CopyCode,
  deleteItem,
  FieldError,
  FieldsSkeleton,
  InventoryPanel,
  LoadErrorNotice,
  SectionEmpty,
  useSaving,
  type ActionRunner,
} from "@/components/settings/shared"

type WebhookInfo = { configured: boolean; webhookUrl: string; managedByOwner?: boolean }
type TestResult = { status: "ok"; commit: string; testedAt: string } | { status: "failed" }

export function GitSourcePanel({
  workspace,
  credentials,
  sources,
  busy,
  action,
  onUpdated,
  onDeleted,
}: {
  workspace: Workspace
  credentials: Credential[]
  sources: GitSource[]
  busy: boolean
  action: ActionRunner
  onUpdated: (value: GitSource) => void
  onDeleted: (id: string) => void
}) {
  const toast = useToast()
  const isOwner = workspace.role === "owner"
  const [editing, setEditing] = useState<GitSource | null>(null)
  const [webhookSource, setWebhookSource] = useState<GitSource | null>(null)
  const [testingSourceId, setTestingSourceId] = useState("")
  const [testResults, setTestResults] = useState<Record<string, TestResult>>({})

  async function test(source: GitSource) {
    setTestingSourceId(source.id)
    try {
      const ok = await action(
        () => apiPost<{ status: string; commit: string }>(`/api/v1/git-sources/${encodeURIComponent(source.id)}/test?workspaceId=${encodeURIComponent(workspace.id)}`),
        (result) => `${source.name}: HEAD ${result.commit.slice(0, 12)} reachable`,
        (result) => setTestResults((current) => ({ ...current, [source.id]: { status: "ok", commit: result.commit, testedAt: new Date().toISOString() } }))
      )
      if (!ok) setTestResults((current) => ({ ...current, [source.id]: { status: "failed" } }))
    } finally {
      setTestingSourceId("")
    }
  }

  return (
    <div className="space-y-6">
      <InventoryPanel
        title="Connected repositories"
        description={isOwner ? undefined : "Only workspace owners can change Git sources."}
        count={sources.length}
      >
        {sources.length ? (
          <ul className="divide-y">
            {sources.map((source) => {
              const result = testResults[source.id]
              const own = !source.shared && source.workspaceId === workspace.id
              return (
                <ConnectionRow
                  key={source.id}
                  icon={<WorkspaceIcon name="branch" className="size-4" />}
                  title={source.name}
                  subtitle={source.repositoryUrl}
                  badges={
                    <>
                      {source.shared
                        ? <Badge variant="info-light" radius="full">Shared by {source.ownerWorkspaceName ?? "another workspace"}</Badge>
                        : <Badge variant="outline" radius="full">Private to this workspace</Badge>}
                      <Badge variant="outline" radius="full">{source.credentialId ? "Credential configured" : "No credential (public repository)"}</Badge>
                      {result?.status === "ok" && <Badge variant="success-light" radius="full">Reachable · HEAD {result.commit.slice(0, 8)}</Badge>}
                      {result?.status === "failed" && <Badge variant="destructive-light" radius="full">Last test failed</Badge>}
                    </>
                  }
                  meta={result?.status === "ok" ? `Last tested ${new Date(result.testedAt).toLocaleString()}` : result?.status === "failed" ? undefined : "Not tested yet in this session"}
                  actions={
                    <RowActions
                      label={source.name}
                      primary={
                        <Button
                          size="sm"
                          variant="outline"
                          type="button"
                          aria-label={`Test ${source.name}`}
                          loading={testingSourceId === source.id}
                          loadingText="Testing…"
                          disabled={busy}
                          onClick={() => void test(source)}
                        >
                          Test
                        </Button>
                      }
                      items={[
                        ...(isOwner && own ? [{ label: "Edit", onSelect: () => setEditing(source), disabled: busy }] : []),
                        ...(isOwner && !source.shared ? [{ label: "Push webhook", onSelect: () => setWebhookSource(source), disabled: busy }] : []),
                        ...(isOwner && own
                          ? [deleteItem({
                              name: source.name,
                              description: "This removes the Git source from JustCD. The repository itself is untouched. Applications, repository configurations, and active shares must be removed first.",
                              confirmLabel: "Delete Git source",
                              endpoint: `/api/v1/git-sources/${encodeURIComponent(source.id)}`,
                              successMessage: `${source.name} deleted.`,
                              onDeleted: () => {
                                if (editing?.id === source.id) setEditing(null)
                                if (webhookSource?.id === source.id) setWebhookSource(null)
                                onDeleted(source.id)
                              },
                              toast,
                              disabled: busy,
                            })]
                          : []),
                      ]}
                    />
                  }
                />
              )
            })}
          </ul>
        ) : (
          <SectionEmpty
            title="No Git sources connected yet"
            description="Connect a repository to deploy from, or reuse one that another workspace shared with you."
            action={isOwner ? (
              <Button nativeButton={false} render={<Link href={`/workspaces/${workspace.id}/git-sources/new`} />}>
                <HugeiconsIcon icon={PlusSignIcon} strokeWidth={2} aria-hidden="true" />
                Connect Git source
              </Button>
            ) : undefined}
            hint="Ask a workspace owner to connect a Git source."
          />
        )}
      </InventoryPanel>
      <RepositoryConfigurations key={workspace.id} workspace={workspace} sources={sources} />
      {webhookSource && <PushWebhookDialog source={webhookSource} workspace={workspace} action={action} onClose={() => setWebhookSource(null)} />}
      {editing && <EditGitSourceDialog source={editing} workspace={workspace} credentials={credentials} action={action} onClose={() => setEditing(null)} onSaved={onUpdated} />}
    </div>
  )
}

function PushWebhookDialog({ source, workspace, action, onClose }: {
  source: GitSource
  workspace: Workspace
  action: ActionRunner
  onClose: () => void
}) {
  const { saving, track } = useSaving()
  const [info, setInfo] = useState<WebhookInfo | null>(null)
  const [loadError, setLoadError] = useState<unknown | null>(null)
  const [tick, setTick] = useState(0)
  const [secret, setSecret] = useState("")
  const [attempted, setAttempted] = useState(false)
  useEffect(() => {
    let active = true
    api<WebhookInfo>(`/api/v1/git-sources/${encodeURIComponent(source.id)}/push-webhook?workspaceId=${encodeURIComponent(workspace.id)}`)
      .then((result) => { if (active) setInfo(result) })
      .catch((cause) => { if (active) setLoadError(cause) })
    return () => { active = false }
  }, [source.id, workspace.id, tick])
  const secretError = secret.length >= 16 ? "" : "Use at least 16 characters."
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (secretError) {
      setAttempted(true)
      return
    }
    const ok = await track(action(
      () => api<WebhookInfo>(`/api/v1/git-sources/${encodeURIComponent(source.id)}/push-webhook`, { method: "PUT", body: JSON.stringify({ secret }) }),
      "Push webhook configured.",
      (value) => setInfo(value)
    ))
    if (ok) onClose()
  }
  return (
    <AppDialog
      open
      onOpenChange={(open) => { if (!open) onClose() }}
      busy={saving}
      title={`Push webhook · ${source.name}`}
      description="Configure push events for this Git source. An existing pull request webhook already accepts pushes too."
    >
      <form className="space-y-5" onSubmit={submit} noValidate>
        {loadError != null
          ? <LoadErrorNotice error={loadError} title="Could not load webhook settings" onRetry={() => { setLoadError(null); setTick((value) => value + 1) }} />
          : !info
            ? <FieldsSkeleton label="Loading webhook settings" />
            : (
              <div className="space-y-3 rounded-lg border bg-muted/30 p-3 text-sm">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium">Status</span>
                  <Badge variant={info.configured ? "success-light" : "outline"} radius="full">{info.configured ? "Configured" : "Not configured"}</Badge>
                </div>
                <CopyCode value={info.webhookUrl} label="webhook URL" />
                <p className="text-muted-foreground">
                  For GitHub or GitLab, enable push events and use this URL with the secret below. For other senders, POST JSON with <code className="font-mono">ref</code> and <code className="font-mono">after</code> (commit SHA), sign the raw body with HMAC-SHA256 in <code className="font-mono">X-JustCD-Signature-256</code>, and send a unique <code className="font-mono">X-JustCD-Delivery</code> ID.
                </p>
              </div>
            )}
        <FormField label={info?.configured ? "New webhook secret" : "Webhook secret"} htmlFor="generic-push-secret" hint={info?.configured ? "Replaces the current secret. At least 16 characters; never shown again." : "At least 16 characters. The value is never shown again."}>
          <Input id="generic-push-secret" type="password" value={secret} onChange={(event) => setSecret(event.target.value)} autoComplete="new-password" aria-invalid={attempted && !!secretError} aria-describedby={attempted && secretError ? "generic-push-secret-error" : undefined} />
          {attempted && <FieldError id="generic-push-secret-error">{secretError}</FieldError>}
        </FormField>
        <DialogFooter>
          <DialogCancel disabled={saving} />
          <Button type="submit" loading={saving} loadingText="Saving…" disabled={!info}>Save webhook</Button>
        </DialogFooter>
      </form>
    </AppDialog>
  )
}

function EditGitSourceDialog({ source, workspace, credentials, action, onClose, onSaved }: {
  source: GitSource
  workspace: Workspace
  credentials: Credential[]
  action: ActionRunner
  onClose: () => void
  onSaved: (value: GitSource) => void
}) {
  const { saving, track } = useSaving()
  const [name, setName] = useState(source.name)
  const [repositoryUrl, setRepositoryUrl] = useState(source.repositoryUrl)
  const [credentialId, setCredentialId] = useState(source.credentialId ?? "")
  const [attempted, setAttempted] = useState(false)
  const nameError = name.trim() ? "" : "Enter a source name."
  const urlError = repositoryUrl.trim() ? "" : "Enter the repository URL."
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (nameError || urlError) {
      setAttempted(true)
      return
    }
    const ok = await track(action(
      () => api<GitSource>(`/api/v1/git-sources/${encodeURIComponent(source.id)}`, {
        method: "PUT",
        body: JSON.stringify({ name, repositoryUrl, credentialId: credentialId || null }),
      }),
      "Git source updated.",
      onSaved
    ))
    if (ok) onClose()
  }
  return (
    <AppDialog
      open
      onOpenChange={(open) => { if (!open) onClose() }}
      busy={saving}
      title={`Edit Git source · ${source.name}`}
      description="Update this repository and the credential used to access it."
    >
      <form className="space-y-5" onSubmit={submit} noValidate>
        <div className="grid gap-3 sm:grid-cols-2">
          <FormField label="Source name" htmlFor="source-name">
            <Input id="source-name" placeholder="platform-config" value={name} onChange={(event) => setName(event.target.value)} aria-invalid={attempted && !!nameError} aria-describedby={attempted && nameError ? "source-name-error" : undefined} />
            {attempted && <FieldError id="source-name-error">{nameError}</FieldError>}
          </FormField>
          <FormField label="Repository URL" htmlFor="source-url">
            <Input id="source-url" placeholder="https://github.com/org/repo.git" value={repositoryUrl} onChange={(event) => setRepositoryUrl(event.target.value)} aria-invalid={attempted && !!urlError} aria-describedby={attempted && urlError ? "source-url-error" : undefined} />
            {attempted && <FieldError id="source-url-error">{urlError}</FieldError>}
          </FormField>
        </div>
        <FormField label="Git credential" htmlFor="source-credential">
          <FormSelect
            id="source-credential"
            value={credentialId}
            onValueChange={setCredentialId}
            emptyOption="No credential (public repository)"
            items={credentials.map((credential) => ({ value: credential.id, label: `${credential.name} · ${credentialKindLabels[credential.kind]}` }))}
          />
        </FormField>
        {!credentials.length && <InlineLink href={`/workspaces/${workspace.id}/connections/credentials`}>Add a credential for a private repository</InlineLink>}
        <DialogFooter>
          <DialogCancel disabled={saving} />
          <Button type="submit" loading={saving} loadingText="Saving…" disabled={workspace.role !== "owner"}>Save changes</Button>
        </DialogFooter>
      </form>
    </AppDialog>
  )
}
