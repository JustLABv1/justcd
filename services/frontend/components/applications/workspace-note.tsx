import { HugeiconsIcon } from "@hugeicons/react"
import { Folder01Icon } from "@hugeicons/core-free-icons"
import { Badge } from "@/components/panelui/badge"

/**
 * Tells the user which workspace they are publishing in. The workspace follows the one selected in
 * the sidebar, so publishing never asks for it again; the hint on how to change it is the tooltip.
 */
export function WorkspaceNote({ name }: { name?: string }) {
  if (!name) return null
  return (
    <Badge variant="outline" title="Switch it with the workspace menu in the sidebar" className="w-fit gap-1.5">
      <HugeiconsIcon icon={Folder01Icon} strokeWidth={1.8} className="size-3.5" aria-hidden="true" />
      <span className="sr-only">Publishing in workspace: </span>{name}
    </Badge>
  )
}
