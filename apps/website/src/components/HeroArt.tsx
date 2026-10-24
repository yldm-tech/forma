import { COLORS, DESIGN_HEIGHT, DESIGN_WIDTH, answeredCard } from "../hero/layout";

// The hero's visual: a fixed 4:3 box holding an SVG of the answered survey card, which is all that a visitor without WebGL, with reduced motion or on a small screen ever sees. client/main.ts may later fade the three.js scene in over it (client/hero-scene.ts), drawn from the same layout, and hides the SVG once the canvas has rendered its first frame.
export const HeroArt = () => (
  <div data-hero className="relative mx-auto aspect-[4/3] w-full max-w-xl" aria-hidden="true">
    <div className="hero-glow pointer-events-none absolute -inset-12" />
    <svg
      data-hero-fallback
      className="absolute inset-0 h-full w-full"
      viewBox={`${-DESIGN_WIDTH / 2} ${-DESIGN_HEIGHT / 2} ${DESIGN_WIDTH} ${DESIGN_HEIGHT}`}
      focusable="false">
      {answeredCard().map(({ rect, color, edge }, i) => (
        <rect
          key={i}
          x={rect.x - rect.w / 2}
          y={-rect.y - rect.h / 2}
          width={rect.w}
          height={rect.h}
          rx={Math.min(rect.r, rect.w / 2, rect.h / 2)}
          fill={COLORS[color]}
          stroke={edge ? COLORS.edge : undefined}
          strokeWidth={edge ? 1 : undefined}
        />
      ))}
    </svg>
  </div>
);
