"use client"

import { useEffect, useRef } from "react"
import { Steps } from "@/components/panelui/steps"

/** Keep wizard labels readable on phones and the current step within its scroll rail. */
export function WizardSteps({ step, onStepChange, titles, label }: { step: number; onStepChange: (step: number) => void; titles: string[]; label: string }) {
  const rail = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const container = rail.current
    const current = container?.querySelector<HTMLElement>('[aria-current="step"]')
    if (!container || !current) return
    const bounds = container.getBoundingClientRect()
    const item = current.getBoundingClientRect()
    if (item.left < bounds.left || item.right > bounds.right) {
      container.scrollLeft += item.left - bounds.left - (bounds.width - item.width) / 2
    }
  }, [step])
  return <div ref={rail} className="max-w-full overflow-x-auto pb-2">
    <Steps className="min-w-max" value={step} onValueChange={onStepChange} aria-label={label}>
      {titles.map((title, index) => <Steps.Item key={title} step={index + 1} className="min-w-28">
        <Steps.Trigger type="button"><Steps.Indicator /><Steps.Title>{title}</Steps.Title></Steps.Trigger>
      </Steps.Item>)}
    </Steps>
  </div>
}
