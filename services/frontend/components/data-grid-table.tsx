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

export type GridColumn<T extends object> = {
  id: string
  title: string
  cell: (row: T) => React.ReactNode
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
    <div className="overflow-hidden rounded-xl border bg-card">
      {rows.length === 0 ? (
        <div className="px-5 py-10 text-center text-sm text-muted-foreground">{empty}</div>
      ) : (
        <DataGrid
          table={table}
          recordCount={rows.length}
          tableLayout={{ dense: true, rowBorder: true, width: "fixed", headerSticky: true }}
        >
          <DataGridContainer className="max-h-[460px] overflow-auto">
            <DataGridTable />
          </DataGridContainer>
          <div className="px-3">
            <DataGridPagination sizes={[10, 25, 50]} />
          </div>
        </DataGrid>
      )}
    </div>
  )
}
