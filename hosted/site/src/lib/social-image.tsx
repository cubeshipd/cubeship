import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { ImageResponse } from "next/og";

export async function socialImage({
  title,
  description,
  section = "The self-hosted platform",
}: {
  title: string;
  description?: string;
  section?: string;
}) {
  const art = await readFile(join(process.cwd(), "src/images/brand/cube.png"));
  const src = `data:image/png;base64,${art.toString("base64")}`;
  return new ImageResponse(
    <div
      style={{
        width: "100%",
        height: "100%",
        display: "flex",
        background: "#05070a",
        color: "#d7e3ec",
        position: "relative",
        overflow: "hidden",
      }}
    >
      {/* biome-ignore lint/performance/noImgElement: ImageResponse renders the image into the social card. */}
      <img
        src={src}
        alt=""
        width={650}
        height={630}
        style={{ position: "absolute", right: -75, top: 0, objectFit: "cover", opacity: 0.8 }}
      />
      <div
        style={{
          display: "flex",
          position: "absolute",
          inset: 0,
          background: "linear-gradient(90deg, #05070a 35%, transparent 100%)",
        }}
      />
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          width: "100%",
          padding: "55px 65px",
          position: "relative",
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 18, color: "#2de2e6" }}>
          <svg
            width="34"
            height="38"
            viewBox="0 0 24 24"
            fill="none"
            stroke="#2de2e6"
            strokeWidth="1.3"
          >
            <title>Cubeship</title>
            <path d="M12 2 21 6.9v10.2L12 22l-9-4.9V6.9zM3 6.9 12 11.8l9-4.9M12 11.8V22" />
          </svg>
          <span style={{ fontSize: 24, letterSpacing: 6 }}>CUBESHIP</span>
        </div>
        <div style={{ marginTop: 66, display: "flex", color: "#2de2e6", fontSize: 18 }}>
          {section}
        </div>
        <div
          style={{
            display: "flex",
            marginTop: 22,
            maxWidth: 780,
            fontSize: title.length > 65 ? 46 : title.length > 38 ? 60 : 72,
            fontWeight: 700,
            lineHeight: 1.08,
            letterSpacing: -3,
          }}
        >
          {title}
        </div>
        {description ? (
          <div
            style={{
              display: "flex",
              marginTop: 22,
              maxWidth: 660,
              fontSize: 23,
              lineHeight: 1.45,
              color: "#94aabb",
            }}
          >
            {description.length > 170 ? `${description.slice(0, 167)}…` : description}
          </div>
        ) : null}
        <div
          style={{
            display: "flex",
            marginTop: "auto",
            paddingTop: 24,
            borderTop: "1px solid #263c49",
            justifyContent: "space-between",
            fontSize: 16,
            color: "#8fa6b8",
          }}
        >
          <span>Open source. Self hosted. All yours.</span>
          <span>cubeship.dev</span>
        </div>
      </div>
    </div>,
    { width: 1200, height: 630 },
  );
}
