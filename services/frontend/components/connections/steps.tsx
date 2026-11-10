"use client"

import { useSyncExternalStore } from "react"
import { Steps } from "@/components/panelui/steps"

const query = "(min-width: 1024px)"
const subscribe = (notify: () => void) => {
  const media = window.matchMedia(query)
  media.addEventListener("change", notify)
  return () => media.removeEventListener("change", notify)
}
const desktopSnapshot = () => window.matchMedia(query).matches
const serverSnapshot = () => false

/** A sidebar on desktop and a compact progress row above the form on phones. */
export function ConnectionSteps({ steps, value, highestStep, onValueChange }: {
  steps: { title: string; description: string }[]
  value: number
  highestStep: number
  onValueChange: (value: number) => void
}) {
  const desktop = useSyncExternalStore(subscribe, desktopSnapshot, serverSnapshot)
  return <Steps orientation={desktop ? "vertical" : "horizontal"} value={value} onValueChange={(next) => { if (next <= highestStep) onValueChange(next) }} aria-label="Connection steps">
    {steps.map((item, index) => <Steps.Item key={item.title} step={index + 1} reachable={index + 1 <= highestStep} disabled={index + 1 > highestStep}>
      <Steps.Trigger><Steps.Indicator /><span className="min-w-0"><Steps.Title className="block text-xs lg:text-sm">{item.title}</Steps.Title>{desktop && <Steps.Description className="mt-1 block">{item.description}</Steps.Description>}</span></Steps.Trigger>
    </Steps.Item>)}
  </Steps>
}
