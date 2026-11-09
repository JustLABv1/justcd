"use client"

import { useState, type FormEvent, type ReactNode } from "react"
import { Button } from "@/components/panelui/button"
import { Checkbox } from "@/components/panelui/checkbox"
import { Field } from "@/components/panelui/field"
import { Frame } from "@/components/panelui/frame"
import { Label } from "@/components/panelui/label"
import { Select } from "@/components/panelui/select"
import { Steps } from "@/components/panelui/steps"
import { Switch } from "@/components/panelui/switch"

/**
 * PanelUI building blocks for the multi-step "new application" form. Step state and
 * validation stay in `useWizard` (see wizard.tsx); this file only draws them.
 */

/** One wizard page: a Frame with a title strip and a single padded panel. */
export function PanelStep({ title, description, children, className = "space-y-5" }: { title: string; description?: string; children: ReactNode; className?: string }) {
  return (
    <Frame>
      <Frame.Header>
        <Frame.Title as="h2">{title}</Frame.Title>
        {description && <Frame.Description>{description}</Frame.Description>}
      </Frame.Header>
      <Frame.Panel>
        {/* Frame.Panel draws a hairline between its children, so the content is one child. */}
        <div className={`min-w-0 p-5 ${className}`}>{children}</div>
      </Frame.Panel>
    </Frame>
  )
}

/**
 * Label, control, help text and the native validation message, wired together by Field.
 * The wizard validates with the browser's constraint API, which raises a non-bubbling
 * "invalid" event; it is caught here (capture phase) and shown through Field.Error.
 */
export function PanelField({ label, hint, required, htmlFor, children, className }: { label: ReactNode; hint?: ReactNode; required?: boolean; /** Only for custom controls Field cannot see, such as a file drop zone. */ htmlFor?: string; children: ReactNode; className?: string }) {
  const [message, setMessage] = useState("")
  return (
    <Field
      className={className}
      invalid={message !== ""}
      onInvalidCapture={(event: FormEvent<HTMLDivElement>) => setMessage((event.target as HTMLInputElement).validationMessage || "This field is invalid.")}
      onInputCapture={() => setMessage("")}
    >
      <Field.Label required={required} {...(htmlFor ? ({ htmlFor } as object) : {})}>{label}</Field.Label>
      {children}
      {hint && <Field.Description>{hint}</Field.Description>}
      <Field.Error match={message !== ""}>{message}</Field.Error>
    </Field>
  )
}

export function PanelSelect({ id, value, onValueChange, items, placeholder, disabled, ariaLabel, className, size }: {
  id?: string
  value: string
  onValueChange: (value: string) => void
  items: { value: string; label: string; description?: ReactNode; icon?: ReactNode }[]
  placeholder?: string
  disabled?: boolean
  ariaLabel?: string
  className?: string
  size?: "sm" | "md" | "lg"
}) {
  return (
    <Select items={items.map(({ value: itemValue, label }) => ({ value: itemValue, label }))} value={value || null} onValueChange={(next) => onValueChange(next ?? "")} disabled={disabled}>
      <Select.Trigger id={id} aria-label={ariaLabel} size={size} className={`w-full ${className ?? ""}`}>
        <Select.Value placeholder={placeholder} />
      </Select.Trigger>
      <Select.Content>
        {items.map((item) => <Select.Item key={item.value} value={item.value} description={item.description} icon={item.icon}>{item.label}</Select.Item>)}
      </Select.Content>
    </Select>
  )
}

/** A bordered option with a checkbox, title and description; the whole card toggles it. */
export function PanelCheckboxCard({ checked, onCheckedChange, title, description, disabled, id }: {
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  title: ReactNode
  description?: ReactNode
  disabled?: boolean
  id?: string
}) {
  return (
    <Label className={`flex items-start gap-3 rounded-lg border p-3 text-sm ${disabled ? "" : "cursor-pointer hover:bg-muted/40"}`}>
      <Checkbox id={id} checked={checked} disabled={disabled} onCheckedChange={(next) => onCheckedChange(Boolean(next))} className="mt-0.5" />
      <span className="min-w-0"><span className="block font-medium">{title}</span>{description && <span className="mt-0.5 block font-normal text-muted-foreground">{description}</span>}</span>
    </Label>
  )
}

/** Boolean setting with a Switch, label and help text on one row. */
export function PanelSwitchRow({ checked, onCheckedChange, label, description, id }: {
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  label: string
  description?: string
  id: string
}) {
  return (
    <div className="flex items-start justify-between gap-4 rounded-lg border p-3">
      <div className="min-w-0">
        <Label htmlFor={id} className="block text-sm font-medium">{label}</Label>
        {description && <p className="mt-0.5 text-sm text-muted-foreground">{description}</p>}
      </div>
      <Switch id={id} checked={checked} onCheckedChange={onCheckedChange} label={label} />
    </div>
  )
}

/** Kustomize renderer toggles, drawn with PanelUI controls. */
export function PanelKustomizeOptions({ namespaceOverride, onNamespaceOverrideChange, helmEnabled, onHelmEnabledChange, idPrefix }: {
  namespaceOverride: boolean
  onNamespaceOverrideChange: (checked: boolean) => void
  helmEnabled: boolean
  onHelmEnabledChange: (checked: boolean) => void
  idPrefix: string
}) {
  return (
    <div className="space-y-3">
      <PanelCheckboxCard id={`${idPrefix}-namespace-override`} checked={namespaceOverride} onCheckedChange={onNamespaceOverrideChange} title="Apply namespace transform" description="Build once per selected namespace and set namespace metadata to that target." />
      <PanelCheckboxCard id={`${idPrefix}-helm-enabled`} checked={helmEnabled} onCheckedChange={onHelmEnabledChange} title="Enable Helm charts in Kustomize" description="Allows helmCharts from the Git revision. Helm may download pinned charts from public HTTPS repositories during rendering." />
    </div>
  )
}

/** Step navigation, with every step page kept mounted so all fields stay in one <form>. */
export function PanelWizard({ step, onStepChange, idPrefix, steps }: { step: number; onStepChange: (step: number) => void; idPrefix: string; steps: { title: string; content: ReactNode }[] }) {
  return (
    <div className="space-y-5">
      <Steps value={step} onValueChange={onStepChange}>
        {steps.map((item, index) => (
          <Steps.Item key={item.title} step={index + 1}>
            <Steps.Trigger type="button">
              <Steps.Indicator />
              <Steps.Title>{item.title}</Steps.Title>
            </Steps.Trigger>
          </Steps.Item>
        ))}
      </Steps>
      {steps.map((item, index) => (
        <div key={item.title} id={`${idPrefix}-step-${index + 1}`} hidden={step !== index + 1} className="space-y-5">{item.content}</div>
      ))}
    </div>
  )
}

/** Back / Next row for steps before the review step. */
export function PanelWizardFooter({ step, isLast, onBack, onNext, submit }: { step: number; isLast: boolean; onBack: () => void; onNext: () => void; submit: ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <Button type="button" variant="outline" onClick={onBack} disabled={step === 1}>Back</Button>
      {isLast ? submit : <Button type="button" onClick={onNext}>Next</Button>}
    </div>
  )
}
