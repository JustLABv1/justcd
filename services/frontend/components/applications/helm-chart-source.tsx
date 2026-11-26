"use client"

import { FormSelect } from "@/components/form-select"
import { Input } from "@/components/panelui/input"
import { FormField } from "@/components/ui-kit"
import type { HelmChartSource } from "@/lib/types"

export function HelmChartFields({ id, chart, onChange, disabled }: {
  id: string
  chart: HelmChartSource | null | undefined
  onChange: (chart: HelmChartSource | null) => void
  disabled?: boolean
}) {
  return <>
    <FormField label="Chart source" htmlFor={id}>
      <FormSelect id={id} value={chart ? "remote" : "git"} disabled={disabled} items={[
        { value: "git", label: "Git chart directory" },
        { value: "remote", label: "OCI registry or Helm repository" },
      ]} onValueChange={(value) => onChange(value === "remote" ? { repository: "", chart: "", version: "" } : null)} />
    </FormField>
    {chart && <>
      <FormField label="Chart repository" htmlFor={id + "-repository"} hint="Use an oci:// or https:// URL. Manage private registry credentials in workspace Connections.">
        <Input id={id + "-repository"} value={chart.repository} placeholder="oci://registry.example.com/charts" required disabled={disabled} onChange={(event) => onChange({ ...chart, repository: event.target.value })} />
      </FormField>
      <FormField label="Chart name" htmlFor={id + "-name"}>
        <Input id={id + "-name"} value={chart.chart} required disabled={disabled} onChange={(event) => onChange({ ...chart, chart: event.target.value })} />
      </FormField>
      <FormField label="Chart version" htmlFor={id + "-version"} hint="Enter an exact version. Values files still come from Git.">
        <Input id={id + "-version"} value={chart.version} placeholder="1.2.3" required disabled={disabled} onChange={(event) => onChange({ ...chart, version: event.target.value })} />
      </FormField>
    </>}
  </>
}
