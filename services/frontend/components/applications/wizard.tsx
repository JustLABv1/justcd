"use client"

import { useCallback, useState } from "react"
import { Button } from "@/components/panelui/button"
import { WizardSteps } from "@/components/wizard-steps"

/** Marks every failing control inline (see FormField) and focuses the first. */
export function showInvalid(container: HTMLElement | null) {
  const failing = [...(container?.querySelectorAll<HTMLInputElement>(":invalid") ?? [])]
  failing.forEach((control) => control.checkValidity())
  failing[0]?.focus()
  failing[0]?.scrollIntoView?.({ block: "center", behavior: "smooth" })
  return failing.length === 0
}

/**
 * Step state for multi-step forms. Every step stays mounted (hidden) so all fields
 * remain part of one <form>; validation is therefore done per step by looking for
 * invalid native controls inside the step container.
 */
export function useWizard(stepCount: number, idPrefix: string) {
  const [step, setStep] = useState(1)
  const stepId = useCallback((n: number) => `${idPrefix}-step-${n}`, [idPrefix])
  const invalidIn = useCallback((n: number) => document.getElementById(`${idPrefix}-step-${n}`)?.querySelector<HTMLInputElement>(":invalid") ?? null, [idPrefix])
  const next = useCallback((extraCheck?: (n: number) => boolean) => {
    const invalid = invalidIn(step)
    if (invalid) { showInvalid(document.getElementById(`${idPrefix}-step-${step}`)); return false }
    if (extraCheck && !extraCheck(step)) return false
    setStep((current) => Math.min(stepCount, current + 1))
    return true
  }, [invalidIn, idPrefix, step, stepCount])
  const back = useCallback(() => setStep((current) => Math.max(1, current - 1)), [])
  /** Returns true when every step passes native validation; otherwise jumps to the first failing step. */
  const validateAll = useCallback(() => {
    for (let n = 1; n <= stepCount; n += 1) {
      const invalid = invalidIn(n)
      if (invalid) {
        setStep(n)
        window.setTimeout(() => showInvalid(document.getElementById(`${idPrefix}-step-${n}`)), 50)
        return false
      }
    }
    return true
  }, [invalidIn, idPrefix, stepCount])
  return { step, setStep, stepId, next, back, validateAll, isLast: step === stepCount }
}

export type WizardStep = { title: string; content: React.ReactNode }

/** Stepper navigation plus all step panels. Place the footer buttons after it. */
export function Wizard({ step, onStepChange, idPrefix, steps }: { step: number; onStepChange: (step: number) => void; idPrefix: string; steps: WizardStep[] }) {
  return (
    <div className="space-y-5">
      <WizardSteps step={step} onStepChange={onStepChange} titles={steps.map(item => item.title)} label="Creation steps" />
      {steps.map((item, index) => <div key={item.title} id={`${idPrefix}-step-${index + 1}`} hidden={step !== index + 1} className="space-y-5">{item.content}</div>)}
    </div>
  )
}

/**
 * Keep Next distinct from the final submit control. Reusing its DOM button can
 * change its type during the click and submit the form before Review is shown.
 */
export function WizardFooter({ step, isLast, onBack, onNext, submit }: { step: number; isLast: boolean; onBack: () => void; onNext: () => void; submit: React.ReactNode }) {
  return (
    <div className="flex items-center justify-end gap-2">
      {step > 1 && <Button type="button" variant="outline" onClick={onBack}>Back</Button>}
      {isLast ? <div key="submit">{submit}</div> : <Button key="next" type="button" onClick={(event) => { event.preventDefault(); onNext() }}>Next</Button>}
    </div>
  )
}

export function SummaryList({ items }: { items: { label: string; value: React.ReactNode }[] }) {
  return (
    <dl className="grid grid-cols-[minmax(0,160px)_minmax(0,1fr)] gap-x-4 gap-y-2.5 text-sm">
      {items.map((item) => <div key={item.label} className="contents"><dt className="text-muted-foreground">{item.label}</dt><dd className="min-w-0 break-words font-medium">{item.value}</dd></div>)}
    </dl>
  )
}
