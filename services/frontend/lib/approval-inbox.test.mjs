import assert from "node:assert/strict"
import test from "node:test"
import { approvalOutcomeMessage } from "./approval-inbox.ts"

test("approval completion distinguishes deployment from additional approvals", () => {
  assert.equal(approvalOutcomeMessage({ operation: { status: "running" } }), "Approval recorded. Sync is running.")
  assert.equal(approvalOutcomeMessage({ operation: { status: "queued" } }), "Approval recorded. Sync is queued.")
  assert.equal(approvalOutcomeMessage({ requiredApprovals: 2, approvedApprovals: 1 }), "Your approval is recorded. Waiting for additional approvers.")
  assert.equal(approvalOutcomeMessage(undefined), "Approval recorded. Open the application to review deployment status.")
})

test("recorded approvals preserve automatic deployment warnings", () => {
  assert.equal(approvalOutcomeMessage({ autoApplyError: "Approval recorded, but automatic sync could not be queued." }), "Approval recorded, but automatic sync could not be queued.")
})
