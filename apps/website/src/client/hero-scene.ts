// "Answers assemble into a form": chips drift in a cloud, gather into a survey card, the card is answered, condenses into the Forma mark while dots stream out into a small bar chart, and everything drifts apart again. A 14-second loop computed entirely in the vertex shader from per-instance attributes, so a frame costs two draw calls and two uniform updates.
//
// Loaded lazily by main.ts, only where it can run well; the static SVG (components/HeroArt.tsx, the same layout) stays on screen until this has drawn its first frame and comes back whenever this gives up.
import {
  InstancedBufferAttribute,
  InstancedMesh,
  OrthographicCamera,
  PlaneGeometry,
  Scene,
  ShaderMaterial,
  WebGLRenderer,
} from "three";
import {
  CARD_CHIPS,
  COLORS,
  type ColorName,
  DESIGN_HEIGHT,
  DESIGN_WIDTH,
  DOTS,
  DOT_SIZE,
  KIND,
  LOGO,
  TYPED_SWEEP,
} from "../hero/layout";

const CLOUD_CHIPS = 177;
const LOOP_SECONDS = 14;
/** A moment in the loop where the card rests answered: what the scene freezes on when it stops animating. */
const RESTING_TIME = 7.9;
/** Where the clock starts: the card at rest, answered, which is exactly the static SVG it fades in over, held for the length of that fade before it condenses into the mark. Starting at 0 would swap the card the visitor is looking at for a cloud of chips. */
const START_TIME = 7.7;
const MAX_TILT = (6 * Math.PI) / 180;
const MAX_DPR = 1.5;
const MAX_CSS_WIDTH = 1200;
const MAX_CSS_HEIGHT = 900;

// Deterministic, so every visitor sees the same cloud.
const random = (() => {
  let seed = 0x5eed;
  return (): number => {
    seed = (seed + 0x6d2b79f5) | 0;
    let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
})();
const between = (min: number, max: number): number => min + random() * (max - min);

// The palette's sRGB values as they are. The shaders write their output unconverted, so the colours must not go through three.js's colour management (Color would linearise them and every tint would come out darker).
const rgb = (name: ColorName): [number, number, number] => {
  const hex = Number.parseInt(COLORS[name].slice(1), 16);
  return [((hex >> 16) & 255) / 255, ((hex >> 8) & 255) / 255, (hex & 255) / 255];
};

// Shared GLSL: the loop clock, easing, and the tilt toward the pointer.
const COMMON = /* glsl */ `
uniform float uTime;
uniform vec2 uPointer;
uniform float uTilt;
varying vec2 vUv;
varying vec2 vSize;
varying float vRadius;
varying vec3 vColor;
varying float vEdge;

float ease(float x) { x = clamp(x, 0.0, 1.0); return 1.0 - pow(1.0 - x, 3.0); }

vec3 tilt(vec3 p) {
  float ax = -uPointer.y * uTilt;
  float ay = uPointer.x * uTilt;
  vec3 q = vec3(p.x * cos(ay) + p.z * sin(ay), p.y, -p.x * sin(ay) + p.z * cos(ay));
  return vec3(q.x, q.y * cos(ax) - q.z * sin(ax), q.y * sin(ax) + q.z * cos(ax));
}

vec3 drift(vec3 base, float phase) {
  return base + vec3(sin(uTime * 0.45 + phase), cos(uTime * 0.38 + phase * 1.7), sin(uTime * 0.3 + phase * 0.6)) * vec3(10.0, 8.0, 20.0);
}

void place(vec3 center, vec2 size, float radius) {
  vUv = uv;
  vSize = max(size, vec2(0.0));
  vRadius = min(radius, min(vSize.x, vSize.y) * 0.5);
  vec3 p = tilt(center) + vec3(position.xy * vSize, 0.0);
  gl_Position = projectionMatrix * modelViewMatrix * vec4(p, 1.0);
}
`;

const CHIP_VERTEX = /* glsl */ `
${COMMON}
attribute vec4 aScatter;   // xyz in the cloud, w = scale of the card size while in the cloud
attribute vec4 aCard;      // centre and size in the card
attribute vec4 aLogo;      // centre and size in the logo
attribute vec4 aMeta;      // delay, phase, kind, edge
attribute vec2 aRadius;    // corner radius in the card and in the logo
attribute vec3 aColor;
attribute vec3 aLogoColor;

const vec3 CRIMSON = vec3(${rgb("crimson").join(", ")});

void main() {
  float t = mod(uTime, ${LOOP_SECONDS.toFixed(1)});
  float delay = aMeta.x;
  float kind = aMeta.z;
  float assemble = ease((t - 3.0 - delay) / 1.4);
  float condense = ease((t - 8.0 - delay * 0.3) / 1.3);
  float release = ease((t - 10.5 - delay * 0.6) / 2.2);
  float answered = smoothstep(5.6, 6.0, t);
  float sweep = clamp((t - 6.0) / 1.4, 0.0, 1.0);

  vec3 cloud = drift(aScatter.xyz, aMeta.y);
  vec2 cardSize = aCard.zw;
  vec2 cardCenter = aCard.xy;
  if (kind == ${KIND.vanish.toFixed(1)}) cardSize = vec2(0.0);
  if (kind == ${KIND.typed.toFixed(1)}) {
    float width = aCard.z * sweep;
    cardCenter.x = aCard.x - aCard.z * 0.5 + width * 0.5;
    cardSize.x = width;
  }
  if (kind == ${KIND.caret.toFixed(1)}) cardCenter.x += ${TYPED_SWEEP.toFixed(1)} * sweep;
  if (kind == ${KIND.pulse.toFixed(1)}) cardSize *= 1.0 + 0.06 * sin(3.14159265 * clamp((t - 7.2) / 0.5, 0.0, 1.0));

  vec3 center = mix(cloud, vec3(cardCenter, 0.0), assemble);
  center = mix(center, vec3(aLogo.xy, 0.0), condense);
  center = mix(center, cloud, release);
  vec2 size = mix(aCard.zw * aScatter.w, cardSize, assemble);
  size = mix(size, aLogo.zw, condense);
  size = mix(size, aCard.zw * aScatter.w, release);
  float radius = mix(mix(aRadius.x * aScatter.w, aRadius.x, assemble), aRadius.y, condense);
  radius = mix(radius, aRadius.x * aScatter.w, release);

  vec3 color = kind == ${KIND.answer.toFixed(1)} ? mix(aColor, CRIMSON, answered) : aColor;
  color = mix(color, aLogoColor, condense);
  vColor = mix(color, aColor, release);
  vEdge = aMeta.w * (1.0 - condense) + aMeta.w * release;

  place(center, size, radius);
}
`;

const DOT_VERTEX = /* glsl */ `
${COMMON}
attribute vec4 aScatter;   // xyz in the cloud, w = size there
attribute vec4 aBar;       // position in its bar, size, delay
attribute vec3 aColor;

void main() {
  float t = mod(uTime, ${LOOP_SECONDS.toFixed(1)});
  float delay = aBar.w;
  float absorb = ease((t - 3.0 - delay * 0.5) / 1.4);
  float stream = ease((t - 8.3 - delay) / 0.7);
  float release = ease((t - 10.5 - delay * 0.4) / 2.2);

  vec3 cloud = drift(aScatter.xyz, delay * 6.2831);
  vec3 source = vec3(${LOGO.dot.x.toFixed(2)}, ${LOGO.dot.y.toFixed(2)}, 0.0);
  vec3 center = mix(mix(mix(cloud, source, absorb), vec3(aBar.xy, 0.0), stream), cloud, release);
  float size = mix(mix(mix(aScatter.w, 0.0, absorb), aBar.z, stream), aScatter.w, release);

  vColor = aColor;
  vEdge = 0.0;
  place(center, vec2(size), size * 0.5);
}
`;

// A rounded rectangle as a signed distance, antialiased over one screen pixel, with an optional 1px edge.
const FRAGMENT = /* glsl */ `
varying vec2 vUv;
varying vec2 vSize;
varying float vRadius;
varying vec3 vColor;
varying float vEdge;

const vec3 EDGE = vec3(${rgb("edge").join(", ")});

void main() {
  if (vSize.x < 0.01 || vSize.y < 0.01) discard;
  vec2 p = (vUv - 0.5) * vSize;
  vec2 q = abs(p) - vSize * 0.5 + vRadius;
  float d = length(max(q, 0.0)) + min(max(q.x, q.y), 0.0) - vRadius;
  float aa = max(fwidth(d), 1e-4);
  float alpha = clamp(0.5 - d / aa, 0.0, 1.0);
  if (alpha <= 0.0) discard;
  float edge = clamp(vEdge, 0.0, 1.0) * clamp(0.5 - (-1.0 - d) / aa, 0.0, 1.0);
  gl_FragColor = vec4(mix(vColor, EDGE, edge), alpha);
}
`;

const chipGeometry = (): { geometry: PlaneGeometry; count: number } => {
  const geometry = new PlaneGeometry(1, 1);
  // The cloud first, then the card from back to front: instances draw in order, so later ones sit on top.
  const chips = [
    ...Array.from({ length: CLOUD_CHIPS }, () => {
      const w = random() < 0.4 ? between(10, 18) : between(18, 40);
      const h = w < 18 ? w : between(8, 12);
      const tint: ColorName = (["crimson", "ink", "sunken"] as const)[Math.floor(random() * 3)] ?? "sunken";
      return {
        card: { x: between(-140, 140), y: between(-100, 100), w, h, r: Math.min(w, h) * 0.3 },
        logo: { x: LOGO.frame.x, y: LOGO.frame.y, w: 0, h: 0, r: 0 },
        color: tint,
        logoColor: tint,
        edge: tint === "sunken",
        kind: KIND.vanish,
        scale: 1,
      };
    }),
    ...CARD_CHIPS.map((chip) => ({ ...chip, scale: chip.card.w > 200 ? 0.2 : between(0.5, 0.8) })),
  ];
  const attribute = (size: number, values: number[]) =>
    new InstancedBufferAttribute(new Float32Array(values), size);
  geometry.setAttribute(
    "aScatter",
    attribute(
      4,
      chips.flatMap((chip) => [between(-190, 190), between(-125, 125), between(-60, 60), chip.scale])
    )
  );
  geometry.setAttribute(
    "aCard",
    attribute(
      4,
      chips.flatMap((c) => [c.card.x, c.card.y, c.card.w, c.card.h])
    )
  );
  geometry.setAttribute(
    "aLogo",
    attribute(
      4,
      chips.flatMap((c) => [c.logo.x, c.logo.y, c.logo.w, c.logo.h])
    )
  );
  geometry.setAttribute(
    "aMeta",
    attribute(
      4,
      chips.flatMap((c) => [random(), random() * Math.PI * 2, c.kind, c.edge ? 1 : 0])
    )
  );
  geometry.setAttribute(
    "aRadius",
    attribute(
      2,
      chips.flatMap((c) => [c.card.r, c.logo.r])
    )
  );
  geometry.setAttribute(
    "aColor",
    attribute(
      3,
      chips.flatMap((c) => rgb(c.color))
    )
  );
  geometry.setAttribute(
    "aLogoColor",
    attribute(
      3,
      chips.flatMap((c) => rgb(c.logoColor))
    )
  );
  return { geometry, count: chips.length };
};

const dotGeometry = (): PlaneGeometry => {
  const geometry = new PlaneGeometry(1, 1);
  const attribute = (size: number, values: number[]) =>
    new InstancedBufferAttribute(new Float32Array(values), size);
  geometry.setAttribute(
    "aScatter",
    attribute(
      4,
      DOTS.flatMap(() => [between(-190, 190), between(-125, 125), between(-60, 60), between(3, 6)])
    )
  );
  geometry.setAttribute(
    "aBar",
    attribute(
      4,
      DOTS.flatMap((dot) => [dot.x, dot.y, DOT_SIZE, dot.bar * 0.12 + dot.row * 0.07])
    )
  );
  geometry.setAttribute(
    "aColor",
    attribute(
      3,
      DOTS.flatMap((dot) => rgb(dot.color))
    )
  );
  return geometry;
};

/**
 * Mounts the scene over the static SVG in `container` and runs it until the hero leaves the screen, the tab hides, reduced motion is turned on, the frame rate cannot be held, or the WebGL context is lost. Returns a function that tears it down.
 */
export const mountHeroScene = (container: HTMLElement, reducedMotion: MediaQueryList): (() => void) => {
  const fallback = container.querySelector<SVGElement>("[data-hero-fallback]");
  const canvas = document.createElement("canvas");
  canvas.className = "hero-canvas absolute inset-0 h-full w-full";
  canvas.setAttribute("aria-hidden", "true");

  const renderer = new WebGLRenderer({ canvas, alpha: true, antialias: false, powerPreference: "low-power" });
  renderer.setClearColor(0x000000, 0);
  // three.js logs a shader the driver rejects and carries on drawing nothing; record it instead, so the SVG stays.
  let shaderFailed = false;
  renderer.debug.onShaderError = () => {
    shaderFailed = true;
  };
  const scene = new Scene();
  const camera = new OrthographicCamera(
    -DESIGN_WIDTH / 2,
    DESIGN_WIDTH / 2,
    DESIGN_HEIGHT / 2,
    -DESIGN_HEIGHT / 2,
    -1000,
    1000
  );

  const pointerUniform: [number, number] = [0, 0];
  const uniforms = { uTime: { value: 0 }, uPointer: { value: pointerUniform }, uTilt: { value: MAX_TILT } };
  const material = (vertexShader: string) =>
    new ShaderMaterial({
      uniforms,
      vertexShader,
      fragmentShader: FRAGMENT,
      transparent: true,
      depthTest: false,
      depthWrite: false,
    });

  const { geometry: chips, count: chipCount } = chipGeometry();
  const chipMaterial = material(CHIP_VERTEX);
  const chipMesh = new InstancedMesh(chips, chipMaterial, chipCount);
  const dots = dotGeometry();
  const dotMaterial = material(DOT_VERTEX);
  const dotMesh = new InstancedMesh(dots, dotMaterial, DOTS.length);
  for (const mesh of [chipMesh, dotMesh]) {
    mesh.frustumCulled = false;
    scene.add(mesh);
  }
  dotMesh.renderOrder = 1;

  const resize = (): void => {
    const width = Math.min(container.clientWidth, MAX_CSS_WIDTH);
    const height = Math.min(container.clientHeight, MAX_CSS_HEIGHT);
    renderer.setPixelRatio(Math.min(window.devicePixelRatio, MAX_DPR));
    renderer.setSize(width, height, false);
  };
  resize();

  // The pointer, in -1..1 of the hero box, followed with a lag; fine pointers only.
  const pointer = { x: 0, y: 0, targetX: 0, targetY: 0 };
  const finePointer = matchMedia("(pointer: fine)").matches;
  const onPointerMove = (event: PointerEvent): void => {
    const box = container.getBoundingClientRect();
    pointer.targetX = Math.max(-1, Math.min(1, ((event.clientX - box.left) / box.width) * 2 - 1));
    pointer.targetY = Math.max(-1, Math.min(1, ((event.clientY - box.top) / box.height) * 2 - 1));
  };
  const onPointerLeave = (): void => {
    pointer.targetX = 0;
    pointer.targetY = 0;
  };
  if (finePointer) {
    window.addEventListener("pointermove", onPointerMove, { passive: true });
    document.documentElement.addEventListener("pointerleave", onPointerLeave);
  }

  // The clock only advances while frames are drawn, so a pause resumes where it stopped instead of jumping.
  let elapsed = START_TIME;
  let last = 0;
  let frame = 0;
  let running = false;
  let visible = true;
  let frozen = false;
  let disposed = false;
  let shown = false;
  // Frame-rate watch: after a second of warm-up, a two-second window averaging under 30 fps freezes the scene.
  let warmUp = 0;
  let watchStart = 0;
  let watchFrames = 0;

  const draw = (): void => {
    uniforms.uTime.value = elapsed;
    pointerUniform[0] = pointer.x;
    pointerUniform[1] = pointer.y;
    renderer.render(scene, camera);
    if (!shown) {
      shown = true;
      container.append(canvas);
      requestAnimationFrame(() => {
        canvas.setAttribute("data-ready", "");
        window.setTimeout(() => {
          if (!disposed && fallback) fallback.style.visibility = "hidden";
        }, 300);
      });
    }
  };

  const tick = (now: number): void => {
    if (!running) return;
    const dt = last === 0 ? 0 : Math.min((now - last) / 1000, 0.1);
    last = now;
    elapsed += dt;
    warmUp += dt;
    pointer.x += (pointer.targetX - pointer.x) * 0.08;
    pointer.y += (pointer.targetY - pointer.y) * 0.08;
    draw();

    if (warmUp > 1) {
      if (watchStart === 0) watchStart = now;
      watchFrames += 1;
      if (now - watchStart >= 2000) {
        if ((watchFrames * 1000) / (now - watchStart) < 30) {
          freeze();
          return;
        }
        watchStart = now;
        watchFrames = 0;
      }
    }
    frame = requestAnimationFrame(tick);
  };

  const start = (): void => {
    if (running || frozen || disposed || !visible || document.hidden) return;
    running = true;
    last = 0;
    watchStart = 0;
    watchFrames = 0;
    frame = requestAnimationFrame(tick);
  };

  const stop = (): void => {
    running = false;
    cancelAnimationFrame(frame);
  };

  function freeze(): void {
    stop();
    frozen = true;
    elapsed = RESTING_TIME;
    pointer.x = pointer.y = pointer.targetX = pointer.targetY = 0;
    draw();
  }

  // setSize resets the canvas's drawing buffer, which clears it. A running scene draws again on its next frame, but a frozen or paused one is drawn here, or the hero stays blank over the SVG it has already hidden.
  const resizeObserver = new ResizeObserver(() => {
    resize();
    if (shown && !running && !disposed) draw();
  });
  resizeObserver.observe(container);

  const intersection = new IntersectionObserver(([entry]) => {
    visible = entry?.isIntersecting ?? false;
    if (visible) start();
    else stop();
  });
  intersection.observe(container);

  const onVisibility = (): void => {
    if (document.hidden) stop();
    else start();
  };
  document.addEventListener("visibilitychange", onVisibility);

  const dispose = (): void => {
    if (disposed) return;
    disposed = true;
    stop();
    intersection.disconnect();
    resizeObserver.disconnect();
    document.removeEventListener("visibilitychange", onVisibility);
    reducedMotion.removeEventListener("change", onReducedMotion);
    window.removeEventListener("pointermove", onPointerMove);
    document.documentElement.removeEventListener("pointerleave", onPointerLeave);
    canvas.removeEventListener("webglcontextlost", onContextLost);
    chips.dispose();
    dots.dispose();
    chipMaterial.dispose();
    dotMaterial.dispose();
    renderer.dispose();
    canvas.remove();
    if (fallback) fallback.style.visibility = "";
  };

  function onReducedMotion(query: MediaQueryListEvent): void {
    if (query.matches) dispose();
  }
  function onContextLost(event: Event): void {
    event.preventDefault();
    dispose();
  }
  reducedMotion.addEventListener("change", onReducedMotion);
  canvas.addEventListener("webglcontextlost", onContextLost);

  try {
    // Compile both programs now, so a shader the driver rejects throws here and leaves the SVG up.
    renderer.compile(scene, camera);
    if (shaderFailed) throw new Error("hero scene: shader compilation failed");
    start();
  } catch (error) {
    dispose();
    throw error;
  }
  return dispose;
};
