"use client"

import { useCallback, useState } from "react"
import { Button } from "@/components/ui/button"
import { Stepper, StepperContent, StepperIndicator, StepperItem, StepperNav, StepperPanel, StepperSeparator, StepperTrigger } from "@/components/reui/stepper"

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
    if (invalid) { invalid.reportValidity?.(); return false }
    if (extraCheck && !extraCheck(step)) return false
    setStep((current) => Math.min(stepCount, current + 1))
    return true
  }, [invalidIn, step, stepCount])
  const back = useCallback(() => setStep((current) => Math.max(1, current - 1)), [])
  /** Returns true when every step passes native validation; otherwise jumps to the first failing step. */
  const validateAll = useCallback(() => {
    for (let n = 1; n <= stepCount; n += 1) {
      const invalid = invalidIn(n)
      if (invalid) {
        setStep(n)
        window.setTimeout(() => invalid.reportValidity?.(), 50)
        return false
      }
    }
    return true
  }, [invalidIn, stepCount])
  return { step, setStep, stepId, next, back, validateAll, isLast: step === stepCount }
}

export type WizardStep = { title: string; content: React.ReactNode }

/** Stepper navigation plus all step panels. Place the footer buttons after it. */
export function Wizard({ step, onStepChange, idPrefix, steps }: { step: number; onStepChange: (step: number) => void; idPrefix: string; steps: WizardStep[] }) {
  return (
    <Stepper value={step} onValueChange={onStepChange} className="space-y-5">
      <StepperNav>
        {steps.map((item, index) => (
          <StepperItem key={item.title} step={index + 1} className="not-last:flex-1">
            <StepperTrigger type="button" aria-label={`Step ${index + 1}: ${item.title}`} className="gap-2">
              <StepperIndicator className="size-7 text-sm" ><span>{index + 1}</span></StepperIndicator>
              <span className={`hidden text-sm font-medium sm:inline ${step === index + 1 ? "" : "text-muted-foreground"}`}>{item.title}</span>
            </StepperTrigger>
            {index < steps.length - 1 && <StepperSeparator className="mx-2" />}
          </StepperItem>
        ))}
      </StepperNav>
      <StepperPanel>
        {steps.map((item, index) => (
          <StepperContent key={item.title} value={index + 1} forceMount>
            <div id={`${idPrefix}-step-${index + 1}`} className="space-y-5">{item.content}</div>
          </StepperContent>
        ))}
      </StepperPanel>
    </Stepper>
  )
}

/** Back / Next row for steps before the review step. */
export function WizardFooter({ step, isLast, onBack, onNext, submit }: { step: number; isLast: boolean; onBack: () => void; onNext: () => void; submit: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <Button type="button" variant="outline" onClick={onBack} disabled={step === 1}>Back</Button>
      {isLast ? submit : <Button type="button" onClick={onNext}>Next</Button>}
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
