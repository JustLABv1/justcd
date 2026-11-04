import assert from "node:assert/strict"
import test from "node:test"
import { isPublishingInProgress } from "./publishing-status.ts"

test("publishing indicator tracks builds and completion, not deployment review", () => {
  for (const completionStatus of [
    "",
    "pending",
    "retrying",
    "build_created",
    "build_pending",
    "build_waiting_for_resource",
    "build_preparing",
    "build_running",
    "build_success",
  ]) {
    assert.equal(
      isPublishingInProgress({ managedCompletion: true, completionStatus }),
      true,
      completionStatus
    )
  }
  for (const completionStatus of [
    "completed",
    "build_failed",
    "build_canceled",
    "build_canceling",
    "build_skipped",
    "build_manual",
    "unknown",
  ]) {
    assert.equal(
      isPublishingInProgress({ managedCompletion: true, completionStatus }),
      false,
      completionStatus
    )
  }
  assert.equal(isPublishingInProgress(null), false)
  assert.equal(
    isPublishingInProgress({ completionStatus: "build_running" }),
    false
  )
})
