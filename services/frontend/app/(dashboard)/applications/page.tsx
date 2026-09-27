"use client"

import { Suspense } from "react"
import { useSearchParams } from "next/navigation"
import { ApplicationCollection } from "@/components/application-collection"
import { PageHeading } from "@/components/ui-kit"
import {
  ActionLink,
  CollectionSkeleton,
  LoadError,
} from "@/components/workspace-ui"
import { useWorkspace } from "@/hooks/use-workspace"
import { useWorkspaceSelection } from "@/hooks/workspace-selection"

export default function ApplicationsPage() {
  return (
    <Suspense fallback={<CollectionSkeleton />}>
      <ApplicationsContent />
    </Suspense>
  )
}

function ApplicationsContent() {
  const { workspaces, applications, loading, error, refresh } = useWorkspace()
  const { workspace, workspaceId } = useWorkspaceSelection()
  const searchParams = useSearchParams()
  const initialFilter = ["attention", "synced", "other"].includes(
    searchParams.get("status") ?? ""
  )
    ? searchParams.get("status")!
    : "all"
  return (
    <>
      <PageHeading
        title="Applications"
        description={workspace ? `Deployments in ${workspace.name}, from Git revision to running workload.` : "Select a workspace to view its applications."}
        actions={<>
          <ActionLink href={workspaceId ? `/applications/new?workspaceId=${encodeURIComponent(workspaceId)}` : "/workspaces"}>
            <span aria-hidden="true">＋</span> New application
          </ActionLink>
          <ActionLink href={workspaceId ? `/application-groups/new?workspaceId=${encodeURIComponent(workspaceId)}` : "/workspaces"}>
            <span aria-hidden="true">＋</span> Deploy to multiple targets
          </ActionLink>
        </>}
      />
      {error ? (
        <LoadError error={error} retry={refresh} />
      ) : loading ? (
        <CollectionSkeleton />
      ) : (
        <ApplicationCollection
          key={initialFilter}
          initialFilter={initialFilter}
          applications={applications}
          workspaces={workspaces}
        />
      )}
    </>
  )
}
