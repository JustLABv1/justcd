import assert from "node:assert/strict"
import test from "node:test"
import { getViewportForBounds } from "@xyflow/react"
import { productionTopologyFixture } from "./topology-map.fixture.mjs"
import { packedTopologyLayout, packedTopologyScene, topologyScope, mapNodeHeightFor, mapNodeWidth } from "./topology-map.ts"

const graph=productionTopologyFixture()
const nodes=topologyScope(graph,null)
function assertNoOverlap(layout) {
  for(const [index,a] of nodes.entries()) for(const b of nodes.slice(index+1)) {
    const pa=layout.positions.get(a.id),pb=layout.positions.get(b.id)
    assert.ok(pa.x+mapNodeWidth<=pb.x||pb.x+mapNodeWidth<=pa.x||pa.y+mapNodeHeightFor(a)<=pb.y||pb.y+mapNodeHeightFor(b)<=pa.y,`${a.id} overlaps ${b.id}`)
  }
  for(const group of layout.groups) for(const id of group.resourceIds) {
    const node=nodes.find(n=>n.id===id),p=layout.positions.get(id)
    assert.ok(p.x>=group.x&&p.y>=group.y+32&&p.x+mapNodeWidth<=group.x+group.width&&p.y+mapNodeHeightFor(node)<=group.y+group.height,`${id} leaves its group`)
  }
}
test("27 application resources stay on the map; all 52 runtime children remain collapsed",()=>{
 assert.equal(graph.nodes.length,79)
 assert.equal(nodes.length,27)
 const layout=packedTopologyLayout(nodes,graph.edges,{width:1900,height:760})
 assert.equal(layout.positions.size,27)
 assert.equal(nodes.filter(n=>['ConfigMap','Role','RoleBinding','ServiceAccount','NetworkPolicy','PersistentVolumeClaim'].includes(n.identity.kind)).length,12)
 assertNoOverlap(layout)
 const bounds={x:0,y:0,width:Math.max(...layout.groups.map(g=>g.x+g.width)),height:Math.max(...layout.groups.map(g=>g.y+g.height))}
 const camera=getViewportForBounds(bounds,1900,760,.15,1.1,.06)
 assert.ok(camera.zoom>=.75,`Initial zoom ${camera.zoom} makes labels unreadable`)
 for(const node of nodes){const p=layout.positions.get(node.id);assert.ok(p.x*camera.zoom+camera.x>=0&&p.y*camera.zoom+camera.y>=0&&(p.x+mapNodeWidth)*camera.zoom+camera.x<=1900&&(p.y+mapNodeHeightFor(node))*camera.zoom+camera.y<=760)}
})
test("packing uses available width instead of stacking independent groups into a column",()=>{
 const wide=packedTopologyLayout(nodes,graph.edges,{width:1900,height:760})
 const narrow=packedTopologyLayout(nodes,graph.edges,{width:1100,height:650})
 assertNoOverlap(wide);assertNoOverlap(narrow)
 assert.ok(new Set(wide.groups.map(group=>group.x)).size>=3)
 assert.ok(Math.max(...wide.groups.map(group=>group.y+group.height))<1100)
 assert.deepEqual(wide,packedTopologyLayout([...nodes].reverse(),[...graph.edges].reverse(),{width:1900,height:760}))
})
test("shared configuration appears once without merging independent traffic groups",()=>{
 const shared={id:'shared',source:'kubernetes',identity:{apiVersion:'v1',kind:'ConfigMap',namespace:'production',name:'shared'}}
 const links=[...graph.edges,{from:'metrics',to:'shared',relation:'uses'},{from:'postgresql',to:'shared',relation:'uses'}]
 const layout=packedTopologyLayout([...nodes,shared],links,{width:1900,height:760})
 assert.equal(layout.positions.size,28)
 assert.deepEqual(layout.groups.filter(group=>group.resourceIds.includes('shared')).map(group=>group.title),['Shared configuration'])
 assert.ok(layout.groups.find(group=>group.resourceIds.includes('metrics')).id!==layout.groups.find(group=>group.resourceIds.includes('postgresql')).id)
})

test("Deployment drill-down keeps two pods and four dependencies compact at readable zoom",()=>{
 const n=(id,kind)=>({id,source:'kubernetes',identity:{apiVersion:'v1',kind,namespace:'app',name:id}})
 const resources=[n('web','Deployment'),n('pod-a','Pod'),n('pod-b','Pod'),n('public','Ingress'),n('service','Service'),n('data','PersistentVolumeClaim'),n('account','ServiceAccount'),n('secret','Secret'),n('autoscaler','HorizontalPodAutoscaler')]
 const edges=[{from:'web',to:'pod-a',relation:'owns'},{from:'web',to:'pod-b',relation:'owns'},{from:'public',to:'service',relation:'routes to'},{from:'service',to:'web',relation:'selects'},...['data','account','secret'].map(to=>({from:'web',to,relation:'uses'})),{from:'autoscaler',to:'web',relation:'scales'}]
 const layout=packedTopologyLayout(resources,edges,{width:1388,height:760})
 const bounds={x:0,y:0,width:Math.max(...layout.groups.map(g=>g.x+g.width)),height:Math.max(...layout.groups.map(g=>g.y+g.height))}
 const camera=getViewportForBounds(bounds,1388,760,.15,1.1,.06)
 assert.ok(camera.zoom>=1,`Drill-down unnecessarily zooms out to ${camera.zoom}`)
 assert.ok(bounds.height<700,`Configuration creates a tall sparse scene: ${bounds.height}`)
 for(const dependency of ['data','account','secret','autoscaler']) assert.ok(layout.positions.has(dependency))
})


test("hidden ReplicaSets retain the ownership path when laying out a Deployment drill-down",()=>{
 const n=(id,kind)=>({id,source:'kubernetes',identity:{apiVersion:'v1',kind,namespace:'app',name:id}})
 const resources=[n('service','Service'),n('web','Deployment'),n('replicaset','ReplicaSet'),n('pod-a','Pod'),n('pod-b','Pod')]
 const graph={nodes:resources,edges:[{from:'service',to:'web',relation:'selects'},{from:'service',to:'pod-a',relation:'selects'},{from:'service',to:'pod-b',relation:'selects'},{from:'web',to:'replicaset',relation:'owns'},{from:'replicaset',to:'pod-a',relation:'owns'},{from:'replicaset',to:'pod-b',relation:'owns'}],warnings:[]}
 const layout=packedTopologyScene(graph,['service','web','pod-a','pod-b'],{width:1388,height:760})
 assert.ok(layout.positions.get('pod-a').x>layout.positions.get('web').x)
 assert.ok(layout.positions.get('pod-b').x>layout.positions.get('web').x)
 assert.ok(!layout.positions.has('replicaset'))
})

test('dependencies align with their consumers instead of a common bottom row',()=>{
 const n=(id,kind)=>({id,source:'kubernetes',identity:{apiVersion:'v1',kind,namespace:'app',name:id}})
 const resources=[n('entry','Ingress'),n('svc-a','Service'),n('svc-b','Service'),n('a','Deployment'),n('b','Deployment'),n('config-a','ConfigMap'),n('config-b','ConfigMap'),n('shared','Secret')]
 const edges=[{from:'entry',to:'svc-a',relation:'routes to'},{from:'entry',to:'svc-b',relation:'routes to'},{from:'svc-a',to:'a',relation:'selects'},{from:'svc-b',to:'b',relation:'selects'},{from:'a',to:'config-a',relation:'uses'},{from:'b',to:'config-b',relation:'uses'},{from:'a',to:'shared',relation:'uses'},{from:'b',to:'shared',relation:'uses'}]
 const layout=packedTopologyLayout(resources,edges,{width:1600,height:760}),p=layout.positions
 assert.ok(Math.abs(p.get('config-a').y-p.get('a').y)<100)
 assert.ok(Math.abs(p.get('config-b').y-p.get('b').y)<100)
 assert.ok(p.get('config-a').x>p.get('a').x+mapNodeWidth)
 assert.ok(p.get('shared').y>p.get('config-a').y&&p.get('shared').y<p.get('config-b').y)
 assert.equal(layout.groups.flatMap(g=>g.resourceIds).filter(id=>id==='shared').length,1)
})
