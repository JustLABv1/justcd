import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowRight01Icon, BotIcon } from "@hugeicons/core-free-icons"
import { Badge } from "@/components/reui/badge"
import { Button } from "@/components/ui/button"
import { actionLabel, actorLabel, automationTrigger, dateLabel, eventStatus, isAutomation, wherePath, words, type AuditEvent } from "@/lib/audit"

/** Shared by the header and the rows so the columns stay aligned. */
export const auditGrid = "sm:grid-cols-[minmax(0,0.8fr)_minmax(0,1.25fr)_minmax(0,1.7fr)_8.5rem_1rem]"

export function AuditColumnHeader() {
  return <div aria-hidden="true" className={`hidden gap-x-4 border-b bg-muted/20 px-6 py-2 text-[11px] font-semibold tracking-wider text-muted-foreground uppercase sm:grid ${auditGrid}`}>
    <span>Event</span><span>Who</span><span>Where</span><span className="text-right">When</span><span />
  </div>
}

function Who({ event }: { event: AuditEvent }) {
  const automated = isAutomation(event)
  const trigger = automationTrigger(event)
  return <span className="flex min-w-0 items-center gap-2.5">
    <span aria-hidden="true" className={`grid size-8 shrink-0 place-items-center rounded-full border text-[11px] font-semibold ${automated ? "bg-info/10 text-info-foreground dark:text-info" : "bg-muted/40 text-foreground"}`}>
      {automated ? <HugeiconsIcon icon={BotIcon} strokeWidth={1.6} className="size-4" /> : actorLabel(event).slice(0, 2).toUpperCase()}
    </span>
    <span className="min-w-0">
      <span className="line-clamp-1 block text-sm font-medium" title={actorLabel(event)}>{actorLabel(event)}</span>
      <span className="line-clamp-1 block text-xs font-normal text-muted-foreground">{automated ? trigger ? `Automation · ${trigger}` : "Automation" : event.actorId ? "Person" : "System"}</span>
    </span>
  </span>
}

function Where({ event, showWorkspace }: { event: AuditEvent; showWorkspace: boolean }) {
  const { path, more } = wherePath(event, showWorkspace)
  const name = event.resourceName || (event.resourceId ? "" : "Instance")
  const trail = path.length ? path.join(" › ") : ""
  return <span className="block min-w-0">
    <span className="block text-sm font-medium break-all" title={name || words(event.resourceType)}>{name || words(event.resourceType)}</span>
    <span className="line-clamp-2 block text-xs font-normal break-words text-muted-foreground" title={trail}>
      {name && <>{words(event.resourceType)}{trail && " · "}</>}{trail}{more > 0 && ` +${more} more`}
    </span>
  </span>
}

export function AuditRow({ event, showWorkspace, onSelect }: { event: AuditEvent; showWorkspace: boolean; onSelect: (event: AuditEvent) => void }) {
  const status = eventStatus(event.action)
  const message = typeof event.details?.message === "string" ? event.details.message : ""
  return <Button
    variant="ghost"
    onClick={() => onSelect(event)}
    aria-label={`View event ${event.id}: ${actionLabel(event.action)} by ${actorLabel(event)} at ${dateLabel(event.createdAt)}`}
    className={`grid h-auto w-full grid-cols-1 items-center justify-start gap-x-4 gap-y-2 rounded-none px-5 py-3.5 text-left whitespace-normal sm:px-6 ${auditGrid}`}
  >
    <span className="min-w-0">
      <span className="flex flex-wrap items-center gap-2"><span className="text-sm font-medium">{actionLabel(event.action)}</span>{status && <Badge variant={status.variant}>{status.label}</Badge>}</span>
      {message && <span className="mt-1 line-clamp-1 block text-xs font-normal text-muted-foreground">{message}</span>}
    </span>
    <Who event={event} />
    <Where event={event} showWorkspace={showWorkspace} />
    <span className="text-xs font-normal text-muted-foreground sm:text-right"><time dateTime={event.createdAt}>{dateLabel(event.createdAt)}</time><span className="mt-0.5 block font-mono">EVT-{event.id}</span></span>
    <HugeiconsIcon icon={ArrowRight01Icon} className="hidden size-4 shrink-0 text-muted-foreground sm:block" aria-hidden="true" />
  </Button>
}
