// The tile mark from apps/web/public/logo-transparent.png, drawn on the 48-unit grid of apps/web/public/favicon/safari-pinned-tab.svg: a teal tile, a long mint bar, a short white bar and a mint dot. "Forma" is set in type next to it.
export const LogoMark = ({ size = 32 }: { size?: number }) => (
  <svg width={size} height={size} viewBox="4 2 40 44" aria-hidden="true" focusable="false">
    <rect x="6" y="4" width="36" height="40" rx="10" fill="#038178" />
    <rect x="14" y="17" width="23" height="5" rx="2.5" fill="#00E6CA" />
    <rect x="14" y="27" width="15" height="5" rx="2.5" fill="#FFFFFF" />
    <circle cx="34" cy="29.5" r="2.5" fill="#00E6CA" />
  </svg>
);
