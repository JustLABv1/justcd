"use client"

import Link from "next/link"
import { useState } from "react"
import { RowActions } from "@/components/action-menu"
import type { IgnorePrefill, NewIgnore } from "@/components/application/helpers"
import { ConfirmDisclosure } from "@/components/confirm-disclosure"
import { ErrorDetailsButton } from "@/components/error-details"
import { Badge } from "@/components/reui/badge"
import { Button } from "@/components/ui/button"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Textarea } from "@/components/ui/textarea"
import { DangerZone, FormField, Panel, SwitchField } from "@/components/ui-kit"
import { errorMessage } from "@/lib/api"
import type { Application, IgnoreRule, IgnoreSelector } from "@/lib/types"

function ruleLabel(rule: IgnoreRule) {
  return `${rule.identity.kind} ${rule.identity.namespace ? `${rule.identity.namespace}/` : ""}${rule.identity.name}`
}

function RenderSettings({ application, kustomization, kustomizationError, canApprove, busy, hasPendingOperation, saving, onSave }: {
  application: Application
  kustomization: { namespace: string; commit: string } | null
  kustomizationError: unknown | null
  canApprove: boolean
  busy: boolean
  hasPendingOperation: boolean
  saving: boolean
  onSave: (settings: { kustomizeHelmEnabled: boolean; kustomizeNamespaceOverride: boolean }) => Promise<void>
}) {
  const [helm, setHelm] = useState(application.kustomizeHelmEnabled)
  const [namespaceOverride, setNamespaceOverride] = useState(application.kustomizeNamespaceOverride)
  const [seen, setSeen] = useState({ helm: application.kustomizeHelmEnabled, namespaceOverride: application.kustomizeNamespaceOverride })
  // Re-sync the drafts whenever the saved settings change (after a save or reload).
  if (seen.helm !== application.kustomizeHelmEnabled || seen.namespaceOverride !== application.kustomizeNamespaceOverride) {
    setSeen({ helm: application.kustomizeHelmEnabled, namespaceOverride: application.kustomizeNamespaceOverride })
    setHelm(application.kustomizeHelmEnabled)
    setNamespaceOverride(application.kustomizeNamespaceOverride)
  }
  const locked = Boolean(application.repositoryConfigurationId) || !canApprove || busy || hasPendingOperation
  const dirty = helm !== application.kustomizeHelmEnabled || namespaceOverride !== application.kustomizeNamespaceOverride
  return <Panel title="Render settings" description={application.applicationGroupId ? "Kustomize settings are shared by this deployment group." : "Control how Kustomize builds this application's manifests."}>
    <div className="space-y-4 p-5">
      <div className="rounded-lg border bg-muted/30 p-3 text-sm"><p className="font-medium">Namespace from Git</p><p className="mt-1 font-mono text-muted-foreground">{kustomization ? kustomization.namespace || "Not set in kustomization" : kustomizationError ? errorMessage(kustomizationError) : "Checking kustomization…"}</p>{kustomizationError != null && <div className="mt-2"><ErrorDetailsButton error={kustomizationError} /></div>}<p className="mt-1 text-muted-foreground">JustCD target: {application.namespaces.map((binding) => binding.namespace).join(", ")}</p></div>
      {application.applicationGroupId
        ? <div className="rounded-lg border bg-muted/30 p-3 text-sm">Namespace transforms and Helm chart rendering are configured for every target in the <Link href={`/application-groups/${application.applicationGroupId}`} className="font-medium text-primary hover:underline">deployment group</Link>.</div>
        : <>
          {application.namespaces.length > 0
            ? <SwitchField id="render-namespace-transform" label="Apply namespace transform" description="Build the selected Kustomize path once per namespace with granted access and set namespace metadata to that target. This can create or delete resources on the next sync, so review a new plan before applying." checked={namespaceOverride} onCheckedChange={setNamespaceOverride} disabled={locked} />
            : <p className="text-sm text-muted-foreground">Grant namespace access to at least one namespace to configure a namespace transform.</p>}
          <SwitchField id="render-helm-charts" label="Enable Helm charts in Kustomize" description="Permits helmCharts from this Git source. Helm may fetch pinned charts from public HTTPS repositories while building the plan." checked={helm} onCheckedChange={setHelm} disabled={locked} />
          {canApprove && !application.repositoryConfigurationId && <Button size="sm" loading={saving} loadingText="Saving settings…" disabled={busy || hasPendingOperation || !dirty} onClick={() => void onSave({ kustomizeHelmEnabled: helm, kustomizeNamespaceOverride: namespaceOverride })}>Save changes</Button>}
        </>}
    </div>
  </Panel>
}

export function SettingsTab({ application, kustomization, kustomizationError, canApprove, busy, hasPendingOperation, pendingAction, rulesLoading, ignoreRules, ignoreSelectors, managedResourceCount, deletionApprovalCount, prefill, onSaveRenderSettings, onAddIgnore, onRemoveRule, onRemoveSelector, onDeleteApplication, onCancelDecommission }: {
  application: Application
  kustomization: { namespace: string; commit: string } | null
  kustomizationError: unknown | null
  canApprove: boolean
  busy: boolean
  hasPendingOperation: boolean
  pendingAction: string
  rulesLoading: boolean
  ignoreRules: IgnoreRule[]
  ignoreSelectors: IgnoreSelector[]
  managedResourceCount: number
  deletionApprovalCount: number
  prefill: IgnorePrefill | null
  onSaveRenderSettings: (settings: { kustomizeHelmEnabled: boolean; kustomizeNamespaceOverride: boolean }) => Promise<void>
  /** Resolves true when the exclusion was saved. */
  onAddIgnore: (ignore: NewIgnore) => Promise<boolean>
  onRemoveRule: (rule: IgnoreRule) => Promise<void>
  onRemoveSelector: (selector: IgnoreSelector) => Promise<void>
  onDeleteApplication: (resourcePolicy: string) => Promise<void>
  onCancelDecommission: () => void
}) {
  const [ignoreMode, setIgnoreMode] = useState<string>(prefill?.mode ?? "kind")
  const [ignoreVersion, setIgnoreVersion] = useState(prefill?.version ?? "")
  const [ignoreKind, setIgnoreKind] = useState(prefill?.kind ?? "")
  const [ignoreNamespace, setIgnoreNamespace] = useState(prefill?.namespace ?? "")
  const [ignoreName, setIgnoreName] = useState(prefill?.name ?? "")
  const [ignoreLabelKey, setIgnoreLabelKey] = useState("")
  const [ignoreLabelValue, setIgnoreLabelValue] = useState("")
  const [ignoreReason, setIgnoreReason] = useState(prefill?.reason ?? "")
  const [appliedPrefill, setAppliedPrefill] = useState(prefill?.nonce)
  const [deletePolicy, setDeletePolicy] = useState(application.decommissioning ? "delete" : "keep")
  const [wasDecommissioning, setWasDecommissioning] = useState(application.decommissioning)

  // A new prefill request (for example "Configure exclusion" elsewhere on the page) overwrites the form.
  if (prefill && prefill.nonce !== appliedPrefill) {
    setAppliedPrefill(prefill.nonce)
    setIgnoreMode(prefill.mode)
    if (prefill.version !== undefined) setIgnoreVersion(prefill.version)
    if (prefill.kind !== undefined) setIgnoreKind(prefill.kind)
    if (prefill.namespace !== undefined) setIgnoreNamespace(prefill.namespace)
    if (prefill.name !== undefined) setIgnoreName(prefill.name)
    if (prefill.reason !== undefined) setIgnoreReason(prefill.reason)
  }
  if (application.decommissioning !== wasDecommissioning) {
    setWasDecommissioning(application.decommissioning)
    if (application.decommissioning) setDeletePolicy("delete")
  }

  const reasonLength = ignoreReason.trim().length
  const reasonInvalid = reasonLength > 0 && reasonLength < 5
  async function addIgnore() {
    const saved = await onAddIgnore({ mode: ignoreMode, version: ignoreVersion, kind: ignoreKind, namespace: ignoreNamespace, name: ignoreName, labelKey: ignoreLabelKey, labelValue: ignoreLabelValue, reason: ignoreReason })
    if (saved) { setIgnoreReason(""); setIgnoreName("") }
  }

  return <div className="space-y-5">
    {application.renderer === "kustomize" && <RenderSettings application={application} kustomization={kustomization} kustomizationError={kustomizationError} canApprove={canApprove} busy={busy} hasPendingOperation={hasPendingOperation} saving={pendingAction === "render-settings"} onSave={onSaveRenderSettings} />}
    <Panel title="Ignored resources" description="Exclude an exact resource, an entire Kubernetes kind, or resources with a label. Exclusions apply before cluster discovery and live reads.">
      {application.repositoryConfigurationId && <p className="px-5 pt-5 text-sm text-muted-foreground">Define Git-managed exclusions in <code>spec.ignoreResources</code> in <code>{application.configurationPath}</code>. Additional manual exclusions remain available below. <a href="https://github.com/justlabv1/justcd/blob/main/docs/repository-applications.md#ignored-resources" target="_blank" rel="noreferrer" className="underline underline-offset-4">File format and examples</a></p>}
      <div className="space-y-5 p-5">
        {canApprove && <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <FormField label="Exclude by" htmlFor="ignore-mode"><FormSelect id="ignore-mode" value={ignoreMode} onValueChange={setIgnoreMode} items={[{ value: "kind", label: "API kind" }, { value: "label", label: "Label" }, { value: "resource", label: "Exact resource" }]} /></FormField>
          {(ignoreMode === "kind" || ignoreMode === "resource") && <><FormField label="API version" htmlFor="ignore-api-version"><Input id="ignore-api-version" placeholder="secrets.hashicorp.com/v1beta1" value={ignoreVersion} onChange={(event) => setIgnoreVersion(event.target.value)} /></FormField><FormField label="Kind" htmlFor="ignore-kind"><Input id="ignore-kind" placeholder="VaultStaticSecret" value={ignoreKind} onChange={(event) => setIgnoreKind(event.target.value)} /></FormField></>}
          {ignoreMode === "resource" && <><FormField label="Namespace" htmlFor="ignore-namespace"><Input id="ignore-namespace" placeholder={application.namespaces[0]?.namespace ?? "namespace"} value={ignoreNamespace} onChange={(event) => setIgnoreNamespace(event.target.value)} /></FormField><FormField label="Resource name" htmlFor="ignore-name"><Input id="ignore-name" placeholder="ntfy-secret" value={ignoreName} onChange={(event) => setIgnoreName(event.target.value)} /></FormField></>}
          {ignoreMode === "label" && <><FormField label="Label key" htmlFor="ignore-label-key"><Input id="ignore-label-key" placeholder="justcd.io/exclude" value={ignoreLabelKey} onChange={(event) => setIgnoreLabelKey(event.target.value)} /></FormField><FormField label="Label value" htmlFor="ignore-label-value"><Input id="ignore-label-value" placeholder="true" value={ignoreLabelValue} onChange={(event) => setIgnoreLabelValue(event.target.value)} /></FormField></>}
          <div className="sm:col-span-2 lg:col-span-4"><FormField label="Reason" htmlFor="ignore-reason"><Textarea id="ignore-reason" aria-invalid={reasonInvalid} aria-describedby="ignore-reason-help" value={ignoreReason} onChange={(event) => setIgnoreReason(event.target.value)} placeholder="Managed by another controller or outside this service account's permissions" maxLength={500} /><p id="ignore-reason-help" className={reasonInvalid ? "text-sm text-destructive" : "text-sm text-muted-foreground"}>{reasonInvalid ? "Enter at least 5 characters to add the exclusion." : "Required · 5–500 characters"}</p></FormField></div>
          <Button size="sm" className="w-fit sm:col-span-2" loading={pendingAction === "add-ignore"} loadingText="Saving exclusion…" onClick={() => void addIgnore()} disabled={busy || hasPendingOperation || reasonLength < 5 || ((ignoreMode === "kind" || ignoreMode === "resource") && (!ignoreVersion.trim() || !ignoreKind.trim())) || (ignoreMode === "resource" && !ignoreName.trim()) || (ignoreMode === "label" && !ignoreLabelKey.trim())}>Add exclusion</Button>
          {pendingAction === "add-ignore" && <p role="status" aria-live="polite" className="self-center text-sm text-muted-foreground sm:col-span-2">Saving the exclusion and invalidating reviewed plans…</p>}
        </div>}
        <p className="text-sm text-muted-foreground">Ignored resources stay visible here and are not read, updated, or deleted by normal plans. Existing plans become stale when these rules change.</p>
        {rulesLoading
          ? <div role="status" aria-label="Loading exclusions" className="space-y-3"><span className="sr-only">Loading exclusions…</span><Skeleton className="h-5 w-2/3 max-w-72 motion-reduce:animate-none" /><Skeleton className="h-4 w-full max-w-lg motion-reduce:animate-none" /></div>
          : (ignoreSelectors.length > 0 || ignoreRules.length > 0) && <ul className="divide-y rounded-lg border">
            {ignoreSelectors.map((rule) => {
              const label = rule.kind ? `${rule.apiVersion} · ${rule.kind}` : "Any kind"
              return <li key={rule.id} className="flex flex-wrap items-center gap-3 px-4 py-3 text-sm">
                <span className="min-w-0 flex-1"><strong>{label}</strong>{rule.labelKey && <span className="ml-2 font-mono text-muted-foreground">{rule.labelKey}={rule.labelValue}</span>}<span className="mt-1 block text-muted-foreground">{rule.reason}</span></span>
                {rule.managedByGit ? <Badge variant="outline" radius="full">Managed by Git</Badge> : canApprove && <RowActions label={`exclusion ${label}`} items={[{ label: "Remove exclusion", destructive: true, disabled: busy || hasPendingOperation, confirm: { title: `Remove exclusion ${label}?`, description: "Future plans may manage matching resources again.", confirmLabel: "Remove exclusion", onConfirm: () => onRemoveSelector(rule) } }]} />}
              </li>
            })}
            {ignoreRules.map((rule) => <li key={rule.id} className="flex flex-wrap items-center gap-3 px-4 py-3 text-sm">
              <span className="min-w-0 flex-1"><strong>{ruleLabel(rule)}</strong><span className="ml-2 font-mono text-muted-foreground">{rule.path || "whole resource"}</span><span className="mt-1 block text-muted-foreground">{rule.reason}</span></span>
              {rule.managedByGit ? <Badge variant="outline" radius="full">Managed by Git</Badge> : canApprove && <RowActions label={`ignore rule for ${ruleLabel(rule)}`} items={[{ label: "Remove ignore rule", destructive: true, disabled: busy || hasPendingOperation, confirm: { title: `Remove ignore rule for ${ruleLabel(rule)}?`, description: "Future plans may manage this resource again.", confirmLabel: "Remove ignore rule", onConfirm: () => onRemoveRule(rule) } }]} />}
            </li>)}
          </ul>}
      </div>
    </Panel>
    {canApprove && <DangerZone title="Delete application" description="Choose whether JustCD keeps or removes the resources it manages.">
      <p className="min-w-0 flex-1 text-sm text-muted-foreground">{`${managedResourceCount} managed resource${managedResourceCount === 1 ? "" : "s"} currently recorded.`} Keeping them removes ownership tracking from JustCD.{application.decommissioning ? " Deletion is already in progress." : ""}</p>
      <div className="flex flex-wrap items-center gap-2">
        {application.decommissioning && <Button type="button" size="sm" variant="outline" disabled={busy || hasPendingOperation} onClick={onCancelDecommission}>Cancel deletion</Button>}
        <ConfirmDisclosure trigger="Delete application" title={`Delete ${application.name}?`} description="Removing this application from JustCD cannot be undone. Choose what happens to its managed Kubernetes resources." confirmLabel={`Delete ${application.name}`} busyLabel="Deleting…" onConfirm={() => onDeleteApplication(deletePolicy)} disabled={busy || hasPendingOperation}>
          <FormField label="Managed cluster resources" htmlFor="application-delete-policy"><FormSelect id="application-delete-policy" value={deletePolicy} onValueChange={setDeletePolicy} items={[{ value: "keep", label: "Keep resources in Kubernetes" }, { value: "delete", label: "Delete resources through a reviewed plan" }]} /></FormField>
          {deletePolicy === "delete" && managedResourceCount > 0 && <p className="mt-2 text-sm text-muted-foreground">A deletion plan must receive {deletionApprovalCount} approval{deletionApprovalCount === 1 ? "" : "s"} from eligible workspace members before the resources can be removed.</p>}
        </ConfirmDisclosure>
      </div>
    </DangerZone>}
  </div>
}
