"use client"

import Link from "next/link"
import { useParams, useRouter } from "next/navigation"
import { useEffect, useMemo, useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { CheckmarkCircle02Icon, Key01Icon, ServerStack02Icon } from "@hugeicons/core-free-icons"
import { ErrorNotice } from "@/components/workspace-ui"
import { Alert } from "@/components/panelui/alert"
import { Skeleton } from "@/components/ui/skeleton"
import { Section } from "@/components/section"
import { ConnectionSteps } from "@/components/connections/steps"
import { Button } from "@/components/panelui/button"
import { FormSelect } from "@/components/form-select"
import { Input } from "@/components/panelui/input"
import { Textarea } from "@/components/panelui/textarea"
import { Card } from "@/components/panelui/card"
import { ChoiceCards } from "@/components/panelui/choice-cards"
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
      const result = await toast.promise(
        apiPost<{ status: string; commit: string }>(`/api/v1/git-sources/${encodeURIComponent(source.id)}/test?workspaceId=${encodeURIComponent(workspace.id)}`),
        { loading: "Checking repository access…", success: (checked) => `Repository access verified at ${checked.commit.slice(0, 12)}.`, error: null }
      )
      setTestResult({ commit: result.commit })
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
    <PageHeading title="Connect Git source" description={`Reuse or add a repository for ${workspace?.name ?? "this workspace"}. Credentials stay owned by this workspace.`} actions={<Button variant="outline" render={<Link href={`/workspaces/${workspaceID}/connections/git-sources`} />}>Cancel</Button>} />
    {!loading && error && <div className="mb-5"><ErrorNotice error={error} /></div>}
    {loading ? <Skeleton role="status" aria-label="Loading workspace connections" className="h-44 motion-reduce:animate-none" /> : !workspace ? <Alert tone="destructive" variant="plain"><Alert.Icon /><Alert.Title>Workspace unavailable</Alert.Title><Alert.Description>Workspace not found or unavailable to your account.</Alert.Description></Alert> : workspace.role !== "owner" ? <Alert tone="warning" variant="plain"><Alert.Icon /><Alert.Title>Workspace owner access required</Alert.Title><Alert.Description>Workspace owner access is required to connect or configure Git sources.</Alert.Description></Alert> : <div className="grid grid-cols-1 items-start gap-7 lg:grid-cols-[240px_minmax(0,1fr)]">
      <aside className="min-w-0 lg:sticky lg:top-8"><Card size="sm"><Card.Content>
        <div className="mb-6 hidden items-center gap-4 lg:flex"><span aria-hidden="true" className="inline-flex shrink-0 items-center justify-center rounded-2xl bg-primary/10 p-3 cluster-icon-stack h-14 w-12 text-primary lg:h-16 lg:w-14"><HugeiconsIcon icon={ServerStack02Icon} className="size-5" /></span><div><p className="text-sm font-semibold">Git source</p><p className="mt-1 text-sm text-muted-foreground">Three focused steps</p></div></div>
        <ConnectionSteps steps={steps} value={step} highestStep={highestStep} onValueChange={setStep} />
        <div className="mt-6 hidden text-xs leading-5 text-muted-foreground sm:block"><strong className="text-foreground">Private by default.</strong> A repository share never grants access to another workspace’s Git secret.</div>
      </Card.Content></Card></aside>
      <div className="min-w-0">
        <div hidden={step !== 1}><WorkflowPanel icon={ServerStack02Icon} eyebrow="Step 1 of 3" title="How would you like to connect?" description="Reuse a source already available to this workspace, or add a new repository.">
          <div className="space-y-5"><div className="space-y-2"><ChoiceCards columns={1} className="sm:grid-cols-2" aria-label="Connection type" value={mode} onValueChange={(next) => { if (next === "existing" || next === "create") setMode(next) }}>
                <ChoiceCards.Item value="existing"><ChoiceCards.Label>Use existing source</ChoiceCards.Label><ChoiceCards.Description>Use a repository already available to this workspace.</ChoiceCards.Description></ChoiceCards.Item>
                <ChoiceCards.Item value="create"><ChoiceCards.Label>Add new source</ChoiceCards.Label><ChoiceCards.Description>Connect a repository URL and its credentials.</ChoiceCards.Description></ChoiceCards.Item>
              </ChoiceCards><p className="text-sm text-muted-foreground">{mode === "existing" ? "Choose one owned here or accepted from another workspace." : "Connect a Git URL and optionally create a credential in this flow."}</p></div>
            {mode === "existing" && <FormField label="Accessible Git source" htmlFor="existing-git-source"><FormSelect id="existing-git-source" value={selectedSourceId} onValueChange={chooseExistingSource} emptyOption="Select a Git source" items={sources.map((item) => ({ value: item.id, label: `${item.name}${item.shared ? ` · shared by ${item.ownerWorkspaceName}` : " · this workspace"}` }))} /></FormField>}
            {mode === "existing" && !sources.length && <div className="min-w-0"><EmptyState title="No accessible Git sources" description="An owner can accept a shared connection, or you can add a new source." /></div>}
            <WorkflowActions><Button type="button" disabled={mode === "existing" && !selectedSource} onClick={() => { if (mode === "existing" && selectedSource) chooseExistingSource(selectedSource.id); advance(2) }}>Continue</Button></WorkflowActions>
          </div>
        </WorkflowPanel></div>
        <div hidden={step !== 2}><WorkflowPanel icon={Key01Icon} eyebrow="Step 2 of 3" title={mode === "create" ? "Add repository details" : "Review the selected source"} description={mode === "create" ? "Use an HTTPS or SSH URL. Any credential selected here must belong to this workspace." : "This source is already accessible to the workspace. Its owner’s credential remains private."}>
          {mode === "existing" && source ? <div className="space-y-5"><dl className="grid grid-cols-1 gap-4 sm:grid-cols-2"><div className="min-w-0"><dt className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Source</dt><dd className="mt-1 text-xs font-medium">{source.name}</dd></div><div className="min-w-0"><dt className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Ownership</dt><dd className="mt-1 text-xs font-medium">{source.shared ? `Shared by ${source.ownerWorkspaceName}` : "Owned by this workspace"}</dd></div><div className="min-w-0 sm:col-span-2"><dt className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Repository URL</dt><dd className="mt-1 break-all font-mono text-xs">{source.repositoryUrl}</dd></div></dl>
            {!createCredential && <FormField label="Git credential owned by this workspace" htmlFor="existing-git-credential" hint="A shared repository never includes the owner’s credential. Leave blank only for public repositories."><FormSelect id="existing-git-credential" value={sharedCredentialId} onValueChange={setSharedCredentialId} emptyOption="Public repository / no credential" items={credentials.map((item) => ({ value: item.id, label: item.name }))} /></FormField>}
            <Button type="button" variant="outline" onClick={() => setCreateCredential((value) => !value)}>{createCredential ? "Use a saved credential instead" : "Create credential"}</Button>
            <p className="-mt-3 text-sm text-muted-foreground">Credential ownership stays with {workspace.name}, even when the repository is shared.</p>
            {createCredential && <div className="grid grid-cols-1 gap-4 sm:grid-cols-2"><FormField label="Credential name" htmlFor="existing-git-credential-name"><Input id="existing-git-credential-name" value={credentialName} onChange={(event) => setCredentialName(event.target.value)} placeholder="Git deploy token" required /></FormField><FormField label="Credential type" htmlFor="existing-git-credential-kind"><FormSelect id="existing-git-credential-kind" value={credentialKind} onValueChange={(value) => setCredentialKind(value as typeof credentialKind)} items={[{ value: "git-https", label: "HTTPS token" }, { value: "git-ssh", label: "SSH deploy key" }]} /></FormField>{credentialKind === "git-https" ? <><FormField label="Git username" htmlFor="existing-git-username"><Input id="existing-git-username" value={username} onChange={(event) => setUsername(event.target.value)} /></FormField><div className="sm:col-span-2"><FormField label="Access token" htmlFor="existing-git-secret"><Textarea id="existing-git-secret" className="min-h-24 font-mono text-xs" value={secret} onChange={(event) => setSecret(event.target.value)} required /></FormField></div></> : <><div className="sm:col-span-2"><FormField label="Private key" htmlFor="existing-git-secret"><Textarea id="existing-git-secret" className="min-h-24 font-mono text-xs" value={secret} onChange={(event) => setSecret(event.target.value)} required /></FormField></div><div className="sm:col-span-2"><FormField label="Pinned host keys (known_hosts)" htmlFor="existing-git-known-hosts"><Textarea id="existing-git-known-hosts" className="min-h-20 font-mono text-xs" value={knownHosts} onChange={(event) => setKnownHosts(event.target.value)} required /></FormField></div></>}</div>}
            <WorkflowActions back={() => setStep(1)}><Button onClick={() => void saveExistingAccess()} loading={busy} loadingText="Saving access…">Continue</Button></WorkflowActions></div> : <form onSubmit={(event) => void saveRepository(event)} className="space-y-5"><div className="grid grid-cols-1 gap-4 sm:grid-cols-2"><FormField label="Source name" htmlFor="wizard-git-name"><Input id="wizard-git-name" value={name} onChange={(event) => setName(event.target.value)} placeholder="platform-config" required /></FormField><FormField label="Repository URL" htmlFor="wizard-git-url" hint="HTTPS or SSH, such as git@github.com:org/repo.git"><Input id="wizard-git-url" type="text" value={repositoryUrl} onChange={(event) => setRepositoryUrl(event.target.value)} placeholder="https://github.com/org/repo.git" required /></FormField></div>
            {credentials.length > 0 && !createCredential && <FormField label="Workspace Git credential" htmlFor="wizard-git-credential" hint="Credential sharing is independent of repository sharing."><FormSelect id="wizard-git-credential" value={credentialId} onValueChange={setCredentialId} emptyOption="Public repository / no credential" items={credentials.map((item) => ({ value: item.id, label: `${item.name} · ${item.kind === "git-ssh" ? "SSH" : "HTTPS"}` }))} /></FormField>}
            <Button type="button" variant="outline" onClick={() => setCreateCredential((value) => !value)}>{createCredential ? "Use an existing credential" : "Create credential"}</Button>
            <p className="-mt-3 text-sm text-muted-foreground">Secrets are encrypted at rest and never returned to the browser.</p>
            {createCredential && <div className="grid grid-cols-1 gap-4 sm:grid-cols-2"><FormField label="Credential name" htmlFor="wizard-git-credential-name"><Input id="wizard-git-credential-name" value={credentialName} onChange={(event) => setCredentialName(event.target.value)} placeholder="Git deploy token" required /></FormField><FormField label="Credential type" htmlFor="wizard-git-credential-kind"><FormSelect id="wizard-git-credential-kind" value={credentialKind} onValueChange={(value) => setCredentialKind(value as typeof credentialKind)} items={[{ value: "git-https", label: "HTTPS token" }, { value: "git-ssh", label: "SSH deploy key" }]} /></FormField>{credentialKind === "git-https" ? <><FormField label="Git username" htmlFor="wizard-git-username" hint="Optional for providers that use a token-only URL."><Input id="wizard-git-username" value={username} onChange={(event) => setUsername(event.target.value)} /></FormField><div className="sm:col-span-2"><FormField label="Access token" htmlFor="wizard-git-secret"><Textarea id="wizard-git-secret" className="min-h-24 font-mono text-xs" value={secret} onChange={(event) => setSecret(event.target.value)} required /></FormField></div></> : <><div className="sm:col-span-2"><FormField label="Private key" htmlFor="wizard-git-secret"><Textarea id="wizard-git-secret" className="min-h-24 font-mono text-xs" value={secret} onChange={(event) => setSecret(event.target.value)} required /></FormField></div><div className="sm:col-span-2"><FormField label="Pinned host keys (known_hosts)" htmlFor="wizard-git-known-hosts"><Textarea id="wizard-git-known-hosts" className="min-h-20 font-mono text-xs" value={knownHosts} onChange={(event) => setKnownHosts(event.target.value)} required /></FormField></div></>}</div>}
            <WorkflowActions back={() => setStep(1)}><Button type="submit" loading={busy} loadingText="Saving repository…">Continue</Button></WorkflowActions></form>}
        </WorkflowPanel></div>
        <div hidden={step !== 3}><WorkflowPanel icon={testResult ? CheckmarkCircle02Icon : ServerStack02Icon} eyebrow="Step 3 of 3" title={testResult ? "Repository access verified" : "Verify repository access"} description="JustCD performs a read-only fetch to confirm the source and this workspace’s credential can reach the repository.">
          <div className="space-y-5"><dl className="grid grid-cols-1 gap-4 sm:grid-cols-2"><div className="min-w-0"><dt className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Workspace</dt><dd className="mt-1 text-xs font-medium">{workspace.name}</dd></div><div className="min-w-0"><dt className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Git credential</dt><dd className="mt-1 text-xs font-medium">{selectedCredential?.name ?? (source?.credentialId ? "Configured for this source" : "No credential / public source")}</dd></div><div className="min-w-0 sm:col-span-2"><dt className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Repository</dt><dd className="mt-1 break-all font-mono text-xs">{source?.repositoryUrl ?? repositoryUrl}</dd></div></dl>
            {testResult && <Alert tone="success" variant="plain"><Alert.Icon /><Alert.Title>Read access confirmed</Alert.Title><Alert.Description>HEAD <code>{testResult.commit.slice(0, 12)}</code> is reachable.</Alert.Description></Alert>}
            <WorkflowActions><Button variant="outline" onClick={() => void verify()} loading={busy} loadingText="Testing repository…">{testResult ? "Test again" : "Run verification"}</Button><Button disabled={!testResult} onClick={() => router.push(`/workspaces/${workspaceID}/connections/git-sources`)}>Finish connection</Button></WorkflowActions>
          </div>
        </WorkflowPanel></div>
      </div>
    </div>}
  </>
}

function WorkflowPanel({ eyebrow, title, description, children }: { icon: typeof Key01Icon; eyebrow: string; title: string; description: string; children: React.ReactNode }) {
  return <Section surface title={title} description={description} action={<span className="text-xs font-medium text-muted-foreground">{eyebrow}</span>}>{children}</Section>
}

function WorkflowActions({ back, children }: { back?: () => void; children: React.ReactNode }) { return <div className="flex flex-wrap justify-end gap-2 pt-3">{back && <Button type="button" variant="outline" onClick={back}>Back</Button>}{children}</div> }
