"use client"

import { useParams } from "next/navigation"
import { SettingsWorkspace } from "@/components/settings-workspace"

export default function ProjectConnectionPage() {
  const { projectID, section } = useParams<{ projectID: string; section: string }>()
  return <SettingsWorkspace key={projectID} fixedProjectId={projectID} sectionOverride={section} />
}
