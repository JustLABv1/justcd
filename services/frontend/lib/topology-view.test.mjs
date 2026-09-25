import assert from "node:assert/strict"
import test from "node:test"
import { relatedNodes, topologyLayout, syncState, nodeHeight, nodeWidth } from "./topology-view.ts"

const node = (id, kind = "Deployment", extra = {}) => ({ id, identity: { clusterId: "cluster", apiVersion: "v1", kind, namespace: "demo", name: id }, source: "desired", ...extra })
const edge = (from, to, relation = "uses") => ({ from, to, relation })

test("focusing a workload does not traverse sideways through a shared config", () => {
  const graph = { nodes: [node("a"), node("b"), node("config", "ConfigMap"), node("service", "Service")], edges: [edge("a", "config"), edge("b", "config"), edge("service", "a", "selects")] }
  assert.deepEqual([...relatedNodes(graph, "a")].sort(), ["a", "config", "service"])
})

test("groups unique dependencies with their workload and identifies shared and disconnected resources", () => {
  const graph = { nodes: [node("a"), node("b"), node("shared", "ConfigMap"), node("volume", "PersistentVolumeClaim"), node("policy", "NetworkPolicy")], edges: [edge("a", "shared"), edge("b", "shared"), edge("a", "volume")] }
  const layout = topologyLayout(graph, graph.nodes)
  assert.equal(layout.positions.size, graph.nodes.length)
  assert.ok(layout.bands.some(b => b.id === "shared"))
  assert.ok(layout.bands.some(b => b.id === "unconnected"))
  const a = layout.bands.find(b => b.id === "a")
  assert.ok(layout.positions.get("volume").y >= a.y && layout.positions.get("volume").y < a.y + a.height)
  const positions = [...layout.positions.values()]
  positions.forEach((a, i) => positions.slice(i + 1).forEach(b => assert.ok(a.x + nodeWidth <= b.x || b.x + nodeWidth <= a.x || a.y + nodeHeight <= b.y || b.y + nodeHeight <= a.y)))
})

test("planned creation is not synced, health is not inferred", () => {
  const resource = node("web")
  const plan = { id: "plan", status: "current", plan: { changes: [{ identity: resource.identity, kind: "create" }] } }
  assert.equal(syncState(resource, plan), "create")
  assert.equal(syncState(node("pod", "Pod", { source: "kubernetes", readiness: "Ready" }), plan), "observed")
})

test("failed sync retains completed steps and identifies the failed current step", () => {
  const a = node("a"), b = node("b"), plan = { id: "plan", status: "failed", plan: { changes: [] } }
  const operation = { planId: "plan", status: "failed", progress: { completed: [a.identity], current: b.identity } }
  assert.equal(syncState(a, plan, operation), "applied")
  assert.equal(syncState(b, plan, operation), "failed")
  assert.equal(syncState(b, plan, { ...operation, status: "running" }), "syncing")
  assert.equal(syncState(a, plan, { ...operation, planId: "old" }), "unknown")
})

test("relationship cycles terminate and empty layouts stay valid", () => {
  assert.equal(relatedNodes({ nodes: [], edges: [edge("a", "b"), edge("b", "a")] }, "a").size, 2)
  const empty = topologyLayout({ nodes: [], edges: [] }, [])
  assert.ok(Number.isFinite(empty.width) && Number.isFinite(empty.height))
})
