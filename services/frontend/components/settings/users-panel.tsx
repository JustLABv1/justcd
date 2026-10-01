"use client"

import { useCallback, useEffect, useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { PlusSignIcon } from "@hugeicons/core-free-icons"
import { Badge } from "@/components/reui/badge"
import { Button } from "@/components/ui/button"
import { AppDialog, DialogCancel, DialogFooter } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { RowActions, type ActionItem } from "@/components/action-menu"
import { ConnectionRow, FormField, SwitchField } from "@/components/ui-kit"
import { useToast } from "@/components/toast-provider"
import { api, apiPost } from "@/lib/api"
import type { ListResponse, User } from "@/lib/types"
import {
  FieldError,
  InventoryPanel,
  LoadErrorNotice,
  SectionEmpty,
  SectionSkeleton,
  useSaving,
  type ActionRunner,
} from "@/components/settings/shared"

const passwordHint = "At least 12 characters."

export function PlatformUsersPanel({
  currentUserId,
  busy,
  action,
}: {
  currentUserId: string
  busy: boolean
  action: ActionRunner
}) {
  const toast = useToast()
  const [users, setUsers] = useState<User[]>([])
  const [createOpen, setCreateOpen] = useState(false)
  const [editing, setEditing] = useState<User | null>(null)
  const [mutatingUserId, setMutatingUserId] = useState<string | null>(null)
  const [usersLoading, setUsersLoading] = useState(true)
  const [usersError, setUsersError] = useState<unknown | null>(null)
  const loadUsers = useCallback(async () => {
    setUsersLoading(true)
    setUsersError(null)
    try {
      const result = await api<ListResponse<User>>("/api/v1/admin/users")
      setUsers(result.items)
    } catch (cause) {
      setUsersError(cause)
    } finally {
      setUsersLoading(false)
    }
  }, [])
  useEffect(() => {
    let active = true
    api<ListResponse<User>>("/api/v1/admin/users")
      .then((result) => { if (active) setUsers(result.items) })
      .catch((cause) => { if (active) setUsersError(cause) })
      .finally(() => { if (active) setUsersLoading(false) })
    return () => { active = false }
  }, [])

  function replaceUser(updated: User) {
    setUsers((items) => items.map((item) => (item.id === updated.id ? updated : item)))
  }

  async function toggleLock(user: User) {
    await action(
      () => api<User>(`/api/v1/admin/users/${encodeURIComponent(user.id)}`, {
        method: "PUT",
        body: JSON.stringify({ email: user.email, displayName: user.displayName, isAdmin: user.isAdmin, disabled: !user.disabled }),
      }),
      user.disabled ? "User unlocked." : "User locked. Active sessions were revoked.",
      replaceUser
    )
  }

  async function deleteUser(user: User) {
    setMutatingUserId(user.id)
    try {
      await api<void>(`/api/v1/admin/users/${encodeURIComponent(user.id)}`, { method: "DELETE" })
      const deletedAt = new Date().toISOString()
      setUsers((items) => items.map((item) => item.id === user.id
        ? { ...item, email: `deleted+${item.id}@deleted.justcd.invalid`, displayName: "Deleted user", isAdmin: false, disabled: true, deletedAt }
        : item))
      if (editing?.id === user.id) setEditing(null)
      toast.success("User deleted. Account details were anonymized and access was revoked.")
    } finally {
      setMutatingUserId(null)
    }
  }

  const createButton = (
    <Button type="button" size="sm" onClick={() => setCreateOpen(true)}>
      <HugeiconsIcon icon={PlusSignIcon} strokeWidth={2} aria-hidden="true" />
      Create local user
    </Button>
  )
  if (usersLoading) return <SectionSkeleton label="Loading platform users" />
  if (usersError != null) return <LoadErrorNotice error={usersError} title="Could not load platform users" onRetry={() => void loadUsers()} />
  return (
    <div className="space-y-6">
      <InventoryPanel
        title="Platform users"
        description="Accounts that can sign in to this JustCD instance."
        count={users.length}
        action={createButton}
      >
        {users.length ? (
          <ul className="divide-y">
            {users.map((user) => {
              const deleted = Boolean(user.deletedAt)
              const self = user.id === currentUserId
              const rowBusy = busy || mutatingUserId !== null
              const label = user.displayName || user.email
              const items: ActionItem[] = [
                { label: "Edit", onSelect: () => setEditing(user), disabled: rowBusy },
                self
                  ? { label: "Lock (not available for your own account)", disabled: true }
                  : { label: user.disabled ? "Unlock" : "Lock", onSelect: () => void toggleLock(user), disabled: rowBusy },
                ...(self ? [] : [{
                  label: "Delete",
                  destructive: true,
                  disabled: rowBusy,
                  confirm: {
                    title: `Delete ${label}?`,
                    description: "This removes the account's workspace and SSO access, revokes sessions, and anonymizes its identity. Plan, approval, and audit history remains attached to a deleted user record. Transfer ownership first if this user is the last active owner of a workspace.",
                    confirmLabel: "Delete user",
                    onConfirm: () => deleteUser(user),
                  },
                } satisfies ActionItem]),
              ]
              return (
                <ConnectionRow
                  key={user.id}
                  icon={<span className="text-xs font-semibold uppercase">{label.slice(0, 2)}</span>}
                  title={label}
                  subtitle={user.email}
                  badges={
                    <>
                      <Badge variant="outline" radius="full">{user.isAdmin ? "Instance admin" : "User"}</Badge>
                      {deleted ? <Badge variant="destructive-light" radius="full">Deleted</Badge> : user.disabled ? <Badge variant="warning-light" radius="full">Locked</Badge> : null}
                      {self && !deleted && <Badge variant="info-light" radius="full">You</Badge>}
                    </>
                  }
                  actions={deleted ? undefined : <RowActions label={label} items={items} />}
                />
              )
            })}
          </ul>
        ) : (
          <SectionEmpty title="No platform users yet" description="Create the first local account to let someone sign in with an email address and password." action={createButton} />
        )}
      </InventoryPanel>
      {createOpen && (
        <CreateUserDialog
          action={action}
          onClose={() => setCreateOpen(false)}
          onCreated={(user) => setUsers((items) => [...items, user].sort((a, b) => a.email.localeCompare(b.email)))}
        />
      )}
      {editing && (
        <EditUserDialog user={editing} self={editing.id === currentUserId} action={action} onClose={() => setEditing(null)} onSaved={replaceUser} />
      )}
    </div>
  )
}

function CreateUserDialog({ action, onClose, onCreated }: { action: ActionRunner; onClose: () => void; onCreated: (user: User) => void }) {
  const { saving, track } = useSaving()
  const [email, setEmail] = useState("")
  const [displayName, setDisplayName] = useState("")
  const [password, setPassword] = useState("")
  const [isAdmin, setIsAdmin] = useState(false)
  const [attempted, setAttempted] = useState(false)
  const emailError = email.trim() ? "" : "Enter an email address."
  const passwordError = password.length >= 12 ? "" : "Use at least 12 characters."
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (emailError || passwordError) {
      setAttempted(true)
      return
    }
    const ok = await track(action(
      () => apiPost<User>("/api/v1/admin/users", { email, displayName, password, isAdmin }),
      "Local user created.",
      onCreated
    ))
    if (ok) onClose()
  }
  return (
    <AppDialog
      open
      onOpenChange={(open) => { if (!open) onClose() }}
      busy={saving}
      title="Create local user"
      description="Add a person who signs in directly with an email address and password."
    >
      <form className="space-y-5" onSubmit={submit} noValidate>
        <div className="grid gap-3 sm:grid-cols-2">
          <FormField label="Email" htmlFor="user-email">
            <Input id="user-email" type="email" value={email} onChange={(event) => setEmail(event.target.value)} aria-invalid={attempted && !!emailError} aria-describedby={attempted && emailError ? "user-email-error" : undefined} />
            {attempted && <FieldError id="user-email-error">{emailError}</FieldError>}
          </FormField>
          <FormField label="Display name" htmlFor="user-display">
            <Input id="user-display" value={displayName} onChange={(event) => setDisplayName(event.target.value)} />
          </FormField>
        </div>
        <FormField label="Password" htmlFor="user-password" hint={`${passwordHint} Share the initial password securely with the account owner.`}>
          <Input id="user-password" type="password" autoComplete="new-password" value={password} onChange={(event) => setPassword(event.target.value)} aria-invalid={attempted && !!passwordError} aria-describedby={attempted && passwordError ? "user-password-error" : undefined} />
          {attempted && <FieldError id="user-password-error">{passwordError}</FieldError>}
        </FormField>
        <SwitchField id="user-is-admin" checked={isAdmin} onCheckedChange={setIsAdmin} label="Instance administrator" description="Can manage sign-in providers and all platform users." />
        <DialogFooter>
          <DialogCancel disabled={saving} />
          <Button type="submit" loading={saving} loadingText="Creating…">Create user</Button>
        </DialogFooter>
      </form>
    </AppDialog>
  )
}

function EditUserDialog({ user, self, action, onClose, onSaved }: { user: User; self: boolean; action: ActionRunner; onClose: () => void; onSaved: (user: User) => void }) {
  const { saving, track } = useSaving()
  const [email, setEmail] = useState(user.email)
  const [displayName, setDisplayName] = useState(user.displayName)
  const [password, setPassword] = useState("")
  const [isAdmin, setIsAdmin] = useState(user.isAdmin)
  const [attempted, setAttempted] = useState(false)
  const emailError = email.trim() ? "" : "Enter an email address."
  const passwordError = !password || password.length >= 12 ? "" : "Use at least 12 characters."
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (emailError || passwordError) {
      setAttempted(true)
      return
    }
    const ok = await track(action(
      () => api<User>(`/api/v1/admin/users/${encodeURIComponent(user.id)}`, {
        method: "PUT",
        body: JSON.stringify({ email, displayName, password, isAdmin: self ? user.isAdmin : isAdmin, disabled: user.disabled }),
      }),
      "Platform user updated.",
      onSaved
    ))
    if (ok) onClose()
  }
  return (
    <AppDialog
      open
      onOpenChange={(open) => { if (!open) onClose() }}
      busy={saving}
      title={`Edit user · ${user.displayName || user.email}`}
      description="Update this account's details or reset its password."
    >
      <form className="space-y-5" onSubmit={submit} noValidate>
        <div className="grid gap-3 sm:grid-cols-2">
          <FormField label="Email" htmlFor="edit-user-email">
            <Input id="edit-user-email" type="email" value={email} onChange={(event) => setEmail(event.target.value)} aria-invalid={attempted && !!emailError} aria-describedby={attempted && emailError ? "edit-user-email-error" : undefined} />
            {attempted && <FieldError id="edit-user-email-error">{emailError}</FieldError>}
          </FormField>
          <FormField label="Display name" htmlFor="edit-user-name">
            <Input id="edit-user-name" value={displayName} onChange={(event) => setDisplayName(event.target.value)} maxLength={200} />
          </FormField>
        </div>
        <FormField label="Reset password" htmlFor="edit-user-password" hint={`Leave blank to keep the current password. A new password needs at least 12 characters.`}>
          <Input id="edit-user-password" type="password" autoComplete="new-password" value={password} onChange={(event) => setPassword(event.target.value)} aria-invalid={attempted && !!passwordError} aria-describedby={attempted && passwordError ? "edit-user-password-error" : undefined} />
          {attempted && <FieldError id="edit-user-password-error">{passwordError}</FieldError>}
        </FormField>
        {self
          ? <p className="text-sm text-muted-foreground">Your administrator access cannot be changed here.</p>
          : <SwitchField id="edit-user-is-admin" checked={isAdmin} onCheckedChange={setIsAdmin} label="Instance administrator" description="Can manage sign-in providers and all platform users." />}
        <DialogFooter>
          <DialogCancel disabled={saving} />
          <Button type="submit" loading={saving} loadingText="Saving…">Save changes</Button>
        </DialogFooter>
      </form>
    </AppDialog>
  )
}
