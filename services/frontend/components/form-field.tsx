"use client"

import { useState, type ReactNode } from "react"

function nativeMessage(control: HTMLInputElement) {
  const v = control.validity
  if (v.valueMissing) return "This field is required."
  if (v.patternMismatch) return control.title || "Use the requested format."
  if (v.tooShort) return `Use at least ${control.minLength} characters.`
  if (v.tooLong) return `Use at most ${control.maxLength} characters.`
  if (v.typeMismatch) return "Enter a valid value."
  return control.validationMessage || "Check this value."
}

/**
 * Labelled field. Besides an explicit `error`, it shows the message of any
 * native constraint (`required`, `pattern`, …) that fails inside it, so forms
 * get inline errors instead of the browser's own bubble.
 */
export function FormField({
  label,
  htmlFor,
  hint,
  error,
  children,
}: {
  label: string
  htmlFor: string
  hint?: string
  error?: string
  children: ReactNode
}) {
  const [native, setNative] = useState("")
  const shown = error || native
  return (
    <div
      className="space-y-1.5"
      onInvalid={(event) => {
        event.preventDefault()
        const control = event.target as HTMLInputElement
        control.setAttribute("aria-invalid", "true")
        setNative(nativeMessage(control))
      }}
      onInput={(event) => {
        ;(event.target as HTMLElement).removeAttribute?.("aria-invalid")
        setNative("")
      }}
    >
      <label htmlFor={htmlFor} className="block text-sm font-medium">{label}</label>
      {children}
      {shown ? <p role="alert" className="text-sm leading-4 text-destructive">{shown}</p> : hint && <p className="text-sm leading-4 text-muted-foreground">{hint}</p>}
    </div>
  )
}
