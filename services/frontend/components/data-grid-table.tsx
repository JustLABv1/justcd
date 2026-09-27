"use client"

import { useMemo } from "react"
import {
  createColumnHelper,
  useTable,
} from "@tanstack/react-table"
import {
  DataGrid,
  DataGridContainer,
  dataGridFeatures,
} from "@/components/reui/data-grid/data-grid"
import { DataGridTable } from "@/components/reui/data-grid/data-grid-table"
import { DataGridPagination } from "@/components/reui/data-grid/data-grid-pagination"
import { EmptyState } from "@/components/ui-kit"

export type GridColumn<T extends object> = {
  id: string
  title: string
  cell: (row: T) => React.ReactNode
  size?: number
}

export function DataGridList<T extends object>({
  rows,
  columns,
  empty = "Nothing here yet.",
}: {
  rows: T[]
  columns: GridColumn<T>[]
  empty?: string
}) {
  const definitions = useMemo(
    () => {
      const helper = createColumnHelper<typeof dataGridFeatures, T>()
      return helper.columns(
        columns.map((column) =>
          helper.display({
            id: column.id,
            header: column.title,
            cell: ({ row }) => column.cell(row.original),
            size: column.size,
            meta: { headerTitle: column.title },
          }),
        ),
      )
    },
    [columns],
  )
  const table = useTable({
    features: dataGridFeatures,
    data: rows,
    columns: definitions,
  })

  return (
    <div className="min-w-0 overflow-hidden rounded-xl border bg-card">
      {rows.length === 0 ? (
        <EmptyState title="No items yet" description={empty} />
      ) : (
        <DataGrid
          table={table}
          recordCount={rows.length}
          tableLayout={{ dense: true, rowBorder: true, width: "fixed", headerSticky: true }}
        >
          <DataGridContainer className="max-h-[460px] overflow-auto">
            <DataGridTable />
          </DataGridContainer>
          <div className="border-t px-4 py-1">
            <DataGridPagination sizes={[10, 25, 50]} />
          </div>
        </DataGrid>
      )}
    </div>
  )
}
