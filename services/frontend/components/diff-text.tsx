"use client"

import { useEffect, useId, useState, type CSSProperties } from "react"
import {
  highlightCode,
  type CodeBlockLine,
  type CodeBlockToken,
} from "@/components/reui/code-block/code-block-highlight"
import type { TextRange } from "@/lib/embedded-diff"

export function useDiffSyntax(before: string, after: string, language: string) {
  const id = useId()
  const [result, setResult] = useState<{
    before: string
    after: string
    language: string
    lines: CodeBlockLine[][]
  } | null>(null)
  useEffect(() => {
    let active = true
    // Keep very large manifests responsive; plain text and diffs remain available.
    if (before.length + after.length > 200000) return
    Promise.all([
      highlightCode(before, { language, instanceKey: `${id}-before` }),
      highlightCode(after, { language, instanceKey: `${id}-after` }),
    ])
      .then((lines) => {
        if (active) setResult({ before, after, language, lines })
      })
      .catch(() => {
        /* Plain text is already rendered. */
      })
    return () => {
      active = false
    }
  }, [before, after, language, id])
  return result?.before === before &&
    result.after === after &&
    result.language === language
    ? result.lines
    : undefined
}

/** Reuse ReUI's tokens while preserving the virtual diff's paired rows/gutters. */
export function DiffText({
  text,
  syntax,
  ranges = [],
  tone,
}: {
  text: string
  syntax?: CodeBlockLine
  ranges?: TextRange[]
  tone?: "removed" | "added"
}) {
  const tokens: CodeBlockToken[] =
    syntax?.text === text ? syntax.tokens : [{ content: text }]
  let offset = 0
  return tokens.map((token, index) => {
    const start = offset,
      end = start + token.content.length
    offset = end
    const boundaries = [
      ...new Set([
        start,
        end,
        ...ranges
          .flatMap((range) => [range.start, range.end])
          .filter((n) => n > start && n < end),
      ]),
    ].sort((a, b) => a - b)
    return (
      <span
        key={index}
        className="[color:var(--diff-token-light)] dark:[color:var(--diff-token-dark)]"
        style={
          {
            "--diff-token-light": token.color ?? "inherit",
            "--diff-token-dark": token.colorDark ?? token.color ?? "inherit",
            fontStyle: token.fontStyle === "italic" ? "italic" : undefined,
            fontWeight: token.fontStyle === "bold" ? "bold" : undefined,
          } as CSSProperties
        }
      >
        {boundaries.slice(0, -1).map((from, n) => {
          const content = token.content.slice(
            from - start,
            boundaries[n + 1] - start
          )
          return ranges.some(
            (range) => from >= range.start && from < range.end
          ) ? (
            <mark
              key={from}
              className={`rounded-sm text-inherit ${tone === "removed" ? "bg-destructive/25" : "bg-success/25"}`}
            >
              {content}
            </mark>
          ) : (
            content
          )
        })}
      </span>
    )
  })
}
