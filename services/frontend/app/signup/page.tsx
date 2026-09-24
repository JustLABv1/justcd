"use client"

import Link from "next/link"
import { useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import { Button } from "@/components/ui/button"
import { ThemePicker } from "@/components/theme-picker"
import { Input } from "@/components/ui/input"
import { api, apiPost, errorMessage } from "@/lib/api"
import type { User } from "@/lib/types"

export default function SignupPage() {
  const router = useRouter()
  const [available, setAvailable] = useState<boolean | null>(null)
  const [displayName, setDisplayName] = useState("")
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [confirm, setConfirm] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api<{ user: User }>("/api/v1/auth/session").then(() => router.replace("/")).catch(() => undefined)
    api<{ signupAvailable: boolean }>("/api/v1/auth/setup").then((result) => setAvailable(result.signupAvailable)).catch((cause) => setError(errorMessage(cause)))
  }, [router])

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (password !== confirm) { setError("Passwords do not match."); return }
    setBusy(true); setError("")
    try {
      await apiPost("/api/v1/auth/signup", { displayName, email, password })
      router.replace("/")
    } catch (cause) { setError(errorMessage(cause)) }
    finally { setBusy(false) }
  }

  return <main className="relative grid min-h-svh place-items-center bg-muted/30 px-5 py-10">
    <div className="absolute right-4 top-4"><ThemePicker /></div>
    <section className="w-full max-w-md rounded-2xl border bg-background p-6 shadow-sm sm:p-8">
      <Link href="/login" className="inline-flex items-center gap-3 text-sm font-semibold"><span className="grid size-9 place-items-center rounded-xl bg-primary text-primary-foreground">J</span> JustCD</Link>
      <p className="mt-8 text-xs font-semibold uppercase tracking-widest text-primary">First-time setup</p>
      <h1 className="mt-2 text-2xl font-semibold tracking-tight">Create your administrator</h1>
      <p className="mt-2 text-sm leading-6 text-muted-foreground">The first account becomes the instance administrator. After setup, new accounts are created by an administrator or through configured OIDC providers.</p>
      {available === false ? <div className="mt-7 rounded-lg border bg-muted/40 p-4 text-sm">Setup is already complete. <Link href="/login" className="font-medium text-primary hover:underline">Sign in instead</Link>.</div> : available ? <form onSubmit={submit} className="mt-7 space-y-4">
        <div className="space-y-1.5"><label htmlFor="name" className="text-xs font-medium">Display name</label><Input id="name" autoComplete="name" value={displayName} onChange={(event) => setDisplayName(event.target.value)} maxLength={100} required /></div>
        <div className="space-y-1.5"><label htmlFor="email" className="text-xs font-medium">Email address</label><Input id="email" type="email" autoComplete="username" value={email} onChange={(event) => setEmail(event.target.value)} required /></div>
        <div className="space-y-1.5"><label htmlFor="password" className="text-xs font-medium">Password</label><Input id="password" type="password" autoComplete="new-password" minLength={12} value={password} onChange={(event) => setPassword(event.target.value)} required /><p className="text-xs text-muted-foreground">At least 12 characters.</p></div>
        <div className="space-y-1.5"><label htmlFor="confirm" className="text-xs font-medium">Confirm password</label><Input id="confirm" type="password" autoComplete="new-password" minLength={12} value={confirm} onChange={(event) => setConfirm(event.target.value)} required /></div>
        {error && <p role="alert" className="rounded-lg border border-destructive/20 bg-destructive/5 px-3 py-2 text-xs text-destructive">{error}</p>}
        <Button className="w-full" loading={busy} loadingText="Creating administrator…" type="submit">Create administrator</Button>
      </form> : <p role="status" className="mt-7 text-sm text-muted-foreground">Checking setup status…</p>}
      {error && !available && <p role="alert" className="mt-4 text-xs text-destructive">{error}</p>}
    </section>
  </main>
}
