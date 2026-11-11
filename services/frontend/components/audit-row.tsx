import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowRight01Icon, BotIcon } from "@hugeicons/core-free-icons"
import { Table } from "@/components/panelui/table"
import { Avatar } from "@/components/panelui/avatar"
import { Badge } from "@/components/panelui/badge"
import { Button } from "@/components/panelui/button"
import { actionLabel, actorLabel, automationTrigger, dateLabel, eventStatus, isAutomation, wherePath, words, type AuditEvent } from "@/lib/audit"

export function AuditColumnHeader() {
  return <Table.Header><Table.Row>
    <Table.Head>Event</Table.Head><Table.Head>Who</Table.Head><Table.Head>Where</Table.Head><Table.Head align="end">When</Table.Head><Table.Head><span className="sr-only">Details</span></Table.Head>
  </Table.Row></Table.Header>
}

function Who({ event }: { event: AuditEvent }) {
  const automated = isAutomation(event)
  const trigger = automationTrigger(event)
  return <span className="flex min-w-0 items-center gap-2.5">
    <Avatar name={actorLabel(event)} size="sm" color="muted" aria-hidden="true" fallback={automated ? <HugeiconsIcon icon={BotIcon} strokeWidth={1.6} className="size-4" /> : undefined} />
    <span className="min-w-0">
      <span className="line-clamp-1 text-sm font-medium" title={actorLabel(event)}>{actorLabel(event)}</span>
      <span className="line-clamp-1 text-xs font-normal text-muted-foreground">{automated ? trigger ? `Automation · ${trigger}` : "Automation" : event.actorId ? "Person" : "System"}</span>
    </span>
  </span>
}

function Where({ event, showWorkspace }: { event: AuditEvent; showWorkspace: boolean }) {
  const { path, more } = wherePath(event, showWorkspace)
  const name = event.resourceName || (event.resourceId ? "" : "Instance")
  const trail = path.length ? path.join(" › ") : ""
  return <span className="block min-w-0">
    <span className="block text-sm font-medium break-all" title={name || words(event.resourceType)}>{name || words(event.resourceType)}</span>
    <span className="line-clamp-2 text-xs font-normal break-words text-muted-foreground" title={trail}>
      {name && <>{words(event.resourceType)}{trail && " · "}</>}{trail}{more > 0 && ` +${more} more`}
    </span>
  </span>
}

export function AuditRow({ event, showWorkspace, onSelect }: { event: AuditEvent; showWorkspace: boolean; onSelect: (event: AuditEvent) => void }) {
  const status = eventStatus(event.action)
  const message = typeof event.details?.message === "string" ? event.details.message : ""
  return <Table.Row>
    <Table.Cell className="min-w-48">
      <span className="flex flex-wrap items-center gap-2"><span className="text-sm font-medium">{actionLabel(event.action)}</span>{status && <Badge variant={status.variant}>{status.label}</Badge>}</span>
      {message && <span className="mt-1 line-clamp-1 text-xs text-muted-foreground">{message}</span>}
    </Table.Cell>
    <Table.Cell className="min-w-44"><Who event={event} /></Table.Cell>
    <Table.Cell className="min-w-48"><Where event={event} showWorkspace={showWorkspace} /></Table.Cell>
    <Table.Cell align="end" className="whitespace-nowrap text-xs text-muted-foreground"><time dateTime={event.createdAt}>{dateLabel(event.createdAt)}</time><span className="mt-0.5 block font-mono">EVT-{event.id}</span></Table.Cell>
    <Table.Cell><Button variant="outline" size="icon-sm" onClick={() => onSelect(event)} aria-label={`View event ${event.id}: ${actionLabel(event.action)} by ${actorLabel(event)}`}><HugeiconsIcon icon={ArrowRight01Icon} className="size-4" aria-hidden="true" /></Button></Table.Cell>
  </Table.Row>
}
