"use client"

import {
  useImperativeHandle,
  useRef,
  type ReactNode,
  type KeyboardEvent,
} from "react"
import { defaultRangeExtractor, useVirtualizer } from "@tanstack/react-virtual"

/** Navigation and code rows are not tabular data. Reuse TanStack's measured
 * windowing (also used by installed ReUI DataGrid) with the shared controls. */
export function VirtualReviewList({
  count,
  render,
  estimate = 110,
  className = "h-48 lg:h-[60svh]",
  label,
  activeIndex,
  onKeyDown,
  role,
  activeId,
  describedBy,
  apiRef,
}: {
  count: number
  render: (index: number) => ReactNode
  estimate?: number
  className?: string
  label: string
  activeIndex?: number
  onKeyDown?: (event: KeyboardEvent<HTMLDivElement>) => void
  role?: "listbox"
  describedBy?:string
  activeId?: string
  apiRef?: React.RefObject<((index: number) => void) | null>
}) {
  const viewport = useRef<HTMLDivElement>(null)
  // React Compiler deliberately leaves TanStack Virtual un-memoized.
  // eslint-disable-next-line react-hooks/incompatible-library
  const virtualizer = useVirtualizer({
    count,
    getScrollElement: () => viewport.current,
    estimateSize: () => estimate,
    overscan: 5,
    rangeExtractor: (range) => {
      const indexes = defaultRangeExtractor(range)
      return activeIndex !== undefined && activeIndex >= 0
        ? [...new Set([...indexes, activeIndex])].sort((a, b) => a - b)
        : indexes
    },
  })
  useImperativeHandle(
    apiRef,
    () => (index) => virtualizer.scrollToIndex(index, { align: "auto" }),
    [virtualizer]
  )
  return (
    <div
      ref={viewport}
      role={role}
      tabIndex={0}
      aria-label={label}
      aria-activedescendant={activeId}
      aria-describedby={describedBy}
      onKeyDown={onKeyDown}
      onClick={
        role === "listbox"
          ? () => viewport.current?.focus({ preventScroll: true })
          : undefined
      }
      className={`relative overflow-auto focus-visible:outline-2 focus-visible:outline-ring ${className}`}
    >
      <div
        className="relative w-full"
        style={{ height: virtualizer.getTotalSize() }}
      >
        {virtualizer.getVirtualItems().map((item) => (
          <div
            key={item.key}
            data-index={item.index}
            ref={virtualizer.measureElement}
            className="absolute top-0 left-0 w-full"
            style={{ transform: `translateY(${item.start}px)` }}
          >
            {render(item.index)}
          </div>
        ))}
      </div>
    </div>
  )
}
