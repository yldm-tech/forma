// The hero scene's geometry, shared by the static SVG every visitor gets (components/HeroArt.tsx) and the WebGL scene that replaces it on capable devices (client/hero-scene.ts), so the canvas fades in over a picture of its own resting frame.
//
// Units are a 480 × 360 design space centred on the origin with y pointing up, which is also the orthographic camera's frustum. A rectangle is given by its centre, size and corner radius.

export const DESIGN_WIDTH = 480;
export const DESIGN_HEIGHT = 360;

export interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
  r: number;
}

export const COLORS = {
  crimson: "#d3264a",
  ink: "#0f172a",
  sunken: "#f1f5f9",
  white: "#ffffff",
  crimson100: "#fae5e9",
  edge: "#e2e8f0",
} as const;
export type ColorName = keyof typeof COLORS;

/** How a chip behaves beyond moving between its three states. */
export const KIND = {
  plain: 0,
  /** Turns crimson in the Answer phase. */
  answer: 1,
  /** The text field's caret: sweeps right in the Answer phase. */
  caret: 2,
  /** The submit pill: pulses once in the Answer phase. */
  pulse: 3,
  /** A cloud chip that is absorbed into the card: zero size in the card and logo states. */
  vanish: 4,
  /** The typed text in the field: grows from the left with the caret. */
  typed: 5,
} as const;

const CARD: Rect = { x: 0, y: 0, w: 340, h: 260, r: 16 };

/** A rectangle placed by its top-left corner inside the card, in the card's own y-down coordinates. */
const inCard = (left: number, top: number, w: number, h: number, r: number): Rect => ({
  x: -CARD.w / 2 + left + w / 2,
  y: CARD.h / 2 - top - h / 2,
  w,
  h,
  r,
});

// The logo symbol (tooling/brand/symbol.svg, a 48-unit grid) at three times its size, centred at (-90, 0): the card condenses into it in the Understand phase. Every part is a filled rounded rectangle, so the strokes are drawn as fills: the frame is an ink square with a white one inside it, each unselected option's ring an ink disc with a white one inside it, and each line a pill as long as the stroke with its round caps.
const LOGO_SCALE = 3;
const LOGO_CENTER = { x: -90, y: 0 };
const inLogo = (cx: number, cy: number, w: number, h: number, r: number): Rect => ({
  x: LOGO_CENTER.x + (cx - 24) * LOGO_SCALE,
  y: LOGO_CENTER.y + (24 - cy) * LOGO_SCALE,
  w: w * LOGO_SCALE,
  h: h * LOGO_SCALE,
  r: r * LOGO_SCALE,
});
/** symbol.svg's stroke widths and ring radius: a stroke is centred on its path, so its outer edge is half a stroke outside it and its inner edge half a stroke inside. */
const FRAME_STROKE = 4.2;
const RING_STROKE = 3;
const RING_RADIUS = 2.7;
const disc = (cx: number, cy: number, r: number): Rect => inLogo(cx, cy, r * 2, r * 2, r);
/** A round-capped line from x1 to x2 at y, `width` thick. */
const line = (x1: number, x2: number, y: number, width: number): Rect =>
  inLogo((x1 + x2) / 2, y, x2 - x1 + width, width, width / 2);
export const LOGO = {
  frame: inLogo(24, 24, 36.5 + FRAME_STROKE, 36.5 + FRAME_STROKE, 10.5 + FRAME_STROKE / 2),
  frameInside: inLogo(24, 24, 36.5 - FRAME_STROKE, 36.5 - FRAME_STROKE, 10.5 - FRAME_STROKE / 2),
  topRing: disc(16.1, 15.5, RING_RADIUS + RING_STROKE / 2),
  topRingInside: disc(16.1, 15.5, RING_RADIUS - RING_STROKE / 2),
  topLine: line(22.6, 31.6, 15.5, 4.2),
  dot: disc(16.1, 24, 4.1),
  selectedLine: line(22.6, 34.8, 24, 4.6),
  bottomRing: disc(16.1, 32.5, RING_RADIUS + RING_STROKE / 2),
  bottomRingInside: disc(16.1, 32.5, RING_RADIUS - RING_STROKE / 2),
  bottomLine: line(22.6, 28.2, 32.5, 4.2),
};

const HIDDEN_IN_LOGO: Rect = { x: LOGO_CENTER.x, y: LOGO_CENTER.y, w: 0, h: 0, r: 0 };

export interface Chip {
  /** Card state (Assemble and Answer). */
  card: Rect;
  /** Logo state (Understand). */
  logo: Rect;
  color: ColorName;
  /** Colour in the logo state. */
  logoColor: ColorName;
  /** Drawn with a 1px edge in the card state. */
  edge: boolean;
  kind: number;
}

export const RATING_COUNT = 5;
export const NPS_COUNT = 11;
const ANSWERED_RATING = 3;
const ANSWERED_NPS = 8;
const TYPED_WIDTH = 150;

const field = inCard(24, 146, 292, 30, 8);

/** Where each rating chip goes in the logo: the two unselected rings and the shortest line; the answered one and the last one vanish. */
const RATING_IN_LOGO: (Rect | undefined)[] = [LOGO.topRing, LOGO.bottomRing, LOGO.bottomLine];
/** Where each NPS chip goes in the logo: the white insides of the two rings, drawn over the ink discs the rating chips become, and the answered one becomes the selected dot. */
const NPS_IN_LOGO: (Rect | undefined)[] = [LOGO.topRingInside, LOGO.bottomRingInside];

/** The chips that make up the survey card, in drawing order (later ones on top). The text field comes straight after the card: it overlaps nothing but its own typed text and caret in the card, and in the logo it is the frame's white inside, which everything else is drawn over. */
export const CARD_CHIPS: Chip[] = [
  { card: CARD, logo: LOGO.frame, color: "white", logoColor: "ink", edge: true, kind: KIND.plain },
  { card: field, logo: LOGO.frameInside, color: "white", logoColor: "white", edge: true, kind: KIND.plain },
  {
    card: inCard(24, 24, 180, 14, 7),
    logo: LOGO.selectedLine,
    color: "ink",
    logoColor: "crimson",
    edge: false,
    kind: KIND.plain,
  },
  {
    card: inCard(24, 46, 120, 10, 5),
    logo: LOGO.topLine,
    color: "sunken",
    logoColor: "ink",
    edge: false,
    kind: KIND.plain,
  },
  ...Array.from({ length: RATING_COUNT }, (_, i): Chip => ({
    card: inCard(24 + i * 30, 72, 22, 22, 6),
    logo: RATING_IN_LOGO[i] ?? HIDDEN_IN_LOGO,
    color: "sunken",
    logoColor: "ink",
    edge: true,
    kind: i === ANSWERED_RATING ? KIND.answer : KIND.plain,
  })),
  ...Array.from({ length: NPS_COUNT }, (_, i): Chip => ({
    card: inCard(24 + i * 27, 110, 22, 18, 5),
    logo: i === ANSWERED_NPS ? LOGO.dot : (NPS_IN_LOGO[i] ?? HIDDEN_IN_LOGO),
    color: "sunken",
    logoColor: i === ANSWERED_NPS ? "crimson" : "white",
    edge: true,
    kind: i === ANSWERED_NPS ? KIND.answer : KIND.plain,
  })),
  {
    card: { x: field.x - field.w / 2 + 10 + TYPED_WIDTH / 2, y: field.y, w: TYPED_WIDTH, h: 8, r: 4 },
    logo: HIDDEN_IN_LOGO,
    color: "crimson100",
    logoColor: "crimson100",
    edge: false,
    kind: KIND.typed,
  },
  {
    card: { x: field.x - field.w / 2 + 10, y: field.y, w: 2, h: 16, r: 1 },
    logo: HIDDEN_IN_LOGO,
    color: "ink",
    logoColor: "ink",
    edge: false,
    kind: KIND.caret,
  },
  {
    card: inCard(220, 198, 96, 30, 15),
    logo: HIDDEN_IN_LOGO,
    color: "crimson",
    logoColor: "crimson",
    edge: false,
    kind: KIND.pulse,
  },
];

/** How far the caret travels, and so how wide the typed text grows, in the Answer phase. */
export const TYPED_SWEEP = TYPED_WIDTH;

/** The card as it rests after the Answer phase: the frame the static SVG shows and the scene freezes on. */
export const answeredCard = (): { rect: Rect; color: ColorName; edge: boolean }[] =>
  CARD_CHIPS.map((chip) => {
    if (chip.kind === KIND.caret) {
      return { rect: { ...chip.card, x: chip.card.x + TYPED_SWEEP }, color: chip.color, edge: chip.edge };
    }
    return { rect: chip.card, color: chip.kind === KIND.answer ? "crimson" : chip.color, edge: chip.edge };
  });

// The five rising bars the dots stream into, to the right of the logo: two dots wide, no numbers.
export const BAR_ROWS = [5, 8, 6, 10, 11] as const;
export const DOT_SIZE = 9;
const DOT_PITCH = 11;
const BAR_BASE_Y = -60 + DOT_SIZE / 2;

export interface Dot {
  x: number;
  y: number;
  color: ColorName;
  bar: number;
  row: number;
}

export const DOTS: Dot[] = BAR_ROWS.flatMap((rows, bar) =>
  Array.from({ length: rows * 2 }, (_, i): Dot => {
    const row = Math.floor(i / 2);
    const column = i % 2;
    return {
      x: 30 + bar * 36 + (column === 0 ? -DOT_PITCH / 2 : DOT_PITCH / 2),
      y: BAR_BASE_Y + row * DOT_PITCH,
      color: bar === BAR_ROWS.length - 1 ? "crimson" : "ink",
      bar,
      row,
    };
  })
);
