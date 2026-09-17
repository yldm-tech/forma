interface FormaLogoProps {
  className?: string;
}

/**
 * The product mark: a card with a filled field, a shorter one and a chosen option.
 *
 * It replaces the inherited mark, which was the upstream product's letter F. This one is drawn for
 * this product instead — the shape says what the thing does, which is also what its name means, and
 * it shares no geometry with what it replaced.
 *
 * The card is `--forma-brand` (#038178) rather than the lighter brand token, because the mark has to
 * hold on white: `--color-brand-dark` measures 2.19:1 there, which `globals.css` already calls out as
 * under the 3:1 floor for non-text. The bright token stays as the accent on top of the card, where it
 * has the dark fill behind it. The same reason makes the mark legible on a dark surface, so one
 * drawing serves both themes and the monochrome Safari pinned-tab silhouette in `public/favicon/`.
 */
export const FormaLogo = ({ className }: Readonly<FormaLogoProps>) => (
  <svg
    viewBox="0 0 48 48"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    className={className}
    role="presentation">
    <rect x="6" y="4" width="36" height="40" rx="10" fill="#038178" />
    <rect x="14" y="17" width="20" height="5" rx="2.5" fill="#00E6CA" />
    <rect x="14" y="27" width="12" height="5" rx="2.5" fill="#FFFFFF" />
    <rect x="29" y="27" width="5" height="5" rx="2.5" fill="#00E6CA" />
  </svg>
);
