"use client"

import { useState, type FormEvent, type ReactNode } from "react"
import { Button } from "@/components/panelui/button"
import { Field } from "@/components/panelui/field"
import { Section } from "@/components/section"
import { Select } from "@/components/panelui/select"
import { WizardSteps } from "@/components/wizard-steps"
import { CheckboxCard, CheckboxField } from "@/components/ui-kit"

/**
 * PanelUI building blocks for the multi-step "new application" form. Step state and
 * validation stay in `useWizard` (see wizard.tsx); this file only draws them.
 */

/** One wizard step, drawn as a card so the fields never float on the page canvas. */
export function PanelStep({ title, description, children, className = "space-y-5" }: { title: string; description?: string; children: ReactNode; className?: string }) {
  return <Section surface title={title} description={description}>
    <div className={`min-w-0 ${className}`}>{children}</div>
  </Section>
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

/** Draft options reuse the shared checkbox row. */
export const PanelCheckboxCard = CheckboxCard

/** A renderer choice saved with the enclosing wizard form. */
export function PanelCheckboxRow(props: { checked: boolean; onCheckedChange: (checked: boolean) => void; label: string; description?: string; id: string }) {
  return <CheckboxField {...props} />
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
      <WizardSteps step={step} onStepChange={onStepChange} titles={steps.map(item => item.title)} label="Creation steps" />
      {steps.map((item, index) => (
        <div key={item.title} id={`${idPrefix}-step-${index + 1}`} hidden={step !== index + 1} className="space-y-5">{item.content}</div>
      ))}
    </div>
  )
}

/**
 * Keep Next distinct from the final submit control. Reusing its DOM button can
 * change its type during the click and submit the form before Review is shown.
 */
export function PanelWizardFooter({ step, isLast, onBack, onNext, submit }: { step: number; isLast: boolean; onBack: () => void; onNext: () => void; submit: ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <Button type="button" variant="outline" onClick={onBack} disabled={step === 1}>Back</Button>
      {isLast ? <div key="submit">{submit}</div> : <Button key="next" type="button" onClick={(event) => { event.preventDefault(); onNext() }}>Next</Button>}
    </div>
  )
}
