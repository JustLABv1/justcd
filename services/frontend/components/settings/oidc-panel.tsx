"use client"

import { useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { PlusSignIcon } from "@hugeicons/core-free-icons"
import { Badge } from "@/components/reui/badge"
import { Button } from "@/components/ui/button"
import { AppDialog, DialogCancel, DialogFooter } from "@/components/ui/dialog"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { RowActions } from "@/components/action-menu"
import { ConnectionRow, FormField, SwitchField } from "@/components/ui-kit"
import { WorkspaceIcon } from "@/components/workspace-ui"
import { useToast } from "@/components/toast-provider"
import { api, apiPost } from "@/lib/api"
import type { OIDCProvider, Workspace } from "@/lib/types"
import {
  CopyCode,
  deleteItem,
  FieldError,
  InventoryPanel,
  SectionEmpty,
  useSaving,
  type ActionRunner,
} from "@/components/settings/shared"

function isHttpUrl(value: string) {
  try {
    const url = new URL(value.trim())
    return url.protocol === "https:" || url.protocol === "http:"
  } catch {
    return false
  }
}

export function OIDCPanel({
  providers,
  workspaces,
  busy,
  action,
  onCreated,
  onDeleted,
}: {
  providers: OIDCProvider[]
  workspaces: Workspace[]
  busy: boolean
  action: ActionRunner
  onCreated: (value: OIDCProvider) => void
  onDeleted: (id: string) => void
}) {
  const toast = useToast()
  const [providerDialog, setProviderDialog] = useState<{ editing: OIDCProvider | null } | null>(null)
  const [groupProviderId, setGroupProviderId] = useState<string | null>(null)
  const addButton = (
    <Button type="button" size="sm" onClick={() => setProviderDialog({ editing: null })}>
      <HugeiconsIcon icon={PlusSignIcon} strokeWidth={2} aria-hidden="true" />
      Add OIDC provider
    </Button>
  )
  return (
    <div className="space-y-6">
      <InventoryPanel
        title="Identity providers"
        description="Verified groups from the identity provider can grant workspace roles at each sign-in."
        count={providers.length}
        action={addButton}
      >
        {providers.length ? (
          <ul className="divide-y">
            {providers.map((provider) => (
              <ConnectionRow
                key={provider.id}
                icon={<WorkspaceIcon name="shield" className="size-4" />}
                title={provider.name}
                subtitle={provider.redirectUrl ? `Callback URL: ${provider.redirectUrl}` : undefined}
                badges={<Badge variant={provider.enabled ? "success-light" : "outline"} radius="full">{provider.enabled ? "Enabled" : "Disabled"}</Badge>}
                actions={
                  <RowActions
                    label={provider.name}
                    items={[
                      { label: "Edit", onSelect: () => setProviderDialog({ editing: provider }), disabled: busy },
                      ...(workspaces.length ? [{ label: "Assign group access", onSelect: () => setGroupProviderId(provider.id), disabled: busy }] : []),
                      deleteItem({
                        name: provider.name,
                        description: "Sign-in through this provider will stop. Its identity links, pending logins, group mappings, and granted workspace access will be removed. User accounts and existing sessions remain.",
                        confirmLabel: "Delete provider",
                        endpoint: `/api/v1/admin/oidc-providers/${encodeURIComponent(provider.id)}`,
                        successMessage: `${provider.name} deleted.`,
                        onDeleted: () => onDeleted(provider.id),
                        toast,
                        disabled: busy,
                      }),
                    ]}
                  />
                }
              />
            ))}
          </ul>
        ) : (
          <SectionEmpty
            title="No identity providers yet"
            description="Add a provider to let people sign in with your organization's account."
            action={addButton}
          />
        )}
      </InventoryPanel>
      {providerDialog && (
        <ProviderDialog editing={providerDialog.editing} action={action} onClose={() => setProviderDialog(null)} onSaved={onCreated} />
      )}
      {groupProviderId && (
        <GroupAccessDialog providers={providers} workspaces={workspaces} initialProviderId={groupProviderId} action={action} onClose={() => setGroupProviderId(null)} />
      )}
    </div>
  )
}

function ProviderDialog({ editing, action, onClose, onSaved }: {
  editing: OIDCProvider | null
  action: ActionRunner
  onClose: () => void
  onSaved: (value: OIDCProvider) => void
}) {
  const { saving, track } = useSaving()
  const [name, setName] = useState(editing?.name ?? "")
  const [issuer, setIssuer] = useState(editing?.issuer ?? "")
  const [clientId, setClientId] = useState(editing?.clientId ?? "")
  const [clientSecret, setClientSecret] = useState("")
  const [groupsClaim, setGroupsClaim] = useState(editing?.groupsClaim ?? "groups")
  const [enabled, setEnabled] = useState(editing?.enabled ?? true)
  const [attempted, setAttempted] = useState(false)
  const errors = {
    name: name.trim() ? "" : "Enter a display name.",
    issuer: !issuer.trim() ? "Enter the issuer URL." : isHttpUrl(issuer) ? "" : "Enter a valid URL, for example https://id.example.com.",
    clientId: clientId.trim() ? "" : "Enter the client ID.",
    clientSecret: editing || clientSecret ? "" : "Enter the client secret.",
  }
  const invalid = Object.values(errors).some(Boolean)
  const field = (key: keyof typeof errors) => ({
    "aria-invalid": attempted && !!errors[key],
    "aria-describedby": attempted && errors[key] ? `oidc-${key}-error` : undefined,
  })
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (invalid) {
      setAttempted(true)
      return
    }
    const values = { name, issuer, clientId, clientSecret, groupsClaim, enabled }
    const ok = await track(action(
      () => editing
        ? api<OIDCProvider>(`/api/v1/admin/oidc-providers/${encodeURIComponent(editing.id)}`, { method: "PUT", body: JSON.stringify(values) })
        : apiPost<OIDCProvider>("/api/v1/admin/oidc-providers", values),
      editing ? "OIDC provider updated." : "OIDC provider added. Verify the callback URL in your identity provider.",
      onSaved
    ))
    if (ok) onClose()
  }
  return (
    <AppDialog
      open
      onOpenChange={(open) => { if (!open) onClose() }}
      busy={saving}
      title={editing ? `Edit OIDC provider · ${editing.name}` : "Add OIDC provider"}
      description="Configure organization sign-in and workspace group claims."
    >
      <form className="space-y-5" onSubmit={submit} noValidate>
        <div className="grid gap-3 sm:grid-cols-2">
          <FormField label="Display name" htmlFor="oidc-name">
            <Input id="oidc-name" placeholder="Company SSO" value={name} onChange={(event) => setName(event.target.value)} {...field("name")} />
            {attempted && <FieldError id="oidc-name-error">{errors.name}</FieldError>}
          </FormField>
          <FormField label="Issuer URL" htmlFor="oidc-issuer">
            <Input id="oidc-issuer" type="url" placeholder="https://id.example.com" value={issuer} onChange={(event) => setIssuer(event.target.value)} {...field("issuer")} />
            {attempted && <FieldError id="oidc-issuer-error">{errors.issuer}</FieldError>}
          </FormField>
          <FormField label="Client ID" htmlFor="oidc-client">
            <Input id="oidc-client" value={clientId} onChange={(event) => setClientId(event.target.value)} {...field("clientId")} />
            {attempted && <FieldError id="oidc-clientId-error">{errors.clientId}</FieldError>}
          </FormField>
          <FormField label="Groups claim" htmlFor="oidc-groups" hint="Token claim that lists the user's groups.">
            <Input id="oidc-groups" value={groupsClaim} onChange={(event) => setGroupsClaim(event.target.value)} />
          </FormField>
        </div>
        <FormField label="Client secret" htmlFor="oidc-secret">
          <Input
            id="oidc-secret"
            type="password"
            autoComplete="new-password"
            value={clientSecret}
            onChange={(event) => setClientSecret(event.target.value)}
            placeholder={editing ? "Leave empty to keep the existing secret" : undefined}
            {...field("clientSecret")}
          />
          {attempted && <FieldError id="oidc-clientSecret-error">{errors.clientSecret}</FieldError>}
        </FormField>
        {editing?.redirectUrl ? (
          <div className="space-y-1.5">
            <p className="text-sm font-medium">Callback URL</p>
            <CopyCode value={editing.redirectUrl} label="callback URL" />
            <p className="text-sm text-muted-foreground">Register this URL in your identity provider.</p>
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">
            Callback URL: the JustCD public URL plus <code className="font-mono">/api/v1/auth/oidc/&lt;provider-id&gt;/callback</code>. It is shown here once the provider is created.
          </p>
        )}
        <SwitchField id="oidc-enabled" checked={enabled} onCheckedChange={setEnabled} label="Enabled" description="Allow people to sign in with this provider." />
        <DialogFooter>
          <DialogCancel disabled={saving} />
          <Button type="submit" loading={saving} loadingText={editing ? "Saving…" : "Adding…"}>
            {editing ? "Save changes" : "Add provider"}
          </Button>
        </DialogFooter>
      </form>
    </AppDialog>
  )
}

function GroupAccessDialog({ providers, workspaces, initialProviderId, action, onClose }: {
  providers: OIDCProvider[]
  workspaces: Workspace[]
  initialProviderId: string
  action: ActionRunner
  onClose: () => void
}) {
  const { saving, track } = useSaving()
  const [providerId, setProviderId] = useState(initialProviderId)
  const [workspaceId, setWorkspaceId] = useState(workspaces[0]?.id ?? "")
  const [groupName, setGroupName] = useState("")
  const [groupRole, setGroupRole] = useState("viewer")
  const [attempted, setAttempted] = useState(false)
  const groupError = groupName.trim() ? "" : "Enter the group claim value."
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (groupError) {
      setAttempted(true)
      return
    }
    if (!providerId || !workspaceId) return
    const ok = await track(action(
      () => apiPost(`/api/v1/admin/oidc-providers/${encodeURIComponent(providerId)}/groups`, { group: groupName, workspaceId, role: groupRole }),
      "Group access saved."
    ))
    if (ok) onClose()
  }
  return (
    <AppDialog
      open
      onOpenChange={(open) => { if (!open) onClose() }}
      busy={saving}
      title="Assign group access"
      description="Verified ID-token groups grant the selected workspace role at each sign-in."
    >
      <form className="space-y-5" onSubmit={submit} noValidate>
        <div className="grid gap-3 sm:grid-cols-2">
          <FormField label="Provider" htmlFor="mapping-provider">
            <FormSelect id="mapping-provider" value={providerId} onValueChange={setProviderId} placeholder="Choose provider" items={providers.map((provider) => ({ value: provider.id, label: provider.name }))} />
          </FormField>
          <FormField label="Workspace" htmlFor="mapping-workspace">
            <FormSelect id="mapping-workspace" value={workspaceId} onValueChange={setWorkspaceId} placeholder="Choose workspace" items={workspaces.map((workspace) => ({ value: workspace.id, label: workspace.name }))} />
          </FormField>
          <FormField label="Group claim value" htmlFor="mapping-group">
            <Input id="mapping-group" placeholder="platform-deployers" value={groupName} onChange={(event) => setGroupName(event.target.value)} aria-invalid={attempted && !!groupError} aria-describedby={attempted && groupError ? "mapping-group-error" : undefined} />
            {attempted && <FieldError id="mapping-group-error">{groupError}</FieldError>}
          </FormField>
          <FormField label="Granted role" htmlFor="mapping-role">
            <FormSelect
              id="mapping-role"
              value={groupRole}
              onValueChange={setGroupRole}
              items={[
                { value: "viewer", label: "Viewer" },
                { value: "deployer", label: "Deployer" },
                { value: "owner", label: "Owner" },
              ]}
            />
          </FormField>
        </div>
        <DialogFooter>
          <DialogCancel disabled={saving} />
          <Button type="submit" loading={saving} loadingText="Saving…" disabled={!providerId || !workspaceId}>Assign access</Button>
        </DialogFooter>
      </form>
    </AppDialog>
  )
}
