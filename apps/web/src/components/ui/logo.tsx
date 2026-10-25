import { cn } from "../../lib/cn";

const INK = "#0F172A";
const WHITE = "#FFFFFF";
const CRIMSON = "#D3264A";

/** On a light surface the mark is ink; on a dark one, white. The selected option and the wordmark's dot stay crimson in both. */
type Variant = "default" | "white";

/** The symbol's paths on its 48-unit grid (tooling/brand/symbol.svg, symbol-white.svg): a rounded card of three single-choice options, the middle one selected. */
const SymbolPaths = ({ ink }: { ink: string }) => (
  <>
    <rect x="5.75" y="5.75" width="36.5" height="36.5" rx="10.5" fill="none" stroke={ink} strokeWidth="4.2" />
    <circle cx="16.1" cy="15.5" r="2.7" fill="none" stroke={ink} strokeWidth="3" />
    <path d="M22.6 15.5H31.6" stroke={ink} strokeWidth="4.2" strokeLinecap="round" />
    <circle cx="16.1" cy="24" r="4.1" fill={CRIMSON} />
    <path d="M22.6 24H34.8" stroke={CRIMSON} strokeWidth="4.6000000000000005" strokeLinecap="round" />
    <circle cx="16.1" cy="32.5" r="2.7" fill="none" stroke={ink} strokeWidth="3" />
    <path d="M22.6 32.5H28.2" stroke={ink} strokeWidth="4.2" strokeLinecap="round" />
  </>
);

/** "forma" drawn as strokes (tooling/brand/wordmark.svg), its o holding the crimson dot, placed beside the symbol as tooling/brand/lockup.svg places it. */
const WordPaths = ({ ink }: { ink: string }) => {
  const stroke = {
    fill: "none",
    stroke: ink,
    strokeWidth: "6.4",
    strokeLinecap: "round",
    strokeLinejoin: "round",
  } as const;
  return (
    <g transform="translate(60,1)">
      <path d="M8.5 40V15.5a8 8 0 0 1 8-8h3.5" {...stroke} />
      <path d="M2.5 18.5h13.5" {...stroke} />
      <circle cx="35.5" cy="28.3" r="10.6" fill="none" stroke={ink} strokeWidth="6.4" />
      <circle cx="35.5" cy="28.3" r="3.9" fill={CRIMSON} />
      <path d="M56.5 40V18.5" {...stroke} />
      <path d="M56.5 27.5a9 9 0 0 1 9-9h2.5" {...stroke} />
      <path d="M78 40V18.5" {...stroke} />
      <path d="M78 27a8.5 8.5 0 0 1 17 0v13" {...stroke} />
      <path d="M95 27a8.5 8.5 0 0 1 17 0v13" {...stroke} />
      <circle cx="131.5" cy="28.3" r="10.6" fill="none" stroke={ink} strokeWidth="6.4" />
      <path d="M142.1 18.5V40" {...stroke} />
    </g>
  );
};

/** The product mark, decorative. The square viewBox is kept so every caller's sizing stays the same; `white` is for a dark surface. */
export const FormaLogo = ({ className, variant = "default" }: { className?: string; variant?: Variant }) => (
  <svg
    viewBox="0 0 48 48"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    className={className}
    aria-hidden="true">
    <SymbolPaths ink={variant === "white" ? WHITE : INK} />
  </svg>
);

/** The mark and the drawn wordmark, sized by the font size: the symbol is 1.15em tall, as the mark beside the typeset name was. It is the name to assistive technology, so a link around it is named "Forma". */
export const Wordmark = ({ className, variant = "default" }: { className?: string; variant?: Variant }) => {
  const ink = variant === "white" ? WHITE : INK;
  return (
    <span className={cn("inline-flex items-center", className)}>
      <svg
        viewBox="0 0 212 48"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        role="img"
        aria-label="Forma"
        className="h-[1.15em] w-auto shrink-0">
        <SymbolPaths ink={ink} />
        <WordPaths ink={ink} />
      </svg>
    </span>
  );
};
