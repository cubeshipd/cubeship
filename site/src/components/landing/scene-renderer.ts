import * as THREE from "three";

export type SceneVariant = "hero" | "cluster";
export type SceneFrame = {
  elapsedSeconds: number;
  pointerX: number;
  pointerY: number;
  scrollProgress: number;
};
export type SceneRenderer = {
  dispose: () => void;
  render: (frame: SceneFrame) => void;
  resize: (width: number, height: number, devicePixelRatio: number) => void;
};

type Resource = { dispose(): void };
type Track = <T extends Resource>(resource: T) => T;
const CYAN = new THREE.Color("#2de2e6");
const SILVER = new THREE.Color("#aac8d3");

function random(seed: number) {
  const x = Math.sin(seed * 127.1 + 311.7) * 43758.5453;
  return x - Math.floor(x);
}

const pointVertex = `
attribute vec3 color;
attribute float aSize;
attribute float aPhase;
uniform float uTime;
uniform float uRatio;
uniform float uSpread;
uniform float uMotion;
varying vec3 vColor;
varying float vAlpha;
void main() {
  vec3 p = position;
  float sweep = sin(p.y * 2.4 + p.x * 1.2 - uTime * 0.55);
  p *= 1.0 + uSpread * 0.14;
  p.x += sin(aPhase + uTime * 0.22) * 0.018 * uMotion;
  p.z += cos(aPhase + uTime * 0.18) * 0.018 * uMotion;
  vec4 mv = modelViewMatrix * vec4(p, 1.0);
  gl_Position = projectionMatrix * mv;
  gl_PointSize = aSize * uRatio;
  vColor = color;
  vAlpha = (0.46 + 0.22 * sweep) * clamp(1.5 + mv.z * 0.075, 0.2, 1.0);
}
`;
const pointFragment = `
varying vec3 vColor;
varying float vAlpha;
void main() {
  float d = length(gl_PointCoord - 0.5);
  float a = 1.0 - smoothstep(0.16, 0.5, d);
  if (a < 0.01) discard;
  gl_FragColor = vec4(vColor, a * vAlpha);
  #include <colorspace_fragment>
}
`;

function pointCloud(positions: number[], colors: number[], sizes: number[], track: Track) {
  const geometry = track(new THREE.BufferGeometry());
  geometry.setAttribute("position", new THREE.Float32BufferAttribute(positions, 3));
  geometry.setAttribute("color", new THREE.Float32BufferAttribute(colors, 3));
  geometry.setAttribute("aSize", new THREE.Float32BufferAttribute(sizes, 1));
  geometry.setAttribute(
    "aPhase",
    new THREE.Float32BufferAttribute(
      sizes.map((_, i) => random(i + 4) * Math.PI * 2),
      1,
    ),
  );
  const material = track(
    new THREE.ShaderMaterial({
      vertexShader: pointVertex,
      fragmentShader: pointFragment,
      uniforms: {
        uTime: { value: 0 },
        uRatio: { value: 1 },
        uSpread: { value: 0 },
        uMotion: { value: 1 },
      },
      transparent: true,
      depthWrite: false,
      blending: THREE.AdditiveBlending,
    }),
  );
  return new THREE.Points(geometry, material);
}

function line(
  points: THREE.Vector3[],
  color: THREE.ColorRepresentation,
  opacity: number,
  track: Track,
) {
  return new THREE.Line(
    track(new THREE.BufferGeometry().setFromPoints(points)),
    track(new THREE.LineBasicMaterial({ color, transparent: true, opacity, depthWrite: false })),
  );
}

function hero(scene: THREE.Scene, track: Track) {
  const assembly = new THREE.Group();
  scene.add(assembly);
  const positions: number[] = [];
  const colors: number[] = [];
  const sizes: number[] = [];
  const divisions = 38;
  const extent = 1.36;
  for (let face = 0; face < 6; face++) {
    for (let i = 0; i <= divisions; i++) {
      for (let j = 0; j <= divisions; j++) {
        const u = ((i / divisions) * 2 - 1) * extent;
        const v = ((j / divisions) * 2 - 1) * extent;
        const p =
          face < 2
            ? [extent * (face ? 1 : -1), u, v]
            : face < 4
              ? [u, extent * (face === 3 ? 1 : -1), v]
              : [u, v, extent * (face === 5 ? 1 : -1)];
        if (Math.abs(p[1] - 0.46) < 0.045 || Math.abs(p[1] + 0.46) < 0.045) continue;
        positions.push(...p);
        const edge = i === 0 || j === 0 || i === divisions || j === divisions;
        const color = (edge ? SILVER : CYAN)
          .clone()
          .multiplyScalar(edge ? 1.6 : 0.6 + random(i * 100 + j + face) * 0.8);
        colors.push(color.r, color.g, color.b);
        sizes.push(edge ? 2.6 : 1.5 + random(i * 21 + j) * 1.1);
      }
    }
  }
  const cloud = pointCloud(positions, colors, sizes, track);
  assembly.add(cloud);

  const guides = new THREE.Group();
  for (const y of [-extent, -0.46, 0.46, extent]) {
    guides.add(
      line(
        [
          new THREE.Vector3(-extent, y, -extent),
          new THREE.Vector3(extent, y, -extent),
          new THREE.Vector3(extent, y, extent),
          new THREE.Vector3(-extent, y, extent),
          new THREE.Vector3(-extent, y, -extent),
        ],
        "#5bafb8",
        0.22,
        track,
      ),
    );
  }
  assembly.add(guides);

  const paths: THREE.CatmullRomCurve3[] = [];
  const routeColors = ["#2de2e6", "#72a9b7", "#2de2e6", "#b482b4"];
  for (let i = 0; i < 4; i++) {
    const side = i % 2 ? 1 : -1;
    const y = (i - 1.5) * 0.59;
    const curve = new THREE.CatmullRomCurve3([
      new THREE.Vector3(side * 4.3, y - 1.5, -1.6),
      new THREE.Vector3(side * 2.35, y - 0.25, -1.25),
      new THREE.Vector3(side * 1.55, y, 0.05),
      new THREE.Vector3(side * 0.45, y, 0.2),
      new THREE.Vector3(-side * 1.85, y + 0.35, 0.8),
      new THREE.Vector3(-side * 3.8, y + 0.8, 0.9),
    ]);
    paths.push(curve);
    assembly.add(line(curve.getPoints(100), routeColors[i], 0.25, track));
  }
  const trailPositions = new Float32Array(4 * 30 * 3);
  const trailsGeometry = track(new THREE.BufferGeometry());
  trailsGeometry.setAttribute("position", new THREE.BufferAttribute(trailPositions, 3));
  const trailMaterial = track(
    new THREE.PointsMaterial({
      color: "#7ef7f1",
      size: 2.2,
      sizeAttenuation: false,
      transparent: true,
      opacity: 0.75,
      blending: THREE.AdditiveBlending,
      depthWrite: false,
    }),
  );
  assembly.add(new THREE.Points(trailsGeometry, trailMaterial));

  const dustPositions: number[] = [],
    dustColors: number[] = [],
    dustSizes: number[] = [];
  for (let i = 0; i < 650; i++) {
    dustPositions.push(
      (random(i * 3) - 0.5) * 10,
      (random(i * 3 + 1) - 0.5) * 7,
      (random(i * 3 + 2) - 0.5) * 6,
    );
    const color = (i % 19 === 0 ? new THREE.Color("#ba6e99") : SILVER)
      .clone()
      .multiplyScalar(0.2 + random(i + 51) * 0.35);
    dustColors.push(color.r, color.g, color.b);
    dustSizes.push(1 + random(i + 18) * 1.4);
  }
  const dust = pointCloud(dustPositions, dustColors, dustSizes, track);
  scene.add(dust);
  const point = new THREE.Vector3();
  let rotationY = -0.56;
  let rotationX = 0.38;
  return {
    update(frame: SceneFrame) {
      rotationY = THREE.MathUtils.lerp(
        rotationY,
        -0.56 + frame.pointerX * 0.11 + frame.scrollProgress * 0.22,
        0.035,
      );
      rotationX = THREE.MathUtils.lerp(rotationX, 0.38 + frame.pointerY * 0.06, 0.035);
      assembly.rotation.set(rotationX, rotationY, -0.06);
      assembly.position.y = Math.sin(frame.elapsedSeconds * 0.25) * 0.045;
      cloud.material.uniforms.uTime.value = frame.elapsedSeconds;
      cloud.material.uniforms.uSpread.value = frame.scrollProgress;
      guides.scale.setScalar(1 + frame.scrollProgress * 0.14);
      dust.material.uniforms.uTime.value = frame.elapsedSeconds * 0.5;
      for (let route = 0; route < paths.length; route++) {
        for (let k = 0; k < 30; k++) {
          const progress = (frame.elapsedSeconds * 0.07 + route * 0.24 - k * 0.002 + 10) % 1;
          paths[route].getPoint(progress, point);
          point.toArray(trailPositions, (route * 30 + k) * 3);
        }
      }
      trailsGeometry.attributes.position.needsUpdate = true;
    },
    pixelRatio(ratio: number) {
      cloud.material.uniforms.uRatio.value = ratio;
      dust.material.uniforms.uRatio.value = ratio;
    },
  };
}

function cluster(scene: THREE.Scene, track: Track) {
  const network = new THREE.Group();
  scene.add(network);
  const base = track(new THREE.MeshBasicMaterial({ color: "#0a171f" }));
  const top = track(new THREE.MeshBasicMaterial({ color: "#102b34" }));
  const edgeMaterial = track(
    new THREE.LineBasicMaterial({ color: "#69bcc5", transparent: true, opacity: 0.55 }),
  );
  const nodeGeometry = track(new THREE.BoxGeometry(1.08, 0.1, 1.08));
  const edgeGeometry = track(new THREE.EdgesGeometry(nodeGeometry));
  const nodes = [
    { x: 0, z: 0, layers: 4, name: "CONTROL PLANE" },
    { x: -3.35, z: -1.6, layers: 2, name: "WORKER / 01" },
    { x: 3.15, z: -1.9, layers: 3, name: "WORKER / 02" },
    { x: -2.35, z: 2.4, layers: 2, name: "WORKER / 03" },
    { x: 3.55, z: 2.1, layers: 2, name: "WORKER / 04" },
  ];
  const columns: THREE.Group[] = [];
  const routes: THREE.CurvePath<THREE.Vector3>[] = [];
  const labelTexture = (text: string) => {
    const canvas = document.createElement("canvas");
    canvas.width = 512;
    canvas.height = 64;
    const context = canvas.getContext("2d");
    if (!context) throw new Error("Unable to create scene labels");
    context.font = "500 26px monospace";
    context.textAlign = "center";
    context.fillStyle = "#8db8c6";
    context.fillText(text, 256, 39);
    const texture = track(new THREE.CanvasTexture(canvas));
    texture.colorSpace = THREE.SRGBColorSpace;
    return texture;
  };
  nodes.forEach((node, index) => {
    const column = new THREE.Group();
    column.position.set(node.x, 0, node.z);
    for (let layer = 0; layer < node.layers; layer++) {
      const slab = new THREE.Mesh(nodeGeometry, layer === node.layers - 1 ? top : base);
      slab.position.y = 0.12 + layer * 0.22;
      const edges = new THREE.LineSegments(edgeGeometry, edgeMaterial);
      slab.add(edges);
      column.add(slab);
      const rail = line(
        [new THREE.Vector3(-0.4, 0.055, 0.55), new THREE.Vector3(0.14, 0.055, 0.55)],
        index === 0 ? "#2de2e6" : "#69b5c2",
        0.9,
        track,
      );
      slab.add(rail);
    }
    const label = new THREE.Sprite(
      track(
        new THREE.SpriteMaterial({
          map: labelTexture(node.name),
          transparent: true,
          opacity: 0.95,
          depthTest: false,
        }),
      ),
    );
    label.position.set(0, node.layers * 0.22 + 0.4, 0);
    label.scale.set(2.1, 0.2625, 1);
    column.add(label);
    network.add(column);
    columns.push(column);
    if (index === 0) return;
    const route = new THREE.CurvePath<THREE.Vector3>();
    const points = [
      new THREE.Vector3(0, 0.025, 0),
      new THREE.Vector3(node.x * 0.48, 0.025, 0),
      new THREE.Vector3(node.x * 0.48, 0.025, node.z),
      new THREE.Vector3(node.x, 0.025, node.z),
    ];
    for (let i = 0; i < points.length - 1; i++)
      route.add(new THREE.LineCurve3(points[i], points[i + 1]));
    network.add(line(points, "#2a6675", 0.72, track));
    routes.push(route);
  });
  const dots: number[] = [];
  for (let x = -28; x <= 28; x++)
    for (let z = -16; z <= 16; z++) dots.push(x * 0.22, -0.035, z * 0.22);
  const ground = track(new THREE.BufferGeometry());
  ground.setAttribute("position", new THREE.Float32BufferAttribute(dots, 3));
  network.add(
    new THREE.Points(
      ground,
      track(
        new THREE.PointsMaterial({
          color: "#345767",
          size: 1.2,
          sizeAttenuation: false,
          transparent: true,
          opacity: 0.65,
        }),
      ),
    ),
  );

  const packets = new Float32Array(routes.length * 12 * 3);
  const packetGeometry = track(new THREE.BufferGeometry());
  packetGeometry.setAttribute("position", new THREE.BufferAttribute(packets, 3));
  network.add(
    new THREE.Points(
      packetGeometry,
      track(
        new THREE.PointsMaterial({
          color: "#72ffef",
          size: 3,
          sizeAttenuation: false,
          transparent: true,
          opacity: 0.95,
          blending: THREE.AdditiveBlending,
          depthWrite: false,
        }),
      ),
    ),
  );
  const packet = new THREE.Vector3();
  let yaw = -0.15;
  return {
    update(frame: SceneFrame) {
      yaw = THREE.MathUtils.lerp(yaw, -0.15 + frame.pointerX * 0.035, 0.03);
      network.rotation.y = yaw;
      for (let i = 0; i < routes.length; i++)
        for (let k = 0; k < 12; k++) {
          routes[i].getPoint((frame.elapsedSeconds * 0.15 + i * 0.23 - k * 0.005 + 10) % 1, packet);
          packet.y = 0.05;
          packet.toArray(packets, (i * 12 + k) * 3);
        }
      packetGeometry.attributes.position.needsUpdate = true;
      columns.forEach((column, i) => {
        column.position.y = i === 0 ? 0 : Math.sin(frame.elapsedSeconds * 0.45 + i) * 0.018;
      });
    },
    pixelRatio(_ratio: number) {},
  };
}

export function createSceneRenderer(
  canvas: HTMLCanvasElement,
  variant: SceneVariant,
): SceneRenderer {
  const resources: Resource[] = [];
  const track: Track = (resource) => {
    resources.push(resource);
    return resource;
  };
  const renderer = new THREE.WebGLRenderer({
    canvas,
    alpha: true,
    antialias: true,
    powerPreference: "low-power",
  });
  const scene = new THREE.Scene();
  const camera = new THREE.OrthographicCamera(-3.6, 3.6, 3.6, -3.6, 0.1, 100);
  renderer.setClearColor(0x05070a, 0);
  renderer.outputColorSpace = THREE.SRGBColorSpace;
  const dispose = () => {
    for (const resource of resources) resource.dispose();
    renderer.dispose();
  };
  try {
    camera.position.set(
      ...((variant === "hero" ? [0, 0, 10] : [6.5, 7.8, 10]) as [number, number, number]),
    );
    camera.lookAt(0, 0, 0);
    const content = variant === "hero" ? hero(scene, track) : cluster(scene, track);
    return {
      dispose,
      resize(width, height, devicePixelRatio) {
        const ratio = Math.min(devicePixelRatio, 1.5);
        renderer.setPixelRatio(ratio);
        renderer.setSize(width, height, false);
        const aspect = width / height;
        const halfHeight =
          variant === "hero" ? Math.max(2.45, 2.5 / aspect) : Math.max(2.35, 5.9 / aspect);
        camera.left = -halfHeight * aspect;
        camera.right = halfHeight * aspect;
        camera.top = halfHeight;
        camera.bottom = -halfHeight;
        camera.updateProjectionMatrix();
        content.pixelRatio(ratio);
      },
      render(frame) {
        content.update(frame);
        renderer.render(scene, camera);
      },
    };
  } catch (error) {
    dispose();
    throw error;
  }
}
