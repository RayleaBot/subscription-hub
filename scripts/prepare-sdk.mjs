import fs from "node:fs";
import path from "node:path";
import { execFileSync, spawnSync } from "node:child_process";

const root = path.resolve(import.meta.dirname, "..");
const target = path.join(root, ".rayleabot", "sdk", "vue");
// The main repository's development workspace may already supply this mirror.
if (!fs.existsSync(path.join(target, "package.json"))) {
  const ref = fs.readFileSync(path.join(root, ".rayleabot-sdk-ref"), "utf8").trim();
  if (!/^[a-f0-9]{40}$/.test(ref)) throw new Error("Invalid pinned SDK commit");
  const core = path.join(root, ".rayleabot", "core");
  const git = (...args) => execFileSync("git", ["-C", core, ...args], { stdio: "inherit" });
  if (!fs.existsSync(core)) {
    fs.mkdirSync(core, { recursive: true });
    git("init");
    git("remote", "add", "origin", "https://github.com/RayleaBot/RayleaBot.git");
    git("sparse-checkout", "set", "sdk");
  }
  const head = spawnSync("git", ["-C", core, "rev-parse", "--verify", "HEAD"], { stdio: "ignore" });
  if (head.status !== 0) {
    git("fetch", "--depth=1", "--filter=blob:none", "origin", ref);
    git("checkout", "--detach", "FETCH_HEAD");
  }
  const actual = execFileSync("git", ["-C", core, "rev-parse", "HEAD"], { encoding: "utf8" }).trim();
  if (actual !== ref) throw new Error("The .rayleabot/core checkout differs from .rayleabot-sdk-ref; use a matching SDK checkout");
  fs.mkdirSync(path.dirname(target), { recursive: true });
  const sourceSDK = path.join(core, "sdk", "vue");
  fs.cpSync(sourceSDK, target, {
    recursive: true,
    filter: source => source !== path.join(sourceSDK, "package.json") && !["node_modules", "dist"].includes(path.basename(source)),
  });
  fs.copyFileSync(path.join(sourceSDK, "package.json"), path.join(target, "package.json"));
}
