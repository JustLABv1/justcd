import { redirect } from "next/navigation"

export default async function WorkspaceConnectionsPage({ params }: { params: Promise<{ workspaceID: string }> }) {
  const { workspaceID } = await params
  redirect(`/workspaces/${encodeURIComponent(workspaceID)}/connections/git-sources`)
}
