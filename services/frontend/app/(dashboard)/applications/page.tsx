"use client"

import { Suspense, type ReactNode } from "react"
import { useSearchParams } from "next/navigation"
import { ApplicationCollection } from "@/components/application-collection"
import { Add01Icon, Layers01Icon } from "@hugeicons/core-free-icons"
import { ActionMenu } from "@/components/action-menu"
import { PageHeading } from "@/components/ui-kit"
import {
  CollectionSkeleton,
  LoadError,
} from "@/components/workspace-ui"
import { useClusterStatuses } from "@/hooks/use-cluster-statuses"
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
  const { workspaces, applications, loading, error, refresh, updateApplication } = useWorkspace({
    includeAllApplications: true,
  })
  const { workspaceId } = useWorkspaceSelection()
  const clusterStatuses = useClusterStatuses(workspaces.map((workspace) => workspace.id))
  const searchParams = useSearchParams()
  const initialFilter = ["attention", "synced", "other", "paused"].includes(
    searchParams.get("status") ?? ""
  )
    ? searchParams.get("status")!
    : "all"
  const renderHeader = (viewMenu?: ReactNode) => (
      <PageHeading
        title="Applications"
        description="Deployments across your workspaces, from Git revision to running workload."
        actions={<div className="flex flex-wrap items-center gap-2">{viewMenu}<ActionMenu
          label="New"
          variant="default"
          icon={Add01Icon}
          items={[
            { label: "New application", icon: Add01Icon, href: workspaceId ? `/applications/new?workspaceId=${encodeURIComponent(workspaceId)}` : "/workspaces" },
            { label: "New deployment group", icon: Layers01Icon, href: workspaceId ? `/application-groups/new?workspaceId=${encodeURIComponent(workspaceId)}` : "/workspaces" },
          ]}
        /></div>}
      />
  )
  return (
    <>
      {error ? (
        <>{renderHeader()}<LoadError error={error} retry={refresh} /></>
      ) : loading ? (
        <>{renderHeader()}<CollectionSkeleton /></>
      ) : (
        <ApplicationCollection
          key={initialFilter}
          renderHeader={renderHeader}
          initialFilter={initialFilter}
          applications={applications}
          onApplicationUpdated={updateApplication}
          workspaces={workspaces}
          clusterStatuses={clusterStatuses}
        />
      )}
    </>
  )
}
