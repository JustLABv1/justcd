"use client"

import { Button } from "@/components/ui/button"
import { PublishingCard } from "./publishing-fields"
export function PublishingDraftNotice({
  draft,
  needsArchive = true,
}: {
  draft: {
    ready: boolean
    savedAt: number | null
    restored: boolean
    error: string
    clear: () => boolean
  }
  needsArchive?: boolean
}) {
  return (
    <PublishingCard contentClassName="flex flex-wrap items-center justify-between gap-3">
      <div className="min-w-0">
        <p className="text-sm font-medium" role="status">
          {draft.error ||
            (!draft.ready
              ? "Loading your draft…"
              : draft.restored
                ? "Draft restored · changes saved automatically"
                : "Changes saved automatically")}
        </p>
        <p className="mt-1 text-xs leading-5 text-muted-foreground">
          Saved in this browser for 30 days. Credential fields
          {needsArchive ? " and ZIP files are" : " are"} not saved.
          {draft.restored
            ? needsArchive
              ? " Reselect your ZIP and re-enter credentials before publishing."
              : " Re-enter credentials and inspect the repository again before continuing."
            : ""}
        </p>
      </div>
      {draft.ready && (draft.savedAt || draft.error) && (
        <Button
          type="button"
          variant="destructive"
          onClick={() => {
            if (draft.clear()) window.location.reload()
          }}
        >
          Discard draft
        </Button>
      )}
      {!draft.ready && draft.error && (
        <Button
          type="button"
          variant="outline"
          onClick={() => window.location.reload()}
        >
          Reload
        </Button>
      )}
    </PublishingCard>
  )
}
