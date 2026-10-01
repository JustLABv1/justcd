"use client"

import { useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { PlusSignIcon, SecurityLockIcon } from "@hugeicons/core-free-icons"
import { Badge } from "@/components/reui/badge"
import { Button } from "@/components/ui/button"
import { AppDialog, DialogCancel, DialogFooter } from "@/components/ui/dialog"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { RowActions } from "@/components/action-menu"
import { ConnectionRow, FormField, Panel } from "@/components/ui-kit"
import { WorkspaceIcon } from "@/components/workspace-ui"
import { useToast } from "@/components/toast-provider"
import { api, apiPost } from "@/lib/api"
import type { Credential, User, Workspace } from "@/lib/types"
import {
  credentialKindLabels,
  deleteItem,
  InventoryPanel,
  monoTextareaClass,
  SectionEmpty,
  useSaving,
  type ActionRunner,
} from "@/components/settings/shared"

const secretFields: Record<Credential["kind"], { first: string; firstKey: string; second?: string; secondKey?: string }> = {
  "git-ssh": { first: "Private key", firstKey: "privateKey", second: "Pinned host keys", secondKey: "knownHosts" },
  "git-https": { first: "Access token", firstKey: "token" },
  kubeconfig: { first: "Static kubeconfig content", firstKey: "content" },
  "kubernetes-token": { first: "Bearer token", firstKey: "token" },
}

export function CredentialPanel({
  workspace,
  user,
  credentials,
  busy,
  action,
  createOpen,
  onCreateOpenChange,
  onCreated,
  onUpdated,
  onDeleted,
}: {
  workspace: Workspace
  user: User | null
  credentials: Credential[]
  busy: boolean
  action: ActionRunner
  createOpen: boolean
  onCreateOpenChange: (open: boolean) => void
  onCreated: (value: Credential) => void
  onUpdated: (value: Credential) => void
  onDeleted: (id: string) => void
}) {
  const toast = useToast()
  const [editing, setEditing] = useState<Credential | null>(null)
  const [now] = useState(() => Date.now())
  const isAdmin = user?.isAdmin ?? false
  const isOwner = workspace.role === "owner"
  const canChange = (credential: Credential) => (credential.workspaceId ? isOwner : isAdmin)
  return (
    <div className="space-y-6">
      <InventoryPanel
        title="Available credentials"
        description={isOwner ? "Encrypted secrets used to reach Git repositories and Kubernetes clusters." : "Only workspace owners can add or change credentials."}
        count={credentials.length}
      >
        {credentials.length ? (
          <ul className="divide-y">
            {credentials.map((credential) => {
              const expired = credential.expiresAt ? new Date(credential.expiresAt).getTime() < now : false
              return (
                <ConnectionRow
                  key={credential.id}
                  icon={<WorkspaceIcon name={credential.kind.startsWith("git") ? "branch" : "server"} className="size-4" />}
                  title={credential.name}
                  subtitle={credentialKindLabels[credential.kind]}
                  badges={
                    <>
                      {!credential.workspaceId && <Badge variant="outline" radius="full">Instance-wide</Badge>}
                      {credential.expiresAt && (
                        <Badge variant={expired ? "destructive-light" : "outline"} radius="full">
                          {expired ? "Expired" : "Expires"} {new Date(credential.expiresAt).toLocaleDateString()}
                        </Badge>
                      )}
                    </>
                  }
                  actions={
                    canChange(credential) ? (
                      <RowActions
                        label={credential.name}
                        items={[
                          { label: "Edit", onSelect: () => setEditing(credential), disabled: busy },
                          deleteItem({
                            name: credential.name,
                            description: "This permanently deletes the stored credential. Anything that uses it must be switched to another credential first.",
                            confirmLabel: "Delete credential",
                            endpoint: `/api/v1/credentials/${encodeURIComponent(credential.id)}`,
                            successMessage: `${credential.name} deleted.`,
                            onDeleted: () => onDeleted(credential.id),
                            toast,
                            disabled: busy,
                          }),
                        ]}
                      />
                    ) : undefined
                  }
                />
              )
            })}
          </ul>
        ) : (
          <SectionEmpty
            title="No credentials yet"
            description="Add a credential to connect a private repository or a Kubernetes cluster. Secret values are never shown after saving."
            action={isOwner ? (
              <Button type="button" onClick={() => onCreateOpenChange(true)}>
                <HugeiconsIcon icon={PlusSignIcon} strokeWidth={2} aria-hidden="true" />
                Add credential
              </Button>
            ) : undefined}
            hint="Ask a workspace owner to add a credential."
          />
        )}
      </InventoryPanel>
      <Panel>
        <div className="flex flex-col justify-between gap-3 px-5 py-4 sm:flex-row sm:items-center">
          <div className="min-w-0">
            <h2 className="text-sm font-semibold">Secrets stay on the server</h2>
            <p className="mt-1 max-w-3xl text-sm leading-5 text-muted-foreground">
              Secrets are encrypted before they are stored and are never sent back to the browser. SSH credentials need pinned host keys, and kubeconfig credentials cannot run external commands or read token files.
            </p>
            <p className="mt-1 text-sm text-muted-foreground">
              Operators: encryption uses <code className="font-mono">JUSTCD_ENCRYPTION_KEY</code>; host keys use the <code className="font-mono">known_hosts</code> format.
            </p>
          </div>
          <Badge variant="success-light" radius="full" size="lg">
            <HugeiconsIcon icon={SecurityLockIcon} strokeWidth={2} aria-hidden="true" />
            Encrypted
          </Badge>
        </div>
      </Panel>
      {createOpen && (
        <CredentialDialog
          workspace={workspace}
          isAdmin={isAdmin}
          action={action}
          onClose={() => onCreateOpenChange(false)}
          onSaved={onCreated}
        />
      )}
      {editing && (
        <CredentialDialog
          workspace={workspace}
          isAdmin={isAdmin}
          action={action}
          editing={editing}
          onClose={() => setEditing(null)}
          onSaved={onUpdated}
        />
      )}
    </div>
  )
}

function CredentialDialog({ workspace, isAdmin, action, editing, onClose, onSaved }: {
  workspace: Workspace
  isAdmin: boolean
  action: ActionRunner
  editing?: Credential
  onClose: () => void
  onSaved: (value: Credential) => void
}) {
  const { saving, track } = useSaving()
  const [name, setName] = useState(editing?.name ?? "")
  const [kind, setKind] = useState<Credential["kind"]>(editing?.kind ?? "kubernetes-token")
  const [secretOne, setSecretOne] = useState("")
  const [secretTwo, setSecretTwo] = useState("")
  const [username, setUsername] = useState(editing?.username ?? "")
  const fields = secretFields[kind]
  const allowed = editing ? (editing.workspaceId ? workspace.role === "owner" : isAdmin) : workspace.role === "owner"
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const ok = await track(action(
      async () => {
        const secret = {
          [fields.firstKey]: secretOne,
          ...(fields.secondKey ? { [fields.secondKey]: secretTwo } : {}),
          ...(kind === "git-https" ? { username } : {}),
        }
        return editing
          ? await api<Credential>(`/api/v1/credentials/${encodeURIComponent(editing.id)}`, {
              method: "PUT",
              body: JSON.stringify({
                name,
                ...(kind === "git-https" ? { username } : {}),
                ...(secretOne ? { secret } : {}),
              }),
            })
          : await apiPost<Credential>("/api/v1/credentials", { workspaceId: workspace.id, name, kind, secret })
      },
      editing ? "Credential updated. The stored secret was kept unless you entered a replacement." : "Encrypted credential added.",
      onSaved
    ))
    if (ok) onClose()
  }
  return (
    <AppDialog
      open
      onOpenChange={(open) => { if (!open) onClose() }}
      busy={saving}
      title={editing ? "Edit credential" : "Add credential"}
      description="Credentials are encrypted and their secret values are never shown again."
    >
      <form className="space-y-5" onSubmit={submit}>
        <div className="grid gap-3 sm:grid-cols-2">
          <FormField label="Name" htmlFor="credential-name">
            <Input id="credential-name" placeholder="production deploy token" value={name} onChange={(event) => setName(event.target.value)} required maxLength={100} />
          </FormField>
          <FormField label="Credential type" htmlFor="credential-kind" hint={editing ? "The type cannot be changed after creation." : undefined}>
            <FormSelect
              id="credential-kind"
              value={kind}
              onValueChange={(value) => setKind(value as Credential["kind"])}
              disabled={!!editing}
              items={Object.entries(credentialKindLabels).map(([value, label]) => ({ value, label }))}
            />
          </FormField>
        </div>
        {kind === "git-https" && (
          <FormField label="Git username" htmlFor="git-username" hint="Use the username required by your Git provider; the token is sent as the password.">
            <Input id="git-username" value={username} onChange={(event) => setUsername(event.target.value)} required />
          </FormField>
        )}
        <FormField
          label={fields.first}
          htmlFor="credential-secret"
          hint={editing ? "Leave blank to keep the stored secret. To rotate it, enter all secret fields again." : kind === "kubeconfig" ? "Exec plugins, auth-provider plugins, and token files are rejected." : undefined}
        >
          <Textarea id="credential-secret" className={monoTextareaClass} value={secretOne} onChange={(event) => setSecretOne(event.target.value)} required={!editing} />
        </FormField>
        {fields.second && (
          <FormField label={fields.second} htmlFor="credential-secret-two" hint="Paste the server's host keys (known_hosts format). Unknown hosts are rejected.">
            <Textarea id="credential-secret-two" className={monoTextareaClass} value={secretTwo} onChange={(event) => setSecretTwo(event.target.value)} required={!editing || !!secretOne} />
          </FormField>
        )}
        <DialogFooter>
          <DialogCancel disabled={saving} />
          <Button type="submit" loading={saving} loadingText={editing ? "Saving…" : "Adding…"} disabled={!allowed}>
            {editing ? "Save changes" : "Add credential"}
          </Button>
        </DialogFooter>
      </form>
    </AppDialog>
  )
}
