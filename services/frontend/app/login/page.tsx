"use client"

import Link from "next/link"
import { useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import { HugeiconsIcon } from "@hugeicons/react"
import { Login01Icon } from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { AuthShell } from "@/components/auth-shell"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { FormField } from "@/components/ui-kit"
import { ErrorDetailsButton } from "@/components/error-details"
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
  const [loadingOptions, setLoadingOptions] = useState(true)
  const [loadError, setLoadError] = useState<unknown | null>(null)

  useEffect(() => {
    let active = true
    const providersRequest = api<{ items: OIDCProvider[] }>("/api/v1/auth/providers").then((response) => { if (active) setProviders(response.items) })
    const setupRequest = api<{ signupAvailable: boolean }>("/api/v1/auth/setup").then((response) => {
      if (!active) return
      if (response.signupAvailable) router.replace("/signup")
      setSignupAvailable(response.signupAvailable)
    })
    // An unauthenticated visitor is expected to fail the session check; only a signed-in user is redirected.
    api<{ user: User }>("/api/v1/auth/session").then(() => router.replace("/")).catch(() => undefined)
    void Promise.allSettled([providersRequest, setupRequest]).then((results) => {
      if (!active) return
      const failed = results.find((result): result is PromiseRejectedResult => result.status === "rejected")
      setLoadError(failed ? failed.reason : null)
      setLoadingOptions(false)
    })
    return () => { active = false }
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
        <FormField label="Email address" htmlFor="email"><Input id="email" type="email" autoComplete="username" value={email} onChange={(event) => setEmail(event.target.value)} required /></FormField>
        <FormField label="Password" htmlFor="password"><Input id="password" type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} required /></FormField>
        <Button className="w-full" type="submit" loading={busy} loadingText="Signing in…">Sign in</Button>
      </form>

      {loadingOptions && <div role="status" aria-label="Loading sign-in options" className="mt-6 space-y-3"><Skeleton className="mx-auto h-4 w-32" /><Skeleton className="h-11 w-full rounded-lg" /></div>}
      {!loadingOptions && loadError != null && <div role="alert" className="mt-6 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-destructive/20 bg-destructive/5 p-3 text-sm text-destructive"><span>Sign-in options could not be loaded. {errorMessage(loadError)}</span><ErrorDetailsButton error={loadError} /></div>}
      {!loadingOptions && providers.length > 0 && <div className="mt-6"><div className="relative mb-4 text-center"><span className="relative z-10 bg-card px-3 text-xs uppercase tracking-wider text-muted-foreground">or continue with</span><span className="absolute inset-x-0 top-1/2 border-t" /></div><div className="space-y-2">{providers.map((provider) => <Button key={provider.id} render={<a href={`/api/v1/auth/oidc/${encodeURIComponent(provider.id)}/start`} />} nativeButton={false} variant="outline" className="h-11 w-full"><HugeiconsIcon icon={Login01Icon} strokeWidth={1.8} aria-hidden="true" />{provider.name}</Button>)}</div></div>}

      <div className="mt-8 text-center text-sm leading-5 text-muted-foreground">{loadingOptions ? <Skeleton className="mx-auto h-4 w-56" /> : signupAvailable ? <Link href="/signup" className="font-medium text-primary hover:underline">Set up the first administrator</Link> : "Access is managed by your JustCD administrator. OIDC accounts need a verified email address."}</div>
    </AuthShell>
  )
}
