import assert from "node:assert/strict"
import test from "node:test"
import { routeTopologyConnection } from "./topology-routing.ts"
import { productionTopologyFixture } from "./topology-map.fixture.mjs"
import { packedTopologyScene, topologyScope, mapNodeHeightFor, mapNodeWidth, supportingTopologyResource } from "./topology-map.ts"
const box=(id,x,y,compact=false,width=200,height=compact?80:184)=>({id,x,y,compact,width,height})
function assertClear(route,boxes,from,to) {
 assert.ok(route,'No route found for non-overlapping resources')
 // Check the rendered SVG curves, rather than treating cubic control points
 // as straight segments. Include full names/status rectangles as obstacles.
 const tokens=route.path.match(/[MLQC]|-?\d+(?:\.\d+)?(?:e[+-]?\d+)?/gi)
 let current={x:0,y:0},index=0
 const point=()=>({x:Number(tokens[index++]),y:Number(tokens[index++])})
 while(index<tokens.length){
  const command=tokens[index++],a=current
  if(command==='M'){current=point();continue}
  const c=point(),d=command==='Q'||command==='C'?point():c,b=command==='C'?point():d
  const steps=Math.ceil((Math.hypot(c.x-a.x,c.y-a.y)+Math.hypot(d.x-c.x,d.y-c.y)+Math.hypot(b.x-d.x,b.y-d.y))*2)+1
  for(let i=0;i<=steps;i++){
   const t=i/steps,u=1-t
   const p=command==='C'?{x:u*u*u*a.x+3*u*u*t*c.x+3*u*t*t*d.x+t*t*t*b.x,y:u*u*u*a.y+3*u*u*t*c.y+3*u*t*t*d.y+t*t*t*b.y}:command==='Q'?{x:u*u*a.x+2*u*t*c.x+t*t*b.x,y:u*u*a.y+2*u*t*c.y+t*t*b.y}:{x:a.x+(b.x-a.x)*t,y:a.y+(b.y-a.y)*t}
   for(const obstacle of boxes){
    if(obstacle.id===from||obstacle.id===to)continue
    assert.ok(!(p.x>obstacle.x+0.1&&p.x<obstacle.x+obstacle.width-.1&&p.y>obstacle.y+.1&&p.y<obstacle.y+obstacle.height-.1),`Curve crosses ${obstacle.id}`)
   }
   for(const id of [from,to]){const endpoint=boxes.find(b=>b.id===id);if(endpoint.compact)assert.ok(!(p.x>endpoint.x+48&&p.x<endpoint.x+endpoint.width&&p.y>endpoint.y+8&&p.y<endpoint.y+endpoint.height),`Curve crosses inline label ${id}`)}
  }
  current=b
 }

}
test('dependency avoids an intervening workload and its entire label area',()=>{
 const boxes=[box('source',0,0),box('middle',232,100),box('target',464,320,true)]
 const route=routeTopologyConnection(boxes,'source','target')
 assertClear(route,boxes,'source','target')
 assert.ok(route.path.includes('Q'),'Route keeps gently rounded corners')
})
test('compact resources do not connect through their inline names, kinds or status text',()=>{
 const boxes=[box('source',0,0),box('config',232,40,true)]
 const route=routeTopologyConnection(boxes,'source','config')
 assertClear(route,boxes,'source','config')
 assert.notEqual(route.targetHandle,'target-right')
 const last=route.points.at(-1)
 assert.ok(last.x<=boxes[1].x+26)
})
test('second dependency row is reachable without crossing the first row',()=>{
 const boxes=[box('deployment',464,0),box('config-a',0,250,true),box('config-b',212,250,true),box('config-c',424,250,true),box('claim',0,342,true),box('secret',212,342,true),box('account',424,342,true)]
 for(const to of ['claim','secret','account'])assertClear(routeTopologyConnection(boxes,'deployment',to),boxes,'deployment',to)
})
test('moving a resource onto a previous route recalculates a clear route',()=>{
 const boxes=[box('source',0,0),box('target',600,0,true),box('movable',232,300)]
 const before=routeTopologyConnection(boxes,'source','target')
 const moved=boxes.map(b=>b.id==='movable'?{...b,y:0}:b)
 const after=routeTopologyConnection(moved,'source','target')
 assertClear(after,moved,'source','target')
 assert.notEqual(after.path,before.path)
})
test('overlapping resources never force a fallback line through another resource',()=>{
 const boxes=[box('source',0,0),box('target',500,0,true),box('cover',-100,-100,false,500,500)]
 assert.equal(routeTopologyConnection(boxes,'source','target'),null)
})
test('production-sized dependency routes clear every resource at the packed positions',()=>{
 const graph=productionTopologyFixture(),nodes=topologyScope(graph,null)
 const layout=packedTopologyScene(graph,nodes.map(n=>n.id),{width:1900,height:760})
 const boxes=nodes.map(n=>({id:n.id,...layout.positions.get(n.id),width:mapNodeWidth,height:mapNodeHeightFor(n),compact:supportingTopologyResource(n)}))
 for(const edge of graph.edges.filter(e=>e.relation==='uses'))assertClear(routeTopologyConnection(boxes,edge.from,edge.to),boxes,edge.from,edge.to)
})

test('unobstructed dependencies use floating cubic curves',()=>{const route=routeTopologyConnection([box('a',0,0),box('b',330,50,true)],'a','b');assert.ok(route.path.includes(' C '))})
