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

export default function ApplicationsPage() {
  return (
    <Suspense fallback={<CollectionSkeleton />}>
      <ApplicationsContent />
    </Suspense>
  )
}

function ApplicationsContent() {
  const { projects, applications, loading, error, refresh } = useWorkspace()
  const searchParams = useSearchParams()
  const initialFilter = ["attention", "synced", "other"].includes(
    searchParams.get("status") ?? ""
  )
    ? searchParams.get("status")!
    : "all"
  return (
    <>
      <p className="mb-2 text-[10px] font-semibold tracking-[0.18em] text-muted-foreground uppercase">
        Build. Review. Deliver.
      </p>
      <PageHeading
        title="Applications"
        description="Your deployments, from Git revision to running workload."
        actions={
          <ActionLink href="/applications/new">
            <span aria-hidden="true">＋</span> New application
          </ActionLink>
        }
      />
      {error ? (
        <LoadError message={error} retry={refresh} />
      ) : loading ? (
        <CollectionSkeleton />
      ) : (
        <ApplicationCollection
          key={initialFilter}
          initialFilter={initialFilter}
          applications={applications}
          projects={projects}
        />
      )}
    </>
  )
}
