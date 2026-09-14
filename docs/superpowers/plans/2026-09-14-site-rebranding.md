# Site Rebranding Implementation Plan

> **For agentic workers:** Use superpowers:subagent-driven-development for isolated components and review; execute the shared visual composition in this session. Track completion below.

**Goal:** Deliver the approved cinematic cyberpunk product site, including generated artwork, an interactive Three.js cube and coherent documentation, catalog and comparison pages.

**Architecture:** Server-rendered product content and existing Fumadocs routes remain intact. Marketing styles are scoped, the Three.js scene is a lazy client enhancement with a static fallback, and catalog failure behavior remains unchanged.

**Tech Stack:** Next.js 16, React 19, Tailwind 4, Fumadocs, Three.js, built-in ImageGen.

**Spec:** ../specs/2026-09-14-site-rebranding-design.md (approved in conversation, including generated art).

## Global Constraints

- Work on master in the repository root; no worktrees, push or publication.
- Preserve existing unrelated changes, catalog deadlines, routes and analytics.
- Keep the palette #05070a, #0a0e14, #182430, #d7e3ec, #2de2e6, #ff2e88.
- Keep text and actions usable before JavaScript and without WebGL.
- Honor reduced motion and suspend GPU work outside the viewport.
- Use generated images through the integrated tool; exact model selection is unavailable and has been disclosed.
- Test meaningful behavior and existing regressions; visual styling is verified in the browser instead of implementation-mirroring tests.

## Task 1: Product composition and artwork

Files: landing components, `src/components/site-header.tsx`, `src/app/(home)/layout.tsx`, `src/app/global.css`, new `src/app/marketing.css`, `src/images/brand/`, home social images.

- [x] Generate a cinematic cube image and infrastructure landscape; persist final originals and prompts in the repository and serve optimized derivatives through Next Image.
- [x] Replace the home hierarchy with hero, product preview, deploy workflow, grouped features, cluster, MCP, catalog and install CTA.
- [x] Implement shared marketing header and footer with mobile navigation and real links.
- [x] Set deliberate responsive type and layout scales; use a single scene as the main animated moment.
- [x] Update social imagery with brand composition and real HTML/SVG text rendered by Next ImageResponse.

Interfaces: `SiteHeader()` is shared by marketing layouts; `HeroScene()` fills its positioned parent and is decorative; `Screens()` owns accessible preview selection.

Hero composition:
```tsx
<section className="hero-stage" id="product">
  <div className="site-container hero-layout">
    <div className="hero-copy"><h1>Your infrastructure.<br />Ready to ship.</h1></div>
    <div className="hero-art"><HeroScene /></div>
  </div>
</section>
```

## Task 2: Three.js enhancement

Files: `src/components/landing/hero-scene.tsx`, `src/components/landing/cube-renderer.ts`, package manifest and lockfile.

- [x] Add Three.js and its types; remove OGL only after checking consumers.
- [x] Render dark translucent modular cube geometry with cyan edges and secondary magenta illumination.
- [x] Connect scroll to module separation and pointer to restrained orientation; do not hijack scrolling.
- [x] Keep a static cube fallback and lazy initialization; stop on reduced motion, viewport exit, page hiding, errors or context loss.
- [x] Dispose renderer, geometries, materials, observers and listeners on unmount.
- Omitted per user request: scenario testing of fallback, visibility suspension and context loss.

Interface:
```ts
export function HeroScene(): React.ReactNode;
```

## Task 3: Inner-page identity

Files: docs layout and page, templates layouts/list/detail/error/cards, comparisons, `src/app/editorial.css`, OG generators.

- [x] Apply shared navigation and editorial title scale to catalog and comparisons.
- [x] Improve docs reading rhythm, code blocks, active sidebar and focus states without changing MDX content or URLs.
- [x] Refine template cards, filters and detail information hierarchy; preserve API behavior and sanitization.
- [x] Apply the shared social-image treatment to docs and templates.
- Omitted per user request: a responsive review pass of representative pages.

## Task 4: Delivery

User override during implementation: no tests and no review. Those workflows are omitted. The local preview remains available during development.

- [x] Add the generated artwork and new social-image routes.
- [x] Document the new visual architecture in `docs/design/hosted/site.md`.
- [x] Finish integrating the inner-page stylesheet.
- [x] Compile the completed site for executable delivery, without running tests or review.
- [x] Leave the local preview open and report the implementation.

## Execution notes

The site remains a preview change with no commit or deployment requested. The approved direction and existing repository rules resolve routine implementation choices without additional approval gates.

Delivery: `pnpm build` completed successfully (308 static pages). No tests or review were run. Preview: http://localhost:3002/.
