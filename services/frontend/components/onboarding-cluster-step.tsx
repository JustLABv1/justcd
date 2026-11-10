"use client"

import Link from "next/link"
import { useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowRight01Icon, Copy01Icon, Tick02Icon } from "@hugeicons/core-free-icons"
import { ErrorDetailsButton } from "@/components/error-details"
import { useToast } from "@/components/toast-provider"
import { Button } from "@/components/panelui/button"
import { Card } from "@/components/panelui/card"
import { Badge } from "@/components/panelui/badge"
import { Input } from "@/components/panelui/input"
import { FormField, StatusBadge, type StatusTone } from "@/components/ui-kit"
import { apiPost, errorMessage } from "@/lib/api"
import type { OnboardingCluster, OnboardingClusterState } from "@/lib/types"

const stateLabel: Record<OnboardingClusterState, string> = { online: "Agent online", waiting: "Waiting for agent", offline: "Agent offline", revoked: "Agent revoked", not_enrolled: "Not enrolled", verified: "Verified", partial: "Partly verified", unverified: "Not verified", failed: "Check failed" }
const stateTone: Record<OnboardingClusterState, StatusTone> = { online: "success", verified: "success", partial: "warning", waiting: "warning", offline: "warning", unverified: "warning", not_enrolled: "warning", revoked: "destructive", failed: "destructive" }

type Enrollment = { clusterId: string; clusterName: string; token: string; expiresAt: string; namespaces: string }

/** Shell commands that install the agent. The real token is only copied, never rendered. */
function installCommands(namespaces: string, workspaceId: string, serverUrl: string) {
  const list = namespaces.split(",").map((item) => item.trim()).filter(Boolean).join(",")
  return (secret: string) => [
    "kubectl create namespace justcd-agent",
    `kubectl -n justcd-agent create secret generic justcd-agent-enrollment --from-literal=enrollmentToken='${secret}'`,
    "helm upgrade --install justcd-agent ./charts/justcd-agent --namespace justcd-agent \\",
    `  --set serverUrl=${serverUrl} --set enrollmentSecret=justcd-agent-enrollment \\`,
    `  --set 'profiles[0].name=default' --set 'profiles[0].workspaceIds={${workspaceId}}'${list ? ` \\\n  --set 'profiles[0].namespaces={${list}}'` : ""}`,
  ].join("\n")
}

export function OnboardingClusterStep({ workspaceId, clusters, onChanged }: { workspaceId: string; clusters: OnboardingCluster[]; onChanged: () => void }) {
  const toast = useToast()
  const [name, setName] = useState("")
  const [namespaces, setNamespaces] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [enrollment, setEnrollment] = useState<Enrollment | null>(null)
  const [copied, setCopied] = useState(false)
  const serverUrl = typeof window === "undefined" ? "" : window.location.origin
  const live = enrollment ? clusters.find((cluster) => cluster.id === enrollment.clusterId) : undefined
  const connected = live?.state === "online"

  async function enroll(existing?: OnboardingCluster) {
    setBusy(true); setError(null)
    try {
      const cluster = existing ?? await apiPost<{ id: string; name: string }>("/api/v1/clusters", { workspaceId, name: name.trim(), connectionMode: "agent" })
      const result = await apiPost<{ enrollmentToken: string; expiresAt: string }>(`/api/v1/clusters/${encodeURIComponent(cluster.id)}/agent/enrollment`, { defaultProfile: "default", clusterProfile: "" })
      setEnrollment({ clusterId: cluster.id, clusterName: cluster.name, token: result.enrollmentToken, expiresAt: result.expiresAt, namespaces })
      toast.success("Enrollment token created.")
      onChanged()
    } catch (cause) { setError(cause) }
    finally { setBusy(false) }
  }

  async function copy(text: string) {
    try { await navigator.clipboard.writeText(text); setCopied(true); window.setTimeout(() => setCopied(false), 2000) } catch (cause) { setError(cause) }
  }

  const commands = enrollment ? installCommands(enrollment.namespaces, workspaceId, serverUrl) : null
  return <div className="space-y-6">
    {clusters.length > 0 && <section aria-label="Clusters in this workspace" className="space-y-2">
      <h3 className="text-sm font-semibold">Your clusters</h3>
      <ul className="space-y-3">
        {clusters.map((cluster) => <Card as="li" size="sm" key={cluster.id}><Card.Content className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
          <div className="min-w-0"><p className="text-sm font-medium break-all">{cluster.name}</p><p className="text-xs text-muted-foreground">{cluster.mode === "agent" ? "Outbound agent" : "Direct API access"}{cluster.message && cluster.state !== "online" && cluster.state !== "verified" ? ` · ${cluster.message}` : ""}</p></div>
          <div className="flex items-center gap-2">
            <StatusBadge status={stateLabel[cluster.state]} tone={stateTone[cluster.state]} />
            {cluster.mode === "agent" && ["not_enrolled", "waiting", "offline", "revoked"].includes(cluster.state) && <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => void enroll(cluster)}>New token</Button>}
          </div>
        </Card.Content></Card>)}
      </ul>
    </section>}

    {enrollment && commands && <section aria-label="Install the agent" className="space-y-4">
      {connected ? <div role="status" className="flex items-start gap-3"><span className="grid size-8 shrink-0 place-items-center rounded-full bg-success/10 text-success-foreground dark:text-success"><HugeiconsIcon icon={Tick02Icon} strokeWidth={2} className="size-4" aria-hidden="true" /></span><div><p className="text-sm font-semibold">{enrollment.clusterName} is connected</p><p className="text-sm text-muted-foreground">The agent checked in. You can continue with the next step.</p></div></div> : <>
        <div className="flex items-start gap-3"><span aria-hidden="true" className="mt-1.5 size-2.5 shrink-0 animate-pulse rounded-full bg-warning" /><div role="status"><p className="text-sm font-semibold">Waiting for {enrollment.clusterName} to connect…</p><p className="text-sm text-muted-foreground">Run these commands against the cluster. This page updates by itself as soon as the agent checks in. The token is single-use and expires at {new Date(enrollment.expiresAt).toLocaleTimeString()}.</p></div></div>
        <div className="relative">
          <pre tabIndex={0} aria-label="Agent install commands" className="overflow-x-auto bg-muted/30 p-3 pr-24 font-mono text-xs leading-5">{commands("••••••••")}</pre>
          <Button type="button" size="sm" variant="outline" className="absolute top-2 right-2" onClick={() => void copy(commands(enrollment.token))}><HugeiconsIcon icon={copied ? Tick02Icon : Copy01Icon} strokeWidth={1.8} aria-hidden="true" />{copied ? "Copied" : "Copy"}</Button>
          <span role="status" aria-live="polite" className="sr-only">{copied ? "Install commands copied to clipboard" : ""}</span>
        </div>
        <p className="text-xs text-muted-foreground">Run from a checkout of the JustCD repository, or point <code>helm</code> at your chart registry. The copied commands include the real token; it is not stored anywhere else and disappears when you leave this page.</p>
      </>}
    </section>}

    <section aria-label="Add a cluster" className="grid gap-4 lg:grid-cols-2">
      <section className="space-y-4">
        <div><h3 className="text-sm font-semibold">Install the agent <Badge variant="info" size="sm" className="ml-1">Recommended</Badge></h3><p className="mt-1 text-sm text-muted-foreground">A small pod connects outbound to JustCD. No inbound firewall rules and no cluster credentials stored here.</p></div>
        <FormField label="Cluster name" htmlFor="onboarding-cluster-name"><Input id="onboarding-cluster-name" value={name} onChange={(event) => setName(event.target.value)} maxLength={80} placeholder="production-eu" /></FormField>
        <FormField label="Namespaces this workspace may deploy to" htmlFor="onboarding-cluster-namespaces" hint="Comma-separated. The agent enforces this allowlist locally. You can change it later."><Input id="onboarding-cluster-namespaces" value={namespaces} onChange={(event) => setNamespaces(event.target.value)} placeholder="apps, staging" /></FormField>
        {error != null && <div role="alert" className="flex items-start justify-between gap-3 text-sm text-destructive"><span>{errorMessage(error)}</span><ErrorDetailsButton error={error} /></div>}
        <Button type="button" onClick={() => void enroll()} loading={busy} loadingText="Creating…" disabled={!name.trim()}>Create cluster and token</Button>
      </section>
      <section className="flex flex-col gap-4">
        <div><h3 className="text-sm font-semibold">Connect the Kubernetes API directly</h3><p className="mt-1 text-sm text-muted-foreground">Use a kubeconfig or service-account token when JustCD can reach the API server. Secret values stay masked and backend-only; the access check only reads discovery and permission information.</p></div>
        <Button render={<Link href={`/workspaces/${workspaceId}/clusters/new`} />} nativeButton={false} variant="outline" className="self-start">Open the connection wizard<HugeiconsIcon icon={ArrowRight01Icon} strokeWidth={1.8} data-icon="inline-end" aria-hidden="true" /></Button>
      </section>
    </section>
  </div>
}
