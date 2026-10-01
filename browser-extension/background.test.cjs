const { test } = require("node:test");
const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const vm = require("node:vm");
const source = readFileSync(require("node:path").join(__dirname,"background.js"),"utf8");
function setup(options={}) {
 const sent=[];const config={enabled:true,token:"secret",app:"Google Chrome",...options.config};
 const listener={addListener(){}};
 const context=vm.createContext({URL,setInterval(){},chrome:{storage:{local:{get:async()=>config,set:async()=>{}}},windows:{getLastFocused:async()=>({id:1,focused:true,incognito:false,...options.window}),onFocusChanged:listener},tabs:{query:async()=>[{active:true,url:"https://user:pass@docs.example/path?secret=key#fragment",title:"Docs",...options.tab}],onActivated:listener,onUpdated:listener},alarms:{onAlarm:listener,create(){}},runtime:{onInstalled:listener,onStartup:listener,onMessage:listener,openOptionsPage(){}},action:{onClicked:listener}},fetch:async(url,init)=>{sent.push({url,init,body:JSON.parse(init.body)});return{ok:true}}});
 vm.runInContext(source,context);return{sent,report:()=>vm.runInContext("report()",context)};
}
test("sends only focused opt-in metadata with redacted URL and token",async()=>{const c=setup();await c.report();assert.equal(c.sent[0].body.url,"https://docs.example/path");assert.equal(c.sent[0].init.headers.Authorization,"Bearer secret");assert.equal(c.sent[0].body.focused,true);});
test("disabled or unpaired collection sends nothing",async()=>{for(const config of [{enabled:false},{token:""}]){const c=setup({config});await c.report();assert.equal(c.sent.length,0);}});
test("private, unfocused, excluded and internal tabs suppress native fallback",async()=>{for(const options of [{window:{incognito:true}},{window:{focused:false}},{config:{excludedDomains:"docs.example"}},{tab:{url:"chrome://settings"}}]){const c=setup(options);await c.report();assert.equal(c.sent[0].body.focused,false);assert.equal(c.sent[0].body.url,"");}});
