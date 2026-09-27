"use client"

import { createContext, useContext } from "react"
import type { Workspace } from "@/lib/types"

type WorkspaceSelection = {
  workspaces: Workspace[]
  workspaceId: string
  workspace: Workspace | null
  selectWorkspace: (id: string) => void
}

const WorkspaceSelectionContext = createContext<WorkspaceSelection | null>(null)

export function WorkspaceSelectionProvider({
  value,
  children,
}: {
  value: WorkspaceSelection
  children: React.ReactNode
}) {
  return (
    <WorkspaceSelectionContext.Provider value={value}>
      {children}
    </WorkspaceSelectionContext.Provider>
  )
}

export function useWorkspaceSelection() {
  const value = useContext(WorkspaceSelectionContext)
  if (!value) {
    throw new Error("useWorkspaceSelection must be used within its provider")
  }
  return value
}
