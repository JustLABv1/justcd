import assert from "node:assert/strict"
import test from "node:test"
import { groupApplications } from "./application-grouping.ts"

const classify = (a) => (a.health === "synced" ? "synced" : "attention")
const app = (id, clusterId, extra = {}) => ({ id, clusterId, clusterName: clusterId.toUpperCase(), namespaces: [{ namespace: "ns" }], health: "synced", healthCondition: { status: "Healthy" }, ...extra })

test("groups by cluster with stable order and label", () => {
  const groups = groupApplications([app("1", "b"), app("2", "a"), app("3", "b")], "cluster", classify)
  assert.deepEqual(groups.map((g) => [g.key, g.label, g.applications.length]), [["a", "A", 1], ["b", "B", 2]])
})

test("status mode orders attention first and none keeps one bucket", () => {
  const apps = [app("1", "a"), app("2", "a", { health: "degraded" })]
  assert.deepEqual(groupApplications(apps, "status", classify).map((g) => g.key), ["attention", "synced"])
  assert.equal(groupApplications(apps, "none", classify).length, 1)
  assert.equal(groupApplications([], "none", classify).length, 0)
})

test("label grouping separates missing and empty values without losing applications", () => {
  const apps = [app("1", "a", { labels: { team: "platform" } }), app("2", "a", { labels: { team: "payments" } }), app("3", "a", { labels: {} }), app("4", "a", { labels: { team: "" } }), app("5", "a", { labels: { team: "platform" } })]
  const groups = groupApplications(apps, "label", classify, "team")
  assert.equal(groups.length, 4)
  assert.deepEqual(groups.find(group => group.label === "platform").applications.map(app => app.id), ["1", "5"])
  assert.equal(groups.find(group => group.label === "No team label").applications[0].id, "3")
  assert.equal(groups.find(group => group.label === "(empty value)").applications[0].id, "4")
  assert.equal(groups.flatMap(group => group.applications).length, apps.length)
})
