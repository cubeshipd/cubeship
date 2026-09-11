// What `./mock` resolves to when the preview is off.
//
// The alias in next.config.ts points here for every build that is not
// `make web-preview`, which is what keeps the fixtures — invented
// hostnames, invented keys, an invented instance — out of the image
// somebody actually runs.
//
// A stub rather than nothing, because the import in lib/api.ts is
// typed: this satisfies it and is unreachable, since the branch that
// would call it is behind a flag the same build set to off.
export function handle(): never {
  throw new Error("the preview fixtures are not built into this dashboard");
}
