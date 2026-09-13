import { isMarkdownPreferred, rewritePath } from "fumadocs-core/negotiation";
import { type NextRequest, NextResponse } from "next/server";
import { catalogRewrite } from "@/lib/catalog-proxy";
import { catalogUrl, umamiUrl } from "@/lib/env";
import { docsContentRoute, docsRoute } from "@/lib/shared";
import { umamiRewrite } from "@/lib/umami-proxy";

const { rewrite: rewriteDocs } = rewritePath(
  `${docsRoute}{/*path}`,
  `${docsContentRoute}{/*path}/content.md`,
);
const { rewrite: rewriteSuffix } = rewritePath(
  `${docsRoute}{/*path}.md`,
  `${docsContentRoute}{/*path}/content.md`,
);

export default function proxy(request: NextRequest) {
  const catalog = catalogRewrite(request.nextUrl.pathname, request.nextUrl.search, catalogUrl());
  if (catalog) return NextResponse.rewrite(new URL(catalog));

  const umami = umamiRewrite(request.nextUrl.pathname, request.nextUrl.search, umamiUrl());
  if (umami) return NextResponse.rewrite(new URL(umami));

  const result = rewriteSuffix(request.nextUrl.pathname);
  if (result) {
    return NextResponse.rewrite(new URL(result, request.nextUrl));
  }

  if (isMarkdownPreferred(request)) {
    const result = rewriteDocs(request.nextUrl.pathname);

    if (result) {
      return NextResponse.rewrite(new URL(result, request.nextUrl), {
        // this URL has two representations, selected by `Accept`
        headers: { Vary: "Accept" },
      });
    }
  }

  return NextResponse.next();
}
