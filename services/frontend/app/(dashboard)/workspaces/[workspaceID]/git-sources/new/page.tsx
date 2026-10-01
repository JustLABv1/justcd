"use client"

import Link from "next/link"
import { useParams, useRouter } from "next/navigation"
import { useEffect, useMemo, useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { CheckmarkCircle02Icon, Key01Icon, ServerStack02Icon } from "@hugeicons/core-free-icons"
import { ErrorDetailsButton } from "@/components/error-details"
import { IconStack } from "@/components/reui/icon-stack"
import { Stepper, StepperContent, StepperDescription, StepperIndicator, StepperItem, StepperNav, StepperPanel, StepperSeparator, StepperTitle, StepperTrigger } from "@/components/reui/stepper"
import { Button } from "@/components/ui/button"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { EmptyState, FormField, PageHeading } from "@/components/ui-kit"
import { useToast } from "@/components/toast-provider"
import { api, apiPost, errorMessage } from "@/lib/api"
import type { Credential, GitSource, ListResponse, Workspace, WorkspaceConnectionShare } from "@/lib/types"

const steps = [
  { title: "Connection", description: "Create or reuse a source" },
  { title: "Repository", description: "URL and workspace credentials" },
  { title: "Verify", description: "Check repository access" },
]

export default function ConnectGitSourcePage() {
  const { workspaceID } = useParams<{ workspaceID: string }>()
  const router = useRouter()
  const toast = useToast()
  const [workspace, setWorkspace] = useState<Workspace | null>(null)
  const [sources, setSources] = useState<GitSource[]>([])
  const [shares, setShares] = useState<WorkspaceConnectionShare[]>([])
  const [credentials, setCredentials] = useState<Credential[]>([])
  const [mode, setMode] = useState<"create" | "existing">("create")
  const [selectedSourceId, setSelectedSourceId] = useState("")
  const [source, setSource] = useState<GitSource | null>(null)
  const [credentialId, setCredentialId] = useState("")
  const [sharedCredentialId, setSharedCredentialId] = useState("")
  const [createCredential, setCreateCredential] = useState(false)
  const [credentialName, setCredentialName] = useState("")
  const [credentialKind, setCredentialKind] = useState<"git-https" | "git-ssh">("git-https")
  const [secret, setSecret] = useState("")
  const [knownHosts, setKnownHosts] = useState("")
  const [username, setUsername] = useState("")
  const [name, setName] = useState("")
  const [repositoryUrl, setRepositoryUrl] = useState("")
  const [step, setStep] = useState(1)
  const [highestStep, setHighestStep] = useState(1)
  const [loadedWorkspaceId, setLoadedWorkspaceId] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [testResult, setTestResult] = useState<{ commit: string } | null>(null)
  const loading = loadedWorkspaceId !== workspaceID

  useEffect(() => {
    let active = true
    Promise.all([
      api<ListResponse<Workspace>>("/api/v1/workspaces"),
      api<ListResponse<GitSource>>(`/api/v1/git-sources?workspaceId=${encodeURIComponent(workspaceID)}`),
      api<ListResponse<Credential>>(`/api/v1/credentials?workspaceId=${encodeURIComponent(workspaceID)}`),
      api<ListResponse<WorkspaceConnectionShare>>(`/api/v1/workspace-connection-shares?workspaceId=${encodeURIComponent(workspaceID)}`),
    ]).then(([workspaces, sourceList, credentialList, shareList]) => {
      if (!active) return
      setError(null)
      setWorkspace(workspaces.items.find((item) => item.id === workspaceID) ?? null)
      setSources(sourceList.items)
      setShares(shareList.items)
      setSelectedSourceId(sourceList.items[0]?.id ?? "")
      setCredentials(credentialList.items.filter((item) => item.workspaceId === workspaceID && (item.kind === "git-https" || item.kind === "git-ssh")))
    }).catch((cause) => active && setError(cause)).finally(() => active && setLoadedWorkspaceId(workspaceID))
    return () => { active = false }
  }, [workspaceID])

  const selectedSource = useMemo(() => sources.find((item) => item.id === selectedSourceId) ?? null, [sources, selectedSourceId])
  const selectedCredential = useMemo(() => credentials.find((item) => item.id === credentialId), [credentials, credentialId])

  function advance(next: number) {
    setStep(next)
    setHighestStep((current) => Math.max(current, next))
    setError(null)
  }

  async function saveRepository(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!workspace) return
    setBusy(true)
    setError(null)
    try {
      let savedCredentialId = credentialId
      if (createCredential) {
        const createdCredential = await apiPost<Credential>("/api/v1/credentials", {
          workspaceId: workspace.id,
          name: credentialName,
          kind: credentialKind,
          secret: credentialKind === "git-ssh" ? { privateKey: secret, knownHosts } : { token: secret, ...(username.trim() ? { username: username.trim() } : {}) },
        })
        setCredentials((items) => [...items, createdCredential])
        savedCredentialId = createdCredential.id
        toast.success("Workspace Git credential encrypted and saved.")
      }
      const created = await apiPost<GitSource>("/api/v1/git-sources", {
        workspaceId: workspace.id,
        name,
        repositoryUrl,
        ...(savedCredentialId ? { credentialId: savedCredentialId } : {}),
      })
      setSources((items) => [...items, created])
      setSource(created)
      toast.success("Git source connected to this workspace.")
      advance(3)
    } catch (cause) {
      setError(cause)
    } finally {
      setBusy(false)
    }
  }

  async function verify() {
    if (!workspace || !source) return
    setBusy(true)
    setError(null)
    try {
      const result = await apiPost<{ status: string; commit: string }>(`/api/v1/git-sources/${encodeURIComponent(source.id)}/test?workspaceId=${encodeURIComponent(workspace.id)}`)
      setTestResult({ commit: result.commit })
      toast.success(`Repository access verified at ${result.commit.slice(0, 12)}.`)
    } catch (cause) {
      setError(cause)
      toast.error(errorMessage(cause), cause)
    } finally {
      setBusy(false)
    }
  }

  async function saveExistingAccess() {
    if (!workspace || !source) return
    setBusy(true)
    setError(null)
    try {
      let savedCredentialId = sharedCredentialId
      if (createCredential) {
        const createdCredential = await apiPost<Credential>("/api/v1/credentials", {
          workspaceId: workspace.id,
          name: credentialName,
          kind: credentialKind,
          secret: credentialKind === "git-ssh" ? { privateKey: secret, knownHosts } : { token: secret, ...(username.trim() ? { username: username.trim() } : {}) },
        })
        setCredentials((items) => [...items, createdCredential])
        savedCredentialId = createdCredential.id
        toast.success("Workspace Git credential encrypted and saved.")
      }
      if (source.shared) {
        const share = shares.find((item) => item.kind === "git-source" && item.resourceId === source.id && item.status === "accepted" && item.targetWorkspaceId === workspace.id)
        if (!share) throw new Error("The accepted Git source offer could not be found. Refresh the shared connections page and try again.")
        await api(`/api/v1/workspace-connection-shares/${encodeURIComponent(share.id)}/credential`, {
          method: "PUT",
          body: JSON.stringify({ workspaceId: workspace.id, credentialId: savedCredentialId || null }),
        })
      } else {
        const updated = await api<GitSource>(`/api/v1/git-sources/${encodeURIComponent(source.id)}`, {
          method: "PUT",
          body: JSON.stringify({ name: source.name, repositoryUrl: source.repositoryUrl, credentialId: savedCredentialId || null }),
        })
        setSources((items) => items.map((item) => item.id === updated.id ? updated : item))
        setSource(updated)
      }
      toast.success("Workspace Git access saved.")
      advance(3)
    } catch (cause) {
      setError(cause)
    } finally {
      setBusy(false)
    }
  }

  function chooseExistingSource(value: string) {
    setSelectedSourceId(value)
    const selected = sources.find((item) => item.id === value) ?? null
    setSource(selected)
    const share = selected?.shared ? shares.find((item) => item.kind === "git-source" && item.resourceId === selected.id && item.targetWorkspaceId === workspaceID && item.status === "accepted") : undefined
    setSharedCredentialId(share?.credentialId ?? selected?.credentialId ?? "")
    setCreateCredential(false)
  }

  return <>
    <PageHeading title="Connect a Git source" description={`A single guided flow to reuse or add a repository for ${workspace?.name ?? "this workspace"}. Credentials stay owned by this workspace.`} actions={<Link href={`/workspaces/${workspaceID}/connections/git-sources`}><Button variant="outline">Exit workflow</Button></Link>} />
    {!loading && error && <div role="alert" className="mb-5 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-destructive/20 bg-destructive/5 p-4 text-sm text-destructive"><span>{errorMessage(error)}</span><ErrorDetailsButton error={error} /></div>}
    {loading ? <div role="status" className="h-44 animate-pulse rounded-xl border bg-muted/40 motion-reduce:animate-none" /> : !workspace ? <div role="alert" className="rounded-xl border bg-card p-6 text-sm text-muted-foreground">Workspace not found or unavailable to your account.</div> : workspace.role !== "owner" ? <div role="alert" className="rounded-xl border bg-card p-6 text-sm text-muted-foreground">Workspace owner access is required to connect or configure Git sources.</div> : <Stepper orientation="vertical" value={step} onValueChange={(value) => { if (value <= highestStep) setStep(value) }} className="grid items-start gap-7 lg:grid-cols-[240px_minmax(0,1fr)]">
      <aside className="rounded-2xl border bg-card p-4 sm:p-5 lg:sticky lg:top-8">
        <div className="mb-4 flex items-center gap-4 lg:mb-6"><IconStack className="cluster-icon-stack h-14 w-12 text-primary lg:h-16 lg:w-14"><HugeiconsIcon icon={ServerStack02Icon} className="size-5" /></IconStack><div><p className="text-sm font-semibold">Git source</p><p className="mt-1 text-sm text-muted-foreground">Three focused steps</p></div></div>
        <StepperNav className="grid w-full grid-cols-3 gap-1 lg:flex lg:grid-cols-none lg:gap-0">{steps.map((item, index) => <StepperItem key={item.title} step={index + 1} disabled={index + 1 > highestStep} className="items-center! justify-start! lg:items-start!"><StepperTrigger className="w-full flex-col items-center rounded-lg px-1 py-2 text-center transition-[background-color,transform] duration-150 motion-reduce:transition-none active:scale-[0.98] motion-reduce:active:scale-100 data-[state=active]:bg-muted lg:flex-row lg:items-start lg:px-2 lg:text-left"><StepperIndicator>{index + 1}</StepperIndicator><span className="min-w-0"><StepperTitle className="text-xs lg:text-sm">{item.title}</StepperTitle><StepperDescription className="mt-1 hidden text-xs leading-4 lg:block">{item.description}</StepperDescription></span></StepperTrigger>{index < steps.length - 1 && <StepperSeparator className="ml-[19px] hidden h-6! lg:block" />}</StepperItem>)}</StepperNav>
        <div className="mt-6 hidden rounded-xl bg-muted/50 p-3 text-xs leading-5 text-muted-foreground sm:block"><strong className="text-foreground">Private by default.</strong> A repository share never grants access to another workspace’s Git secret.</div>
      </aside>
      <StepperPanel className="min-w-0">
        <StepperContent value={1}><WorkflowPanel icon={ServerStack02Icon} eyebrow="Step 1 of 3" title="How would you like to connect?" description="Reuse a source already available to this workspace, or add a new repository.">
          <div className="space-y-5"><div className="grid gap-3 sm:grid-cols-2"><button type="button" aria-pressed={mode === "existing"} onClick={() => setMode("existing")} className={`rounded-xl border p-4 text-left transition-[border-color,background-color,transform] duration-150 motion-reduce:transition-none hover:border-primary/40 active:scale-[0.99] motion-reduce:active:scale-100 ${mode === "existing" ? "border-primary bg-primary/5" : "bg-card"}`}><span className="block text-sm font-semibold">Use an accessible source</span><span className="mt-1 block text-xs leading-5 text-muted-foreground">Choose one owned here or accepted from another workspace.</span></button><button type="button" aria-pressed={mode === "create"} onClick={() => setMode("create")} className={`rounded-xl border p-4 text-left transition-[border-color,background-color,transform] duration-150 motion-reduce:transition-none hover:border-primary/40 active:scale-[0.99] motion-reduce:active:scale-100 ${mode === "create" ? "border-primary bg-primary/5" : "bg-card"}`}><span className="block text-sm font-semibold">Add a new source</span><span className="mt-1 block text-xs leading-5 text-muted-foreground">Connect a Git URL and optionally create credentials in this flow.</span></button></div>
            {mode === "existing" && <FormField label="Accessible Git source" htmlFor="existing-git-source"><FormSelect id="existing-git-source" value={selectedSourceId} onValueChange={chooseExistingSource} emptyOption="Select a Git source" items={sources.map((item) => ({ value: item.id, label: `${item.name}${item.shared ? ` · shared by ${item.ownerWorkspaceName}` : " · this workspace"}` }))} /></FormField>}
            {mode === "existing" && !sources.length && <div className="rounded-xl border border-dashed"><EmptyState title="No accessible Git sources" description="An owner can accept a share, or you can add a new source." /></div>}
            <WorkflowActions><Button type="button" disabled={mode === "existing" && !selectedSource} onClick={() => { if (mode === "existing" && selectedSource) chooseExistingSource(selectedSource.id); advance(2) }}>Continue</Button></WorkflowActions>
          </div>
        </WorkflowPanel></StepperContent>
        <StepperContent value={2}><WorkflowPanel icon={Key01Icon} eyebrow="Step 2 of 3" title={mode === "create" ? "Add repository details" : "Review the selected source"} description={mode === "create" ? "Use an HTTPS or SSH URL. Any credential selected here must belong to this workspace." : "This source is already accessible to the workspace. Its owner’s credential remains private."}>
          {mode === "existing" && source ? <div className="space-y-5"><dl className="grid gap-px overflow-hidden rounded-xl border bg-border sm:grid-cols-2"><div className="bg-card p-4"><dt className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Source</dt><dd className="mt-1 text-xs font-medium">{source.name}</dd></div><div className="bg-card p-4"><dt className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Ownership</dt><dd className="mt-1 text-xs font-medium">{source.shared ? `Shared by ${source.ownerWorkspaceName}` : "Owned by this workspace"}</dd></div><div className="bg-card p-4 sm:col-span-2"><dt className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Repository URL</dt><dd className="mt-1 break-all font-mono text-xs">{source.repositoryUrl}</dd></div></dl>
            {!createCredential && <FormField label="Git credential owned by this workspace" htmlFor="existing-git-credential" hint="A shared repository never includes the owner’s credential. Leave blank only for public repositories."><FormSelect id="existing-git-credential" value={sharedCredentialId} onValueChange={setSharedCredentialId} emptyOption="Public repository / no credential" items={credentials.map((item) => ({ value: item.id, label: item.name }))} /></FormField>}
            <button type="button" onClick={() => setCreateCredential((value) => !value)} className="w-full rounded-xl border border-dashed p-4 text-left transition-[border-color,background-color,transform] duration-150 motion-reduce:transition-none hover:border-primary/40 hover:bg-primary/3 active:scale-[0.99] motion-reduce:active:scale-100"><span className="block text-xs font-semibold">{createCredential ? "Use a saved credential instead" : "Create a workspace Git credential"}</span><span className="mt-1 block text-xs text-muted-foreground">Credential ownership stays with {workspace.name}, even when the repository is shared.</span></button>
            {createCredential && <div className="grid gap-4 rounded-xl bg-muted/30 p-4 sm:grid-cols-2"><FormField label="Credential name" htmlFor="existing-git-credential-name"><Input id="existing-git-credential-name" value={credentialName} onChange={(event) => setCredentialName(event.target.value)} placeholder="Git deploy token" required /></FormField><FormField label="Credential type" htmlFor="existing-git-credential-kind"><FormSelect id="existing-git-credential-kind" value={credentialKind} onValueChange={(value) => setCredentialKind(value as typeof credentialKind)} items={[{ value: "git-https", label: "HTTPS token" }, { value: "git-ssh", label: "SSH deploy key" }]} /></FormField>{credentialKind === "git-https" ? <><FormField label="Git username" htmlFor="existing-git-username"><Input id="existing-git-username" value={username} onChange={(event) => setUsername(event.target.value)} /></FormField><div className="sm:col-span-2"><FormField label="Access token" htmlFor="existing-git-secret"><Textarea id="existing-git-secret" className="min-h-24 font-mono text-xs" value={secret} onChange={(event) => setSecret(event.target.value)} required /></FormField></div></> : <><div className="sm:col-span-2"><FormField label="Private key" htmlFor="existing-git-secret"><Textarea id="existing-git-secret" className="min-h-24 font-mono text-xs" value={secret} onChange={(event) => setSecret(event.target.value)} required /></FormField></div><div className="sm:col-span-2"><FormField label="Pinned known_hosts" htmlFor="existing-git-known-hosts"><Textarea id="existing-git-known-hosts" className="min-h-20 font-mono text-xs" value={knownHosts} onChange={(event) => setKnownHosts(event.target.value)} required /></FormField></div></>}</div>}
            <WorkflowActions back={() => setStep(1)}><Button onClick={() => void saveExistingAccess()} loading={busy} loadingText="Saving access…">Save and verify</Button></WorkflowActions></div> : <form onSubmit={(event) => void saveRepository(event)} className="space-y-5"><div className="grid gap-4 sm:grid-cols-2"><FormField label="Source name" htmlFor="wizard-git-name"><Input id="wizard-git-name" value={name} onChange={(event) => setName(event.target.value)} placeholder="platform-config" required /></FormField><FormField label="Repository URL" htmlFor="wizard-git-url" hint="HTTPS or SSH, such as git@github.com:org/repo.git"><Input id="wizard-git-url" type="text" value={repositoryUrl} onChange={(event) => setRepositoryUrl(event.target.value)} placeholder="https://github.com/org/repo.git" required /></FormField></div>
            {credentials.length > 0 && !createCredential && <FormField label="Workspace Git credential" htmlFor="wizard-git-credential" hint="Credential sharing is independent of repository sharing."><FormSelect id="wizard-git-credential" value={credentialId} onValueChange={setCredentialId} emptyOption="Public repository / no credential" items={credentials.map((item) => ({ value: item.id, label: `${item.name} · ${item.kind === "git-ssh" ? "SSH" : "HTTPS"}` }))} /></FormField>}
            <button type="button" onClick={() => setCreateCredential((value) => !value)} className="w-full rounded-xl border border-dashed p-4 text-left transition-[border-color,background-color,transform] duration-150 motion-reduce:transition-none hover:border-primary/40 hover:bg-primary/3 active:scale-[0.99] motion-reduce:active:scale-100"><span className="block text-xs font-semibold">{createCredential ? "Use an existing credential or public repository" : "Create a Git credential in this flow"}</span><span className="mt-1 block text-xs text-muted-foreground">Secrets are encrypted at rest and never returned to the browser.</span></button>
            {createCredential && <div className="grid gap-4 rounded-xl bg-muted/30 p-4 sm:grid-cols-2"><FormField label="Credential name" htmlFor="wizard-git-credential-name"><Input id="wizard-git-credential-name" value={credentialName} onChange={(event) => setCredentialName(event.target.value)} placeholder="Git deploy token" required /></FormField><FormField label="Credential type" htmlFor="wizard-git-credential-kind"><FormSelect id="wizard-git-credential-kind" value={credentialKind} onValueChange={(value) => setCredentialKind(value as typeof credentialKind)} items={[{ value: "git-https", label: "HTTPS token" }, { value: "git-ssh", label: "SSH deploy key" }]} /></FormField>{credentialKind === "git-https" ? <><FormField label="Git username" htmlFor="wizard-git-username" hint="Optional for providers that use a token-only URL."><Input id="wizard-git-username" value={username} onChange={(event) => setUsername(event.target.value)} /></FormField><div className="sm:col-span-2"><FormField label="Access token" htmlFor="wizard-git-secret"><Textarea id="wizard-git-secret" className="min-h-24 font-mono text-xs" value={secret} onChange={(event) => setSecret(event.target.value)} required /></FormField></div></> : <><div className="sm:col-span-2"><FormField label="Private key" htmlFor="wizard-git-secret"><Textarea id="wizard-git-secret" className="min-h-24 font-mono text-xs" value={secret} onChange={(event) => setSecret(event.target.value)} required /></FormField></div><div className="sm:col-span-2"><FormField label="Pinned known_hosts" htmlFor="wizard-git-known-hosts"><Textarea id="wizard-git-known-hosts" className="min-h-20 font-mono text-xs" value={knownHosts} onChange={(event) => setKnownHosts(event.target.value)} required /></FormField></div></>}</div>}
            <WorkflowActions back={() => setStep(1)}><Button type="submit" loading={busy} loadingText="Saving repository…">Connect and continue</Button></WorkflowActions></form>}
        </WorkflowPanel></StepperContent>
        <StepperContent value={3}><WorkflowPanel icon={testResult ? CheckmarkCircle02Icon : ServerStack02Icon} eyebrow="Step 3 of 3" title={testResult ? "Repository access verified" : "Verify repository access"} description="JustCD performs a read-only fetch to confirm the source and this workspace’s credential can reach the repository.">
          <div className="space-y-5"><dl className="grid gap-px overflow-hidden rounded-xl border bg-border sm:grid-cols-2"><div className="bg-card p-4"><dt className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Workspace</dt><dd className="mt-1 text-xs font-medium">{workspace.name}</dd></div><div className="bg-card p-4"><dt className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Git credential</dt><dd className="mt-1 text-xs font-medium">{selectedCredential?.name ?? (source?.credentialId ? "Configured for this source" : "No credential / public source")}</dd></div><div className="bg-card p-4 sm:col-span-2"><dt className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Repository</dt><dd className="mt-1 break-all font-mono text-xs">{source?.repositoryUrl ?? repositoryUrl}</dd></div></dl>
            {testResult && <div role="status" className="rounded-xl border border-emerald-500/30 bg-emerald-500/5 p-4 text-xs"><p className="font-semibold text-emerald-800 dark:text-emerald-300">Read access confirmed</p><p className="mt-1 text-muted-foreground">HEAD <code>{testResult.commit.slice(0, 12)}</code> is reachable.</p></div>}
            <WorkflowActions><Button variant="outline" onClick={() => void verify()} loading={busy} loadingText="Testing repository…">{testResult ? "Test again" : "Run verification"}</Button><Button disabled={!testResult} onClick={() => router.push(`/workspaces/${workspaceID}/connections/git-sources`)}>Finish connection</Button></WorkflowActions>
          </div>
        </WorkflowPanel></StepperContent>
      </StepperPanel>
    </Stepper>}
  </>
}

function WorkflowPanel({ icon, eyebrow, title, description, children }: { icon: typeof Key01Icon; eyebrow: string; title: string; description: string; children: React.ReactNode }) {
  return <section className="workflow-panel overflow-hidden rounded-2xl border bg-card"><header className="flex items-start gap-4 border-b p-5 sm:p-6"><span className="grid size-10 shrink-0 place-items-center rounded-xl bg-primary/10 text-primary"><HugeiconsIcon icon={icon} className="size-5" /></span><div><p className="text-sm font-semibold uppercase tracking-[0.14em] text-muted-foreground">{eyebrow}</p><h2 className="mt-1 text-lg font-semibold tracking-tight">{title}</h2><p className="mt-1 max-w-2xl text-sm leading-5 text-muted-foreground">{description}</p></div></header><div className="p-5 sm:p-6">{children}</div></section>
}

function WorkflowActions({ back, children }: { back?: () => void; children: React.ReactNode }) { return <div className="flex flex-wrap justify-end gap-2 border-t pt-5">{back && <Button type="button" variant="outline" onClick={back}>Back</Button>}{children}</div> }
