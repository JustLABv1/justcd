"use client"

import Link from "next/link"
import { MCPPolicySettings } from "./mcp-policy"
import { useCallback, useEffect, useState } from "react"
import { Badge } from "@/components/panelui/badge"
import { Button } from "@/components/panelui/button"
import { FormSelect } from "@/components/form-select"
import { Input } from "@/components/panelui/input"
import { Switch } from "@/components/panelui/switch"
import { ConfirmDisclosure } from "@/components/confirm-disclosure"
import { useToast } from "@/components/toast-provider"
import { SettingsNotice } from "@/components/settings/layout"
import { SettingsGroup, SettingsRow, SettingsSection } from "@/components/settings/settings-section"
import { api, errorMessage } from "@/lib/api"
import { LoadErrorNotice, SectionSkeleton } from "./shared"

export type AccessSettings = {
  localLoginEnabled: boolean
  localRegistrationEnabled: boolean
  oidcAutoProvision: boolean
  oidcAllowedDomains: string[]
  workspaceCreation: "anyone" | "admins"
  /** True when the operator's JUSTCD_FORCE_LOCAL_LOGIN keeps password sign-in on whatever the setting says. */
  localLoginForced: boolean
  readiness: { enabledProviders: number; ssoAdmins: number }
}

type Patch = Partial<Pick<AccessSettings, "localLoginEnabled" | "localRegistrationEnabled" | "oidcAutoProvision" | "oidcAllowedDomains" | "workspaceCreation">>

/** Why password sign-in cannot be turned off yet, or "" when it can. Mirrors the server's check. */
export function lockoutReason(readiness: AccessSettings["readiness"]) {
  if (readiness.enabledProviders === 0) return "Enable a single sign-on provider first."
  if (readiness.ssoAdmins === 0) return "Sign in once with an administrator account through a provider first, so you cannot lock yourself out."
  return ""
}

export function parseDomains(text: string) {
  return text.split(/[\s,;]+/).map((item) => item.trim().toLowerCase().replace(/^@/, "")).filter(Boolean)
}

/** Instance-wide sign-in, registration and workspace-creation controls. */
export function AccessPanel() {
  const toast = useToast()
  const [settings, setSettings] = useState<AccessSettings | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)
  const [domainText, setDomainText] = useState("")
  const [tick, setTick] = useState(0)

  const load = useCallback(() => {
    const controller = new AbortController()
    api<AccessSettings>("/api/v1/admin/access-settings", { signal: controller.signal })
      .then((result) => { setSettings(result); setDomainText(result.oidcAllowedDomains.join(", ")) })
      .catch((cause) => { if (!controller.signal.aborted) setError(cause) })
    return () => controller.abort()
  }, [])
  useEffect(() => load(), [load, tick])

  async function save(patch: Patch, success: string) {
    setBusy(true)
    try {
      const result = await api<AccessSettings>("/api/v1/admin/access-settings", { method: "PUT", body: JSON.stringify(patch) })
      setSettings(result)
      setDomainText(result.oidcAllowedDomains.join(", "))
      toast.success(success)
    } catch (cause) {
      toast.error(errorMessage(cause), cause)
      throw cause
    } finally {
      setBusy(false)
    }
  }
  const attempt = (patch: Patch, success: string) => void save(patch, success).catch(() => undefined)

  if (error) return <LoadErrorNotice error={error} title="Could not load access settings" onRetry={() => { setError(null); setTick(tick + 1) }} />
  if (!settings) return <SectionSkeleton rows={3} label="Loading access settings" />

  const blocked = lockoutReason(settings.readiness)
  const domains = parseDomains(domainText)
  const domainsChanged = domains.join(",") !== settings.oidcAllowedDomains.join(",")

  return <div className="space-y-8">
    <SettingsSection title="Sign-in methods" description="How people prove who they are. Turning off password sign-in makes single sign-on the only way in.">
      {settings.localLoginForced && !settings.localLoginEnabled && <SettingsNotice inline tone="warning" title="Password sign-in is being forced on">The server is started with <code>JUSTCD_FORCE_LOCAL_LOGIN=true</code>, which keeps password sign-in available even though it is turned off here. Remove it once single sign-on works again.</SettingsNotice>}
      <SettingsGroup className="mt-3">
        <SettingsRow
          label="Password sign-in"
          description={<>Let people sign in with an email address and password. Accounts that use two-factor authentication keep it.{settings.localLoginEnabled && blocked && <span className="mt-1 block text-warning-foreground dark:text-warning">Before you can turn this off: {blocked}</span>}</>}
          value={<Badge size="sm"  variant={settings.localLoginEnabled ? "success" : "secondary"}>{settings.localLoginEnabled ? "On" : "Off"}</Badge>}
          action={settings.localLoginEnabled
            ? <ConfirmDisclosure trigger="Turn off" triggerVariant="outline" confirmVariant="destructive" disabled={busy || Boolean(blocked)} title="Turn off password sign-in?" description="Nobody will be able to sign in with a password, including you. Make sure single sign-on works for your administrator account: if your identity provider is unavailable, you can only recover access with the server's JUSTCD_FORCE_LOCAL_LOGIN setting." confirmLabel="Turn off password sign-in" busyLabel="Turning off…" onConfirm={() => save({ localLoginEnabled: false }, "Password sign-in turned off. People now sign in with single sign-on.")} />
            : <Button type="button" variant="outline" size="sm" disabled={busy} loading={busy} loadingText="Turning on…" onClick={() => attempt({ localLoginEnabled: true }, "Password sign-in turned on.")}>Turn on</Button>}
        />
        <SettingsRow
          label="Single sign-on"
          description="OIDC providers your organisation signs in with."
          value={<span className="text-muted-foreground">{settings.readiness.enabledProviders === 0 ? "None enabled" : `${settings.readiness.enabledProviders} enabled`}</span>}
          action={<Button size="sm" variant="outline" nativeButton={false} render={<Link href="/settings/oidc" />}>Manage providers</Button>}
        />
      </SettingsGroup>
    </SettingsSection>

    <SettingsSection title="Registration" description="Who can get an account. Administrators can always create local accounts under Platform users.">
      <SettingsGroup>
        <SettingsRow
          label="Let people register with a password"
          description={settings.localLoginEnabled
            ? settings.localRegistrationEnabled
              ? "The sign-in page offers \"Create an account\". New accounts have no access until you approve them under Platform users, because their email address is not verified. Allowed email domains apply."
              : "Off: only administrators create password accounts. Turn on to show a registration page on the sign-in screen."
            : "Turn on password sign-in first. Registration creates password accounts."}
          action={<Switch checked={settings.localRegistrationEnabled} disabled={busy || !settings.localLoginEnabled} aria-label="Let people register with a password" onCheckedChange={(value) => attempt({ localRegistrationEnabled: value }, value ? "Registration is open. New accounts wait for your approval." : "Registration is closed.")} />}
        />
        <SettingsRow
          label="Create accounts on first single sign-on sign-in"
          description={settings.oidcAutoProvision ? "Anyone who signs in through an enabled provider with a verified email address gets an account. Workspace access still depends on group mapping or an invitation." : "Only people you have already added can sign in. A new identity is refused, with a message to ask an administrator."}
          action={<Switch checked={settings.oidcAutoProvision} disabled={busy} aria-label="Create accounts on first single sign-on sign-in" onCheckedChange={(value) => attempt({ oidcAutoProvision: value }, value ? "New accounts are created on first sign-in." : "New accounts are no longer created automatically.")} />}
        />
        <div className="space-y-3 px-5 py-4">
          <div className="space-y-1">
            <h3 className="text-sm font-medium">Allowed email domains</h3>
            <p className="text-sm leading-5 text-muted-foreground">Limit self-service account creation, by registration or first single sign-on sign-in, to these domains. Leave empty to allow any domain. Accounts that already exist are never affected.</p>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Input aria-label="Allowed email domains" className="min-w-64 flex-1" placeholder="example.com, corp.example.org" value={domainText} disabled={busy} onChange={(event) => setDomainText(event.target.value)} />
            <Button type="button" size="sm" disabled={busy || !domainsChanged} onClick={() => attempt({ oidcAllowedDomains: domains }, domains.length ? "Allowed domains saved." : "Any email domain may create an account.")}>Save domains</Button>
          </div>
          {settings.oidcAllowedDomains.length > 0 && <div className="flex flex-wrap gap-1.5" aria-label="Saved domains">{settings.oidcAllowedDomains.map((domain) => <Badge key={domain} size="sm"  variant="outline">{domain}</Badge>)}</div>}
        </div>
      </SettingsGroup>
    </SettingsSection>

    <MCPPolicySettings />
    <SettingsSection title="Workspaces" description="Who can start new workspaces on this instance.">
      <SettingsGroup>
        <SettingsRow
          label="Who can create workspaces"
          description={settings.workspaceCreation === "anyone" ? "Every signed-in user can create a workspace and becomes its owner." : "Only instance administrators create workspaces. Everyone else is added to existing ones."}
          action={<FormSelect ariaLabel="Who can create workspaces" size="sm" className="w-56" value={settings.workspaceCreation} disabled={busy} onValueChange={(value) => attempt({ workspaceCreation: value as AccessSettings["workspaceCreation"] }, value === "admins" ? "Only administrators can create workspaces." : "Any signed-in user can create workspaces.")} items={[{ value: "anyone", label: "Any signed-in user" }, { value: "admins", label: "Instance administrators only" }]} />}
        />
      </SettingsGroup>
    </SettingsSection>
  </div>
}
