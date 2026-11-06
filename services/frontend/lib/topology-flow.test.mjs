import assert from "node:assert/strict"
import test from "node:test"
import { flowModel } from "./topology-flow.ts"

const node = (id, kind) => ({ id, identity: { apiVersion: "v1", kind, namespace: "demo", name: id }, source: "kubernetes" })
const edge = (from, to, relation) => ({ from, to, relation })

test("builds a traffic → service → workload pipeline with pods grouped by replica set", () => {
  const graph = {
    nodes: ["ing", "svc", "web", "rs1", "rs2", "p1", "p2", "p3", "cfg", "lonely"].map((id) => node(id, { ing: "Ingress", svc: "Service", web: "Deployment", rs1: "ReplicaSet", rs2: "ReplicaSet", p1: "Pod", p2: "Pod", p3: "Pod", cfg: "ConfigMap", lonely: "Secret" }[id])),
    edges: [edge("ing", "svc", "routes to"), edge("svc", "p1", "selects"), edge("svc", "p3", "selects"), edge("web", "rs1", "owns"), edge("web", "rs2", "owns"), edge("rs1", "p1", "owns"), edge("rs1", "p2", "owns"), edge("rs2", "p3", "owns"), edge("web", "cfg", "uses")],
    warnings: [],
  }
  const model = flowModel(graph)
  assert.equal(model.rows.length, 1)
  const [row] = model.rows
  assert.deepEqual(row.traffic.map((n) => n.id), ["ing"])
  assert.deepEqual(row.services.map((n) => n.id), ["svc"])
  assert.deepEqual(row.groups.map((g) => [g.replicaSet.id, g.pods.map((p) => p.id)]), [["rs1", ["p1", "p2"]], ["rs2", ["p3"]]])
  assert.deepEqual(row.config.map((n) => n.id), ["cfg"])
  assert.deepEqual(model.other.map((n) => n.id), ["lonely"])
})

test("a standalone pod is its own workload", () => {
  const model = flowModel({ nodes: [node("p", "Pod")], edges: [], warnings: [] })
  assert.equal(model.rows[0].pods.length, 1)
  assert.equal(model.rows[0].groups[0].replicaSet, null)
})

test("a route owned by an ingress is not listed as an unattached resource", () => {
  const graph = { nodes: [node("ing", "Ingress"), node("route", "Route"), node("svc", "Service"), node("web", "Deployment"), node("p", "Pod")], edges: [edge("ing", "route", "owns"), edge("ing", "svc", "routes to"), edge("route", "svc", "routes to"), edge("svc", "p", "selects"), edge("web", "p", "owns")], warnings: [] }
  const model = flowModel(graph)
  assert.deepEqual(model.rows[0].traffic.map((n) => n.id), ["ing"])
  assert.deepEqual(model.other, [])
})
