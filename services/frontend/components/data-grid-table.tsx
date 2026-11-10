"use client"

import { useState } from "react"
import { Button } from "@/components/panelui/button"
import { Card } from "@/components/panelui/card"
import { Table } from "@/components/panelui/table"
import { EmptyState } from "@/components/ui-kit"

export type GridColumn<T extends object> = {
  id: string
  title: string
  cell: (row: T) => React.ReactNode
  size?: number
}

const PAGE = 25

export function DataGridList<T extends object>({
  rows,
  columns,
  empty = "Nothing here yet.",
  emptyTitle = "Nothing to show yet",
  emptyDescription,
}: {
  rows: T[]
  columns: GridColumn<T>[]
  /** Backwards-compatible alias for `emptyDescription`. */
  empty?: string
  emptyTitle?: string
  emptyDescription?: string
}) {
  const [shown, setShown] = useState(PAGE)
  if (rows.length === 0) {
    return <EmptyState title={emptyTitle} description={emptyDescription ?? empty} />
  }
  const visible = rows.slice(0, shown)
  return (
    <Card className="min-w-0 overflow-hidden">
      <Table density="compact" maxHeight={520} framed>
        <Table.Header>
          <Table.Row>
            {columns.map((column) => <Table.Head key={column.id} style={column.size ? { minWidth: column.size } : undefined}>{column.title}</Table.Head>)}
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {visible.map((row, index) => (
            <Table.Row key={index}>
              {columns.map((column) => <Table.Cell key={column.id}>{column.cell(row)}</Table.Cell>)}
            </Table.Row>
          ))}
        </Table.Body>
      </Table>
      {rows.length > shown && (
        <div className="flex items-center justify-between gap-3 border-t px-4 py-2 text-sm text-muted-foreground">
          <span>Showing {shown} of {rows.length}</span>
          <Button type="button" size="sm" variant="outline" onClick={() => setShown((current) => current + PAGE)}>Show more</Button>
        </div>
      )}
    </Card>
  )
}
