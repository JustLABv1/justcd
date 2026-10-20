import assert from "node:assert/strict"
import test from "node:test"
import { aggregateRuntime, connectionSignal, mapNodeHeight, mapNodeHeightFor, mapNodeWidth, resourceURLs, podSummary, topologyUpstream, topologyContextLayout, topologyVisibleEdges, supportingTopologyResource, topologyCanvasResources, runtimeSignal, topologyHierarchy, topologyMapLayout, topologyScope } from "./topology-map.ts"

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

test("generated OpenShift Routes are grouped under their Ingress but remain explorable", () => {
  const ingress=node("ingress","Ingress"),route=node("route","Route",{identity:{apiVersion:"route.openshift.io/v1",kind:"Route",namespace:"demo",name:"route"}})
  const network = {nodes:[ingress,route],edges:[edge("ingress","route")]}
  assert.deepEqual(topologyScope(network,null).map(n => n.id),["ingress"])
  assert.deepEqual(topologyScope(network,"ingress").map(n => n.id),["ingress","route"])
  assert.deepEqual(topologyScope({...network,nodes:[ingress,{...route,tracked:true}]},null).map(n => n.id),["ingress","route"])
  assert.equal(topologyScope({nodes:[route],edges:[]},null).length,1)
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


test("supporting inventory is separate; only explicitly enabled connected dependencies enter the canvas", () => {
  const nodes=[node("workload"),node("config","ConfigMap"),node("account","ServiceAccount"),node("binding","RoleBinding"),node("role","Role"),node("unrelated","ConfigMap"),node("operator","CustomDatabase")]
  const edges=[edge("workload","config","uses"),edge("workload","account","uses"),edge("account","binding","uses"),edge("binding","role","uses")]
  assert.deepEqual(topologyCanvasResources(nodes,edges,false).map(n=>n.id),["workload","operator"])
  assert.deepEqual(topologyCanvasResources(nodes,edges,true).map(n=>n.id),["workload","config","account","binding","role","operator"])
  assert.equal(supportingTopologyResource(node("volume","PersistentVolumeClaim")),true)
  // A hidden workload cannot serve as a bridge into its unrelated configuration.
  assert.deepEqual(topologyCanvasResources(nodes, [...edges,edge("unseen","workload","selects"),edge("unseen","unrelated","uses")], true).map(n=>n.id),["workload","config","account","binding","role","operator"])
})

test("expanded OpenShift paths remove only the duplicated Ingress-to-Service shortcut", () => {
  const nodes=[node("entry","Ingress"),node("generated","Route"),node("web","Service"),node("api","Service")]
  const network={nodes,edges:[edge("entry","generated"),edge("generated","web","routes to"),edge("entry","web","routes to"),edge("entry","api","routes to")],warnings:[]}
  const links=topologyVisibleEdges(network,new Set(nodes.map(n=>n.id)))
  assert.equal(links.some(e=>e.from==="entry"&&e.to==="web"),false)
  assert.equal(links.some(e=>e.from==="entry"&&e.to==="api"),true)
  assert.equal(topologyVisibleEdges(network,new Set(["entry","web"])).length,1)
  const layout=topologyMapLayout(nodes,null,network.edges)
  assert.ok(layout.get("entry").x<layout.get("generated").x)
  assert.ok(layout.get("generated").x<layout.get("web").x)
})

test("collapsed Ingress retains generated Route rejection instead of reporting healthy", () => {
  const ingress=node("entry","Ingress",{routingHealth:{status:"healthy"}})
  const route=node("generated","Route",{routingHealth:{status:"degraded",reason:"RouteNotAdmitted"}})
  assert.equal(aggregateRuntime(ingress,[route]),"degraded")
  assert.equal(aggregateRuntime(ingress,[{...route,routingHealth:{status:"unknown"}}]),"healthy")
})

test("flows follow their Services rather than alphabetical workload order", () => {
  const nodes=[node("entry-a","Ingress"),node("entry-b","Ingress"),node("svc-a","Service"),node("svc-b","Service"),node("workload-z"),node("workload-a")]
  const edges=[edge("entry-a","svc-a","routes to"),edge("entry-b","svc-b","routes to"),edge("svc-a","workload-z","selects"),edge("svc-b","workload-a","selects")]
  const layout=topologyMapLayout(nodes,null,edges)
  for (const e of edges) { assert.equal(layout.get(e.from).y,layout.get(e.to).y);assert.ok(layout.get(e.from).x<layout.get(e.to).x) }
  assert.deepEqual(layout,topologyMapLayout([...nodes].reverse(),null,[...edges].reverse()))
})

test("production-shaped topology groups generated routes and separates isolated configuration", () => {
  const ingressA=node("public-a","Ingress"),ingressB=node("public-b","Ingress")
  const services=["frontend","backend","metrics","database"].map(id=>node(id+"-svc","Service"))
  const workloads=["frontend","backend","metrics","database"].map(id=>node(id+"-workload"))
  const generated=[0,1,2,3,4,5].map(i=>node(`generated-${i}`,"Route",{identity:{apiVersion:"route.openshift.io/v1",kind:"Route",namespace:"demo",name:`generated-${i}`}}))
  const config=Array.from({length:17},(_,i)=>node(`config-${i}`,i%2?"ConfigMap":"Role"))
  const edges=services.map((s,i)=>edge(s.id,workloads[i].id,"selects"))
  for(const [offset,ingress] of [ingressA,ingressB].entries()) {
    edges.push(edge(ingress.id,services[0].id,"routes to"),edge(ingress.id,services[1].id,"routes to"))
    for(let i=0;i<3;i++) edges.push(edge(ingress.id,generated[offset*3+i].id),edge(generated[offset*3+i].id,services[i%2].id,"routes to"))
  }
  const network={nodes:[ingressA,ingressB,...services,...workloads,...generated,...config],edges,warnings:[]}
  const shown=topologyCanvasResources(topologyScope(network,null),edges,false)
  assert.equal(network.nodes.length,33)
  assert.equal(shown.length,10)
  const links=topologyVisibleEdges(network,new Set(shown.map(n=>n.id)))
  assert.equal(links.length,8)
  const layout=topologyMapLayout(shown,null,links)
  assert.deepEqual(layout,topologyMapLayout([...shown].reverse(),null,[...links].reverse()))
  for(const link of links) assert.ok(layout.get(link.from).x<layout.get(link.to).x)
  // Independent services/workloads align exactly, rather than spanning other bands.
  for(let i=2;i<4;i++) assert.equal(layout.get(services[i].id).y,layout.get(workloads[i].id).y)
  shown.forEach((a,i)=>shown.slice(i+1).forEach(b=>{
    const pa=layout.get(a.id),pb=layout.get(b.id)
    assert.ok(pa.x+mapNodeWidth<=pb.x||pb.x+mapNodeWidth<=pa.x||pa.y+mapNodeHeightFor(a)<=pb.y||pb.y+mapNodeHeightFor(b)<=pa.y)
  }))
})
