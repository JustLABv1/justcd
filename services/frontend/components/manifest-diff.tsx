"use client"

import { useId, useMemo, useRef, useState } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowDown01Icon, ArrowUp01Icon, Settings02Icon } from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { SwitchField } from "@/components/ui-kit"
import { contextRows, reviewRows } from "@/lib/line-diff"

export function ManifestDiff({ before, after, excluded = false }: { before: unknown; after: unknown; excluded?: boolean }) {
  const [mode, setMode] = useState("split")
  const [fontSize, setFontSize] = useState("14")
  const [wrap, setWrap] = useState(true)
  const [expanded, setExpanded] = useState(false)
  const [position, setPosition] = useState(-1)
  const scroll = useRef<HTMLDivElement>(null)
  const optionsId = useId()
  const singleSide = before == null || after == null
  const split = mode === "split"
  const rows = useMemo(() => reviewRows(before, after), [before, after])
  const visible = useMemo(() => contextRows(rows), [rows])
  const hunks = rows.flatMap((row, i) => row.changed && !rows[i - 1]?.changed ? [i] : [])
  function jump(direction: number) {
    const next = position < 0 ? (direction > 0 ? 0 : hunks.length - 1) : (position + direction + hunks.length) % hunks.length
    setPosition(next)
    const target = scroll.current?.querySelector<HTMLElement>(`[data-row="${hunks[next]}"]`)
    if (target && scroll.current) scroll.current.scrollTop += target.getBoundingClientRect().top - scroll.current.getBoundingClientRect().top - 48
  }
  function line(text: string | undefined, number: number | undefined, tone?: "removed" | "added") {
    return <div className={`flex min-w-0 ${tone === "removed" ? "bg-destructive/10" : tone === "added" ? "bg-success/10" : ""}`}>
      <span aria-hidden="true" className="w-12 shrink-0 select-none px-2 text-right text-muted-foreground">{number}</span>
      <span aria-hidden="true" className="w-5 shrink-0 select-none">{tone === "removed" ? "−" : tone === "added" ? "+" : ""}</span>
      <span className={`min-w-0 flex-1 pr-4 ${wrap || split ? "whitespace-pre-wrap break-all" : "whitespace-pre"}`}>{text ?? " "}</span>
    </div>
  }
  if (before == null && after == null) return <section aria-label="Manifest comparison" className="px-4 py-5 text-sm text-muted-foreground">Manifest content is unavailable for this difference.{excluded ? " This entry is excluded and will not be applied." : ""}</section>
  return <section aria-label="Manifest comparison">
    <div className="flex flex-wrap items-center gap-2 border-b p-3">
      <ToggleGroup aria-label="Diff layout" value={[mode]} onValueChange={(next) => { if (next[0]) setMode(next[0]) }}>
        <ToggleGroupItem value="split">Side by side</ToggleGroupItem>
        <ToggleGroupItem value="unified">Unified</ToggleGroupItem>
      </ToggleGroup>
      <Popover>
        <PopoverTrigger render={<Button type="button" variant="outline" size="sm" />}><HugeiconsIcon icon={Settings02Icon} strokeWidth={1.8} aria-hidden="true" />View options</PopoverTrigger>
        <PopoverContent align="start" className="w-80 gap-3 p-3">
          <div className="space-y-1.5">
            <p id={`${optionsId}-font`} className="text-sm font-medium">Code font size</p>
            <ToggleGroup aria-labelledby={`${optionsId}-font`} value={[fontSize]} onValueChange={(next) => { if (next[0]) setFontSize(next[0]) }}>
              {[14, 16, 18].map((n) => <ToggleGroupItem key={n} value={String(n)}>{n} px</ToggleGroupItem>)}
            </ToggleGroup>
          </div>
          <SwitchField id={`${optionsId}-wrap`} label="Wrap lines" description={split ? "Side-by-side view always wraps long lines to keep both manifests visible." : "Wrap long lines instead of scrolling horizontally."} checked={wrap || split} disabled={split} onCheckedChange={setWrap} />
          <SwitchField id={`${optionsId}-expand`} label="Show all lines" description="Include unchanged context lines." checked={expanded} onCheckedChange={setExpanded} />
        </PopoverContent>
      </Popover>
      <div className="ml-auto flex items-center gap-2">
        <span className="text-sm text-muted-foreground" aria-live="polite">{hunks.length ? `${position < 0 ? "–" : position + 1} / ${hunks.length} changes` : "No text changes"}</span>
        <Button type="button" variant="outline" size="icon" aria-label="Previous change" disabled={!hunks.length} onClick={() => jump(-1)}><HugeiconsIcon icon={ArrowUp01Icon} strokeWidth={2} aria-hidden="true" /></Button>
        <Button type="button" variant="outline" size="icon" aria-label="Next change" disabled={!hunks.length} onClick={() => jump(1)}><HugeiconsIcon icon={ArrowDown01Icon} strokeWidth={2} aria-hidden="true" /></Button>
      </div>
    </div>
    <div ref={scroll} tabIndex={0} aria-label="Scrollable manifest diff" className="relative h-[min(65svh,800px)] min-h-64 overflow-auto bg-card font-mono leading-6 focus-visible:outline-2 focus-visible:outline-ring" style={{ fontSize: Number(fontSize) }}>
      <div className={wrap || split ? "w-full min-w-0" : "w-max min-w-full"}>
        <div className={`sticky top-0 z-10 grid border-b bg-muted px-3 py-2 font-sans text-sm font-medium ${split ? "hidden md:grid md:grid-cols-2" : ""}`}>{split ? <><span>Live before</span><span>{excluded ? "Git reference · excluded" : "Desired after"}</span></> : excluded ? "Live state → Git reference · excluded" : "Live before → Desired after"}</div>
        {singleSide && <div className={`border-b px-4 py-3 font-sans text-sm text-muted-foreground ${split ? "md:hidden" : ""}`}>{before == null ? excluded ? "Not present in cluster. Creation is excluded." : "Live before: Not present in cluster. This resource will be created." : excluded ? "Not present in Git. Deletion is excluded." : "Desired after: Resource will be deleted."}</div>}
        {rows.map((row, i) => {
          if (!expanded && visible.size > 0 && !visible.has(i)) {
            if (i > 0 && !visible.has(i - 1)) return null
            let end = i + 1
            while (end < rows.length && !visible.has(end)) end++
            return <Button type="button" key={i} variant="ghost" className="w-full rounded-none border-y font-sans text-xs text-muted-foreground" onClick={() => setExpanded(true)}>Show {end - i} unchanged lines</Button>
          }
          const unified = singleSide ? before == null ? row.after !== undefined && line(row.after, row.newLine, "added") : row.before !== undefined && line(row.before, row.oldLine, "removed") : <>{row.changed ? <>{row.before !== undefined && line(row.before, row.oldLine, "removed")}{row.after !== undefined && line(row.after, row.newLine, "added")}</> : line(row.after, row.newLine)}</>
          return <div key={i} data-row={i}>
            {split ? <><div className="hidden min-w-0 grid-cols-2 divide-x md:grid">{before == null ? <div className="px-4 font-sans text-sm text-muted-foreground">{i === 0 ? "Not present in cluster" : ""}</div> : line(row.before, row.oldLine, row.changed && row.before !== undefined ? "removed" : undefined)}{after == null ? <div className="px-4 font-sans text-sm text-muted-foreground">{i === 0 ? excluded ? "Not present in Git · deletion excluded" : "Resource will be deleted" : ""}</div> : line(row.after, row.newLine, row.changed && row.after !== undefined ? "added" : undefined)}</div><div className="md:hidden">{unified}</div></> : unified}
          </div>
        })}
      </div>
    </div>
  </section>
}
