import type { SceneVariant } from "./scene-renderer";

const project = (x: number, y: number, z: number) => [
  400 + (x - z) * 79,
  345 + (x + z) * 35 - y * 96,
];

export function ScenePoster({ variant }: { variant: SceneVariant }) {
  if (variant === "cluster") {
    return (
      <svg viewBox="0 0 1000 420" fill="none" className="infrastructure-poster" aria-hidden="true">
        <title>Connected infrastructure</title>
        <defs>
          <pattern id="network-dots" width="22" height="22" patternUnits="userSpaceOnUse">
            <circle cx="1" cy="1" r="0.7" fill="#345767" />
          </pattern>
        </defs>
        <rect x="40" y="50" width="920" height="330" fill="url(#network-dots)" />
        <g stroke="#2b6372">
          <path d="M500 210H260V110H180M500 210H710V110H820M500 210H330V320H230M500 210H710V320H820" />
        </g>
        {[
          [500, 210],
          [180, 110],
          [820, 110],
          [230, 320],
          [820, 320],
        ].map(([x, y], i) => (
          <g key={x + y} transform={`translate(${x} ${y})`}>
            {[0, 1, 2].slice(0, i === 0 ? 3 : 2).map((layer) => (
              <path
                key={layer}
                transform={`translate(0 ${-layer * 13})`}
                d="M-48 0 0-22 48 0 0 22ZM-48 0v10L0 32l48-22V0M0 22v10"
                fill="#0a171f"
                stroke={i === 0 ? "#2de2e6" : "#648f9c"}
                strokeWidth="1"
              />
            ))}
            <text
              y={i === 0 ? -62 : -48}
              textAnchor="middle"
              fill="#9ebbc6"
              fontSize="10"
              fontFamily="monospace"
            >
              {i === 0 ? "CONTROL PLANE" : `WORKER / 0${i}`}
            </text>
          </g>
        ))}
      </svg>
    );
  }
  const dots = [];
  for (let face = 0; face < 3; face++)
    for (let i = 0; i <= 18; i++)
      for (let j = 0; j <= 18; j++) {
        const u = ((i / 18) * 2 - 1) * 1.36,
          v = ((j / 18) * 2 - 1) * 1.36;
        const [x, y] =
          face === 0 ? project(1.36, u, v) : face === 1 ? project(u, v, 1.36) : project(u, 1.36, v);
        dots.push(
          <circle
            key={`${face}-${i}-${j}`}
            cx={x}
            cy={y}
            r={i % 6 === 0 || j % 6 === 0 ? 1.25 : 0.85}
            fill={face === 2 ? "#a1ced6" : "#2de2e6"}
            opacity={face === 2 ? 0.7 : 0.5}
          />,
        );
      }
  return (
    <svg viewBox="0 0 800 700" fill="none" className="infrastructure-poster" aria-hidden="true">
      <title>Infrastructure formed from connected particles</title>
      <g stroke="#367482" strokeWidth="0.75" opacity="0.65">
        <path d="M10 490C140 490 140 230 280 240S510 420 790 190M20 260C150 250 190 440 380 440S630 210 780 240M110 570C320 470 120 110 730 120" />
      </g>
      {dots}
    </svg>
  );
}
