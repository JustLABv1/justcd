// Static regression data for the 27-resource/52-runtime-child topology. This is
// imported only by tests, never seeded into JustCD or used by production UI.
export function productionTopologyFixture() {
  const nodes=[],edges=[]
  const node=(id,kind,extra={})=>{const value={id,identity:{apiVersion:kind==='Ingress'?'networking.k8s.io/v1':kind==='Route'?'route.openshift.io/v1':'v1',kind,namespace:'production',name:id},source:'kubernetes',readiness:'Ready',...extra};nodes.push(value);return value}
  const edge=(from,to,relation='owns')=>edges.push({from,to,relation})
  const health={status:'healthy',label:'Backend ready',message:'Backend pods are ready',reason:'ReadyBackends',readyPods:8,totalPods:8}
  for(const name of ['frontend','backend','metrics','postgresql','notebooks']) {
    node(name+'-service','Service',{tracked:true,routingHealth:health})
    node(name,name==='postgresql'?'StatefulSet':'Deployment',{tracked:true})
    edge(name+'-service',name,'selects')
    node(name+'-replicaset','ReplicaSet');edge(name,name+'-replicaset')
    const count=name==='frontend'?9:8
    for(let i=0;i<count;i++) {
      node(`${name}-pod-${i}`,'Pod',{phase:'Running',readiness:name==='notebooks'&&i===0?'Not ready':'Ready',healthSummary:name==='notebooks'&&i===0?{failureReason:'CrashLoopBackOff'}:{}})
      edge(name+'-replicaset',`${name}-pod-${i}`)
    }
  }
  for(const name of ['public-entry','internal-entry']) {
    node(name,'Ingress',{tracked:true,routingHealth:health,urls:[`https://${name}.example.invalid/`]})
    edge(name,'frontend-service','routes to');edge(name,'backend-service','routes to')
    for(let i=0;i<3;i++) {node(`${name}-generated-${i}`,'Route',{routingHealth:health});edge(name,`${name}-generated-${i}`);edge(`${name}-generated-${i}`,i%2?'backend-service':'frontend-service','routes to')}
  }
  for(const name of ['node-agent','log-agent','gpu-agent']) node(name,'DaemonSet',{tracked:true})
  for(const [name,kind,owner] of [
    ['frontend-config','ConfigMap','frontend'],['backend-config','ConfigMap','backend'],['postgresql-config','ConfigMap','postgresql'],
    ['postgresql-data','PersistentVolumeClaim','postgresql'],['notebook-cache','PersistentVolumeClaim','notebooks'],
    ['metrics-account','ServiceAccount','metrics'],['metrics-pod-logs','Role','metrics'],['metrics-pod-logs-binding','RoleBinding','metrics'],
    ['allow-ingress','NetworkPolicy',null],['allow-egress','NetworkPolicy',null],['allow-dns','NetworkPolicy',null],['allow-metrics','NetworkPolicy',null]
  ]) {node(name,kind,{tracked:true});if(owner)edge(owner,name,'uses')}
  return {nodes,edges,warnings:[]}
}
