"use client"

import type { ReactNode } from "react"
import { Button } from "@/components/panelui/button"
import { Alert } from "@/components/panelui/alert"
export function PublishingDraftNotice({
  draft,
  needsArchive = true,
  inline = false,
  note,
  children,
}: {
  draft: {
    ready: boolean
    savedAt: number | null
    restored: boolean
    error: string
    clear: () => boolean
  }
  needsArchive?: boolean
  /** Render as a plain row for use inside another card, such as a form footer. */
  inline?: boolean
  /** An extra line under the draft text, such as what publishing will do. */
  note?: ReactNode
  /** Actions placed after the discard button, such as the form's submit button. */
  children?: ReactNode
}) {
  const title = draft.error ||
    (!draft.ready
      ? "Loading your draft…"
      : draft.restored
        ? "Draft restored · changes saved automatically"
        : "Changes saved automatically")
  const detail = (
    <>
      Saved in this browser for 30 days. Credential fields
      {needsArchive ? " and ZIP files are" : " are"} not saved.
      {draft.restored
        ? needsArchive
          ? " Reselect your ZIP and re-enter credentials before publishing."
          : " Re-enter credentials and inspect the repository again before continuing."
        : ""}
    </>
  )
  const discard = draft.ready && (draft.savedAt || draft.error) ? (
    <Button type="button" variant={inline ? "ghost" : "outline"} size="sm" onClick={() => { if (draft.clear()) window.location.reload() }}>
      Discard draft
    </Button>
  ) : null
  const reload = !draft.ready && draft.error ? (
    <Button type="button" variant="outline" size="sm" onClick={() => window.location.reload()}>
      Reload
    </Button>
  ) : null
  // On its own the notice is a message, not a card: an alert, so nothing on the page looks like something to open.
  if (!inline) {
    return (
      <Alert tone={draft.error ? "destructive" : "info"} variant="plain" role="status">
        <Alert.Icon />
        <Alert.Title>{title}</Alert.Title>
        <Alert.Description>{detail}</Alert.Description>
        {(discard || reload) && <Alert.Actions>{discard}{reload}</Alert.Actions>}
      </Alert>
    )
  }
  return (
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div className="min-w-0">
        <p className="text-sm font-medium" role="status">{title}</p>
        <p className="mt-1 text-xs leading-5 text-muted-foreground">{detail}</p>
        {note && <p className="mt-1 text-xs leading-5 text-muted-foreground">{note}</p>}
      </div>
      <div className="flex flex-wrap items-center gap-2 max-sm:w-full max-sm:justify-end">
        {discard}
        {reload}
        {children}
      </div>
    </div>
  )
}
