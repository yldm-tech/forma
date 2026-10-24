import type { AnchorHTMLAttributes } from "react";

/** A link for a react-i18next Trans `components` slot: Trans fills in the children from the translated message, so the element is written without any. */
export const TransLink = ({ children, ...props }: AnchorHTMLAttributes<HTMLAnchorElement>) => (
  <a {...props}>{children}</a>
);
