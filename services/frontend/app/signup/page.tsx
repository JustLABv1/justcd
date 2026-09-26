"use client"

import Link from "next/link"
import { useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import { Button } from "@/components/ui/button"
import { AuthShell } from "@/components/auth-shell"
import { Input } from "@/components/ui/input"
import { ErrorDetailsButton } from "@/components/error-details"
import { useToast } from "@/components/toast-provider"
import { api, apiPost, errorMessage } from "@/lib/api"
import type { User } from "@/lib/types"

export default function SignupPage() {
  const router = useRouter()
  const toast = useToast()
  const [available, setAvailable] = useState<boolean | null>(null)
  const [displayName, setDisplayName] = useState("")
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [confirm, setConfirm] = useState("")
  const [loadError, setLoadError] = useState<unknown | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api<{ user: User }>("/api/v1/auth/session").then(() => router.replace("/")).catch(() => undefined)
    api<{ signupAvailable: boolean }>("/api/v1/auth/setup").then((result) => setAvailable(result.signupAvailable)).catch((cause) => setLoadError(cause))
  }, [router])

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (password !== confirm) { toast.error("Passwords do not match."); return }
    setBusy(true)
    try {
      await apiPost("/api/v1/auth/signup", { displayName, email, password })
      router.replace("/")
    } catch (cause) { toast.error(errorMessage(cause), cause) }
    finally { setBusy(false) }
  }

  return <AuthShell eyebrow="First-time setup" title="Make yourself at home" description="Create the first administrator account to connect your repositories and clusters.">
      {available === false ? <div className="mt-7 rounded-lg border bg-muted/40 p-4 text-sm">Setup is already complete. <Link href="/login" className="font-medium text-primary hover:underline">Sign in instead</Link>.</div> : available ? <form onSubmit={submit} className="mt-7 space-y-4">
        <div className="space-y-1.5"><label htmlFor="name" className="text-xs font-medium">Display name</label><Input id="name" autoComplete="name" value={displayName} onChange={(event) => setDisplayName(event.target.value)} maxLength={100} required /></div>
        <div className="space-y-1.5"><label htmlFor="email" className="text-xs font-medium">Email address</label><Input id="email" type="email" autoComplete="username" value={email} onChange={(event) => setEmail(event.target.value)} required /></div>
        <div className="space-y-1.5"><label htmlFor="password" className="text-xs font-medium">Password</label><Input id="password" type="password" autoComplete="new-password" minLength={12} value={password} onChange={(event) => setPassword(event.target.value)} required /><p className="text-xs text-muted-foreground">At least 12 characters.</p></div>
        <div className="space-y-1.5"><label htmlFor="confirm" className="text-xs font-medium">Confirm password</label><Input id="confirm" type="password" autoComplete="new-password" minLength={12} value={confirm} onChange={(event) => setConfirm(event.target.value)} required /></div>
        <Button className="w-full" loading={busy} loadingText="Creating administrator…" type="submit">Create administrator</Button>
      </form> : loadError == null ? <p role="status" className="mt-7 text-sm text-muted-foreground">Checking setup status…</p> : null}
      {loadError != null && <div role="alert" className="mt-4 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-destructive/20 bg-destructive/5 p-3 text-xs text-destructive"><span>{errorMessage(loadError)}</span><ErrorDetailsButton error={loadError} /></div>}
    <p className="mt-6 text-center text-xs leading-5 text-muted-foreground">Already have an account? <Link href="/login" className="font-medium text-primary hover:underline">Sign in</Link></p>
  </AuthShell>
}
