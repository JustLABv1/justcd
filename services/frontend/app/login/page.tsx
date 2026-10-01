"use client"

import Link from "next/link"
import { useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import { Button } from "@/components/ui/button"
import { AuthShell } from "@/components/auth-shell"
import { Input } from "@/components/ui/input"
import { useToast } from "@/components/toast-provider"
import { api, apiPost, errorMessage } from "@/lib/api"
import type { OIDCProvider, User } from "@/lib/types"

export default function LoginPage() {
  const router = useRouter()
  const toast = useToast()
  const [providers, setProviders] = useState<OIDCProvider[]>([])
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [busy, setBusy] = useState(false)
  const [signupAvailable, setSignupAvailable] = useState(false)

  useEffect(() => {
    api<{ items: OIDCProvider[] }>("/api/v1/auth/providers").then((response) => setProviders(response.items)).catch(() => setProviders([]))
    api<{ signupAvailable: boolean }>("/api/v1/auth/setup").then((response) => { if (response.signupAvailable) router.replace("/signup"); setSignupAvailable(response.signupAvailable) }).catch(() => undefined)
    api<{ user: User }>("/api/v1/auth/session").then(() => router.replace("/")).catch(() => undefined)
  }, [router])

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true)
    try {
      await apiPost("/api/v1/auth/login", { email, password })
      router.replace("/")
    } catch (cause) {
      toast.error(errorMessage(cause), cause)
    } finally {
      setBusy(false)
    }
  }

  return (
    <AuthShell eyebrow="Welcome back" title="Sign in to JustCD" description="Your next deployment starts here.">
          <form className="mt-7 space-y-4" onSubmit={submit}>
            <div className="space-y-1.5"><label htmlFor="email" className="text-xs font-medium">Email address</label><Input id="email" type="email" autoComplete="username" value={email} onChange={(event) => setEmail(event.target.value)} required /></div>
            <div className="space-y-1.5"><label htmlFor="password" className="text-xs font-medium">Password</label><Input id="password" type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} required /></div>
            <Button className="w-full" type="submit" loading={busy} loadingText="Signing in…">Continue with email</Button>
          </form>

          {providers.length > 0 && <div className="mt-6"><div className="relative mb-4 text-center"><span className="relative z-10 bg-card px-3 text-xs uppercase tracking-wider text-muted-foreground">or continue with</span><span className="absolute inset-x-0 top-1/2 border-t" /></div><div className="space-y-2">{providers.map((provider) => <a key={provider.id} href={`/api/v1/auth/oidc/${encodeURIComponent(provider.id)}/start`} className="flex h-11 w-full items-center justify-center gap-2 rounded-lg border bg-background text-sm font-medium transition-colors hover:bg-muted"><span className="grid size-5 place-items-center rounded-full bg-muted text-xs">↗</span>{provider.name}</a>)}</div></div>}

          <p className="mt-8 text-center text-sm leading-5 text-muted-foreground">{signupAvailable ? <Link href="/signup" className="font-medium text-primary hover:underline">Set up the first administrator</Link> : "Access is managed by your JustCD administrator. OIDC accounts need a verified email address."}</p>
    </AuthShell>
  )
}
