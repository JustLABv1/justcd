"use client"

import {
  useDeferredValue,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
} from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import {
  ArrowDown01Icon,
  ArrowUp01Icon,
  Settings02Icon,
} from "@hugeicons/core-free-icons"
import { Input } from "@/components/ui/input"
import { VirtualReviewList } from "@/components/virtual-review-list"
import { Button } from "@/components/ui/button"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { SwitchField } from "@/components/ui-kit"
import { contextRows, reviewRows } from "@/lib/line-diff"

export function ManifestDiff({
  before,
  after,
  excluded = false,
}: {
  before: unknown
  after: unknown
  excluded?: boolean
}) {
  const [mode, setMode] = useState("split")
  const [fontSize, setFontSize] = useState("14")
  const [wrap, setWrap] = useState(true)
  const [expanded, setExpanded] = useState(false)
  const [position, setPosition] = useState(-1)
  const scrollTo = useRef<((index: number) => void) | null>(null)
  const [query, setQuery] = useState("")
  const search = useDeferredValue(query)
  const optionsId = useId()
  const singleSide = before == null || after == null
  const split = mode === "split"
  const rows = useMemo(() => reviewRows(before, after), [before, after])
  const visible = useMemo(() => contextRows(rows), [rows])
  const hunks = useMemo(
    () =>
      rows.flatMap((row, i) =>
        row.changed && !rows[i - 1]?.changed ? [i] : []
      ),
    [rows]
  )
  const matches = useMemo(
    () =>
      rows.flatMap((row, i) =>
        search &&
        `${row.before ?? ""} ${row.after ?? ""}`
          .toLowerCase()
          .includes(search.toLowerCase())
          ? [i]
          : []
      ),
    [rows, search]
  )
  const searchContext = useMemo(() => {
    const result = new Set<number>()
    for (const i of matches)
      for (
        let n = Math.max(0, i - 3);
        n <= Math.min(rows.length - 1, i + 3);
        n++
      )
        result.add(n)
    return result
  }, [matches, rows.length])
  const display = useMemo(() => {
    const result: { index: number; hidden: number }[] = []
    for (let i = 0; i < rows.length; i++) {
      const shown = search
        ? searchContext.has(i)
        : expanded || visible.size === 0 || visible.has(i)
      if (shown) result.push({ index: i, hidden: 0 })
      else {
        let end = i + 1
        while (
          end < rows.length &&
          !(search ? searchContext.has(end) : expanded || visible.has(end))
        )
          end++
        result.push({ index: i, hidden: end - i })
        i = end - 1
      }
    }
    return result
  }, [rows, search, searchContext, expanded, visible])
  useEffect(() => {
    if (position >= 0) {
      const i = display.findIndex((item) => item.index === hunks[position])
      if (i >= 0) scrollTo.current?.(i)
    }
  }, [position, display, hunks])
  function jump(direction: number) {
    const next =
      position < 0
        ? direction > 0
          ? 0
          : hunks.length - 1
        : (position + direction + hunks.length) % hunks.length
    setExpanded(true)
    setQuery("")
    setPosition(next)
  }
  function line(
    text: string | undefined,
    number: number | undefined,
    tone?: "removed" | "added"
  ) {
    return (
      <div
        className={`flex min-w-0 ${tone === "removed" ? "bg-destructive/10" : tone === "added" ? "bg-success/10" : ""}`}
      >
        <span
          aria-hidden="true"
          className="w-12 shrink-0 px-2 text-right text-muted-foreground select-none"
        >
          {number}
        </span>
        <span aria-hidden="true" className="w-5 shrink-0 select-none">
          {tone === "removed" ? "−" : tone === "added" ? "+" : ""}
        </span>
        <span
          className={`min-w-0 flex-1 pr-4 ${wrap || split ? "break-all whitespace-pre-wrap" : "whitespace-pre"}`}
        >
          {text ?? " "}
        </span>
      </div>
    )
  }
  if (before == null && after == null)
    return (
      <section
        aria-label="Manifest comparison"
        className="px-4 py-5 text-sm text-muted-foreground"
      >
        Manifest content is unavailable for this difference.
        {excluded ? " This entry is excluded and will not be applied." : ""}
      </section>
    )
  return (
    <section aria-label="Manifest comparison">
      <div className="flex flex-wrap items-center gap-2 border-b p-3">
        <ToggleGroup
          aria-label="Diff layout"
          value={[mode]}
          onValueChange={(next) => {
            if (next[0]) setMode(next[0])
          }}
        >
          <ToggleGroupItem value="split">Side by side</ToggleGroupItem>
          <ToggleGroupItem value="unified">Unified</ToggleGroupItem>
        </ToggleGroup>
        <Popover>
          <PopoverTrigger
            render={<Button type="button" variant="outline" size="sm" />}
          >
            <HugeiconsIcon
              icon={Settings02Icon}
              strokeWidth={1.8}
              aria-hidden="true"
            />
            View options
          </PopoverTrigger>
          <PopoverContent align="start" className="w-80 gap-3 p-3">
            <div className="space-y-1.5">
              <p id={`${optionsId}-font`} className="text-sm font-medium">
                Code font size
              </p>
              <ToggleGroup
                aria-labelledby={`${optionsId}-font`}
                value={[fontSize]}
                onValueChange={(next) => {
                  if (next[0]) setFontSize(next[0])
                }}
              >
                {[14, 16, 18].map((n) => (
                  <ToggleGroupItem key={n} value={String(n)}>
                    {n} px
                  </ToggleGroupItem>
                ))}
              </ToggleGroup>
            </div>
            <SwitchField
              id={`${optionsId}-wrap`}
              label="Wrap lines"
              description={
                split
                  ? "Side-by-side view always wraps long lines to keep both manifests visible."
                  : "Wrap long lines instead of scrolling horizontally."
              }
              checked={wrap || split}
              disabled={split}
              onCheckedChange={setWrap}
            />
            <SwitchField
              id={`${optionsId}-expand`}
              label="Show all lines"
              description="Include unchanged context lines."
              checked={expanded}
              onCheckedChange={setExpanded}
            />
          </PopoverContent>
        </Popover>
        <Input
          aria-label="Search manifest diff"
          placeholder="Search diff text…"
          className="w-48"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
        />
        {search && (
          <span role="status" className="text-sm text-muted-foreground">
            {matches.length} matching lines
          </span>
        )}
        <div className="ml-auto flex items-center gap-2">
          <span className="text-sm text-muted-foreground" aria-live="polite">
            {hunks.length
              ? `${position < 0 ? "–" : position + 1} / ${hunks.length} changes`
              : "No text changes"}
          </span>
          <Button
            type="button"
            variant="outline"
            size="icon"
            aria-label="Previous change"
            disabled={!hunks.length}
            onClick={() => jump(-1)}
          >
            <HugeiconsIcon
              icon={ArrowUp01Icon}
              strokeWidth={2}
              aria-hidden="true"
            />
          </Button>
          <Button
            type="button"
            variant="outline"
            size="icon"
            aria-label="Next change"
            disabled={!hunks.length}
            onClick={() => jump(1)}
          >
            <HugeiconsIcon
              icon={ArrowDown01Icon}
              strokeWidth={2}
              aria-hidden="true"
            />
          </Button>
        </div>
      </div>
      <div
        className="bg-card font-mono leading-6"
        style={{ fontSize: Number(fontSize) }}
      >
        <div className="grid border-b bg-muted px-3 py-2 font-sans text-sm font-medium md:grid-cols-2">
          <span>Live before</span>
          <span>{excluded ? "Git reference · excluded" : "Desired after"}</span>
        </div>
        {singleSide && (
          <p className="border-b px-4 py-3 font-sans text-sm text-muted-foreground">
            {before == null
              ? excluded
                ? "Creation is excluded."
                : "This resource will be created."
              : excluded
                ? "Deletion is excluded."
                : "This resource will be deleted."}
          </p>
        )}
        <VirtualReviewList
          count={display.length}
          apiRef={scrollTo}
          estimate={48}
          className="h-[min(65svh,800px)] min-h-64"
          label="Scrollable manifest diff"
          render={(index) => {
            const { index: i, hidden } = display[index],
              row = rows[i]

            if (hidden)
              return (
                <Button
                  type="button"
                  variant="ghost"
                  className="w-full rounded-none border-y font-sans text-xs text-muted-foreground"
                  onClick={() => {
                    setExpanded(true)
                    setQuery("")
                  }}
                >
                  Show {hidden} {search ? "non-matching" : "unchanged"} lines
                </Button>
              )
            const unified = singleSide ? (
              before == null ? (
                row.after !== undefined && line(row.after, row.newLine, "added")
              ) : (
                row.before !== undefined &&
                line(row.before, row.oldLine, "removed")
              )
            ) : (
              <>
                {row.changed ? (
                  <>
                    {row.before !== undefined &&
                      line(row.before, row.oldLine, "removed")}
                    {row.after !== undefined &&
                      line(row.after, row.newLine, "added")}
                  </>
                ) : (
                  line(row.after, row.newLine)
                )}
              </>
            )
            return (
              <div key={i} data-row={i}>
                {split ? (
                  <>
                    <div className="hidden min-w-0 grid-cols-2 divide-x md:grid">
                      {before == null ? (
                        <div className="px-4 font-sans text-sm text-muted-foreground">
                          {i === 0 ? "Not present in cluster" : ""}
                        </div>
                      ) : (
                        line(
                          row.before,
                          row.oldLine,
                          row.changed && row.before !== undefined
                            ? "removed"
                            : undefined
                        )
                      )}
                      {after == null ? (
                        <div className="px-4 font-sans text-sm text-muted-foreground">
                          {i === 0
                            ? excluded
                              ? "Not present in Git · deletion excluded"
                              : "Resource will be deleted"
                            : ""}
                        </div>
                      ) : (
                        line(
                          row.after,
                          row.newLine,
                          row.changed && row.after !== undefined
                            ? "added"
                            : undefined
                        )
                      )}
                    </div>
                    <div className="md:hidden">{unified}</div>
                  </>
                ) : (
                  unified
                )}
              </div>
            )
          }}
        />
      </div>
    </section>
  )
}
