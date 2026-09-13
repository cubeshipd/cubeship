"use client";

import { Color, Mesh, Program, Renderer, Triangle } from "ogl";
import { useEffect, useMemo, useRef } from "react";
import { cn } from "@/lib/cn";

// React Bits' FaultyTerminal (MIT), typed and trimmed: a shader drawing
// a grid of flickering glyphs, used as the hero's ground. Every visual
// knob is a uniform, so nothing here re-renders React to animate.

const vertexShader = `
attribute vec2 position;
attribute vec2 uv;
varying vec2 vUv;
void main() {
  vUv = uv;
  gl_Position = vec4(position, 0.0, 1.0);
}
`;

const fragmentShader = `
precision mediump float;

varying vec2 vUv;

uniform float iTime;
uniform vec3  iResolution;
uniform float uScale;
uniform vec2  uGridMul;
uniform float uDigitSize;
uniform float uScanlineIntensity;
uniform float uGlitchAmount;
uniform float uFlickerAmount;
uniform float uNoiseAmp;
uniform float uCurvature;
uniform vec3  uTint;
uniform vec2  uMouse;
uniform float uMouseStrength;
uniform float uUseMouse;
uniform float uPageLoadProgress;
uniform float uBrightness;

float time;

float noise(vec2 p) {
  return sin(p.x * 10.0) * sin(p.y * (3.0 + sin(time * 0.090909))) + 0.2;
}

mat2 rotate(float angle) {
  float c = cos(angle);
  float s = sin(angle);
  return mat2(c, -s, s, c);
}

float fbm(vec2 p) {
  p *= 1.1;
  float f = 0.0;
  float amp = 0.5 * uNoiseAmp;
  mat2 modify0 = rotate(time * 0.02);
  f += amp * noise(p);
  p = modify0 * p * 2.0;
  amp *= 0.454545;
  mat2 modify1 = rotate(time * 0.02);
  f += amp * noise(p);
  p = modify1 * p * 2.0;
  amp *= 0.454545;
  mat2 modify2 = rotate(time * 0.08);
  f += amp * noise(p);
  return f;
}

float pattern(vec2 p, out vec2 q, out vec2 r) {
  vec2 offset1 = vec2(1.0);
  vec2 offset0 = vec2(0.0);
  mat2 rot01 = rotate(0.1 * time);
  mat2 rot1 = rotate(0.1);
  q = vec2(fbm(p + offset1), fbm(rot01 * p + offset1));
  r = vec2(fbm(rot1 * q + offset0), fbm(q + offset0));
  return fbm(p + r);
}

float digit(vec2 p) {
  vec2 grid = uGridMul * 15.0;
  vec2 s = floor(p * grid) / grid;
  p = p * grid;
  vec2 q, r;
  float intensity = pattern(s * 0.1, q, r) * 1.3 - 0.03;

  if (uUseMouse > 0.5) {
    vec2 mouseWorld = uMouse * uScale;
    float distToMouse = distance(s, mouseWorld);
    float mouseInfluence = exp(-distToMouse * 8.0) * uMouseStrength * 10.0;
    intensity += mouseInfluence;
    intensity += sin(distToMouse * 20.0 - iTime * 5.0) * 0.1 * mouseInfluence;
  }

  float cellRandom = fract(sin(dot(s, vec2(12.9898, 78.233))) * 43758.5453);
  float cellProgress = clamp((uPageLoadProgress - cellRandom * 0.8) / 0.2, 0.0, 1.0);
  intensity *= smoothstep(0.0, 1.0, cellProgress);

  p = fract(p);
  p *= uDigitSize;

  float px5 = p.x * 5.0;
  float py5 = (1.0 - p.y) * 5.0;
  float x = fract(px5);
  float y = fract(py5);
  float i = floor(py5) - 2.0;
  float j = floor(px5) - 2.0;
  float n = i * i + j * j;
  float f = n * 0.0625;

  float isOn = step(0.1, intensity - f);
  float brightness = isOn * (0.2 + y * 0.8) * (0.75 + x * 0.25);
  return step(0.0, p.x) * step(p.x, 1.0) * step(0.0, p.y) * step(p.y, 1.0) * brightness;
}

float onOff(float a, float b, float c) {
  return step(c, sin(iTime + a * cos(iTime * b))) * uFlickerAmount;
}

float displace(vec2 look) {
  float y = look.y - mod(iTime * 0.25, 1.0);
  float window = 1.0 / (1.0 + 50.0 * y * y);
  return sin(look.y * 20.0 + iTime) * 0.0125 * onOff(4.0, 2.0, 0.8) * (1.0 + cos(iTime * 60.0)) * window;
}

vec3 getColor(vec2 p) {
  float bar = step(mod(p.y + time * 20.0, 1.0), 0.2) * 0.4 + 1.0;
  bar *= uScanlineIntensity;

  float displacement = displace(p) * uGlitchAmount;
  p.x += displacement;

  float middle = digit(p);
  const float off = 0.002;
  float sum = digit(p + vec2(-off, -off)) + digit(p + vec2(0.0, -off)) + digit(p + vec2(off, -off)) +
              digit(p + vec2(-off, 0.0)) + digit(p + vec2(0.0, 0.0)) + digit(p + vec2(off, 0.0)) +
              digit(p + vec2(-off, off)) + digit(p + vec2(0.0, off)) + digit(p + vec2(off, off));
  return vec3(0.9) * middle + sum * 0.1 * vec3(1.0) * bar;
}

vec2 barrel(vec2 uv) {
  vec2 c = uv * 2.0 - 1.0;
  float r2 = dot(c, c);
  c *= 1.0 + uCurvature * r2;
  return c * 0.5 + 0.5;
}

void main() {
  time = iTime * 0.333333;
  vec2 uv = vUv;
  if (uCurvature != 0.0) uv = barrel(uv);
  vec3 col = getColor(uv * uScale);
  col *= uTint * uBrightness;
  gl_FragColor = vec4(col, 1.0);
}
`;

function hexToRgb(hex: string): [number, number, number] {
  let h = hex.replace("#", "").trim();
  if (h.length === 3) h = [...h].map((c) => c + c).join("");
  const num = Number.parseInt(h.slice(0, 6), 16);
  return [((num >> 16) & 255) / 255, ((num >> 8) & 255) / 255, (num & 255) / 255];
}

export type FaultyTerminalProps = {
  scale?: number;
  gridMul?: [number, number];
  digitSize?: number;
  timeScale?: number;
  scanlineIntensity?: number;
  glitchAmount?: number;
  flickerAmount?: number;
  noiseAmp?: number;
  curvature?: number;
  tint?: string;
  mouseReact?: boolean;
  mouseStrength?: number;
  brightness?: number;
  className?: string;
};

export function FaultyTerminal({
  scale = 1,
  gridMul = [2, 1],
  digitSize = 1.5,
  timeScale = 0.3,
  scanlineIntensity = 0.3,
  glitchAmount = 1,
  flickerAmount = 1,
  noiseAmp = 1,
  curvature = 0,
  tint = "#ffffff",
  mouseReact = true,
  mouseStrength = 0.2,
  brightness = 1,
  className,
}: FaultyTerminalProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const tintVec = useMemo(() => hexToRgb(tint), [tint]);
  const [gridX, gridY] = gridMul;

  useEffect(() => {
    const ctn = containerRef.current;
    if (!ctn) return;

    // One device pixel per CSS pixel: nine noise samples per fragment is
    // plenty of work for a background, and a Retina factor would quadruple it.
    const renderer = new Renderer({ dpr: 1, alpha: false });
    const gl = renderer.gl;
    gl.clearColor(0, 0, 0, 1);

    const mouse = { x: 0.5, y: 0.5 };
    const smooth = { x: 0.5, y: 0.5 };
    const program = new Program(gl, {
      vertex: vertexShader,
      fragment: fragmentShader,
      uniforms: {
        iTime: { value: 0 },
        iResolution: { value: new Color(1, 1, 1) },
        uScale: { value: scale },
        uGridMul: { value: new Float32Array([gridX, gridY]) },
        uDigitSize: { value: digitSize },
        uScanlineIntensity: { value: scanlineIntensity },
        uGlitchAmount: { value: glitchAmount },
        uFlickerAmount: { value: flickerAmount },
        uNoiseAmp: { value: noiseAmp },
        uCurvature: { value: curvature },
        uTint: { value: new Color(tintVec[0], tintVec[1], tintVec[2]) },
        uMouse: { value: new Float32Array([0.5, 0.5]) },
        uMouseStrength: { value: mouseStrength },
        uUseMouse: { value: mouseReact ? 1 : 0 },
        uPageLoadProgress: { value: 0 },
        uBrightness: { value: brightness },
      },
    });
    const mesh = new Mesh(gl, { geometry: new Triangle(gl), program });

    const resize = () => {
      renderer.setSize(ctn.offsetWidth, ctn.offsetHeight);
      program.uniforms.iResolution.value = new Color(
        gl.canvas.width,
        gl.canvas.height,
        gl.canvas.width / gl.canvas.height,
      );
    };
    const observer = new ResizeObserver(resize);
    observer.observe(ctn);
    resize();

    // A reduced-motion visitor gets the glyphs and none of the movement.
    const still = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    const timeOffset = Math.random() * 100;
    let start = 0;
    let raf = 0;
    const update = (t: number) => {
      raf = requestAnimationFrame(update);
      if (start === 0) start = t;
      if (!still) program.uniforms.iTime.value = (t * 0.001 + timeOffset) * timeScale;
      program.uniforms.uPageLoadProgress.value = Math.min((t - start) / 2000, 1);
      if (mouseReact) {
        smooth.x += (mouse.x - smooth.x) * 0.08;
        smooth.y += (mouse.y - smooth.y) * 0.08;
        program.uniforms.uMouse.value[0] = smooth.x;
        program.uniforms.uMouse.value[1] = smooth.y;
      }
      renderer.render({ scene: mesh });
    };
    raf = requestAnimationFrame(update);

    const onMove = (e: MouseEvent) => {
      const rect = ctn.getBoundingClientRect();
      mouse.x = (e.clientX - rect.left) / rect.width;
      mouse.y = 1 - (e.clientY - rect.top) / rect.height;
    };
    // The hero's content sits over this element, so the movement is
    // read off the section rather than the canvas.
    const surface = ctn.parentElement ?? ctn;
    if (mouseReact) surface.addEventListener("mousemove", onMove);
    ctn.appendChild(gl.canvas);

    return () => {
      cancelAnimationFrame(raf);
      observer.disconnect();
      if (mouseReact) surface.removeEventListener("mousemove", onMove);
      if (gl.canvas.parentElement === ctn) ctn.removeChild(gl.canvas);
      gl.getExtension("WEBGL_lose_context")?.loseContext();
    };
  }, [
    scale,
    gridX,
    gridY,
    digitSize,
    timeScale,
    scanlineIntensity,
    glitchAmount,
    flickerAmount,
    noiseAmp,
    curvature,
    tintVec,
    mouseReact,
    mouseStrength,
    brightness,
  ]);

  return <div ref={containerRef} aria-hidden="true" className={cn("overflow-hidden", className)} />;
}
