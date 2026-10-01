import { spawn, execFile } from "node:child_process";
import { promisify } from "node:util";
import { mkdtemp, mkdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import net from "node:net";

const port = Number(process.env.DENDRITE_TEST_API_PORT || 18080);
await new Promise((resolve, reject) => {
 const probe = net.createServer();
 probe.once("error", reject);
 probe.listen(port, "127.0.0.1", () => probe.close(resolve));
});
const root = await mkdtemp(join(tmpdir(), "dendrite-e2e-"));
await mkdir(join(root, "notes"));
const binary = join(root, "server");
await promisify(execFile)("go", ["build", "-o", binary, "./cmd/server"], { cwd: "../backend" });
const child = spawn(binary, [], { stdio: "inherit", env: { ...process.env, DENDRITE_ROOT: root, DENDRITE_ADDR: `127.0.0.1:${port}`, DENDRITE_ACTIVITY_AUTOSTART: "false" } });
let stopping = false;
async function stop() {
 if (stopping) return;
 stopping = true;
 try { child.kill("SIGTERM"); } catch {}
 await new Promise(resolve => setTimeout(resolve, 500));
 await rm(root, { recursive: true, force: true });
 process.exit(0);
}
process.on("SIGTERM", stop);
process.on("SIGINT", stop);
child.once("error", async err => { console.error(err); await stop(); });
child.once("exit", async code => { if (!stopping) { await rm(root, { recursive: true, force: true }); process.exit(code || 1); } });
