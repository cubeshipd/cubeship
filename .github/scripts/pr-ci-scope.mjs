export function prCIScope(paths) {
  const core = paths.some((path) =>
    path.startsWith("core/") ||
    path.startsWith(".github/") ||
    ["Makefile", "go.work", "go.work.sum", "install.sh", "uninstall.sh"].includes(path),
  );
  return {
    core,
    templates: paths.some(
      (path) =>
        path.startsWith("templates/") ||
        path.startsWith("core/template/") ||
        path.startsWith("core/tools/internal/catalog/") ||
        path.startsWith("core/tools/templatecatalog/"),
    ),
    dashboard: paths.some((path) => path.startsWith("dashboard/")),
    site: paths.some((path) => path.startsWith("site/")),
  };
}

if (process.argv[1]?.endsWith("/pr-ci-scope.mjs")) {
  const chunks = [];
  for await (const chunk of process.stdin) chunks.push(chunk);
  const pages = JSON.parse(Buffer.concat(chunks).toString("utf8"));
  const paths = pages.flat().map((file) => file.filename);
  for (const [name, changed] of Object.entries(prCIScope(paths))) {
    console.log(`${name}=${changed}`);
  }
}
