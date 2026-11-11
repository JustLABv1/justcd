"use client"

import {
  useDeferredValue,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { CheckmarkCircle02Icon, FullScreenIcon, Settings02Icon } from "@hugeicons/core-free-icons"
import { Button } from "@/components/panelui/button"
import { Chip } from "@/components/panelui/chip"
import { Toolbar } from "@/components/panelui/toolbar"
import { DiffToolbarHost, DiffToolbarSlot } from "@/components/manifest-diff"
import { Menu } from "@/components/panelui/menu"
import { Dialog } from "@/components/panelui/dialog"
import { Workbench, useWorkbench } from "@/components/panelui/workbench"
import { useToast } from "@/components/toast-provider"
import { Input } from "@/components/panelui/input"
import { EmptyState } from "@/components/ui-kit"
import { PolicyEvidence, PolicySummary } from "@/components/guardrail-policy"
import { ApprovalEvidence } from "@/components/approval-evidence"
import { VirtualReviewList } from "@/components/virtual-review-list"
import {
  groupLabel,
  makeEntry,
  matchesEntry,
  riskCounts,
  riskLabels,
  type ReviewEntry,
  type ReviewGroup,
} from "@/lib/plan-review"
import type { Change, Plan } from "@/lib/types"

function ReviewNavigation({ children, open, onOpenChange }: { children: ReactNode; open: boolean; onOpenChange: (open: boolean) => void }) {
  const { compact } = useWorkbench()
  // The right padding is the gutter that holds the control rail between the list and the diff.
  if (!compact) return <Workbench.Sidebar className="pe-[3.875rem]">{children}</Workbench.Sidebar>
  return <Dialog open={open} onOpenChange={onOpenChange}>
    <Dialog.Content variant="plain" size="lg"><Dialog.Header title="Plan resources" />
      <Dialog.Body className="h-[70svh] min-h-0 [&>nav]:rounded-none [&>nav]:bg-transparent [&>nav]:p-0 [&>nav]:shadow-none [&>nav]:ring-0">{children}</Dialog.Body>
    </Dialog.Content>
  </Dialog>
}

/**
 * Wide: a vertical rail in the gutter between the resource list and the diff, so
 * it covers neither. Narrow: the list is a dialog, so it floats above the diff.
 */
function ReviewBar({ count, children }: { count: number; children: ReactNode }) {
  const { compact } = useWorkbench()
  const label = `Plan review controls, ${count} ${count === 1 ? "resource" : "resources"}`
  if (compact) return <Workbench.Bar>
    <Toolbar aria-label={label} className="max-w-full">
      <span className="hidden px-2 text-sm font-medium sm:inline">Review resources <span className="font-normal text-muted-foreground">({count})</span></span>
      {children}
    </Toolbar>
  </Workbench.Bar>
  return <Workbench.Bar className="mt-0 -ms-[3.375rem] self-center justify-self-start">
    <Toolbar orientation="vertical" aria-label={label} className="max-h-full">{children}</Toolbar>
  </Workbench.Bar>
}

function ReviewNavigationTrigger({ onClick }: { onClick: () => void }) {
  const { compact } = useWorkbench()
  return compact ? <Toolbar.Button label="Choose resource" onClick={onClick}>Resources</Toolbar.Button> : null
}

export function PlanReview({
  changes,
  ignored,
  selected,
  onSelect,
  identityKey,
  isExcluded,
  renderChange,
  plan,
  workspaceId,
  entries: supplied,
  showApprovalEvidence = true,
  canvasHeader,
  emptyDescription = "This plan contains no resource changes.",
  renderApprovalReview,
}: {
  changes: Change[]
  ignored: Change[]
  selected: string
  onSelect: (key: string) => void
  identityKey: (change: Change) => string
  isExcluded: (change: Change) => boolean
  renderChange: (
    change: Change,
    ignored: boolean,
    entry: ReviewEntry,
    review: (identity: Change["identity"]) => void
  ) => ReactNode
  workspaceId?: string
  plan?: Plan
  entries?: ReviewEntry[]
  showApprovalEvidence?: boolean
  /** Shown at the top of the review canvas, above the selected diff. */
  canvasHeader?: ReactNode
  /** Shown in place of the review workspace when the plan has nothing to review. */
  emptyDescription?: string
  renderApprovalReview?: (
    review: (identity: Change["identity"]) => void
  ) => ReactNode
}) {
  const [navigatorOpen, setNavigatorOpen] = useState(false)
  const [query, setQuery] = useState(""),
    deferred = useDeferredValue(query)
  const [filter, setFilter] = useState("all"),
    [group, setGroup] = useState<ReviewGroup>("risk")
  const toast = useToast(),
    root = useRef<HTMLDivElement>(null),
    scrollTo = useRef<((index: number) => void) | null>(null),
    prefix = useId()
  const entries = useMemo(
    () =>
      supplied ?? [
        ...changes.map((c) =>
          makeEntry(c, false, identityKey(c), undefined, undefined, plan)
        ),
        ...ignored.map((c) =>
          makeEntry(
            c,
            true,
            `ignored:${identityKey(c)}`,
            undefined,
            undefined,
            plan
          )
        ),
      ],
    [supplied, changes, ignored, identityKey, plan]
  )
  const counts = useMemo(() => riskCounts(entries), [entries])
  const matching = useMemo(
    () =>
      entries
        .filter((entry) => matchesEntry(entry, deferred, filter))
        .sort(
          (a, b) =>
            groupLabel(a, group).localeCompare(groupLabel(b, group)) ||
            a.key.localeCompare(b.key)
        ),
    [entries, deferred, filter, group]
  )
  const filterItems = useMemo(() => {
    const live = entries.filter((entry) => !entry.ignored)
    const items = [{ value: "all", label: "All", count: entries.length }]
    for (const kind of ["create", "update", "delete"] as const) {
      const count = live.filter((entry) => entry.change.kind === kind).length
      if (count)
        items.push({
          value: kind,
          label: kind[0].toUpperCase() + kind.slice(1),
          count,
        })
    }
    const excluded = entries.length - live.length
    if (excluded)
      items.push({ value: "excluded", label: "Excluded", count: excluded })
    for (const [value, label] of Object.entries(riskLabels)) {
      const count = counts[value as keyof typeof riskLabels]
      if (value !== "deletion" && value !== "ordinary" && count > 0)
        items.push({ value, label, count })
    }
    return items
  }, [entries, counts])
  const groupCount = new Set(matching.map((entry) => groupLabel(entry, group)))
    .size
  const active =
      matching.find((entry) => entry.key === selected) ?? matching[0],
    index = active ? matching.indexOf(active) : -1
  useEffect(() => {
    if (index >= 0) scrollTo.current?.(index)
  }, [selected, index])
  function select(key: string) {
    onSelect(key)
  }
  function reviewIdentity(identity: Change["identity"], childId?: string) {
    setQuery("")
    setFilter("all")
    const entry = entries.find(
      (e) =>
        !e.ignored &&
        (!childId || e.childId === childId) &&
        identityKey(e.change) === identityKey({ kind: "update", identity })
    )
    if (entry) select(entry.key)
  }
  const [fullscreen, setFullscreen] = useState(false)
  useEffect(() => {
    const update = () => setFullscreen(document.fullscreenElement === root.current)
    document.addEventListener("fullscreenchange", update)
    return () => document.removeEventListener("fullscreenchange", update)
  }, [])
  async function toggleFullscreen() {
    try {
      if (document.fullscreenElement) await document.exitFullscreen()
      else await root.current?.requestFullscreen()
    } catch {
      toast.error(
        "Fullscreen is unavailable in this browser. The review remains available below."
      )
    }
  }
  // Nothing to review: a list, a canvas and a toolbar around nothing only add chrome.
  if (entries.length === 0) {
    const findings = plan?.policyEvaluation && plan.policyEvaluation.results.length > 0
    return (
      <section aria-label="Resource review" className="min-w-0 space-y-4">
        {renderApprovalReview?.(reviewIdentity)}
        <div className="space-y-3 rounded-[26px] bg-muted/60 p-4 ring-1 ring-foreground/[0.06] dark:bg-muted/40 dark:ring-foreground/10">
          {(canvasHeader || plan?.policyEvaluation) && (
            <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2">
              {canvasHeader}
              {plan && <span className="ms-auto"><PolicySummary plan={plan} /></span>}
            </div>
          )}
          {findings && plan && <PolicyEvidence plan={plan} className="mb-0" />}
          <div className="flex items-center gap-3 rounded-2xl bg-card px-4 py-3.5 ring-1 ring-foreground/[0.06] dark:ring-foreground/10">
            <span aria-hidden="true" className="grid size-9 shrink-0 place-items-center rounded-full bg-success/15 text-success"><HugeiconsIcon icon={CheckmarkCircle02Icon} strokeWidth={1.8} className="size-5" /></span>
            <div className="min-w-0">
              <p className="text-sm font-medium">No changes to review</p>
              <p className="text-sm text-muted-foreground">{emptyDescription}</p>
            </div>
          </div>
        </div>
      </section>
    )
  }
  return (
    <section aria-label="Resource review" className="min-w-0 space-y-4">
      {renderApprovalReview?.(reviewIdentity)}
      <div>
        {plan && showApprovalEvidence && (
          <ApprovalEvidence
            plan={plan}
            workspaceId={workspaceId}
            prefix={prefix}
            onResource={(identity) => reviewIdentity(identity)}
          />
        )}
      </div>
      <div ref={root} className={`min-w-0 ${fullscreen ? "h-screen bg-background p-3" : ""}`}>
      <DiffToolbarHost>
      <Workbench className={fullscreen ? "h-full" : "h-[min(82svh,920px)]"} sidebarWidth={402} focusShortcut={false}>
        {(canvasHeader || plan?.policyEvaluation) && <Workbench.Header>
          {canvasHeader}
          {plan && <span className="ms-auto"><PolicySummary plan={plan} /></span>}
          {plan?.policyEvaluation && plan.policyEvaluation.results.length > 0 && <div className="basis-full"><PolicyEvidence plan={plan} className="mb-0" /></div>}
        </Workbench.Header>}
        <ReviewBar count={entries.length}>
            <ReviewNavigationTrigger onClick={() => setNavigatorOpen(true)} />
            {entries.length > 1 && (
              <Menu>
                <Menu.Trigger render={<Toolbar.Button label="Review options" />}>
                  <HugeiconsIcon icon={Settings02Icon} strokeWidth={1.8} aria-hidden="true" />
                </Menu.Trigger>
                <Menu.Content align="end" className="min-w-56">
                  {entries.length > 1 && (
                    <Menu.Group>
                      <Menu.GroupLabel>Group by</Menu.GroupLabel>
                      <Menu.RadioGroup value={group} onValueChange={(value) => setGroup(value as ReviewGroup)}>
                        <Menu.RadioItem value="risk">Risk</Menu.RadioItem>
                        <Menu.RadioItem value="action">Action</Menu.RadioItem>
                        <Menu.RadioItem value="namespace">Namespace</Menu.RadioItem>
                        <Menu.RadioItem value="kind">Kind</Menu.RadioItem>
                        <Menu.RadioItem value="child">Application child</Menu.RadioItem>
                      </Menu.RadioGroup>
                    </Menu.Group>
                  )}
                </Menu.Content>
              </Menu>
            )}
            <DiffToolbarSlot />
            <Toolbar.Button label={fullscreen ? "Exit fullscreen" : "Fullscreen"} onClick={() => void toggleFullscreen()}>
              <HugeiconsIcon icon={FullScreenIcon} strokeWidth={1.8} aria-hidden="true" />
            </Toolbar.Button>
        </ReviewBar>
        <ReviewNavigation open={navigatorOpen} onOpenChange={setNavigatorOpen}>
        <nav
          aria-label="Plan resources"
          className="flex h-full min-h-0 min-w-0 flex-col gap-3 rounded-[20px] bg-popover p-3 text-popover-foreground shadow-[0_10px_30px_-12px_rgb(0_0_0/0.3)] ring-1 ring-foreground/10"
        >
          <Input
            aria-label="Search plan resources and diff text"
            placeholder="Find resource, field, or diff text…"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
          {entries.length > 1 && (
            <Chip.Group multiple={false} aria-label="Filter changes" value={[filter]} onValueChange={(next) => setFilter(next[0] ?? "all")}>
              {filterItems.map((item) => (
                <Chip key={item.value} value={item.value} size="sm" className="gap-1.5">
                  {item.label}
                  <span className="tabular-nums text-muted-foreground">{item.count}</span>
                </Chip>
              ))}
            </Chip.Group>
          )}
          <p
            className="text-sm text-muted-foreground"
            role="status"
            aria-live="polite"
          >
            {matching.length} of {entries.length} resources
            {query !== deferred ? " · searching…" : ""}
          </p>
          <p id={`${prefix}-keys`} className="sr-only">
            Use ↑/↓ and Home/End to review resources.
          </p>
          <VirtualReviewList
            count={matching.length}
            activeIndex={index}
            describedBy={`${prefix}-keys`}
            activeId={index >= 0 ? `${prefix}-resource-${index}` : undefined}
            role="listbox"
            className="min-h-0 flex-1"
            label="Matching plan resources"
            apiRef={scrollTo}
            onKeyDown={(event) => {
              if (event.key === "Enter") { event.preventDefault(); setNavigatorOpen(false); return }
              const next =
                event.key === "ArrowDown"
                  ? Math.min(index + 1, matching.length - 1)
                  : event.key === "ArrowUp"
                    ? Math.max(0, index - 1)
                    : event.key === "Home"
                      ? 0
                      : event.key === "End"
                        ? matching.length - 1
                        : -1
              if (next >= 0) {
                event.preventDefault()
                select(matching[next].key)
                scrollTo.current?.(next)
              }
            }}
            render={(i) => {
              const entry = matching[i],
                label = groupLabel(entry, group),
                start = i === 0 || groupLabel(matching[i - 1], group) !== label
              return (
                <>
                  {start && groupCount > 1 && (
                    <h4 className="mt-2 mb-1 px-2 text-xs font-semibold text-muted-foreground uppercase">
                      {label}
                    </h4>
                  )}
                  <Button
                    id={`${prefix}-resource-${i}`}
                    type="button"
                    role="option"
                    aria-selected={entry === active}
                    aria-posinset={i + 1}
                    aria-setsize={matching.length}
                    tabIndex={-1}
                    variant={entry === active ? "secondary" : "ghost"}
                    className="mb-0.5 h-auto w-full justify-start px-3 py-2 text-left whitespace-normal"
                    onClick={() => { select(entry.key); setNavigatorOpen(false) }}
                  >
                    <span className="flex w-full items-start gap-2">
                      <span className="min-w-0 flex-1">
                        <span className="block text-sm font-medium break-all">
                          {entry.change.identity.name}
                        </span>
                        <span className="block text-xs text-muted-foreground">
                          {entry.change.identity.kind} ·{" "}
                          {entry.change.identity.namespace || "cluster"}
                          {entry.child ? ` · ${entry.child}` : ""}
                          {entry.risks
                            .filter(
                              (risk) =>
                                risk !== "ordinary" && risk !== "deletion"
                            )
                            .map((risk) => ` · ${riskLabels[risk]}`)
                            .join("")}
                          {!entry.ignored &&
                            isExcluded(entry.change) &&
                            " · Exclusion draft"}
                        </span>
                      </span>
                      <span
                        className={`shrink-0 text-xs ${
                          entry.ignored
                            ? "text-muted-foreground"
                            : entry.change.kind === "create"
                              ? "text-success-foreground dark:text-success"
                              : entry.change.kind === "delete"
                                ? "text-destructive"
                                : "text-info-foreground dark:text-info"
                        }`}
                      >
                        {entry.ignored ? "excluded" : entry.change.kind}
                      </span>
                    </span>
                  </Button>
                </>
              )
            }}
          />
        </nav>
        </ReviewNavigation>
        <Workbench.Canvas className="!items-stretch !justify-stretch !px-0 !pt-0 !pb-0">
          <div className="min-h-0 w-full overflow-auto px-4 pb-4">
          {active ? (
            renderChange(active.change, active.ignored, active, (identity) =>
              reviewIdentity(identity, active.childId)
            )
          ) : (
            <EmptyState
              title={
                entries.length
                  ? "No matching resources"
                  : "No resources to review"
              }
              description={
                entries.length
                  ? "Try a different resource filter or search term."
                  : "This plan contains no resource changes."
              }
            />
          )}
          </div>
        </Workbench.Canvas>
      </Workbench>
      </DiffToolbarHost>
      </div>
    </section>
  )
}
