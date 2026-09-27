"use client"

import { useParams } from "next/navigation"
import { SettingsWorkspace } from "@/components/settings-workspace"

export default function WorkspaceConnectionPage() {
  const { workspaceID, section } = useParams<{ workspaceID: string; section: string }>()
  return <SettingsWorkspace key={workspaceID} fixedWorkspaceId={workspaceID} sectionOverride={section} />
}
