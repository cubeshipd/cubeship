export const REQUIRED = [
  "scope",
  "templates",
  "check",
  "database",
  "integration",
  "scripts",
  "dashboard",
  "site",
];

const record = (value) => value !== null && typeof value === "object" && !Array.isArray(value);
const own = (value, key) => Object.hasOwn(value, key);

export function evaluate(required, needs, optional = {}) {
  const reasons = [];
  if (
    !Array.isArray(required) ||
    required.length === 0 ||
    required.some((id) => typeof id !== "string" || !/^[a-zA-Z_][a-zA-Z0-9_-]*$/.test(id)) ||
    new Set(required).size !== required.length ||
    !record(needs) ||
    !record(optional)
  ) {
    return { ok: false, reasons: ["Invalid aggregate input"] };
  }
  for (const [id, selector] of Object.entries(optional)) {
    if (required.includes(id) || !record(selector) || typeof selector.applicable !== "boolean") {
      reasons.push(`Invalid optional selector: ${id}`);
    }
  }
  const allowed = new Set([...required, ...Object.keys(optional)]);
  for (const id of Object.keys(needs)) {
    if (!allowed.has(id)) reasons.push(`Unexpected job: ${id}`);
  }
  for (const id of allowed) {
    const job = own(needs, id) ? needs[id] : undefined;
    if (!record(job) || !own(job, "result") || typeof job.result !== "string") {
      reasons.push(`Missing or malformed job: ${id}`);
      continue;
    }
    if (job.result === "success") continue;
    if (!required.includes(id) && job.result === "skipped" && optional[id]?.applicable === false)
      continue;
    reasons.push(`Job ${id}: ${job.result}`);
  }
  return { ok: reasons.length === 0, reasons };
}

export function evaluateRun(needs) {
  const scope = needs?.scope?.outputs;
  if (
    !record(needs) ||
    needs.scope?.result !== "success" ||
    !record(scope) ||
    ["core", "templates", "dashboard", "site"].some((id) => !["true", "false"].includes(scope[id]))
  ) {
    return { ok: false, reasons: ["Missing CI scope"] };
  }
  const jobScope = {
    templates: "templates",
    check: "core",
    database: "core",
    integration: "core",
    scripts: "core",
    dashboard: "dashboard",
    site: "site",
  };
  const required = ["scope"];
  const optional = {};
  for (const [job, component] of Object.entries(jobScope)) {
    if (scope[component] === "true") required.push(job);
    else optional[job] = { applicable: false };
  }
  return evaluate(required, needs, optional);
}

// The workflow executes this literal body without checking out pull request code.
if (process.argv[1] === undefined || process.argv[1].endsWith("/ci-aggregate.mjs")) {
  try {
    const result = evaluateRun(JSON.parse(process.env.NEEDS_JSON));
    console.log(JSON.stringify(result));
    process.exitCode = result.ok ? 0 : 1;
  } catch {
    console.error("Invalid NEEDS_JSON");
    process.exitCode = 1;
  }
}
