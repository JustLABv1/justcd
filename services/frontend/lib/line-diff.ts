export type JsonLineDiff = {
  before: string[]
  after: string[]
  removed: Set<number>
  added: Set<number>
}
export type ReviewRow = { before?: string; after?: string; oldLine?: number; newLine?: number; changed: boolean }

/** Align unchanged context and pair replacement lines without hiding additions or deletions. */
export function reviewRows(before: unknown, after: unknown): ReviewRow[] {
  const diff = diffJsonLines(before ?? null, after ?? null)
  const rows: ReviewRow[] = []
  let i = 0, j = 0
  while (i < diff.before.length || j < diff.after.length) {
    if (diff.removed.has(i) || diff.added.has(j)) {
      const oldLine = diff.removed.has(i) ? i++ : undefined
      const newLine = diff.added.has(j) ? j++ : undefined
      rows.push({ before: oldLine === undefined ? undefined : diff.before[oldLine], after: newLine === undefined ? undefined : diff.after[newLine], oldLine: oldLine === undefined ? undefined : oldLine + 1, newLine: newLine === undefined ? undefined : newLine + 1, changed: true })
    } else {
      rows.push({ before: diff.before[i], after: diff.after[j], oldLine: i < diff.before.length ? i + 1 : undefined, newLine: j < diff.after.length ? j + 1 : undefined, changed: false })
      i++; j++
    }
  }
  return rows
}

export function contextRows(rows: ReviewRow[], context = 3): Set<number> {
  const visible = new Set<number>()
  rows.forEach((row, index) => {
    if (row.changed) for (let n = Math.max(0, index - context); n <= Math.min(rows.length - 1, index + context); n++) visible.add(n)
  })
  return visible
}

function sameLine(left: string, right: string) {
  return left.replace(/,\s*$/, "") === right.replace(/,\s*$/, "")
}

/** Compare formatted manifests without losing their existing JSON layout. */
export function diffJsonLines(before: unknown, after: unknown): JsonLineDiff {
  const oldLines = JSON.stringify(before, null, 2).split("\n")
  const newLines = JSON.stringify(after, null, 2).split("\n")
  const removed = new Set<number>()
  const added = new Set<number>()

  let prefix = 0
  while (prefix < oldLines.length && prefix < newLines.length && sameLine(oldLines[prefix], newLines[prefix])) prefix++
  let oldEnd = oldLines.length
  let newEnd = newLines.length
  while (oldEnd > prefix && newEnd > prefix && sameLine(oldLines[oldEnd - 1], newLines[newEnd - 1])) {
    oldEnd--
    newEnd--
  }

  const oldCount = oldEnd - prefix
  const newCount = newEnd - prefix
  if (!oldCount && !newCount) return { before: oldLines, after: newLines, removed, added }

  // A very large manifest should still render promptly. In that case, only the
  // shared prefix/suffix are known to be unchanged; highlight the middle.
  if (oldCount * newCount > 1_000_000) {
    for (let i = prefix; i < oldEnd; i++) removed.add(i)
    for (let i = prefix; i < newEnd; i++) added.add(i)
    return { before: oldLines, after: newLines, removed, added }
  }

  const matches = Array.from({ length: oldCount + 1 }, () => new Uint32Array(newCount + 1))
  for (let i = oldCount - 1; i >= 0; i--) {
    for (let j = newCount - 1; j >= 0; j--) {
      matches[i][j] = sameLine(oldLines[prefix + i], newLines[prefix + j])
        ? matches[i + 1][j + 1] + 1
        : Math.max(matches[i + 1][j], matches[i][j + 1])
    }
  }

  let i = 0
  let j = 0
  while (i < oldCount || j < newCount) {
    if (i < oldCount && j < newCount && sameLine(oldLines[prefix + i], newLines[prefix + j])) {
      i++
      j++
    } else if (j < newCount && (i === oldCount || matches[i][j + 1] >= matches[i + 1][j])) {
      added.add(prefix + j++)
    } else {
      removed.add(prefix + i++)
    }
  }
  return { before: oldLines, after: newLines, removed, added }
}
