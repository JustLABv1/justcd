export type Publication = {
  completionStatus?: string
  completionError?: string
  pipelineUrl?: string
  managedCompletion?: boolean
}

// Completed means the manifest is committed; deployment review is a separate step.
export function isPublishingInProgress(
  publication: Publication | null
): boolean {
  if (!publication?.managedCompletion) return false
  return [
    "",
    "pending",
    "retrying",
    "build_created",
    "build_pending",
    "build_waiting_for_resource",
    "build_preparing",
    "build_running",
    "build_success",
  ].includes(publication.completionStatus ?? "")
}
