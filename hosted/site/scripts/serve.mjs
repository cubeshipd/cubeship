import { spawn } from "node:child_process";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const dev = process.argv.includes("--dev");
const site = dev ? resolve(here, "..") : resolve(here, "site");
const demo = dev ? resolve(here, "../../../product/dashboard") : resolve(here, "demo");
const children = [];
let stopping = false;

function stop(code = 0) {
  if (stopping) return;
  stopping = true;
  for (const child of children) child.kill("SIGTERM");
  const timeout = setTimeout(() => {
    for (const child of children) child.kill("SIGKILL");
    process.exit(code);
  }, 5000);
  timeout.unref();
  Promise.all(
    children.map((child) =>
      child.exitCode !== null || child.signalCode !== null
        ? Promise.resolve()
        : new Promise((done) => child.once("exit", done)),
    ),
  ).then(() => process.exit(code));
}

function start(cwd, port, isDemo) {
  const args = dev
    ? [resolve(cwd, "node_modules/next/dist/bin/next"), "dev", "-p", String(port)]
    : ["server.js"];
  const child = spawn(process.execPath, args, {
    cwd,
    stdio: "inherit",
    env: {
      ...process.env,
      PORT: String(port),
      HOSTNAME: isDemo ? "127.0.0.1" : "0.0.0.0",
      ...(isDemo ? { NEXT_PUBLIC_CUBESHIP_MOCK: "1", NEXT_PUBLIC_CUBESHIP_DEMO: "1" } : {}),
    },
  });
  children.push(child);
  child.on("error", (error) => {
    console.error(error);
    stop(1);
  });
  child.on("exit", (code) => {
    if (!stopping) stop(code || 1);
  });
}

process.on("SIGINT", () => stop());
process.on("SIGTERM", () => stop());
start(demo, 3003, true);
start(site, dev ? 3002 : Number(process.env.PORT || 3000), false);
