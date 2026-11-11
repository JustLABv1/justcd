import { WorkspaceSettingsPage } from "@/components/workspace-settings-page"

export default async function WorkspaceSettingsSectionPage({ params }: { params: Promise<{ section: string }> }) {
  const { section } = await params
  return <WorkspaceSettingsPage section={section} />
}
