import assert from "node:assert/strict"
import test from "node:test"
import { planRunProgress, syncRunProgress, planTaskHasOperation } from "./run-progress.ts"

const identity = (name) => ({ clusterId: "cluster", apiVersion: "v1", kind: "ConfigMap", namespace: "demo", name })
const change = (kind, name) => ({ kind, identity: identity(name) })
const plan = { id: "plan", plan: { changes: [change("create", "a"), change("update", "b"), change("delete", "c")] } }
const operation = (phase, status = "running", completed = []) => ({ id: "sync", planId: "plan", status, type: "sync", message: "", startedAt: "2026-10-12T10:00:00Z", progress: { phase, total: 3, completed } })
const states = (view) => view.stages.map((stage) => [stage.id, stage.status])

test("plan generation uses only reported task states and shows no fabricated percentage", () => {
  const task = { id: "task", kind: "plan", status: "queued", createdAt: "2026-10-12T10:00:00Z" }
  assert.deepEqual(states(planRunProgress(task)), [["queued", "queued"], ["generating", "pending"], ["ready", "pending"]])
  assert.deepEqual(states(planRunProgress({ ...task, status: "running" })), [["queued", "complete"], ["generating", "running"], ["ready", "pending"]])
  const done = planRunProgress({ ...task, status: "succeeded", result: plan })
  assert.ok(done.stages.every((stage) => stage.status === "complete"))
  assert.equal(done.stages.at(-1).details[0].value, "3")
})

test("plan failure skips review and does not invent missing timestamps", () => {
  const view = planRunProgress({ status: "failed", createdAt: "2026-10-12T10:00:00Z", startedAt: "2026-10-12T10:00:01Z" })
  assert.deepEqual(states(view), [["queued", "complete"], ["generating", "failed"], ["ready", "skipped"]])
  assert.equal(view.stages.at(-1).time, undefined)
})

test("sync phases retain the actual completed counts for apply and delete", () => {
  const view = syncRunProgress(operation("deleting", "running", [identity("a"), identity("b")]), plan)
  assert.deepEqual(states(view), [["queued", "complete"], ["validating", "complete"], ["applying", "complete"], ["deleting", "running"], ["complete", "pending"]])
  assert.equal(view.stages[2].details[0].value, "2 / 2")
  assert.equal(view.stages[3].details[0].value, "0 / 1")
})

test("failed and cancelled syncs stop at the reported phase", () => {
  for (const status of ["failed", "cancelled"]) {
    const view = syncRunProgress(operation("applying", status, [identity("a")]), plan)
    assert.equal(view.stages[2].status, status)
    assert.equal(view.stages[2].details[0].value, "1 / 2")
    assert.ok(view.stages.slice(3).every((stage) => stage.status === "skipped"))
  }
})

test("queued and unknown phases do not claim validation or apply finished", () => {
  const queued = syncRunProgress(operation("applying", "queued"), plan)
  assert.equal(queued.current, 1)
  assert.ok(queued.stages.slice(1).every((stage) => stage.status === "pending"))
  const unknown = syncRunProgress(operation(undefined), plan)
  assert.equal(unknown.current, 1)
  assert.ok(unknown.stages.slice(1).every((stage) => stage.status === "pending"))
})

test("a different plan cannot supply counts or extra stages", () => {
  const view = syncRunProgress(operation("applying", "running", [identity("a")]), { ...plan, id: "different" })
  assert.equal(view.stages.some((stage) => stage.id === "deleting"), false)
  assert.equal(view.stages[2].details[0].value, "1 / 3")
  assert.doesNotThrow(() => syncRunProgress({ status: "queued" }))
})

test("plans without deletions omit that stage and zero changes can complete", () => {
  const view = syncRunProgress(operation("applying"), { ...plan, plan: { changes: [change("create", "a")] } })
  assert.equal(view.stages.some((stage) => stage.id === "deleting"), false)
  const zero = syncRunProgress({ ...operation("complete", "succeeded"), progress: { phase: "complete", total: 0, completed: [] } }, { ...plan, plan: { changes: [] } })
  assert.ok(zero.stages.every((stage) => stage.status === "complete"))
  assert.equal(zero.stages[2].details[0].value, "0 / 0")
  assert.match(zero.summary, /Runtime readiness is checked separately/)
})

test("the last phase is still running until the operation reports success", () => {
  const view = syncRunProgress(operation("complete"), plan)
  assert.equal(view.stages.at(-1).status, "running")
  assert.ok(view.current <= view.stages.length)
})

test("durable resource results supply phase counts including namespace prerequisites", () => {
  const op = operation("applying", "running", [identity("namespace")])
  op.progress.resources = [
    { identity: identity("namespace"), action: "create_namespace", status: "succeeded" },
    { identity: identity("api"), action: "create", status: "retrying" },
    { identity: identity("old"), action: "delete", status: "queued" },
  ]
  const progress = syncRunProgress(op)
  assert.equal(progress.stages.find(stage => stage.id === "applying").details[0].value, "1 / 2")
  assert.equal(progress.stages.find(stage => stage.id === "deleting").details[0].value, "0 / 1")
})


test("schedule-deferred syncs explain their wait, while overrides return to worker status", () => {
  const queued = { ...operation(undefined, "queued"), scheduleDeferred: true, waitingReasons: ["Workspace: deployment freeze — Maintenance"] }
  assert.match(syncRunProgress(queued).summary, /deployment schedule/)
  assert.match(syncRunProgress({ ...queued, scheduleOverrideReason: "Incident recovery" }).summary, /sync worker/)
  assert.match(syncRunProgress({ ...queued, status: "running", progress: { phase: "applying", completed: [], total: 3 } }).summary, /resource changes/)
})


test("a generated plan notice is superseded throughout its sync lifecycle", () => {
  const task = { id: "generation", applicationId: "app", kind: "plan", status: "succeeded", result: { id: "plan" } }
  assert.equal(planTaskHasOperation(task, []), false)
  for (const status of ["queued", "running", "succeeded", "failed", "cancelled"]) {
    assert.equal(planTaskHasOperation(task, [{ applicationId: "app", planId: "plan", status, type: "sync" }]), true)
  }
})

test("unrelated syncs cannot hide new, failed, or incomplete plan work", () => {
  const task = { id: "generation", applicationId: "app", kind: "plan", status: "succeeded", result: { id: "new-plan" } }
  const operations = [{ applicationId: "app", planId: "old-plan", status: "running", type: "sync" }]
  assert.equal(planTaskHasOperation(task, operations), false)
  const matching = [{ ...operations[0], planId: "new-plan" }]
  for (const status of ["queued", "running", "failed"]) assert.equal(planTaskHasOperation({ ...task, status }, matching), false)
  for (const result of [undefined, null, {}, { id: 42 }]) assert.equal(planTaskHasOperation({ ...task, result }, matching), false)
  assert.equal(planTaskHasOperation({ ...task, kind: "approval" }, matching), false)
  assert.equal(planTaskHasOperation(task, [{ ...matching[0], applicationId: "other-app" }]), false)
  assert.equal(planTaskHasOperation(task, [{ ...matching[0], type: "resource_action" }]), false)
})
