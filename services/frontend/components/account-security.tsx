"use client"
import { useCallback, useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { FormField, Panel, PageHeading } from "@/components/ui-kit"
import { api, apiPost, errorMessage } from "@/lib/api"
type Session = {
  id: string
  createdAt: string
  expiresAt: string
  current: boolean
}
type MFA = {
  enabled: boolean
  localAccount: boolean
  recoveryCodesRemaining: number
}
export function AccountSecurity() {
  const router = useRouter()
  const [sessions, setSessions] = useState<Session[]>([])
  const [mfa, setMfa] = useState<MFA | null>(null)
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  const [enrollment, setEnrollment] = useState<{
    secret: string
    uri: string
  } | null>(null)
  const [recovery, setRecovery] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const load = useCallback(async () => {
    const [s, m] = await Promise.all([
      api<{ items: Session[] }>("/api/v1/auth/sessions"),
      api<MFA>("/api/v1/auth/mfa"),
    ])
    setSessions(s.items)
    setMfa(m)
  }, [])
  useEffect(() => {
    void Promise.resolve()
      .then(load)
      .catch((e) => setError(errorMessage(e)))
  }, [load])
  async function run(action: () => Promise<unknown>, message: string) {
    setBusy(true)
    setError("")
    setNotice("")
    try {
      await action()
      setNotice(message)
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="mx-auto max-w-4xl space-y-6">
      <PageHeading
        title="Account security"
        description="Manage your sign-in sessions and two-factor authentication."
      />
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
          <Button
            variant="ghost"
            disabled={busy}
            onClick={() => void run(load, "Refreshed.")}
          >
            Retry
          </Button>
        </p>
      )}
      {notice && (
        <p role="status" className="text-sm text-muted-foreground">
          {notice}
        </p>
      )}
      <Panel
        title="Active sessions"
        description="Revoking a session signs that device out."
      >
        <div className="space-y-3 p-5">
          {!mfa && (
            <p className="text-sm text-muted-foreground">
              Loading account security…
            </p>
          )}
          {sessions.map((s) => (
            <div
              key={s.id}
              className="flex flex-wrap items-center justify-between gap-3 rounded-lg border p-3"
            >
              <div className="text-sm">
                <p className="font-medium">
                  {s.current ? "Current session" : "Other session"}
                </p>
                <p className="text-muted-foreground">
                  Started {new Date(s.createdAt).toLocaleString()} · Expires{" "}
                  {new Date(s.expiresAt).toLocaleString()}
                </p>
              </div>
              <Button
                variant="destructive"
                disabled={busy}
                onClick={() =>
                  void run(async () => {
                    await api(`/api/v1/auth/sessions/${s.id}`, {
                      method: "DELETE",
                    })
                    if (s.current) {
                      router.replace("/login")
                      return
                    }
                    await load()
                  }, "Session revoked.")
                }
              >
                Revoke{s.current ? " and sign out" : ""}
              </Button>
            </div>
          ))}
        </div>
      </Panel>
      <Panel
        title="Two-factor authentication"
        description="Use an authenticator app and keep recovery codes somewhere safe."
      >
        <div className="space-y-4 p-5">
          {mfa && !mfa.localAccount && (
            <p className="text-sm text-muted-foreground">
              Your identity provider manages two-factor authentication for this
              account.
            </p>
          )}
          {mfa?.localAccount && (
            <>
              <p className="text-sm">
                {mfa.enabled
                  ? `Enabled · ${mfa.recoveryCodesRemaining} recovery codes remaining`
                  : "Not enabled"}
              </p>
              {recovery.length > 0 && (
                <div role="status" className="space-y-3 rounded-lg border p-4">
                  <p className="text-sm font-medium">
                    Save these recovery codes now. Each code works once and will
                    not be shown again.
                  </p>
                  <ul className="space-y-1 font-mono text-sm">
                    {recovery.map((c) => (
                      <li key={c}>{c}</li>
                    ))}
                  </ul>
                  <Button variant="outline" onClick={() => setRecovery([])}>
                    I saved my recovery codes
                  </Button>
                </div>
              )}
              <form
                className="space-y-4"
                onSubmit={(e) => {
                  e.preventDefault()
                  void run(
                    async () => {
                      if (mfa.enabled) {
                        await api("/api/v1/auth/mfa", {
                          method: "DELETE",
                          body: JSON.stringify({ password, code }),
                        })
                        router.replace("/login")
                        return
                      }
                      if (enrollment) {
                        const result = await apiPost<{
                          recoveryCodes: string[]
                        }>("/api/v1/auth/mfa/confirm", { password, code })
                        setRecovery(result.recoveryCodes)
                        setEnrollment(null)
                        setPassword("")
                        setCode("")
                        await load()
                      } else {
                        setEnrollment(
                          await apiPost<{ secret: string; uri: string }>(
                            "/api/v1/auth/mfa/enroll",
                            { password }
                          )
                        )
                      }
                    },
                    mfa.enabled
                      ? "MFA removed. Sign in again."
                      : enrollment
                        ? "MFA enabled. Other sessions were revoked."
                        : "Add this secret to your authenticator app, then enter its code."
                  )
                }}
              >
                <FormField label="Current password" htmlFor="mfa-password">
                  <Input
                    id="mfa-password"
                    type="password"
                    autoComplete="current-password"
                    value={password}
                    disabled={busy}
                    onChange={(e) => setPassword(e.target.value)}
                    required
                  />
                </FormField>
                {enrollment && (
                  <div className="space-y-2 rounded-lg border p-3">
                    <p className="text-sm font-medium">
                      Authenticator setup key
                    </p>
                    <code className="block text-sm break-all">
                      {enrollment.secret}
                    </code>
                    <p className="text-sm text-muted-foreground">
                      Add a time-based account named JustCD to your
                      authenticator using this key. Enrollment expires after 10
                      minutes.
                    </p>
                  </div>
                )}
                {(mfa.enabled || enrollment) && (
                  <FormField
                    label={
                      mfa.enabled
                        ? "Authenticator or recovery code"
                        : "Authenticator code"
                    }
                    htmlFor="mfa-code"
                  >
                    <Input
                      id="mfa-code"
                      autoComplete="one-time-code"
                      value={code}
                      disabled={busy}
                      onChange={(e) => setCode(e.target.value)}
                      required
                    />
                  </FormField>
                )}
                <div className="flex flex-wrap gap-2">
                  <Button
                    type="submit"
                    disabled={busy}
                    variant={mfa.enabled ? "destructive" : "default"}
                  >
                    {busy
                      ? "Working…"
                      : mfa.enabled
                        ? "Remove MFA and sign out"
                        : enrollment
                          ? "Confirm enrollment"
                          : "Set up MFA"}
                  </Button>
                  {enrollment && (
                    <Button
                      variant="outline"
                      disabled={busy}
                      onClick={() => {
                        setEnrollment(null)
                        setPassword("")
                        setCode("")
                      }}
                    >
                      Cancel
                    </Button>
                  )}
                </div>
              </form>
            </>
          )}
        </div>
      </Panel>
    </div>
  )
}
