import { test, expect } from "@playwright/test";
import { seedWebsites } from "./website-fixture";

test("website explorer, mute/restore, explanations, graph filters and privacy cleanup",async({page})=>{
 await seedWebsites();
 expect((await page.request.post("/api/sync")).ok()).toBe(true);
 const errors:string[]=[];page.on("pageerror",e=>errors.push(e.message));
 await page.goto("/");
 const note=await (await page.request.post("/api/notes",{data:{title:"Go resources",content:"# Go resources\n\n#golang"}})).json();
 const dashboard=page.getByRole("region",{name:"Activity dashboard"});
 // The topbar button toggles, so drive the panel by observed state instead.
 const openActivity=async()=>{ if(!await dashboard.isVisible()) await page.getByRole("button",{name:/^Activity [●○]$/}).click(); await expect(dashboard).toBeVisible(); };
 const closeActivity=async()=>{ if(await dashboard.isVisible()) await page.getByRole("button",{name:"Close activity"}).click(); };
 const edges=async()=>((await (await page.request.get("/api/graph")).json()).edges as {source:string}[]).filter(e=>e.source==="activity").length;
 const detail=async(id:number)=>await (await page.request.get(`/api/activity/nodes/${id}`)).json();

 await openActivity();
 await page.getByText("View chart data table",{exact:true}).click();
 await expect(page.locator(".chart-table table")).toContainText("Google Chrome");
 const daily=await (await page.request.get("/api/activity/series?days=7&granularity=day")).json();
 expect(daily.buckets.reduce((sum:number,b:{total_seconds:number})=>sum+b.total_seconds,0)).toBeGreaterThan(0);

 // Websites: domains, pages, sanitized URLs and note matching.
 await page.getByRole("button",{name:"Websites",exact:true}).click();
 await expect(page.getByRole("region",{name:"Website activity"})).toContainText("en.wikipedia.org");
 await page.getByLabel("Website type").selectOption("pages");
 await page.getByLabel("Search website activity").fill("wikipedia");
 await expect(page.locator(".website-list")).toContainText("Go programming language");
 await expect(page.locator(".website-list")).not.toContainText("?secret");
 const pageId=await page.evaluate(async()=>{const r=await fetch("/api/activity/pages?search=wikipedia");return (await r.json()).pages[0].node_id;});
 const baseline=await edges();
 expect((await detail(pageId)).muted_connections).toBe(0);
 await page.getByRole("button",{name:"Explain connections",exact:true}).click();
 await expect(page.getByRole("region",{name:"Website connection explanations"})).toContainText("Shared note concept: golang");

 // Muting one connection hides it everywhere without touching usage or notes.
 await page.getByRole("button",{name:"Mute this connection",exact:true}).first().click();
 await expect.poll(async()=>(await detail(pageId)).muted_connections).toBe(1);
 await expect.poll(edges).toBe(baseline-1);
 await expect.poll(async()=>(await detail(pageId)).sessions).toBeGreaterThan(0);
 // The panel stays open on the node the user just acted in.
 await expect(page.getByRole("region",{name:"Website connection explanations"})).toContainText("muted connection");
 await page.getByRole("button",{name:"Connections",exact:true}).click();
 await expect(page.getByRole("region",{name:"Muted connections"})).toContainText("Go programming language");
 await page.getByRole("button",{name:"Restore this connection",exact:true}).click();
 await expect.poll(async()=>(await detail(pageId)).muted_connections).toBe(0);
 await expect.poll(edges).toBe(baseline);
 await expect(page.getByRole("region",{name:"Muted connections"})).toHaveCount(0);

 // The node inspector explains the same activity and offers the same mute control.
 await page.getByRole("button",{name:"Websites",exact:true}).click();
 await expect(page.locator(".website-list")).toContainText("Go programming language");
 await page.getByRole("button",{name:"Explain connections",exact:true}).click();
 await page.getByRole("button",{name:"Inspect activity node",exact:true}).click();
 await expect(page.locator(".activity-inspector")).toContainText("en.wikipedia.org");
 await expect(page.locator(".activity-inspector")).toContainText("Sanitized URL");
 await expect(page.locator(".activity-inspector")).toContainText("Go resources");
 await page.locator(".activity-inspector").getByRole("button",{name:"Mute this connection",exact:true}).first().click();
 await expect.poll(async()=>(await detail(pageId)).muted_connections).toBe(1);
 await expect(page.locator(".activity-inspector")).toContainText("muted connection");
 await openActivity();
 await page.getByRole("button",{name:"Connections",exact:true}).click();
 await page.getByRole("button",{name:"Restore this connection",exact:true}).click();
 await expect.poll(async()=>(await detail(pageId)).muted_connections).toBe(0);

 // Graph filters separate activity edges and browser nodes from notes.
 await closeActivity();
 await page.getByText("Graph filters & legend",{exact:true}).click();
 await page.getByLabel("Show activity edges").uncheck();
 await page.getByText(/^Browse graph/).click();
 // Inferred note edges stay visible, so compare against the same visibility rule
 // the graph uses rather than assuming a starting threshold.
 const graph=await (await page.request.get("/api/graph")).json();
 const settings=await (await page.request.get("/api/settings")).json();
 const expected=graph.edges.filter((e:{source:string,strength:number})=>e.source!=="activity"&&(e.source==="explicit"||e.strength>=settings.min_auto_edge_strength)).length;
 await expect(page.locator(".browse").getByRole("button",{name:/Inspect relationship:/})).toHaveCount(expected);
 await expect(page.locator(".browse").getByRole("button",{name:/Inspect relationship:.*\(activity ·/})).toHaveCount(0);
 await page.getByLabel("Show browser nodes").uncheck();
 await expect(page.locator(".browse").getByRole("button",{name:/\(domain\)|\(page\)/})).toHaveCount(0);

 // Privacy exclusions delete retained website data but never notes.
 await openActivity();
 await page.getByText("Privacy, retention & browser setup",{exact:true}).click();
 await page.getByLabel("Excluded domains",{exact:true}).fill("private.example");
 await page.getByRole("button",{name:"Save tracking settings",exact:true}).click();
 await expect.poll(async()=> (await (await page.request.get("/api/activity/domains?search=private.example")).json()).total).toBe(0);
 await page.getByRole("button",{name:"Connections",exact:true}).click();
 await expect(page.getByRole("region",{name:"Activity connections"})).toContainText("go.dev");
 page.once("dialog",d=>d.accept());
 await page.getByRole("button",{name:"Delete activity history",exact:true}).click();
 await expect.poll(async()=> (await (await page.request.get("/api/activity/pages")).json()).total).toBe(0);
 await expect(page.getByRole("region",{name:"Activity connections"})).toContainText("Recording is stopped");
 expect((await page.request.get(`/api/notes/${note.id}`)).ok()).toBe(true);
 await page.setViewportSize({width:390,height:844});
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth)).toBe(true);
 expect(errors).toEqual([]);
});
