import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { ImageResponse } from "next/og";

// Font files are vendored alongside the website fonts. Social cards have no
// dependency on an external font service or generated campaign artwork.
const fonts = Promise.all([
  readFile(join(process.cwd(), "src/fonts/ChakraPetch-Regular.ttf")),
  readFile(join(process.cwd(), "src/fonts/ChakraPetch-Bold.ttf")),
]);

function PointCube() {
  const dots = [];
  for (let face = 0; face < 3; face++) {
    for (let i = 0; i <= 22; i++) {
      for (let j = 0; j <= 22; j++) {
        const u = (i / 11 - 1) * 1.3;
        const v = (j / 11 - 1) * 1.3;
        const [x, y, z] = face === 0 ? [1.3, u, v] : face === 1 ? [u, v, 1.3] : [u, 1.3, v];
        const edge = i === 0 || i === 22 || j === 0 || j === 22;
        dots.push(
          <circle
            key={`${face}-${i}-${j}`}
            cx={230 + (x - z) * 73}
            cy={228 + (x + z) * 33 - y * 88}
            r={edge ? 1.55 : 1.05}
            fill={face === 2 ? "#d7e3ec" : "#2de2e6"}
            opacity={edge ? 0.95 : face === 0 ? 0.62 : 0.36}
          />,
        );
      }
    }
  }
  return (
    <svg
      width="460"
      height="460"
      viewBox="0 0 460 460"
      fill="none"
      role="img"
      aria-label="Cubeship particle cube"
    >
      {dots}
    </svg>
  );
}

export async function socialImage({
  title,
  description,
  section = "The self-hosted platform",
}: {
  title: string;
  description?: string;
  section?: string;
}) {
  const [regular, bold] = await fonts;
  const heading = title.length > 110 ? `${title.slice(0, 107)}…` : title;
  return new ImageResponse(
    <div
      style={{
        width: "100%",
        height: "100%",
        display: "flex",
        position: "relative",
        background: "#05080b",
        color: "#d7e3ec",
        fontFamily: "Chakra Petch",
        overflow: "hidden",
      }}
    >
      <div
        style={{
          position: "absolute",
          top: 24,
          right: 24,
          bottom: 24,
          left: 24,
          border: "1px solid #20303b",
          display: "flex",
        }}
      />
      <div
        style={{
          position: "absolute",
          left: 64,
          top: 24,
          width: 56,
          height: 2,
          background: "#2de2e6",
          display: "flex",
        }}
      />
      <div style={{ position: "absolute", right: 12, top: 55, display: "flex" }}>
        <PointCube />
      </div>
      <div
        style={{
          position: "absolute",
          right: 60,
          bottom: 112,
          width: 340,
          display: "flex",
          justifyContent: "center",
          fontSize: 15,
          letterSpacing: 2,
          color: "#6a929e",
        }}
      >
        YOUR SERVERS. YOUR RULES.
      </div>
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          width: "100%",
          padding: "58px 64px 50px",
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 16 }}>
          <svg
            width="30"
            height="34"
            viewBox="0 0 24 24"
            fill="none"
            stroke="#2de2e6"
            strokeWidth="1.25"
            role="img"
            aria-label="Cubeship"
          >
            <path d="M12 2 21 6.9v10.2L12 22l-9-4.9V6.9zM3 6.9 12 11.8l9-4.9M12 11.8V22" />
          </svg>
          <span style={{ fontSize: 23, letterSpacing: 6 }}>CUBESHIP</span>
        </div>
        <div style={{ display: "flex", marginTop: 64, color: "#2de2e6", fontSize: 18 }}>
          {section}
        </div>
        <div
          style={{
            display: "flex",
            marginTop: 20,
            width: 670,
            fontSize: heading.length > 70 ? 48 : heading.length > 48 ? 60 : 70,
            fontWeight: 700,
            lineHeight: 1.08,
            letterSpacing: -2.4,
          }}
        >
          {heading}
        </div>
        {description && (
          <div
            style={{
              display: "flex",
              width: 575,
              marginTop: 26,
              fontSize: 23,
              lineHeight: 1.45,
              color: "#8fa5b4",
            }}
          >
            {description.length > 140 ? `${description.slice(0, 137)}…` : description}
          </div>
        )}
        <div
          style={{
            marginTop: "auto",
            paddingTop: 20,
            borderTop: "1px solid #20303b",
            display: "flex",
            justifyContent: "space-between",
            color: "#8fa5b4",
            fontSize: 16,
          }}
        >
          <span>Open source. Self hosted. All yours.</span>
          <span style={{ color: "#d7e3ec" }}>cubeship.dev</span>
        </div>
      </div>
    </div>,
    {
      width: 1200,
      height: 630,
      fonts: [
        { name: "Chakra Petch", data: regular, weight: 400, style: "normal" },
        { name: "Chakra Petch", data: bold, weight: 700, style: "normal" },
      ],
    },
  );
}
