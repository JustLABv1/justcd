"use client"

import Link from "next/link"
import { useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import { Button } from "@/components/panelui/button"
import { AuthShell } from "@/components/auth-shell"
import { Skeleton } from "@/components/ui/skeleton"
import { Input } from "@/components/panelui/input"
import { FormField } from "@/components/ui-kit"
import { ErrorDetailsButton } from "@/components/error-details"
import { useToast } from "@/components/toast-provider"
import { api, apiPost, errorMessage } from "@/lib/api"
import type { User } from "@/lib/types"

/** Self-service account creation with a password. Only reachable while an administrator has opened registration. */
export default function RegisterPage() {
  const router = useRouter()
  const toast = useToast()
  const [open, setOpen] = useState<boolean | null>(null)
  const [displayName, setDisplayName] = useState("")
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [confirm, setConfirm] = useState("")
  const [loadError, setLoadError] = useState<unknown | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api<{ user: User }>("/api/v1/auth/session").then(() => router.replace("/")).catch(() => undefined)
    api<{ registrationEnabled?: boolean }>("/api/v1/auth/providers").then((result) => setOpen(result.registrationEnabled === true)).catch((cause) => setLoadError(cause))
  }, [router])

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (password !== confirm) { toast.error("Passwords do not match."); return }
    setBusy(true)
    try {
      await apiPost("/api/v1/auth/register", { displayName, email, password })
      router.replace("/")
    } catch (cause) { toast.error(errorMessage(cause), cause) }
    finally { setBusy(false) }
  }

  return <AuthShell eyebrow="Join JustCD" title="Create your account" description="Register with an email address and a password.">
    {open === true ? <form className="space-y-4" onSubmit={submit}>
      <FormField label="Display name" htmlFor="name"><Input id="name" autoComplete="name" value={displayName} onChange={(event) => setDisplayName(event.target.value)} maxLength={100} required /></FormField>
      <FormField label="Email address" htmlFor="email"><Input id="email" type="email" autoComplete="username" value={email} onChange={(event) => setEmail(event.target.value)} required /></FormField>
      <FormField label="Password" htmlFor="password" hint="Use 12 to 1024 characters. Longer passphrases are fine."><Input id="password" type="password" autoComplete="new-password" minLength={12} maxLength={1024} value={password} onChange={(event) => setPassword(event.target.value)} required /></FormField>
      <FormField label="Confirm password" htmlFor="confirm"><Input id="confirm" type="password" autoComplete="new-password" minLength={12} maxLength={1024} value={confirm} onChange={(event) => setConfirm(event.target.value)} required /></FormField>
      <p className="text-xs leading-5 text-muted-foreground">An administrator has to approve new accounts before they can be added to workspaces. You can sign in right away.</p>
      <Button className="w-full" loading={busy} loadingText="Creating account…" type="submit">Create account</Button>
    </form> : open === false ? <div role="status" className=" text-sm leading-6">Registration is not open on this instance. Ask your JustCD administrator for an account, or <Link href="/login" className="font-medium text-primary hover:underline">sign in</Link>.</div>
      : loadError == null ? <div role="status" aria-label="Checking registration" className="space-y-4"><Skeleton className="h-14 w-full" /><Skeleton className="h-14 w-full" /><Skeleton className="h-10 w-full" /></div> : null}
    {loadError != null && <div role="alert" className="mt-4 flex flex-wrap items-center justify-between gap-3 text-sm text-destructive"><span>{errorMessage(loadError)}</span><ErrorDetailsButton error={loadError} /></div>}
    <p className="mt-6 text-center text-sm leading-5 text-muted-foreground">Already have an account? <Link href="/login" className="font-medium text-primary hover:underline">Sign in</Link></p>
  </AuthShell>
}
