"use client"

import Link from "next/link"
import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowUpRight01Icon } from "@hugeicons/core-free-icons"
import { ApplicationNotificationSettings } from "@/components/application-notification-settings"
import { useState, type ReactNode } from "react"
import { RowActions } from "@/components/action-menu"
import type { IgnorePrefill, NewIgnore } from "@/components/application/helpers"
import { ConfirmDisclosure } from "@/components/confirm-disclosure"
import { ErrorDetailsButton } from "@/components/error-details"
import { Badge } from "@/components/panelui/badge"
import { Button } from "@/components/panelui/button"
import { FormSelect } from "@/components/form-select"
import { Input } from "@/components/panelui/input"
import { SectionLoading } from "@/components/section-loading"
import { Table } from "@/components/panelui/table"
import { AppDialog, DialogCancel, DialogFooter } from "@/components/app-dialog"
import { SettingsNav, type SettingsNavGroup } from "@/components/settings/layout"
import { Textarea } from "@/components/panelui/textarea"
import { EmptyState, FormField, CheckboxField } from "@/components/ui-kit"
import { SettingsDangerZone, SettingsGroup, SettingsPanel, SettingsRow } from "@/components/settings/settings-section"
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
  return <SettingsPanel title="Render settings" description={application.applicationGroupId ? "Kustomize settings are shared by this deployment group." : "Control how Kustomize builds this application's manifests."}>
    <div className="space-y-4 p-5">
      <div className="text-sm"><p className="font-medium">Namespace from Git</p><p className="mt-1 font-mono text-muted-foreground">{kustomization ? kustomization.namespace || "Not set in kustomization" : kustomizationError ? errorMessage(kustomizationError) : "Checking kustomization…"}</p>{kustomizationError != null && <div className="mt-2"><ErrorDetailsButton error={kustomizationError} /></div>}<p className="mt-1 text-muted-foreground">JustCD target: {application.namespaces.map((binding) => binding.namespace).join(", ")}</p></div>
      {application.applicationGroupId
        ? <div className="text-sm">Namespace transforms and Helm chart rendering are configured for every target in the <Link href={`/application-groups/${application.applicationGroupId}`} className="font-medium text-primary hover:underline">deployment group</Link>.</div>
        : <>
          {application.namespaces.length > 0
            ? <CheckboxField id="render-namespace-transform" label="Apply namespace transform" description="Build the selected Kustomize path once per namespace with granted access and set namespace metadata to that target. This can create or delete resources on the next sync, so review a new plan before applying." checked={namespaceOverride} onCheckedChange={setNamespaceOverride} disabled={locked} />
            : <p className="text-sm text-muted-foreground">Grant namespace access to at least one namespace to configure a namespace transform.</p>}
          <CheckboxField id="render-helm-charts" label="Enable Helm charts in Kustomize" description="Permits helmCharts from this Git source. Helm may fetch pinned charts from public HTTPS repositories while building the plan." checked={helm} onCheckedChange={setHelm} disabled={locked} />
          {canApprove && !application.repositoryConfigurationId && <Button size="sm" loading={saving} disabled={busy || hasPendingOperation || !dirty} onClick={() => void onSave({ kustomizeHelmEnabled: helm, kustomizeNamespaceOverride: namespaceOverride })}>Save changes</Button>}
        </>}
    </div>
  </SettingsPanel>
}

const rendererLabels: Record<string, string> = { yaml: "YAML", kustomize: "Kustomize", helm: "Helm" }

/** Where the application's manifests come from. Read-only; editing goes through "Edit application". */
function SourceSettings({ application, canApprove }: { application: Application; canApprove: boolean }) {
  const overlays = Object.entries(application.namespaceManifestPaths ?? {})
  const mono = "font-mono text-xs break-all"
  return <SettingsPanel title="Source" description="Where JustCD reads this application's manifests." action={<>
    {application.applicationGroupId && <Button size="sm" variant="outline" nativeButton={false} render={<Link href={`/application-groups/${application.applicationGroupId}`} />}>Open deployment group</Button>}
    {canApprove && !application.repositoryConfigurationId && !application.decommissioning && <Button size="sm" variant="outline" nativeButton={false} render={<Link href={`/applications/${application.id}/edit`} />}>Edit application</Button>}
  </>}>
    <SettingsGroup>
      <SettingsRow label="Revision" value={<span className="font-medium break-all">{application.revision}</span>} />
      <SettingsRow label="Manifest path" value={<span className={mono}>{application.manifestPath}</span>} />
      <SettingsRow label="Renderer" value={rendererLabels[application.renderer] ?? application.renderer} />
      {application.renderer === "kustomize" && <SettingsRow label="Cluster overlay path" value={<span className={mono}>{application.targetManifestPath || "Shared path"}</span>} />}
      {overlays.length > 0 && <SettingsRow label="Namespace overlays" value={<ul className="space-y-1">{overlays.map(([namespace, path]) => <li key={namespace} className={mono}>{namespace}: {path}</li>)}</ul>} />}
      <SettingsRow label="Cluster" value={application.clusterName || application.clusterId} />
      <SettingsRow label="Poll interval" value={`${application.pollSeconds} seconds`} />
    </SettingsGroup>
  </SettingsPanel>
}

export function SettingsTab({ application, kustomization, kustomizationError, canApprove, busy, hasPendingOperation, pendingAction, rulesLoading, ignoreRules, ignoreSelectors, managedResourceCount, deletionApprovalCount, prefill, onSaveRenderSettings, onAddIgnore, onRemoveRule, onRemoveSelector, onDeleteApplication, onCancelDecommission, schedule, section, onSectionChange }: {
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
  /** Deployment schedule section. */
  schedule?: ReactNode
  /** The open settings section; empty selects the first. */
  section: string
  onSectionChange: (section: string) => void
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
  const [addOpen, setAddOpen] = useState(Boolean(prefill))
  const [deletePolicy, setDeletePolicy] = useState(application.decommissioning ? "delete" : "keep")
  const [wasDecommissioning, setWasDecommissioning] = useState(application.decommissioning)

  // A new prefill request (for example "Configure exclusion" elsewhere on the page) fills and opens the form.
  if (prefill && prefill.nonce !== appliedPrefill) {
    setAppliedPrefill(prefill.nonce)
    setIgnoreMode(prefill.mode)
    if (prefill.version !== undefined) setIgnoreVersion(prefill.version)
    if (prefill.kind !== undefined) setIgnoreKind(prefill.kind)
    if (prefill.namespace !== undefined) setIgnoreNamespace(prefill.namespace)
    if (prefill.name !== undefined) setIgnoreName(prefill.name)
    if (prefill.reason !== undefined) setIgnoreReason(prefill.reason)
    if (canApprove) setAddOpen(true)
  }
  if (application.decommissioning !== wasDecommissioning) {
    setWasDecommissioning(application.decommissioning)
    if (application.decommissioning) setDeletePolicy("delete")
  }

  const reasonLength = ignoreReason.trim().length
  const reasonInvalid = reasonLength > 0 && reasonLength < 5
  const adding = pendingAction === "add-ignore"
  const addDisabled = busy || hasPendingOperation || reasonLength < 5 || ((ignoreMode === "kind" || ignoreMode === "resource") && (!ignoreVersion.trim() || !ignoreKind.trim())) || (ignoreMode === "resource" && !ignoreName.trim()) || (ignoreMode === "label" && !ignoreLabelKey.trim())
  async function addIgnore() {
    const saved = await onAddIgnore({ mode: ignoreMode, version: ignoreVersion, kind: ignoreKind, namespace: ignoreNamespace, name: ignoreName, labelKey: ignoreLabelKey, labelValue: ignoreLabelValue, reason: ignoreReason })
    if (saved) { setIgnoreReason(""); setIgnoreName(""); setAddOpen(false) }
  }

  const sections: { id: string; label: string; node: ReactNode }[] = [
    { id: "source", label: "Source", node: <SourceSettings application={application} canApprove={canApprove} /> },
    ...(application.renderer === "kustomize" ? [{ id: "render", label: "Render settings", node: <RenderSettings application={application} kustomization={kustomization} kustomizationError={kustomizationError} canApprove={canApprove} busy={busy} hasPendingOperation={hasPendingOperation} saving={pendingAction === "render-settings"} onSave={onSaveRenderSettings} /> }] : []),
    { id: "exclusions", label: "Ignored resources", node: <ExclusionList application={application} canApprove={canApprove} busy={busy} hasPendingOperation={hasPendingOperation} loading={rulesLoading} ignoreRules={ignoreRules} ignoreSelectors={ignoreSelectors} onAdd={() => setAddOpen(true)} onRemoveRule={onRemoveRule} onRemoveSelector={onRemoveSelector} /> },
    ...(schedule ? [{ id: "schedule", label: "Deployment schedule", node: schedule }] : []),
    ...(canApprove ? [{ id: "notifications", label: "Notifications", node: <ApplicationNotificationSettings key={application.id} applicationId={application.id} workspaceId={application.workspaceId} /> }] : []),
    ...(canApprove ? [{ id: "danger", label: "Delete application", node: <SettingsDangerZone description="Choose whether JustCD keeps or removes the resources it manages.">
      <SettingsRow label="Delete this application" description={<>{`${managedResourceCount} managed resource${managedResourceCount === 1 ? "" : "s"} currently recorded.`} Keeping them removes ownership tracking from JustCD.{application.decommissioning ? " Deletion is already in progress." : ""}</>} action={<div className="flex flex-wrap items-center gap-2">
        {application.decommissioning && <Button type="button" size="sm" variant="outline" disabled={busy || hasPendingOperation} onClick={onCancelDecommission}>Cancel deletion</Button>}
        <ConfirmDisclosure trigger="Delete application" title={`Delete ${application.name}?`} description="Removing this application from JustCD cannot be undone. Choose what happens to its managed Kubernetes resources." confirmLabel={`Delete ${application.name}`} busyLabel="Deleting…" onConfirm={() => onDeleteApplication(deletePolicy)} disabled={busy || hasPendingOperation}>
          <FormField label="Managed cluster resources" htmlFor="application-delete-policy"><FormSelect id="application-delete-policy" value={deletePolicy} onValueChange={setDeletePolicy} items={[{ value: "keep", label: "Keep resources in Kubernetes" }, { value: "delete", label: "Delete resources through a reviewed plan" }]} /></FormField>
          {deletePolicy === "delete" && managedResourceCount > 0 && <p className="mt-2 text-sm text-muted-foreground">A deletion plan must receive {deletionApprovalCount} approval{deletionApprovalCount === 1 ? "" : "s"} from eligible workspace members before the resources can be removed.</p>}
        </ConfirmDisclosure>
      </div>} />
    </SettingsDangerZone> }] : []),
  ]
  const active = sections.find((item) => item.id === section) ?? sections[0]
  const nav: SettingsNavGroup[] = [{ items: sections.map((item) => ({ id: item.id, label: item.label, onSelect: () => onSectionChange(item.id) })) }]

  return <div className="grid min-w-0 gap-6 lg:grid-cols-[200px_minmax(0,1fr)] lg:gap-8">
    <SettingsNav groups={nav} activeId={active.id} label="Application settings" className="lg:top-20" />
    <div className="min-w-0 max-w-4xl">{active.node}</div>
    {canApprove && <AppDialog open={addOpen} onOpenChange={setAddOpen} busy={adding} title="Add exclusion" description="Excluded resources are not read, updated or deleted by plans. Existing plans become stale when exclusions change.">
      <form className="flex min-h-0 flex-1 flex-col" onSubmit={(event) => { event.preventDefault(); if (!addDisabled) void addIgnore() }}>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div className="sm:col-span-2"><FormField label="Exclude by" htmlFor="ignore-mode"><FormSelect id="ignore-mode" value={ignoreMode} onValueChange={setIgnoreMode} items={[{ value: "kind", label: "API kind" }, { value: "label", label: "Label" }, { value: "resource", label: "Exact resource" }]} /></FormField></div>
          {(ignoreMode === "kind" || ignoreMode === "resource") && <><FormField label="API version" htmlFor="ignore-api-version"><Input id="ignore-api-version" placeholder="secrets.hashicorp.com/v1beta1" value={ignoreVersion} onChange={(event) => setIgnoreVersion(event.target.value)} /></FormField><FormField label="Kind" htmlFor="ignore-kind"><Input id="ignore-kind" placeholder="VaultStaticSecret" value={ignoreKind} onChange={(event) => setIgnoreKind(event.target.value)} /></FormField></>}
          {ignoreMode === "resource" && <><FormField label="Namespace" htmlFor="ignore-namespace"><Input id="ignore-namespace" placeholder={application.namespaces[0]?.namespace ?? "namespace"} value={ignoreNamespace} onChange={(event) => setIgnoreNamespace(event.target.value)} /></FormField><FormField label="Resource name" htmlFor="ignore-name"><Input id="ignore-name" placeholder="ntfy-secret" value={ignoreName} onChange={(event) => setIgnoreName(event.target.value)} /></FormField></>}
          {ignoreMode === "label" && <><FormField label="Label key" htmlFor="ignore-label-key"><Input id="ignore-label-key" placeholder="justcd.io/exclude" value={ignoreLabelKey} onChange={(event) => setIgnoreLabelKey(event.target.value)} /></FormField><FormField label="Label value" htmlFor="ignore-label-value"><Input id="ignore-label-value" placeholder="true" value={ignoreLabelValue} onChange={(event) => setIgnoreLabelValue(event.target.value)} /></FormField></>}
          <div className="sm:col-span-2"><FormField label="Reason" htmlFor="ignore-reason"><Textarea id="ignore-reason" aria-invalid={reasonInvalid} aria-describedby="ignore-reason-help" value={ignoreReason} onChange={(event) => setIgnoreReason(event.target.value)} placeholder="Managed by another controller or outside this service account's permissions" maxLength={500} /><p id="ignore-reason-help" className={reasonInvalid ? "text-sm text-destructive" : "text-sm text-muted-foreground"}>{reasonInvalid ? "Enter at least 5 characters to add the exclusion." : "Required · 5–500 characters"}</p></FormField></div>
        </div>
        <DialogFooter><DialogCancel disabled={adding} /><Button type="submit" loading={adding} disabled={addDisabled}>Add exclusion</Button></DialogFooter>
      </form>
    </AppDialog>}
  </div>
}

function ExclusionList({ application, canApprove, busy, hasPendingOperation, loading, ignoreRules, ignoreSelectors, onAdd, onRemoveRule, onRemoveSelector }: {
  application: Application
  canApprove: boolean
  busy: boolean
  hasPendingOperation: boolean
  loading: boolean
  ignoreRules: IgnoreRule[]
  ignoreSelectors: IgnoreSelector[]
  onAdd: () => void
  onRemoveRule: (rule: IgnoreRule) => Promise<void>
  onRemoveSelector: (selector: IgnoreSelector) => Promise<void>
}) {
  const empty = ignoreRules.length === 0 && ignoreSelectors.length === 0
  const blocked = busy || hasPendingOperation
  const rows = [
    ...ignoreSelectors.map((rule) => {
      const label = rule.kind ? `${rule.apiVersion} · ${rule.kind}` : "Any kind"
      return { id: `selector-${rule.id}`, match: label, scope: rule.labelKey ? `Label ${rule.labelKey}=${rule.labelValue}` : "Every resource of this kind", reason: rule.reason, git: rule.managedByGit,
        remove: { label: "Remove exclusion", title: `Remove exclusion ${label}?`, description: "Future plans may manage matching resources again.", onConfirm: () => onRemoveSelector(rule), name: `exclusion ${label}` } }
    }),
    ...ignoreRules.map((rule) => ({ id: `rule-${rule.id}`, match: ruleLabel(rule), scope: rule.path ? `Field ${rule.path}` : "Whole resource", reason: rule.reason, git: rule.managedByGit,
      remove: { label: "Remove ignore rule", title: `Remove ignore rule for ${ruleLabel(rule)}?`, description: "Future plans may manage this resource again.", onConfirm: () => onRemoveRule(rule), name: `ignore rule for ${ruleLabel(rule)}` } })),
  ]
  return <SettingsPanel title="Ignored resources" description="Exclude an exact resource, an entire Kubernetes kind, or resources with a label. Exclusions apply before cluster discovery and live reads." action={canApprove && !empty && <Button size="sm" variant="outline" disabled={blocked} onClick={onAdd}>Add exclusion</Button>}>
    {application.repositoryConfigurationId && <p className="border-b px-5 py-4 text-sm text-muted-foreground">Define Git-managed exclusions in <code>spec.ignoreResources</code> in <code>{application.configurationPath}</code>. Manual exclusions can be added here as well.{" "}
      <a href="/docs/guides/repository-applications#ignored-resources" target="_blank" rel="noreferrer" className="inline-flex items-center gap-0.5 underline underline-offset-4">File format and examples<HugeiconsIcon icon={ArrowUpRight01Icon} strokeWidth={2} className="size-3.5" aria-hidden="true" /><span className="sr-only"> (opens in a new tab)</span></a></p>}
    {loading ? <SectionLoading label="Loading exclusions" lines={2} className="p-5" />
      : empty ? <EmptyState title="No ignored resources" description="Every resource rendered from Git is planned and managed." {...(canApprove ? { action: "Add exclusion", onAction: onAdd } : {})} />
      : <Table aria-label="Ignored resources">
        <Table.Header><Table.Row><Table.Head>Match</Table.Head><Table.Head>Applies to</Table.Head><Table.Head>Reason</Table.Head><Table.Head>Source</Table.Head><Table.Head><span className="sr-only">Actions</span></Table.Head></Table.Row></Table.Header>
        <Table.Body>{rows.map((row) => <Table.Row key={row.id}>
          <Table.Cell className="min-w-48 font-medium whitespace-normal break-words">{row.match}</Table.Cell>
          <Table.Cell className="min-w-40 font-mono text-xs whitespace-normal break-all text-muted-foreground">{row.scope}</Table.Cell>
          <Table.Cell className="min-w-56 whitespace-normal text-muted-foreground">{row.reason}</Table.Cell>
          <Table.Cell>{row.git ? <Badge variant="outline">Git</Badge> : <span className="text-muted-foreground">Manual</span>}</Table.Cell>
          <Table.Cell className="text-right">{!row.git && canApprove && <RowActions label={row.remove.name} items={[{ label: row.remove.label, destructive: true, disabled: blocked, confirm: { title: row.remove.title, description: row.remove.description, confirmLabel: row.remove.label, onConfirm: row.remove.onConfirm } }]} />}</Table.Cell>
        </Table.Row>)}</Table.Body>
      </Table>}
  </SettingsPanel>
}
