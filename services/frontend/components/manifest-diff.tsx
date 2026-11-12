"use client"

import {
  createContext,
  useContext,
  useDeferredValue,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import {
  ArrowDown01Icon,
  ArrowUp01Icon,
  LayoutTwoColumnIcon,
  Rows2Icon,
  Settings02Icon,
} from "@hugeicons/core-free-icons"
import { Input } from "@/components/panelui/input"
import { FormSelect } from "@/components/form-select"
import { DiffText, useDiffSyntax } from "@/components/diff-text"
import { Alert } from "@/components/panelui/alert"
import { VirtualReviewList } from "@/components/virtual-review-list"
import { Button } from "@/components/panelui/button"
import { Dialog } from "@/components/panelui/dialog"
import { Toolbar } from "@/components/panelui/toolbar"
import { RadioGroup, Radio } from "@/components/panelui/radio-group"
import { Label } from "@/components/panelui/label"
import { SwitchField } from "@/components/ui-kit"
import { contextRows, reviewRows, reviewTextRows } from "@/lib/line-diff"
import {
  embeddedFiles,
  otherManifestChanges,
  changedTextRanges,
  type EmbeddedFile,
} from "@/lib/embedded-diff"
import { hiddenChangedPaths } from "@/lib/plan-review"

type ManifestDiffProps = {
  before: unknown
  after: unknown
  excluded?: boolean
  changedPaths?: string[]
  takeover?: boolean
}

type DiffToolbarApi = { hosted: boolean; setControls: (node: ReactNode) => void }
const DiffToolbarApiContext = createContext<DiffToolbarApi | null>(null)
const DiffToolbarControlsContext = createContext<{
  controls: ReactNode
  setHosted: (hosted: boolean) => void
} | null>(null)

/**
 * Lets a surrounding toolbar host the diff controls instead of the diff drawing its own bar.
 * The controls are rendered by the slot, inside the toolbar's own React tree, so they can use
 * its context.
 */
export function DiffToolbarHost({ children }: { children: ReactNode }) {
  const [hosted, setHosted] = useState(false)
  const [controls, setControls] = useState<ReactNode>(null)
  const api = useMemo(() => ({ hosted, setControls }), [hosted])
  const slot = useMemo(() => ({ controls, setHosted }), [controls])
  return (
    <DiffToolbarApiContext.Provider value={api}>
      <DiffToolbarControlsContext.Provider value={slot}>
        {children}
      </DiffToolbarControlsContext.Provider>
    </DiffToolbarApiContext.Provider>
  )
}

/** Place inside the host toolbar where the diff controls should appear. */
export function DiffToolbarSlot() {
  const host = useContext(DiffToolbarControlsContext)
  const setHosted = host?.setHosted
  useEffect(() => {
    setHosted?.(true)
    return () => setHosted?.(false)
  }, [setHosted])
  return <>{host?.controls}</>
}

const wideDiffQuery = "(min-width: 768px)"
function subscribeDiffWidth(listener: () => void) {
  const query = window.matchMedia(wideDiffQuery)
  query.addEventListener("change", listener)
  return () => query.removeEventListener("change", listener)
}

export function ManifestDiff(props: ManifestDiffProps) {
  const files = useMemo(
    () => embeddedFiles(props.before, props.after),
    [props.before, props.after]
  )
  const [selected, setSelected] = useState(() =>
    files[0]?.status !== "unchanged" && files[0]
      ? `file:${files[0].key}`
      : "manifest"
  )
  const file = files.find((item) => `file:${item.key}` === selected)
  const otherChanges = useMemo(
    () => otherManifestChanges(props.before, props.after, files),
    [props.before, props.after, files]
  )
  return (
    <>
      {files.length > 0 && (
        <div className="space-y-2 border-b p-3">
          <div className="flex flex-wrap items-center gap-2">
            <FormSelect
              ariaLabel="Diff content"
              value={file ? selected : "manifest"}
              onValueChange={setSelected}
              className="sm:max-w-md"
              items={[
                {
                  value: "manifest",
                  label: `Full manifest (JSON)${otherChanges ? " · other fields changed" : ""}`,
                },
                ...files.map((item) => ({
                  value: `file:${item.key}`,
                  label: `data / ${item.key} · ${item.status}`,
                })),
              ]}
            />
            {file && (
              <span className="text-xs text-muted-foreground">
                {file.language.toUpperCase()} · Decoded file content
              </span>
            )}
          </div>
          {file && otherChanges && (
            <div className="flex flex-wrap items-center gap-x-2 text-sm">
              <span>Other manifest fields also changed.</span>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => setSelected("manifest")}
              >
                Review full manifest
              </Button>
            </div>
          )}
        </div>
      )}
      <ManifestDiffView
        key={file ? `file:${file.key}` : "manifest"}
        {...props}
        file={file}
      />
    </>
  )
}

function ManifestDiffView({
  before,
  after,
  excluded = false,
  changedPaths,
  takeover = false,
  file,
}: ManifestDiffProps & { file?: EmbeddedFile }) {
  const [mode, setMode] = useState("split")
  const [fontSize, setFontSize] = useState("14")
  const [wrap, setWrap] = useState(false)
  const [expanded, setExpanded] = useState(false)
  const [position, setPosition] = useState(-1)
  const scrollTo = useRef<((index: number) => void) | null>(null)
  const [query, setQuery] = useState("")
  const search = useDeferredValue(query)
  const optionsId = useId()
  const singleSide = file
    ? file.before === undefined || file.after === undefined
    : before == null || after == null
  const beforeMissing = file ? file.before === undefined : before == null
  const wide = useSyncExternalStore(subscribeDiffWidth, () => window.matchMedia(wideDiffQuery).matches, () => true)
  const split = !singleSide && mode === "split" && wide
  const rows = useMemo(
    () =>
      file
        ? reviewTextRows(file.before, file.after)
        : reviewRows(before, after),
    [before, after, file]
  )
  const oldSource = useMemo(
    () =>
      file ? (file.before ?? "") : JSON.stringify(before ?? null, null, 2),
    [before, file]
  )
  const newSource = useMemo(
    () => (file ? (file.after ?? "") : JSON.stringify(after ?? null, null, 2)),
    [after, file]
  )
  const syntax = useDiffSyntax(oldSource, newSource, file?.language ?? "json")
  const wordChanges = useMemo(
    () =>
      rows.map((row) =>
        row.changed && row.before !== undefined && row.after !== undefined
          ? changedTextRanges(row.before, row.after)
          : undefined
      ),
    [rows]
  )
  const hiddenPaths = useMemo(
    () => hiddenChangedPaths(before, after, changedPaths),
    [before, after, changedPaths]
  )
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
    tone?: "removed" | "added",
    index = 0,
    side: "before" | "after" = "after"
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
          className={`min-w-0 flex-1 pr-4 ${wrap || split ? "[overflow-wrap:anywhere] break-words whitespace-pre-wrap" : "overflow-x-auto whitespace-pre"}`}
        >
          <DiffText
            text={text ?? " "}
            syntax={
              number
                ? syntax?.[side === "before" ? 0 : 1]?.[number - 1]
                : undefined
            }
            ranges={wordChanges[index]?.[side]}
            tone={tone}
          />
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
  const toolbarApi = useContext(DiffToolbarApiContext)
  const host = toolbarApi?.hosted ?? false
  const compact = !host && singleSide && hunks.length <= 1
  const optionsDialog = (
    <Dialog>
      <Dialog.Trigger render={compact ? <Button variant="ghost" size="icon" aria-label={`Diff options${query ? " · Search active" : ""}`} /> : <Toolbar.Button label={`Diff options${query ? " · Search active" : ""}`} />}>
        <HugeiconsIcon icon={Settings02Icon} strokeWidth={1.8} aria-hidden="true" />
      </Dialog.Trigger>
      <Dialog.Content variant="plain" size="sm"><Dialog.Header title="Diff options" /><Dialog.Body>
        <label
          htmlFor={`${optionsId}-search`}
          className="text-sm font-medium"
        >
          Search diff text
        </label>
        <Input
          id={`${optionsId}-search`}
          aria-label="Search manifest diff"
          placeholder="Search diff text…"
          className="w-full"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
        />
        <div className="space-y-1.5">
          <p id={`${optionsId}-font`} className="text-sm font-medium">
            Code font size
          </p>
          <RadioGroup orientation="horizontal" aria-labelledby={`${optionsId}-font`} value={fontSize} onValueChange={setFontSize}>
            {[14, 16, 18].map(n => <Label key={n} className="flex w-auto items-center gap-2"><Radio value={String(n)} />{n} px</Label>)}
          </RadioGroup>
        </div>
        <SwitchField
          id={`${optionsId}-wrap`}
          label="Wrap lines"
          description="Wrap long lines instead of scrolling horizontally."
          checked={wrap}
          onCheckedChange={setWrap}
        />
        <SwitchField
          id={`${optionsId}-expand`}
          label="Show all lines"
          description="Include unchanged context lines."
          checked={expanded}
          onCheckedChange={setExpanded}
        />
      </Dialog.Body></Dialog.Content>
    </Dialog>
  )
  const toolbarControls = (
    <>
    {!singleSide && (
      <Toolbar.Group aria-label="Diff layout" value={split ? "split" : "unified"} onValueChange={setMode}>
        <Toolbar.Tool value="split" label="Side by side" disabled={!wide}><HugeiconsIcon icon={LayoutTwoColumnIcon} strokeWidth={1.8} aria-hidden="true" /></Toolbar.Tool>
        <Toolbar.Tool value="unified" label="Unified"><HugeiconsIcon icon={Rows2Icon} strokeWidth={1.8} aria-hidden="true" /></Toolbar.Tool>
      </Toolbar.Group>
    )}
    {!compact && optionsDialog}

    {search && (
      <span role="status" className="text-sm text-muted-foreground">
        {matches.length} matching lines
      </span>
    )}
    <div className={`flex items-center gap-2 group-data-[orientation=vertical]/toolbar:flex-col group-data-[orientation=vertical]/toolbar:gap-0.5 ${!host && hunks.length > 1 ? "ml-auto" : ""}`}>
      <span className="text-sm text-muted-foreground group-data-[orientation=vertical]/toolbar:sr-only" aria-live="polite">
        {hunks.length
          ? position < 0
            ? `${hunks.length} ${hunks.length === 1 ? "change" : "changes"}`
            : `${position + 1} of ${hunks.length}`
          : hiddenPaths.length
            ? `${hiddenPaths.length} hidden field ${hiddenPaths.length === 1 ? "change" : "changes"}`
            : takeover
              ? "Ownership change only"
              : "No text changes"}
      </span>
      {hunks.length > 1 && (
        <>
          <Toolbar.Button
            label="Previous change"
            disabled={!hunks.length}
            onClick={() => jump(-1)}
          >
            <HugeiconsIcon
              icon={ArrowUp01Icon}
              strokeWidth={2}
              aria-hidden="true"
            />
          </Toolbar.Button>
          <Toolbar.Button
            label="Next change"
            disabled={!hunks.length}
            onClick={() => jump(1)}
          >
            <HugeiconsIcon
              icon={ArrowDown01Icon}
              strokeWidth={2}
              aria-hidden="true"
            />
          </Toolbar.Button>
        </>
      )}
    </div>
    </>
  )
  const setToolbarControls = toolbarApi?.setControls
  useEffect(() => {
    if (host) setToolbarControls?.(toolbarControls)
  })
  useEffect(() => () => setToolbarControls?.(null), [setToolbarControls])
  return (
    <section aria-label="Manifest comparison">
      {host ? null : !compact && (
        <Toolbar variant="inline" aria-label="Manifest comparison tools" className="w-full flex-wrap gap-2 p-3">
          {toolbarControls}
        </Toolbar>
      )}
      {hiddenPaths.length > 0 && (
        <div className="p-3">
          <Alert tone="info" role="status" variant="inline">
            <Alert.Icon />
            <Alert.Description>
              <p>
                <strong>Sensitive values hidden</strong> ·{" "}
                {excluded
                  ? "excluded, not applied"
                  : "changes are included in the plan"}
                :{" "}
                {hiddenPaths.map((path, i) => (
                  <span key={path}>
                    {i > 0 && ", "}
                    <code className="break-all">{path || "/"}</code>
                  </span>
                ))}
              </p>
            </Alert.Description>
          </Alert>
        </div>
      )}
      <div
        className="bg-card font-mono leading-6"
        style={{ fontSize: Number(fontSize) }}
      >
        <div
          className={`border-b bg-muted px-3 py-2 font-sans text-sm font-medium ${compact ? "flex items-center justify-between" : "grid grid-cols-2"}`}
        >
          {!(singleSide && beforeMissing) && (
            <span>
              Live before
              {file && file.before === undefined ? " · file absent" : ""}
            </span>
          )}
          {!(singleSide && !beforeMissing) && (
            <span>
              {excluded ? "Git reference · excluded" : "Desired after"}
              {file && file.after === undefined ? " · file absent" : ""}
            </span>
          )}
          {compact && optionsDialog}
        </div>
        {singleSide && excluded && (
          <p className="border-b px-4 py-3 font-sans text-sm text-muted-foreground">
            {beforeMissing ? "Creation" : "Deletion"} of this{" "}
            {file ? "file" : "resource"} is excluded.
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
              beforeMissing ? (
                row.after !== undefined &&
                line(row.after, row.newLine, "added", i, "after")
              ) : (
                row.before !== undefined &&
                line(row.before, row.oldLine, "removed", i, "before")
              )
            ) : (
              <>
                {row.changed ? (
                  <>
                    {row.before !== undefined &&
                      line(row.before, row.oldLine, "removed", i, "before")}
                    {row.after !== undefined &&
                      line(row.after, row.newLine, "added", i, "after")}
                  </>
                ) : (
                  line(row.after, row.newLine, undefined, i, "after")
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
                            : undefined,
                          i,
                          "before"
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
                            : undefined,
                          i,
                          "after"
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
