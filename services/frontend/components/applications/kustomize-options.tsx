"use client"

import { CheckboxCard } from "@/components/ui-kit"

/** Shared Kustomize renderer toggles used by application and deployment group forms. */
export function KustomizeOptions({ namespaceOverride, onNamespaceOverrideChange, helmEnabled, onHelmEnabledChange, disabled, idPrefix = "kustomize" }: {
  namespaceOverride: boolean
  onNamespaceOverrideChange: (checked: boolean) => void
  helmEnabled: boolean
  onHelmEnabledChange: (checked: boolean) => void
  disabled?: boolean
  idPrefix?: string
}) {
  return (
    <div className="space-y-3">
      <CheckboxCard id={`${idPrefix}-namespace-override`} checked={namespaceOverride} onCheckedChange={onNamespaceOverrideChange} disabled={disabled} title="Apply namespace transform" description="Build once per selected namespace and set namespace metadata to that target." />
      <CheckboxCard id={`${idPrefix}-helm-enabled`} checked={helmEnabled} onCheckedChange={onHelmEnabledChange} disabled={disabled} title="Enable Helm charts in Kustomize" description="Allows helmCharts from the Git revision. Helm may download pinned charts from public HTTPS repositories during rendering." />
    </div>
  )
}
