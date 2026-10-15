"use client"

import Link from "next/link"
import { HugeiconsIcon } from "@hugeicons/react"
import {
  ArrowUp01Icon,
  ArrowDown01Icon,
  Copy01Icon,
  Link01Icon,
} from "@hugeicons/core-free-icons"
import { Badge } from "@/components/reui/badge"
import { Button } from "@/components/ui/button"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetFooter,
} from "@/components/ui/sheet"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useToast } from "@/components/toast-provider"
import {
  actionLabel,
  actorLabel,
  dateLabel,
  eventStatus,
  valueText,
  words,
  type AuditEvent,
  type DeploymentEvidence,
} from "@/lib/audit"

function Facts({ values }: { values: [string, unknown][] }) {
  return (
    <dl className="space-y-4">
      {values.map(([label, value]) => (
        <div
          key={label}
          className="grid min-w-0 gap-1 text-sm sm:grid-cols-[140px_minmax(0,1fr)] sm:gap-5"
        >
          <dt className="text-muted-foreground">{label}</dt>
          <dd className="min-w-0 [overflow-wrap:anywhere] break-words whitespace-pre-wrap">
            {typeof value === "object" && value !== null ? (
              <pre className="overflow-auto rounded-lg border bg-muted/30 p-3 font-mono text-xs leading-5 [overflow-wrap:anywhere] whitespace-pre-wrap">
                {valueText(value)}
              </pre>
            ) : (
              valueText(value)
            )}
          </dd>
        </div>
      ))}
    </dl>
  )
}

function DeploymentDetails({ evidence }: { evidence: DeploymentEvidence }) {
  return (
    <div className="space-y-8">
      <p className="text-sm leading-6 text-muted-foreground">
        Evidence captured when this event was recorded. Each event preserves the
        deployment state at that time.
      </p>
      <section className="space-y-4">
        <h3 className="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
          Reviewed plan
        </h3>
        <Facts
          values={[
            ["Commit", evidence.commit],
            ["Plan ID", evidence.planId],
            ["Plan digest", evidence.planDigest],
            ["Created at", evidence.planCreatedAt],
          ]}
        />
      </section>
      <section className="space-y-4">
        <h3 className="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
          Deployment targets
        </h3>
        {evidence.targets.map((target, index) => (
          <div key={index} className="rounded-lg border p-4">
            <Facts
              values={[
                ["Cluster ID", target.clusterId],
                [
                  "Namespace",
                  target.clusterScope ? "Cluster scope" : target.namespace,
                ],
              ]}
            />
          </div>
        ))}
        {!evidence.targets.length && (
          <p className="text-sm text-muted-foreground">
            No target bindings were recorded.
          </p>
        )}
      </section>
      <section className="space-y-4">
        <h3 className="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
          Approvals · {evidence.approvals.length} recorded /{" "}
          {evidence.requiredApprovals} required
        </h3>
        {evidence.approvals.map((approval) => (
          <div key={approval.id} className="rounded-lg border p-4">
            <Facts
              values={[
                ["Approval ID", approval.id],
                ["Actor ID", approval.actorId],
                ["Plan digest", approval.planDigest],
                ["Approved at", approval.createdAt],
                ["Used at", approval.usedAt],
              ]}
            />
          </div>
        ))}
        {!evidence.approvals.length && (
          <p className="text-sm text-muted-foreground">
            {evidence.requiredApprovals
              ? "No approvals were recorded at this point."
              : "This plan did not require approval."}
          </p>
        )}
      </section>
      <section className="space-y-4">
        <h3 className="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
          Policy evaluation
        </h3>
        {evidence.policy.mode ? (
          <>
            <Facts
              values={[
                ["Mode", evidence.policy.mode],
                ["Policy digest", evidence.policy.configDigest],
              ]}
            />
            {evidence.policy.results.map((result, index) => (
              <div key={index} className="rounded-lg border p-4">
                <Facts
                  values={[
                    ["Rule", result.rule],
                    [
                      "Resource",
                      `${result.resource.kind} ${result.resource.namespace ? `${result.resource.namespace}/` : ""}${result.resource.name}`,
                    ],
                    ["Cluster ID", result.resource.clusterId],
                  ]}
                />
              </div>
            ))}
            {!evidence.policy.results.length && (
              <p className="text-sm text-muted-foreground">
                No policy findings were recorded.
              </p>
            )}
          </>
        ) : (
          <p className="text-sm text-muted-foreground">
            No policy evaluation was recorded for this plan.
          </p>
        )}
      </section>
      <section className="space-y-4">
        <h3 className="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
          Operation outcome
        </h3>
        {evidence.operationId || evidence.result ? (
          <Facts
            values={[
              ["Operation ID", evidence.operationId],
              ["Type", evidence.operationType],
              ["Result", evidence.result],
              ["Started at", evidence.startedAt],
              ["Finished at", evidence.finishedAt],
              ["Error code", evidence.errorCode],
            ]}
          />
        ) : (
          <p className="text-sm text-muted-foreground">
            No operation had been recorded for this event.
          </p>
        )}
      </section>
    </div>
  )
}

export function AuditDetailPanel({
  event,
  events,
  onSelect,
  onClose,
  onHistory,
}: {
  event: AuditEvent | null
  events: AuditEvent[]
  onSelect: (event: AuditEvent) => void
  onClose: () => void
  onHistory: (id: string) => void
}) {
  const toast = useToast()
  const index = events.findIndex((item) => item.id === event?.id)
  const status = event && eventStatus(event.action)
  const related = event
    ? events.filter(
        (item) =>
          item.id !== event.id &&
          item.resourceType === event.resourceType &&
          item.resourceId === event.resourceId
      )
    : []
  async function copy(value: string, message: string) {
    try {
      await navigator.clipboard.writeText(value)
      toast.success(message)
    } catch (error) {
      toast.error("Could not copy to clipboard.", error)
    }
  }
  return (
    <Sheet
      open={event !== null}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <SheetContent className="gap-0 data-[side=right]:w-full motion-reduce:transition-none sm:rounded-2xl sm:border data-[side=right]:sm:inset-y-3 data-[side=right]:sm:right-3 data-[side=right]:sm:h-[calc(100%-1.5rem)] data-[side=right]:sm:max-w-2xl">
        {event && (
          <>
            <SheetHeader className="shrink-0 px-5 pt-5 pb-4 sm:px-7 sm:pt-6">
              <div className="mb-4 flex items-center justify-between gap-3 pr-8 text-xs text-muted-foreground">
                <div className="flex items-center gap-1 font-mono">
                  EVT-{event.id}
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    aria-label="Copy event ID"
                    onClick={() =>
                      void copy(String(event.id), "Event ID copied.")
                    }
                  >
                    <HugeiconsIcon icon={Copy01Icon} />
                  </Button>
                </div>
                <div className="flex items-center gap-1">
                  <span className="mr-2">
                    {index >= 0
                      ? `${index + 1} of ${events.length} loaded`
                      : "Linked event"}
                  </span>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label="Previous event"
                    disabled={index <= 0}
                    onClick={() => onSelect(events[index - 1])}
                  >
                    <HugeiconsIcon icon={ArrowUp01Icon} />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label="Next event"
                    disabled={index < 0 || index >= events.length - 1}
                    onClick={() => onSelect(events[index + 1])}
                  >
                    <HugeiconsIcon icon={ArrowDown01Icon} />
                  </Button>
                </div>
              </div>
              <div className="flex flex-wrap items-center gap-2">
                <SheetTitle className="font-mono text-lg break-all">
                  {event.action}
                </SheetTitle>
                {status && (
                  <Badge variant={status.variant}>{status.label}</Badge>
                )}
              </div>
              <SheetDescription className="mt-2">
                {actorLabel(event)} ·{" "}
                {new Date(event.createdAt).toLocaleString(undefined, {
                  dateStyle: "medium",
                  timeStyle: "long",
                })}
              </SheetDescription>
              <p className="mt-3 text-sm leading-6">
                {typeof event.details?.message === "string"
                  ? event.details.message
                  : `${actionLabel(event.action)}${event.resourceName ? ` · ${event.resourceName}` : ""}.`}
              </p>
            </SheetHeader>
            <Tabs
              key={event.id}
              defaultValue="details"
              className="flex min-h-0 flex-1 flex-col"
            >
              <TabsList
                className="mb-0 shrink-0 px-5 sm:px-7"
                aria-label="Audit event information"
              >
                <TabsTrigger value="details">Details</TabsTrigger>
                {event.deploymentRecord && (
                  <TabsTrigger value="deployment">Deployment</TabsTrigger>
                )}
                <TabsTrigger value="raw">Raw</TabsTrigger>
                <TabsTrigger value="related">
                  Related <Badge variant="outline">{related.length}</Badge>
                </TabsTrigger>
              </TabsList>
              <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-5 py-6 sm:px-7">
                <TabsContent value="details" className="space-y-8">
                  <section className="space-y-4">
                    <h3 className="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
                      Actor
                    </h3>
                    <Facts
                      values={[
                        ["Name", actorLabel(event)],
                        ["Actor ID", event.actorId],
                        [
                          "Actor type",
                          event.actorId === "justcd-system"
                            ? "Automation"
                            : event.actorId
                              ? "User"
                              : "System",
                        ],
                      ]}
                    />
                  </section>
                  <section className="space-y-4">
                    <h3 className="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
                      Target
                    </h3>
                    <Facts
                      values={[
                        [
                          "Resource",
                          event.resourceName || words(event.resourceType),
                        ],
                        ["Resource type", event.resourceType],
                        ["Resource ID", event.resourceId],
                        ["Workspace ID", event.workspaceId || "Instance"],
                      ]}
                    />
                    {event.resourceType === "application" && (
                      <Button
                        variant="outline"
                        render={
                          <Link
                            href={`/applications/${encodeURIComponent(event.resourceId)}`}
                          />
                        }
                      >
                        Open application
                      </Button>
                    )}
                  </section>
                  <section className="space-y-4">
                    <h3 className="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
                      Event
                    </h3>
                    <Facts
                      values={[
                        ["Event ID", event.id],
                        [
                          "Recorded at (UTC)",
                          new Date(event.createdAt).toISOString(),
                        ],
                        ["Schema version", event.schemaVersion],
                      ]}
                    />
                  </section>
                  <section className="space-y-4">
                    <h3 className="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
                      Recorded details
                    </h3>
                    {Object.keys(event.details || {}).length ? (
                      <Facts
                        values={Object.entries(event.details || {}).map(
                          ([key, value]) => [words(key), value]
                        )}
                      />
                    ) : (
                      <p className="text-sm text-muted-foreground">
                        No additional details were recorded.
                      </p>
                    )}
                  </section>
                </TabsContent>
                <TabsContent value="deployment">
                  {event.deploymentRecord && (
                    <DeploymentDetails evidence={event.deploymentRecord} />
                  )}
                </TabsContent>
                <TabsContent value="raw">
                  <pre className="rounded-xl border bg-muted/30 p-4 font-mono text-xs leading-6 break-all whitespace-pre-wrap">
                    {JSON.stringify(event, null, 2)}
                  </pre>
                </TabsContent>
                <TabsContent value="related" className="space-y-4">
                  <p className="text-sm text-muted-foreground">
                    Other loaded events for this resource. Use Resource history
                    to search the full audit trail.
                  </p>
                  {related.map((item) => (
                    <Button
                      key={item.id}
                      variant="outline"
                      className="h-auto w-full justify-start p-3 text-left whitespace-normal"
                      onClick={() => onSelect(item)}
                    >
                      <span>
                        <span className="block">
                          {actionLabel(item.action)}
                        </span>
                        <span className="mt-1 block text-xs font-normal text-muted-foreground">
                          EVT-{item.id} · {dateLabel(item.createdAt)}
                        </span>
                      </span>
                    </Button>
                  ))}
                  {!related.length && (
                    <p className="text-sm">
                      No related events in the loaded results.
                    </p>
                  )}
                </TabsContent>
              </div>
            </Tabs>
            <SheetFooter className="mt-0 shrink-0 flex-row flex-wrap items-center justify-between gap-2 border-t px-5 py-4 sm:px-7">
              <Button
                variant="outline"
                onClick={() => {
                  const url = new URL(window.location.href)
                  url.searchParams.set("event", String(event.id))
                  void copy(url.toString(), "Event link copied.")
                }}
              >
                <HugeiconsIcon icon={Link01Icon} />
                Copy link
              </Button>
              <Button
                variant="outline"
                render={
                  <a
                    href={`/api/v1/audit/export?format=json&eventId=${event.id}`}
                    download
                  />
                }
              >
                Export event
              </Button>
              <Button
                variant="ghost"
                onClick={() => onHistory(event.resourceId)}
              >
                Resource history
              </Button>
            </SheetFooter>
          </>
        )}
      </SheetContent>
    </Sheet>
  )
}
