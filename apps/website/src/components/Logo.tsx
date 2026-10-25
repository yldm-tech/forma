// The Forma mark and wordmark, the brand's vector sources (tooling/brand/symbol.svg, lockup.svg) converted to JSX with their path data unchanged: a rounded card of three single-choice options with the middle one selected in crimson, and "forma" drawn as strokes whose o holds the same crimson dot. The site has no dark surface, so only the ink variant is drawn here; apps/web/src/components/ui/logo.tsx has the white one.
const INK = "#0F172A";
const CRIMSON = "#D3264A";

/** The symbol on its 48-unit grid. */
const SymbolPaths = () => (
  <>
    <rect x="5.75" y="5.75" width="36.5" height="36.5" rx="10.5" fill="none" stroke={INK} strokeWidth="4.2" />
    <circle cx="16.1" cy="15.5" r="2.7" fill="none" stroke={INK} strokeWidth="3" />
    <path d="M22.6 15.5H31.6" stroke={INK} strokeWidth="4.2" strokeLinecap="round" />
    <circle cx="16.1" cy="24" r="4.1" fill={CRIMSON} />
    <path d="M22.6 24H34.8" stroke={CRIMSON} strokeWidth="4.6000000000000005" strokeLinecap="round" />
    <circle cx="16.1" cy="32.5" r="2.7" fill="none" stroke={INK} strokeWidth="3" />
    <path d="M22.6 32.5H28.2" stroke={INK} strokeWidth="4.2" strokeLinecap="round" />
  </>
);

const stroke = {
  fill: "none",
  stroke: INK,
  strokeWidth: "6.4",
  strokeLinecap: "round",
  strokeLinejoin: "round",
} as const;

/** "forma", drawn: wordmark.svg's paths, placed beside the symbol as lockup.svg places them. */
const WordPaths = () => (
  <g transform="translate(60,1)">
    <path d="M8.5 40V15.5a8 8 0 0 1 8-8h3.5" {...stroke} />
    <path d="M2.5 18.5h13.5" {...stroke} />
    <circle cx="35.5" cy="28.3" r="10.6" fill="none" stroke={INK} strokeWidth="6.4" />
    <circle cx="35.5" cy="28.3" r="3.9" fill={CRIMSON} />
    <path d="M56.5 40V18.5" {...stroke} />
    <path d="M56.5 27.5a9 9 0 0 1 9-9h2.5" {...stroke} />
    <path d="M78 40V18.5" {...stroke} />
    <path d="M78 27a8.5 8.5 0 0 1 17 0v13" {...stroke} />
    <path d="M95 27a8.5 8.5 0 0 1 17 0v13" {...stroke} />
    <circle cx="131.5" cy="28.3" r="10.6" fill="none" stroke={INK} strokeWidth="6.4" />
    <path d="M142.1 18.5V40" {...stroke} />
  </g>
);

/** The symbol and the wordmark (lockup.svg), `height` pixels tall. Decorative: the link around it carries the name. */
export const Logo = ({ height = 32 }: { height?: number }) => (
  <svg
    width={Math.round((height * 212) / 48)}
    height={height}
    viewBox="0 0 212 48"
    aria-hidden="true"
    focusable="false">
    <SymbolPaths />
    <WordPaths />
  </svg>
);
