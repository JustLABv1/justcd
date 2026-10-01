"use client"

import { useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { Alert02Icon, CheckListIcon } from "@hugeicons/core-free-icons"
import { ActionMenu } from "@/components/action-menu"
import { diffId } from "@/components/application/helpers"
import { ConfirmDisclosure } from "@/components/confirm-disclosure"
import { Badge } from "@/components/reui/badge"
import { Checkbox } from "@/components/ui/checkbox"
import { AppDialog } from "@/components/ui/dialog"
import { Textarea } from "@/components/ui/textarea"
import { FormField } from "@/components/ui-kit"
import type { OwnershipConflict } from "@/lib/types"

/**
 * Review dialog for resources that already exist in Kubernetes but are not recorded as managed by this application.
 * The takeover reason and "previous controller stopped" acknowledgement are local to this dialog.
 */
export function OwnershipConflictDialog({ open, onOpenChange, conflicts, applicationId, canApprove, busy, selectedKeys, onSelectedKeysChange, claimableKeys, onAdopt, onAdoptSelected, onConfigureIgnore }: {
  open: boolean
  onOpenChange: (open: boolean) => void
  conflicts: OwnershipConflict[]
  applicationId: string
  canApprove: boolean
  busy: boolean
  selectedKeys: string[]
  onSelectedKeysChange: (keys: string[]) => void
  claimableKeys: string[]
  onAdopt: (reason: string, previousControllerDisabled: boolean) => Promise<void>
  onAdoptSelected: (reason: string, previousControllerDisabled: boolean) => Promise<void>
  onConfigureIgnore: () => void
}) {
  const [reason, setReason] = useState("")
  const [previousControllerDisabled, setPreviousControllerDisabled] = useState(false)
  const first = conflicts[0]
  if (!first) return null
  const selectedClaimable = claimableKeys.filter((key) => selectedKeys.includes(key)).length
  const many = conflicts.length > 1
  const singleBlocked = Boolean(first.owner && first.owner !== applicationId && !first.ownerMissing) || first.hasOwnerReferences

  async function adopt() { await onAdopt(reason, previousControllerDisabled); setReason(""); setPreviousControllerDisabled(false) }
  async function adoptSelected() { await onAdoptSelected(reason, previousControllerDisabled); setReason(""); setPreviousControllerDisabled(false) }

  const takeoverFields = (idPrefix: string, acknowledgement: string) => <div className="space-y-3">
    <FormField label="Reason for takeover (optional)" htmlFor={`${idPrefix}-reason`}><Textarea id={`${idPrefix}-reason`} value={reason} onChange={(event) => setReason(event.target.value)} maxLength={500} placeholder="Migrating this deployment from the previous controller" /></FormField>
    <label className="flex items-start gap-2 text-sm"><Checkbox checked={previousControllerDisabled} onCheckedChange={(checked) => setPreviousControllerDisabled(Boolean(checked))} /><span>{acknowledgement}</span></label>
  </div>

  return <AppDialog open={open} onOpenChange={onOpenChange} variant="info" size="xl" title={`${conflicts.length} rendered resource${many ? "s" : ""} already exist${many ? "" : "s"}`} description="Sync is blocked until these conflicts are resolved.">
    <div className="space-y-5">
      <p className="text-sm leading-6 text-muted-foreground">{first.identity.kind} <span className="font-mono text-foreground">{first.identity.namespace || "cluster"}/{first.identity.name}</span>{many ? " and others exist" : " exists"} in Kubernetes, but JustCD has not recorded {many ? "them" : "it"} as managed by this application. Review each resource before claiming it. Resources owned by another application or by Kubernetes owner references cannot be claimed here; labels left by deleted applications can be.</p>
      {!many && <dl className="grid gap-3 rounded-lg border bg-muted/20 p-3 text-sm sm:grid-cols-3">
        <div><dt className="text-muted-foreground">UID</dt><dd className="break-all font-mono text-xs">{first.uid}</dd></div>
        <div><dt className="text-muted-foreground">Resource version</dt><dd className="font-mono text-xs">{first.resourceVersion}</dd></div>
        <div><dt className="text-muted-foreground">JustCD owner label</dt><dd className="break-all font-mono text-xs">{first.owner || "None"}</dd></div>
      </dl>}
      {many && <div className="rounded-lg border">
        <div className="flex flex-wrap items-center justify-between gap-2 border-b px-3 py-2">
          <div><h3 className="text-sm font-semibold">Select resources to claim</h3><p className="text-sm text-muted-foreground">{selectedClaimable} of {claimableKeys.length} eligible selected · {conflicts.length - claimableKeys.length} blocked</p></div>
          {canApprove && <ActionMenu label="Selection" size="sm" icon={CheckListIcon} items={[
            { label: `Select all eligible (${claimableKeys.length})`, disabled: busy || claimableKeys.length === 0 || selectedClaimable === claimableKeys.length, onSelect: () => onSelectedKeysChange(claimableKeys) },
            { label: "Clear selection", disabled: busy || selectedClaimable === 0, onSelect: () => onSelectedKeysChange([]) },
          ]} />}
        </div>
        {claimableKeys.length === 0 && <p className="border-b px-3 py-2 text-sm text-muted-foreground">No resources can be selected until their existing ownership is resolved.</p>}
        <ul className="max-h-72 divide-y overflow-y-auto">{conflicts.map((item) => {
          const key = diffId(item.identity)
          const blocked = item.hasOwnerReferences || Boolean(item.owner && item.owner !== applicationId && !item.ownerMissing)
          const blockedReason = item.hasOwnerReferences ? "Managed by a Kubernetes owner reference" : blocked ? `Owned by JustCD application ${item.owner}` : ""
          return <li key={key}><label className={`flex items-start gap-3 px-3 py-2.5 text-sm ${blocked ? "cursor-not-allowed" : "cursor-pointer"}`}>
            <Checkbox checked={selectedKeys.includes(key)} aria-label={`Select ${item.identity.kind} ${item.identity.namespace || "cluster"}/${item.identity.name}`} aria-describedby={blocked ? `conflict-reason-${key}` : undefined} disabled={blocked || busy || !canApprove} onCheckedChange={(checked) => onSelectedKeysChange(checked ? Array.from(new Set([...selectedKeys, key])) : selectedKeys.filter((value) => value !== key))} />
            <span className="min-w-0 flex-1"><span className="font-medium">{item.identity.kind}</span> <span className="break-all font-mono">{item.identity.namespace || "cluster"}/{item.identity.name}</span><span className="block break-all text-sm text-muted-foreground">UID {item.uid} · RV {item.resourceVersion}</span></span>
            {item.ownerMissing && !blocked && <Badge variant="success-light" radius="full">Previous application deleted · eligible</Badge>}
            {blocked && <Badge id={`conflict-reason-${key}`} variant="warning-light" radius="full" className="max-w-sm whitespace-normal break-all"><HugeiconsIcon icon={Alert02Icon} strokeWidth={2} aria-hidden="true" />{blockedReason}</Badge>}
          </label></li>
        })}</ul>
      </div>}
      {!many && singleBlocked && <p className="rounded-lg border bg-muted/20 p-3 text-sm text-muted-foreground">Takeover is unavailable because the object belongs to another JustCD application or is a Kubernetes dependent. Stop the previous controller and release its ownership before claiming the resource here. If the previous JustCD application has been deleted, its owner label can still remain on the Kubernetes object.</p>}
      <p className="text-sm text-muted-foreground">Or change the Git manifest or remove the old object through its current controller, then refresh the plan. JustCD will not delete an untracked resource for you.</p>
      <div className="flex flex-wrap items-center justify-end gap-2 border-t pt-4">
        {!canApprove && <span className="mr-auto text-sm text-muted-foreground">A workspace owner must choose how to resolve this conflict.</span>}
        {canApprove && <ActionMenu label="Other options" items={[{ label: "Keep externally managed · configure ignore", onSelect: onConfigureIgnore }]} />}
        {canApprove && many && <ConfirmDisclosure trigger={`Take over selected (${selectedKeys.length})`} triggerVariant="default" confirmVariant="default" title={`Take over ${selectedKeys.length} selected resources?`} description="JustCD will claim only ownership labels and inventory records. Auto-sync will pause. Each resource is rechecked before its claim; earlier successful claims remain if a later resource changes. Review a fresh plan before applying workload changes." confirmLabel="Claim selected resources" confirmDisabled={!previousControllerDisabled || selectedKeys.length === 0} onConfirm={adoptSelected} disabled={busy || selectedKeys.length === 0}>
          {takeoverFields("batch-adoption", "I have stopped the previous controller from reconciling these resources.")}
        </ConfirmDisclosure>}
        {canApprove && !many && <ConfirmDisclosure trigger="Take over in JustCD" triggerVariant="default" confirmVariant="default" title={`Take over ${first.identity.kind} ${first.identity.name}?`} description="JustCD will claim the ownership label and inventory record only. It will pause auto-sync; no workload fields change until you review and apply a fresh plan. Stop the previous controller first or it may undo this claim." confirmLabel="Claim resource" confirmDisabled={!previousControllerDisabled} onConfirm={adopt} disabled={busy || singleBlocked}>
          {takeoverFields("adoption", "I have stopped the previous controller from reconciling this resource.")}
        </ConfirmDisclosure>}
      </div>
    </div>
  </AppDialog>
}
