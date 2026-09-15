import { createMcpHandler, McpServer } from "@modelcontextprotocol/server";
import { registerSearchTool, registerSourceTools } from "fumadocs-core/mcp";
import { createFromSource } from "fumadocs-core/search/server";
import { docsLlms, source } from "@/lib/source";

// Built once per process: the docs are compiled in, so the index never
// goes stale. The instance's own `/mcp` is on another host.
const search = createFromSource(source);

const handler = createMcpHandler(() => {
  const mcp = new McpServer({ name: "cubeship-docs", version: "1.0.0" });
  registerSourceTools(mcp, source, docsLlms);
  registerSearchTool(mcp, search);
  return mcp;
});

export function GET(request: Request) {
  return handler.fetch(request);
}

export function POST(request: Request) {
  return handler.fetch(request);
}

export function DELETE(request: Request) {
  return handler.fetch(request);
}
