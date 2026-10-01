"use client"

import Link from "next/link"
import { useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { FormSelect } from "@/components/ui/form-select"
import { FormField } from "@/components/ui-kit"
import { ConnectionDialog } from "@/components/connection-dialog"
import { ConfirmDisclosure } from "@/components/confirm-disclosure"
import { ErrorNotice } from "@/components/workspace-ui"
import { api, apiPost } from "@/lib/api"
import type { Application } from "@/lib/types"

export function BranchTestControls({ application, canManage, disabled, onChanged, onReview, onSettings, open, onOpenChange }: {
  application: Application
  open: boolean
  onOpenChange: (open: boolean) => void
  canManage: boolean
  disabled: boolean
  onChanged: (application: Application) => Promise<void>
  onReview: () => void
  onSettings: () => void
}) {
  const [mode, setMode] = useState("existing")
  const [revision, setRevision] = useState("")
  const [namespace, setNamespace] = useState("")
  const [manifestPath, setManifestPath] = useState("")
  const [confirmed, setConfirmed] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [mergeRequestUrl, setMergeRequestUrl] = useState("")
  const state = application.branchTest

  useEffect(() => {
    if (!state) return
    let active = true
    void api<{ source: { provider: string; apiUrl: string; repository: string; repositoryUrl: string } }>(`/api/v1/applications/${encodeURIComponent(state.parentApplicationId || application.id)}/source-control`).then(({ source }) => {
      let base: URL
      if (source.provider === "gitlab") {
        base = new URL(source.apiUrl)
        base.pathname = `${base.pathname.replace(/\/api\/v4\/?$/, "")}/${source.repository}/-/merge_requests/new`
        base.search = new URLSearchParams({ "merge_request[source_branch]": state.revision, "merge_request[target_branch]": state.baseRevision }).toString()
      } else if (source.provider === "github") {
        base = new URL(`https://github.com/${source.repository}/compare/${encodeURIComponent(state.baseRevision)}...${encodeURIComponent(state.revision)}`)
        base.search = "expand=1"
      } else return
      base.username = ""; base.password = ""
      if (active) setMergeRequestUrl(base.toString())
    }).catch(() => {})
    return () => { active = false }
  }, [application.id, state])

  async function start(event: React.FormEvent) {
    event.preventDefault()
    setBusy(true); setError(null)
    try {
      const updated = await apiPost<Application>(`/api/v1/applications/${encodeURIComponent(application.id)}/branch-test`, { revision: revision.trim(), mode, namespace: namespace.trim(), manifestPath: manifestPath.trim(), confirmExisting: confirmed })
      onOpenChange(false)
      await onChanged(updated)
    } catch (cause) { setError(cause) }
    finally { setBusy(false) }
  }

  return <>
    {state && <section aria-label="Branch test" className="mb-5 rounded-xl border border-primary/30 bg-primary/5 p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0"><p className="break-all text-sm font-semibold text-primary">Testing branch: {state.revision}</p><p className="mt-1 text-sm text-muted-foreground">{state.mode === "existing" ? "Existing environment · normal source reconciliation is paused" : "Isolated preview · manual deployment"}</p></div>
        <div className="flex flex-wrap items-center gap-2">
          {canManage && <Button size="sm" variant="outline" disabled={disabled} onClick={onReview}>Review branch changes</Button>}
          {mergeRequestUrl && <a href={mergeRequestUrl} target="_blank" rel="noreferrer"><Button size="sm" variant="outline">Open PR / MR ↗</Button></a>}
          {state.mode === "existing" && canManage && <ConfirmDisclosure trigger={`Resume ${state.baseRevision}`} triggerVariant="outline" confirmVariant="default" title="Return to the tracked source?" description="This ends the branch override. Review and deploy a fresh plan to restore the tracked source, then resume automatic reconciliation. Database migrations and data changes are not undone." confirmLabel="Restore tracked source" disabled={disabled} onConfirm={async () => {
            try { const updated = await apiPost<Application>(`/api/v1/applications/${encodeURIComponent(application.id)}/branch-test/resume`, {}); await onChanged(updated) }
            catch (cause) { await onChanged(await api<Application>(`/api/v1/applications/${encodeURIComponent(application.id)}`)); throw cause }
          }} />}
          {state.mode === "isolated" && <>{state.parentApplicationId && <Link href={`/applications/${state.parentApplicationId}`} className="text-sm underline underline-offset-4">Parent application</Link>}{canManage && <Button size="sm" variant="outline" disabled={disabled} onClick={onSettings}>Remove preview</Button>}</>}
        </div>
      </div>
    </section>}
    <ConnectionDialog open={open} onOpenChange={onOpenChange} title="Test a branch" description="Choose how to deploy changes before opening a pull or merge request." busy={busy}>
      <form className="space-y-5" onSubmit={(event) => void start(event)}>
        {error != null && <ErrorNotice error={error} />}
        <FormField label="Branch or Git revision" htmlFor="test-branch-revision"><Input id="test-branch-revision" autoFocus value={revision} onChange={(event) => setRevision(event.target.value)} placeholder="feature/my-change" required /></FormField>
        <FormField label="Test environment" htmlFor="test-branch-mode"><FormSelect id="test-branch-mode" value={mode} onValueChange={setMode} items={[{ value: "existing", label: "Deploy branch to existing dev" }, { value: "isolated", label: "Create isolated preview" }]} /></FormField>
        {mode === "existing" ? <div className="space-y-3 rounded-lg border p-4 text-sm"><p>Uses {application.clusterName || application.clusterId} / {application.namespaces.map((binding) => binding.namespace).join(", ")}. Existing credentials and data stay available.</p><p className="text-muted-foreground">Automatic reconciliation is paused. Review the deployment plan before applying. Returning to {application.revision} will not undo database migrations or data changes.</p><label className="flex items-start gap-2"><Checkbox checked={confirmed} onCheckedChange={(value) => setConfirmed(Boolean(value))} /><span>I understand this test changes the shared environment and may modify its data.</span></label></div> : <>
          <FormField label="Dedicated preview namespace" htmlFor="test-branch-namespace"><Input id="test-branch-namespace" value={namespace} onChange={(event) => setNamespace(event.target.value)} placeholder={`${application.name.slice(0, 35)}-preview`} required /></FormField>
          <FormField label="Preview overlay path" htmlFor="test-branch-path" hint="Repository-relative path with preview hostnames and dependencies."><Input id="test-branch-path" value={manifestPath} onChange={(event) => setManifestPath(event.target.value)} placeholder="envs/preview/my-app" required /></FormField>
          <p className="text-sm text-muted-foreground">The preview inherits the workspace cluster credential. Configure preview-safe databases, ingress hosts and secrets in the overlay. External services are not copied or isolated automatically. Remove the preview through its application settings when finished. Cleanup removes its managed resources through a reviewed plan; the namespace and any untracked resources remain.</p>
        </>}
        <div className="flex justify-end gap-2 border-t pt-4"><Button variant="outline" type="button" disabled={busy} onClick={() => onOpenChange(false)}>Cancel</Button><Button type="submit" loading={busy} loadingText="Verifying branch…" disabled={!revision.trim() || (mode === "existing" ? !confirmed : !namespace.trim() || !manifestPath.trim())}>Prepare branch test</Button></div>
      </form>
    </ConnectionDialog>
  </>
}
