const { test } = require("node:test");
const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const vm = require("node:vm");
const source = readFileSync(require("node:path").join(__dirname,"background.js"),"utf8");
function setup(options={}) {
 const sent=[],events={};let now=100000;
 const config={enabled:true,token:"secret",app:"Google Chrome",...options.config};
 const listener=name=>({addListener(fn){events[name]=fn;}});
 const context=vm.createContext({URL,AbortSignal,Date:{now:()=>now},setInterval(){},chrome:{storage:{onChanged:listener("storage"),local:{get:async()=>config,set:async values=>Object.assign(config,values)}},windows:{getLastFocused:async()=>({id:1,focused:true,incognito:false,...options.window}),onFocusChanged:listener("focus")},tabs:{query:async()=>[{active:true,url:"https://user:pass@docs.example/path?secret=key#fragment",title:"Docs",...options.tab}],onActivated:listener("activate"),onUpdated:listener("update")},alarms:{onAlarm:listener("alarm"),create(){}},runtime:{onInstalled:listener("install"),onStartup:listener("startup"),onMessage:listener("message"),openOptionsPage(){}},action:{onClicked:listener("action")}},fetch:async(url,init)=>{sent.push({url,init,body:JSON.parse(init.body)});if(options.failure)throw new Error("connection failed with secret");return{ok:options.status?false:true,status:options.status,json:async()=>({recording:options.recording??true})}}});
 vm.runInContext(source,context);return{sent,config,events,tick:ms=>now+=ms,report:()=>vm.runInContext("report()",context)};
}
test("sends only focused opt-in metadata with redacted URL and token",async()=>{const c=setup();await c.report();assert.equal(c.sent[0].body.url,"https://docs.example/path");assert.equal(c.sent[0].init.headers.Authorization,"Bearer secret");assert.equal(c.sent[0].body.focused,true);});
test("disabled or unpaired collection sends nothing",async()=>{for(const config of [{enabled:false},{token:""}]){const c=setup({config});await c.report();assert.equal(c.sent.length,0);}});
test("private, unfocused, excluded and internal tabs suppress native fallback",async()=>{for(const options of [{window:{incognito:true}},{tab:{incognito:true}},{window:{focused:false}},{config:{excludedDomains:".docs.example."}},{tab:{url:"chrome://settings"}}]){const c=setup(options);await c.report();assert.equal(c.sent[0].body.focused,false);assert.equal(c.sent[0].body.url,"");assert.equal(c.sent[0].body.private,true);}});
test("duplicate events coalesce but heartbeat refreshes",async()=>{const c=setup();await Promise.all([c.report(),c.report(),c.report()]);assert.equal(c.sent.length,1);await c.report();assert.equal(c.sent.length,1);c.tick(5000);await c.report();assert.equal(c.sent.length,2);});
test("backoff persists and errors never disclose token",async()=>{for(const options of [{failure:true},{status:401}]){const c=setup(options);await c.report();assert.equal(c.config.failures,1);assert.equal(c.config.retryAt,105000);assert.ok(!c.config.lastStatus.includes("secret"));await c.report();assert.equal(c.sent.length,1);c.tick(5000);await c.report();assert.equal(c.config.retryAt,115000);}});
test("backend pause status is visible and stopped extension sends nothing",async()=>{const c=setup({recording:false});await c.report();assert.match(c.config.lastStatus,/recording is stopped/);c.config.enabled=false;c.tick(5000);await c.report();assert.equal(c.sent.length,1);});
test("active-tab lifecycle events report, inactive updates do not",async()=>{const c=setup();c.events.update(1,{title:"Other"},{active:false});assert.equal(c.sent.length,0);await c.events.activate();assert.equal(c.sent.length,1);c.tick(5000);await c.events.focus();assert.equal(c.sent.length,2);});
