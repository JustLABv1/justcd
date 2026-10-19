export type EmbeddedFile = {
  key: string
  before?: string
  after?: string
  language: string
  status: "added" | "removed" | "changed" | "unchanged"
}

const languages: Record<string, string> = {
  ini: "ini",
  conf: "ini",
  properties: "ini",
  json: "json",
  yaml: "yaml",
  yml: "yaml",
  toml: "toml",
  xml: "xml",
  html: "html",
  css: "css",
  sh: "shell",
  bash: "bash",
  js: "javascript",
  ts: "typescript",
  sql: "sql",
}

function configData(value: unknown): Record<string, unknown> {
  if (
    !value ||
    typeof value !== "object" ||
    !("kind" in value) ||
    value.kind !== "ConfigMap"
  )
    return {}
  const data = "data" in value ? value.data : null
  return data && typeof data === "object" && !Array.isArray(data)
    ? (data as Record<string, unknown>)
    : {}
}

/** ConfigMap data is already decoded. Never base64-decode Secret/binaryData values. */
export function embeddedFiles(before: unknown, after: unknown): EmbeddedFile[] {
  const oldData = configData(before),
    newData = configData(after)
  return [...new Set([...Object.keys(oldData), ...Object.keys(newData)])]
    .flatMap((key): EmbeddedFile[] => {
      const oldValue = Object.hasOwn(oldData, key) ? oldData[key] : undefined,
        newValue = Object.hasOwn(newData, key) ? newData[key] : undefined
      if (
        (oldValue !== undefined && typeof oldValue !== "string") ||
        (newValue !== undefined && typeof newValue !== "string")
      )
        return []
      const extension = key.split(".").at(-1)?.toLowerCase() ?? ""
      const language = Object.hasOwn(languages, extension)
        ? languages[extension]
        : "text"
      if (
        language === "text" &&
        !oldValue?.includes("\n") &&
        !newValue?.includes("\n")
      )
        return []
      return [
        {
          key,
          before: oldValue,
          after: newValue,
          language,
          status:
            oldValue === newValue
              ? "unchanged"
              : oldValue === undefined
                ? "added"
                : newValue === undefined
                  ? "removed"
                  : "changed",
        },
      ]
    })
    .sort((a, b) => {
      const rank = { changed: 0, added: 1, removed: 2, unchanged: 3 }
      return rank[a.status] - rank[b.status] || a.key.localeCompare(b.key)
    })
}

/** Keep changes outside the extracted files prominent in the file-focused view. */
export function otherManifestChanges(
  before: unknown,
  after: unknown,
  files: EmbeddedFile[]
): boolean {
  const strip = (value: unknown) =>
    JSON.stringify(value, function (key, child) {
      if (
        key === "data" &&
        this === value &&
        child &&
        typeof child === "object"
      ) {
        return Object.fromEntries(
          Object.entries(child).filter(
            ([name]) => !files.some((file) => file.key === name)
          )
        )
      }
      return child
    })
  return strip(before) !== strip(after)
}

export type TextRange = { start: number; end: number }

/** Bounded word diff; huge lines fall back to their common prefix/suffix. */
export function changedTextRanges(
  before: string,
  after: string
): { before: TextRange[]; after: TextRange[] } {
  if (before === after) return { before: [], after: [] }
  const fallback = () => {
    let start = 0,
      end = 0
    while (
      start < Math.min(before.length, after.length) &&
      before[start] === after[start]
    )
      start++
    while (
      end < Math.min(before.length, after.length) - start &&
      before[before.length - end - 1] === after[after.length - end - 1]
    )
      end++
    return {
      before:
        start < before.length - end
          ? [{ start, end: before.length - end }]
          : [],
      after:
        start < after.length - end ? [{ start, end: after.length - end }] : [],
    }
  }
  if (before.length + after.length > 20000) return fallback()
  const words = (text: string) => [
    ...text.matchAll(/\s+|[\p{L}\p{N}_]+|[^\s\p{L}\p{N}_]/gu),
  ]
  const oldWords = words(before),
    newWords = words(after)
  if (oldWords.length * newWords.length > 40000) return fallback()
  const matches = Array.from(
    { length: oldWords.length + 1 },
    () => new Uint32Array(newWords.length + 1)
  )
  for (let i = oldWords.length - 1; i >= 0; i--)
    for (let j = newWords.length - 1; j >= 0; j--)
      matches[i][j] =
        oldWords[i][0] === newWords[j][0]
          ? matches[i + 1][j + 1] + 1
          : Math.max(matches[i + 1][j], matches[i][j + 1])
  const result: { before: TextRange[]; after: TextRange[] } = {
    before: [],
    after: [],
  }
  const push = (side: TextRange[], word: RegExpMatchArray) => {
    const start = word.index!,
      end = start + word[0].length
    if (side.at(-1)?.end === start) side[side.length - 1].end = end
    else side.push({ start, end })
  }
  let i = 0,
    j = 0
  while (i < oldWords.length || j < newWords.length) {
    if (
      i < oldWords.length &&
      j < newWords.length &&
      oldWords[i][0] === newWords[j][0]
    ) {
      i++
      j++
    } else if (
      j < newWords.length &&
      (i === oldWords.length || matches[i][j + 1] >= matches[i + 1][j])
    )
      push(result.after, newWords[j++])
    else push(result.before, oldWords[i++])
  }
  return result
}
