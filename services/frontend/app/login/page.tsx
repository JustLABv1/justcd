"use client"

import Link from "next/link"
import { useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { api, apiPost, errorMessage } from "@/lib/api"
import type { OIDCProvider, User } from "@/lib/types"

export default function LoginPage() {
  const router = useRouter()
  const [providers, setProviders] = useState<OIDCProvider[]>([])
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [error, setError] = useState("")
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
    setError("")
    try {
      await apiPost("/api/v1/auth/login", { email, password })
      router.replace("/")
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setBusy(false)
    }
  }

  return (
    <main className="grid min-h-svh bg-background lg:grid-cols-[1fr_0.86fr]">
      <div className="relative hidden overflow-hidden bg-[#101828] px-12 py-10 text-white lg:flex lg:flex-col lg:justify-between">
        <div className="absolute -right-32 -top-32 size-[520px] rounded-full border border-white/5" />
        <div className="absolute -right-10 -top-10 size-[340px] rounded-full border border-white/5" />
        <Link href="/" className="relative flex items-center gap-3">
          <span className="grid size-9 place-items-center rounded-xl bg-white text-sm font-bold text-[#101828]">J</span>
          <span className="text-sm font-semibold tracking-tight">JustCD</span>
        </Link>
        <div className="relative max-w-xl pb-10">
          <p className="mb-5 text-xs font-semibold uppercase tracking-[0.18em] text-blue-300">Delivery without cluster baggage</p>
          <h1 className="text-4xl font-semibold leading-[1.12] tracking-tight xl:text-5xl">Ship what you declared.<br /><span className="text-white/55">Review every change.</span></h1>
          <p className="mt-6 max-w-md text-sm leading-6 text-slate-300">Git-driven Kubernetes delivery with clear diffs, drift visibility, and an explicit approval step before anything is removed.</p>
          <div className="mt-10 grid max-w-md grid-cols-3 gap-3 text-[11px] text-slate-300">
            <div className="rounded-lg border border-white/10 bg-white/[0.04] p-3"><span className="block text-sm font-semibold text-white">No CRDs</span>Nothing to install on-cluster</div>
            <div className="rounded-lg border border-white/10 bg-white/[0.04] p-3"><span className="block text-sm font-semibold text-white">Plan first</span>Preview before mutation</div>
            <div className="rounded-lg border border-white/10 bg-white/[0.04] p-3"><span className="block text-sm font-semibold text-white">Scoped</span>Credentials per target</div>
          </div>
        </div>
        <p className="relative text-[11px] text-slate-500">JustCD · continuous delivery, kept simple</p>
      </div>

      <div className="flex items-center justify-center px-5 py-12 sm:px-8">
        <section className="w-full max-w-[390px]">
          <Link href="/" className="mb-10 flex items-center gap-2.5 lg:hidden"><span className="grid size-8 place-items-center rounded-lg bg-primary font-bold text-primary-foreground">J</span><span className="font-semibold">JustCD</span></Link>
          <p className="text-[10px] font-semibold uppercase tracking-[0.16em] text-primary">Welcome back</p>
          <h2 className="mt-2 text-2xl font-semibold tracking-tight">Sign in to JustCD</h2>
          <p className="mt-2 text-sm text-muted-foreground">Use your local account or an organization identity provider.</p>

          <form className="mt-7 space-y-4" onSubmit={submit}>
            <div className="space-y-1.5"><label htmlFor="email" className="text-xs font-medium">Email address</label><Input id="email" type="email" autoComplete="username" value={email} onChange={(event) => setEmail(event.target.value)} required /></div>
            <div className="space-y-1.5"><label htmlFor="password" className="text-xs font-medium">Password</label><Input id="password" type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} required /></div>
            {error && <p role="alert" className="rounded-lg border border-destructive/20 bg-destructive/5 px-3 py-2 text-xs text-destructive">{error}</p>}
            <Button className="w-full" type="submit" disabled={busy}>{busy ? "Signing in…" : "Continue with email"}</Button>
          </form>

          {providers.length > 0 && <div className="mt-6"><div className="relative mb-4 text-center"><span className="relative z-10 bg-background px-3 text-[10px] uppercase tracking-wider text-muted-foreground">or continue with</span><span className="absolute inset-x-0 top-1/2 border-t" /></div><div className="space-y-2">{providers.map((provider) => <a key={provider.id} href={`/api/v1/auth/oidc/${encodeURIComponent(provider.id)}/start`} className="flex h-9 w-full items-center justify-center gap-2 rounded-lg border bg-background text-sm font-medium transition-colors hover:bg-muted"><span className="grid size-5 place-items-center rounded-full bg-muted text-[10px]">↗</span>{provider.name}</a>)}</div></div>}

          <p className="mt-8 text-center text-xs leading-5 text-muted-foreground">{signupAvailable ? <Link href="/signup" className="font-medium text-primary hover:underline">Set up the first administrator</Link> : "Access is managed by your JustCD administrator. OIDC accounts need a verified email address."}</p>
        </section>
      </div>
    </main>
  )
}
