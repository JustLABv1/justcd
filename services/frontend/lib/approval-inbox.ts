export type ApprovalOutcome = {
  requiredApprovals?: number
  approvedApprovals?: number
  operation?: { status: string }
  autoApplyError?: string
}

export function approvalOutcomeMessage(outcome: ApprovalOutcome | undefined) {
  if (outcome?.autoApplyError) return outcome.autoApplyError
  if (outcome?.operation) return outcome.operation.status === "running"
    ? "Approval recorded. Sync is running."
    : "Approval recorded. Sync is queued."
  if (outcome?.requiredApprovals && (outcome.approvedApprovals ?? 0) < outcome.requiredApprovals)
    return "Your approval is recorded. Waiting for additional approvers."
  return "Approval recorded. Open the application to review deployment status."
}
