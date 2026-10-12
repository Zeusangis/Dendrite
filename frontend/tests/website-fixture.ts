import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { readFile } from "node:fs/promises";

export async function seedWebsites() {
 const root = await readFile("test-results/backend-root.txt", "utf8");
 await promisify(execFile)("go", ["run", "./internal/activity/testdata/seed", root], { cwd: "../backend" });
}
