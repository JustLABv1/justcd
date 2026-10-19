import assert from "node:assert/strict"
import test from "node:test"
import { aggregateRuntime, connectionSignal, mapNodeHeight, mapNodeHeightFor, mapNodeWidth, resourceURLs, podSummary, topologyUpstream, topologyContextLayout, topologyVisibleEdges, runtimeSignal, topologyHierarchy, topologyMapLayout, topologyScope } from "./topology-map.ts"

const node = (id, kind = "Deployment", extra = {}) => ({ id, identity: { apiVersion: "v1", kind, namespace: "demo", name: id }, source: "kubernetes", ...extra })
const edge = (from, to, relation = "owns") => ({ from, to, relation })
const graph = { nodes: [node("web", "Deployment", { tracked: true }), node("rs", "ReplicaSet"), node("pod", "Pod"), node("config", "ConfigMap", { tracked: true }), node("standalone", "Pod", { tracked: true })], edges: [edge("web", "rs"), edge("rs", "pod"), edge("web", "config", "uses")], warnings: [] }

test("drill-down reveals pods directly and preserves standalone tracked resources", () => {
  assert.deepEqual(topologyScope(graph, null).map((n) => n.id), ["web", "config", "standalone"])
  assert.deepEqual(topologyScope(graph, "web").map((n) => n.id), ["web", "pod"])
  assert.deepEqual(topologyScope(graph, "rs").map((n) => n.id), ["rs", "pod"])
  assert.deepEqual(topologyHierarchy(graph).ancestry("pod"), ["web", "rs"])
  assert.deepEqual(topologyScope(graph, "deleted"), topologyScope(graph, null))
})

test("shared dependencies never become children and malformed ownership terminates", () => {
  const malformed = { nodes: [node("a"), node("b")], edges: [edge("a", "b"), edge("b", "a"), edge("a", "missing"), edge("a", "b"), edge("a", "a")] }
  const hierarchy = topologyHierarchy(malformed)
  assert.deepEqual(hierarchy.descendants("a").map((n) => n.id), ["b"])
  assert.deepEqual(hierarchy.ancestry("a"), ["b"])
  assert.equal(topologyHierarchy(graph).children.get("web").length, 1)
})

test("collapsed parents retain descendant failures without treating unknown health as healthy", () => {
  const parent = node("web", "Deployment", { readiness: "Ready" })
  const starting = node("pod", "Pod", { phase: "Running", readiness: "Not ready" })
  const failed = node("bad", "Pod", { phase: "Failed" })
  assert.equal(aggregateRuntime(parent, [starting]), "progressing")
  assert.equal(aggregateRuntime(parent, [starting, failed]), "degraded")
  assert.equal(aggregateRuntime(node("unknown"), [parent]), "unknown")
  assert.equal(runtimeSignal(node("sample", "Pod", { source: "sample", readiness: "Ready" })), "unknown")
  assert.equal(runtimeSignal(node("done", "Pod", { phase: "Succeeded" })), "healthy")
  assert.equal(runtimeSignal(node("missing", "Deployment", { health: { status: "Missing" } })), "degraded")
})

test("connection activity distinguishes deployment progress, unready resources, and failures", () => {
  assert.equal(connectionSignal("healthy", "healthy", false), "healthy")
  assert.equal(connectionSignal("unknown", "unknown", false), "unknown")
  assert.equal(connectionSignal("healthy", "healthy", true), "syncing")
  assert.equal(connectionSignal("healthy", "progressing", false), "progressing")
  assert.equal(connectionSignal("healthy", "degraded", true), "degraded")
  assert.equal(connectionSignal("unknown", "unknown", false, true), "degraded")
})

test("large groups have deterministic, non-overlapping positions", () => {
  const nodes = [node("parent"), ...Array.from({ length: 60 }, (_, index) => node(`pod-${index}`, "Pod"))]
  for (const parent of [null, "parent"]) {
    const layout = topologyMapLayout(nodes, parent)
    assert.deepEqual(layout, topologyMapLayout([...nodes].reverse(), parent))
    assert.equal(layout.size, nodes.length)
    const positions = [...layout.values()]
    positions.forEach((a, i) => positions.slice(i + 1).forEach((b) => assert.ok(a.x + mapNodeWidth <= b.x || b.x + mapNodeWidth <= a.x || a.y + mapNodeHeight <= b.y || b.y + mapNodeHeight <= a.y)))
  }
})

test("long names receive enough height and do not overlap the following resource", () => {
  const nodes=[node("a".repeat(253)),node("b".repeat(63)),node("c")]
  assert.ok(mapNodeHeightFor(nodes[0])>mapNodeHeightFor(nodes[1]))
  for (const parent of [null,nodes[0].id]) {
    const layout=topologyMapLayout(nodes,parent)
    nodes.forEach((a,i)=>nodes.slice(i+1).forEach(b=>{
      const pa=layout.get(a.id),pb=layout.get(b.id)
      assert.ok(pa.x+mapNodeWidth<=pb.x||pb.x+mapNodeWidth<=pa.x||pa.y+mapNodeHeightFor(a)<=pb.y||pb.y+mapNodeHeightFor(b)<=pa.y)
    }))
  }
})

test("generated OpenShift Routes stay visible beside their owning Ingress", () => {
  const ingress=node("ingress","Ingress"),route=node("route","Route",{identity:{apiVersion:"route.openshift.io/v1",kind:"Route",namespace:"demo",name:"route"}})
  assert.equal(topologyScope({nodes:[ingress,route],edges:[edge("ingress","route")]},null).length,2)
})

test("resource URLs allow only concrete HTTP(S) addresses without embedded credentials", () => {
  assert.deepEqual(resourceURLs(node("ingress","Ingress",{urls:["https://web.example.com/app","https://web.example.com/app","javascript:alert(1)","//example.com","https://user:password@example.com","https://*.example.com/"]})),["https://web.example.com/app"])
})


test("ReplicaSets stay accessible and flattened edges follow verified ownership only", () => {
  assert.deepEqual(topologyScope(graph, "web", true).map(n => n.id), ["web", "rs", "pod"])
  assert.deepEqual(topologyVisibleEdges(graph, new Set(["web", "pod"])), [{ from: "web", to: "pod", relation: "owns", viaReplicaSet: true }])
  assert.equal(topologyVisibleEdges(graph, new Set(["config", "pod"])).length, 0)
  assert.equal(topologyVisibleEdges(graph, new Set(["web", "rs", "pod"])).filter(e => e.viaReplicaSet).length, 0)
  assert.deepEqual(topologyScope({nodes: [node("web"), node("empty", "ReplicaSet")], edges:[edge("web", "empty")]}, "web").map(n => n.id), ["web", "empty"])
})

test("upstream context follows real traffic plus ancestors without including shared configuration", () => {
  const network = { nodes: [...graph.nodes, node("entry", "Ingress"), node("svc", "Service")], edges:[...graph.edges, edge("entry", "svc", "routes to"), edge("svc", "web", "selects")] }
  assert.deepEqual(topologyUpstream(network, "rs").map(n => n.id), ["web", "entry", "svc"])
  assert.deepEqual(topologyUpstream(network, "web").map(n => n.id), ["entry", "svc"])
  assert.equal(topologyUpstream({...network, edges:[...network.edges, edge("web", "svc", "selects")]}, "web").length, 2)
})

test("pod summaries expose crash loops on the parent and label sample observations", () => {
  const pods = [node("good", "Pod", {phase:"Running",readiness:"Ready"}), node("bad", "Pod", {phase:"Running",readiness:"Not ready",healthSummary:{failureReason:"CrashLoopBackOff"}})]
  assert.equal(podSummary(pods), "1/2 pods ready · 1 CrashLoopBackOff")
  assert.equal(podSummary(pods.map(p => ({...p, source:"sample"}))), "Sample · 1/2 pods ready · 1 CrashLoopBackOff")
  assert.equal(podSummary([]), null)
})


test("upstream lanes preserve traffic order and optional ReplicaSets have their own lane", () => {
  const network={ nodes:[...graph.nodes, node("entry", "Ingress"), node("svc", "Service")], edges:[...graph.edges, edge("entry", "svc", "routes to"), edge("svc", "web", "selects")] }
  const layout=topologyContextLayout(network,"web",topologyUpstream(network,"web"),100)
  assert.ok(layout.get("entry").x < layout.get("svc").x)
  assert.equal(layout.get("entry").y,100)
  assert.equal(layout.get("svc").y,100)
  const scoped=topologyMapLayout(topologyScope(network,"web",true),"web",network.edges)
  assert.ok(scoped.get("web").x < scoped.get("rs").x)
  assert.ok(scoped.get("rs").x < scoped.get("pod").x)
})

test("Service selection is drawn once through the visible owning workload", () => {
  const network={nodes:[...graph.nodes,node("svc","Service")],edges:[...graph.edges,edge("svc","web","selects"),edge("svc","pod","selects")]}
  assert.deepEqual(topologyVisibleEdges(network,new Set(["svc","web","pod"])).filter(e=>e.relation==="selects").map(e=>e.to),["web"])
  assert.deepEqual(topologyVisibleEdges(network,new Set(["svc","pod"])).filter(e=>e.relation==="selects").map(e=>e.to),["pod"])
})
