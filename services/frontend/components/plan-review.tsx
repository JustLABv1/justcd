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
import { FullScreenIcon, ArrowDown01Icon } from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { useToast } from "@/components/toast-provider"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { Input } from "@/components/ui/input"
import { FormSelect } from "@/components/ui/form-select"
import { EmptyState } from "@/components/ui-kit"
import { PolicyEvidence } from "@/components/guardrail-policy"
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
  renderApprovalReview?: (
    review: (identity: Change["identity"]) => void
  ) => ReactNode
}) {
  const [query, setQuery] = useState(""),
    deferred = useDeferredValue(query)
  const [filter, setFilter] = useState("all"),
    [group, setGroup] = useState<ReviewGroup>("risk")
  const [optionsOpen, setOptionsOpen] = useState(false)
  const toast = useToast(),
    root = useRef<HTMLElement>(null),
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
  return (
    <section
      ref={root}
      aria-label="Resource review"
      className="fullscreen:overflow-auto fullscreen:p-5 min-w-0 bg-card"
    >
      {renderApprovalReview?.(reviewIdentity)}
      <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
        <h3 className="text-base font-semibold">
          Review resources{" "}
          <span className="font-normal text-muted-foreground">
            ({entries.length})
          </span>
        </h3>
        <Popover open={optionsOpen} onOpenChange={setOptionsOpen}>
          <PopoverTrigger
            render={<Button type="button" variant="outline" size="sm" />}
          >
            Review options
            <HugeiconsIcon
              icon={ArrowDown01Icon}
              strokeWidth={1.8}
              aria-hidden="true"
            />
          </PopoverTrigger>
          <PopoverContent align="end" className="w-72 gap-3 p-3">
            {entries.length > 1 && (
              <FormSelect
                ariaLabel="Group resources"
                value={group}
                onValueChange={(value) => setGroup(value as ReviewGroup)}
                className="w-full"
                items={[
                  { value: "risk", label: "Group by risk" },
                  { value: "action", label: "Group by action" },
                  { value: "namespace", label: "Group by namespace" },
                  { value: "kind", label: "Group by kind" },
                  { value: "child", label: "Group by application child" },
                ]}
              />
            )}
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => {
                setOptionsOpen(false)
                void toggleFullscreen()
              }}
            >
              <HugeiconsIcon
                icon={FullScreenIcon}
                strokeWidth={1.8}
                aria-hidden="true"
              />
              Toggle fullscreen
            </Button>
            <p className="text-xs text-muted-foreground">
              Search, filters, and grouping only change this view. Saved plan
              counts and approval requirements stay the same.
            </p>
          </PopoverContent>
        </Popover>
      </div>
      <div>
        {plan && <PolicyEvidence plan={plan} />}
        {plan && showApprovalEvidence && (
          <ApprovalEvidence
            plan={plan}
            workspaceId={workspaceId}
            prefix={prefix}
            onResource={(identity) => reviewIdentity(identity)}
          />
        )}
      </div>
      <div className="grid min-w-0 gap-4 lg:grid-cols-[280px_minmax(0,1fr)]">
        <nav
          aria-label="Plan resources"
          className="min-w-0 rounded-lg border bg-muted/10 p-3 lg:self-start"
        >
          <Input
            aria-label="Search plan resources and diff text"
            placeholder="Find resource, field, or diff text…"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
          {entries.length > 1 && (
            <FormSelect
              ariaLabel="Filter changes"
              value={filter}
              onValueChange={setFilter}
              className="my-2"
              items={[
                { value: "all", label: "All resources" },
                ...(["create", "update", "delete"] as const)
                  .filter((kind) =>
                    entries.some(
                      (entry) => !entry.ignored && entry.change.kind === kind
                    )
                  )
                  .map((value) => ({
                    value,
                    label: value[0].toUpperCase() + value.slice(1),
                  })),
                ...(entries.some((entry) => entry.ignored)
                  ? [{ value: "excluded", label: "Excluded resources" }]
                  : []),
                ...Object.entries(riskLabels)
                  .filter(
                    ([value]) =>
                      value !== "deletion" &&
                      counts[value as keyof typeof riskLabels] > 0
                  )
                  .map(([value, label]) => ({
                    value,
                    label: `${label} (${counts[value as keyof typeof riskLabels]})`,
                  })),
              ]}
            />
          )}

          <p
            className="mb-2 text-sm text-muted-foreground"
            role="status"
            aria-live="polite"
          >
            {matching.length} of {entries.length} resources
            {query !== deferred ? " · searching…" : ""}
          </p>
          <p
            id={`${prefix}-keys`}
            className="mb-2 text-xs text-muted-foreground"
          >
            Use ↑/↓ and Home/End to review resources.
          </p>
          <VirtualReviewList
            count={matching.length}
            activeIndex={index}
            describedBy={`${prefix}-keys`}
            activeId={index >= 0 ? `${prefix}-resource-${index}` : undefined}
            role="listbox"
            label="Matching plan resources"
            apiRef={scrollTo}
            onKeyDown={(event) => {
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
                  {start && matching.length > 1 && (
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
                    className="mb-1 h-auto w-full justify-start px-3 py-3 text-left whitespace-normal"
                    onClick={() => select(entry.key)}
                  >
                    <span className="min-w-0">
                      <span className="block text-sm font-medium break-all">
                        {entry.change.identity.name}
                      </span>
                      <span className="mt-1 block text-xs text-muted-foreground">
                        {entry.change.identity.kind} ·{" "}
                        {entry.change.identity.namespace || "cluster"}
                        {entry.child ? ` · ${entry.child}` : ""}
                      </span>
                      <span className="mt-1 block text-xs text-muted-foreground">
                        <span
                          className={
                            entry.ignored
                              ? "text-muted-foreground"
                              : entry.change.kind === "create"
                                ? "text-success-foreground dark:text-success"
                                : entry.change.kind === "delete"
                                  ? "text-destructive"
                                  : "text-info-foreground dark:text-info"
                          }
                        >
                          {entry.ignored
                            ? "Excluded · not applied"
                            : entry.change.kind}
                        </span>
                        {entry.risks
                          .filter(
                            (risk) => risk !== "ordinary" && risk !== "deletion"
                          )
                          .map((risk) => ` · ${riskLabels[risk]}`)
                          .join("")}
                        {!entry.ignored &&
                          isExcluded(entry.change) &&
                          " · Exclusion draft"}
                      </span>
                    </span>
                  </Button>
                </>
              )
            }}
          />
        </nav>
        <div className="min-w-0">
          {active ? (
            renderChange(active.change, active.ignored, active, (identity) =>
              reviewIdentity(identity, active.childId)
            )
          ) : (
            <div className="rounded-xl border border-dashed">
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
            </div>
          )}
        </div>
      </div>
    </section>
  )
}
