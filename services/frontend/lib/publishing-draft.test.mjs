import assert from "node:assert/strict"
import test from "node:test"
import { draftKey, pickDraft, readDraft, DRAFT_TTL } from "./publishing-draft.ts"

const shape={clusterId:"",gitlabUrl:"",step:1,config:{name:"",namespace:"",createNamespace:false,buildCommand:""}}
const now=1800000000000

test("restores allowed settings and excludes credential fields and files",()=>{
 const data={clusterId:"cluster",gitlabUrl:"https://gitlab.example.com",step:5,gitlabToken:"secret",registryPassword:"secret",file:{name:"app.zip"},config:{name:"report",namespace:"apps-report",createNamespace:true,buildCommand:"python build.py",password:"secret"},acknowledged:true}
 const saved=pickDraft(shape,data)
 const restored=readDraft(JSON.stringify({version:1,updatedAt:now,data:saved}),shape,now)
 assert.deepEqual(restored.data,{clusterId:"cluster",gitlabUrl:"https://gitlab.example.com",step:5,config:{name:"report",namespace:"apps-report",createNamespace:true,buildCommand:"python build.py"}})
 assert.equal(JSON.stringify(saved).includes("secret"),false)
 assert.equal("file" in saved,false)
 assert.equal("acknowledged" in saved,false)
})
test("draft keys isolate users, workspaces and templates",()=>{
 const keys=[draftKey("a","manual:w1:shiny"),draftKey("b","manual:w1:shiny"),draftKey("a","manual:w2:shiny"),draftKey("a","manual:w1:static"),draftKey("a","repository:w1:shiny")]
 assert.equal(new Set(keys).size,keys.length)
 assert.notEqual(draftKey("a:b","c"),draftKey("a","b:c"))
})
test("expired and unsupported drafts are ignored; corrupt drafts are reported",()=>{
 assert.equal(readDraft(null,shape,now),null)
 assert.equal(readDraft(JSON.stringify({version:1,updatedAt:now-DRAFT_TTL-1,data:{}}),shape,now),null)
 assert.equal(readDraft(JSON.stringify({version:2,updatedAt:now,data:{}}),shape,now),null)
 assert.throws(()=>readDraft("broken",shape,now))
})
test("malformed fields cannot change the form's value types",()=>{
 assert.deepEqual(pickDraft(shape,{step:"6",config:{name:123,createNamespace:"false",namespace:"safe"}}),{...shape,config:{...shape.config,namespace:"safe"}})
})
