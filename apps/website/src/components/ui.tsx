import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { APP_LOGIN_PATH, SIGNUP_OPEN, primaryCtaHref } from "../site";
import { Icon, type IconName } from "./icons";
import { useSite } from "./site-context";

/** Text with `inline code` in backticks. */
export const Rich = ({ text }: { text: string }) => (
  <>
    {text.split(/(`[^`]+`)/).map((part, i) =>
      part.startsWith("`") && part.endsWith("`") && part.length > 1 ? (
        <code key={i} className="rounded-chip bg-surface-sunken text-ink-900 px-1.5 py-0.5">
          {part.slice(1, -1)}
        </code>
      ) : (
        part
      )
    )}
  </>
);

/** A link to a page of this site (the router's <Link>, which marks the current page with aria-current) or anywhere else (a plain anchor). */
export const SmartLink = ({
  href,
  className,
  children,
  ...rest
}: {
  href: string;
  className?: string;
  children: ReactNode;
  "aria-label"?: string;
  lang?: string;
  hrefLang?: string;
}) => {
  const internal = href.startsWith("/") && !href.startsWith("/app");
  if (internal) {
    const [path, hash] = href.split("#");
    return (
      <Link
        to={path}
        hash={hash}
        activeOptions={{ exact: true, includeHash: false }}
        className={className}
        {...rest}>
        {children}
      </Link>
    );
  }
  return (
    <a href={href} className={className} {...rest}>
      {children}
    </a>
  );
};

export const PrimaryCta = ({ className = "" }: { className?: string }) => {
  const { locale, content } = useSite();
  return (
    <SmartLink href={primaryCtaHref(locale)} className={`btn btn-primary ${className}`}>
      {SIGNUP_OPEN ? content.common.cta.primary.open : content.common.cta.primary.closed}
    </SmartLink>
  );
};

export const SignInCta = ({ className = "" }: { className?: string }) => {
  const { content } = useSite();
  return (
    <a href={APP_LOGIN_PATH} className={`btn btn-secondary ${className}`}>
      {content.common.cta.signIn}
    </a>
  );
};

export const BetaBadge = () => {
  const { content } = useSite();
  return (
    <span className="rounded-chip bg-beta-bg text-beta-fg ring-beta-fg/20 inline-flex items-center px-2 py-0.5 text-xs leading-5 font-semibold ring-1">
      {content.common.beta}
    </span>
  );
};

export const IconTile = ({ icon }: { icon: IconName }) => (
  <span className="icon-tile">
    <Icon name={icon} size={22} />
  </span>
);

export const SectionHeading = ({ id, title, lead }: { id?: string; title: string; lead?: string }) => (
  <div className="measure">
    <h2 id={id} className="text-h2">
      {title}
    </h2>
    {lead ? (
      <p className="text-ink-700 mt-4">
        <Rich text={lead} />
      </p>
    ) : null}
  </div>
);

export const FeatureCard = ({
  icon,
  title,
  body,
  href,
  linkLabel,
  badge,
  heading: Heading = "h3",
}: {
  icon: IconName;
  title: string;
  body: string;
  href?: string;
  linkLabel?: string;
  badge?: ReactNode;
  /** h2 where the cards are the first level below the page's h1, as on a page with no section heading above them. */
  heading?: "h2" | "h3";
}) => (
  <div data-reveal className="card relative flex flex-col gap-4 p-6">
    <IconTile icon={icon} />
    <Heading className="text-h3 flex flex-wrap items-center gap-2">
      {href && !linkLabel ? (
        <SmartLink href={href} className="after:absolute after:inset-0 hover:text-teal-700">
          {title}
        </SmartLink>
      ) : (
        title
      )}
      {badge}
    </Heading>
    <p className="text-ink-700">
      <Rich text={body} />
    </p>
    {href && linkLabel ? (
      <p className="mt-auto">
        <SmartLink href={href} className="link">
          {linkLabel}
        </SmartLink>
      </p>
    ) : null}
  </div>
);

export const CheckList = ({ items }: { items: readonly string[] }) => (
  <ul className="flex flex-col gap-3">
    {items.map((item) => (
      <li key={item} className="flex gap-3">
        <span className="bg-mint-100 mt-1 inline-flex size-5 flex-none items-center justify-center rounded-full text-teal-900">
          <Icon name="check" size={14} />
        </span>
        <span className="text-ink-700">
          <Rich text={item} />
        </span>
      </li>
    ))}
  </ul>
);

/** Copy beside a visual; `flip` puts the visual first on wide screens. */
export const SplitSection = ({
  id,
  title,
  body,
  visual,
  flip = false,
  muted = false,
  children,
}: {
  id?: string;
  title: string;
  body: string;
  visual: ReactNode;
  flip?: boolean;
  muted?: boolean;
  children?: ReactNode;
}) => (
  <section aria-labelledby={id} className={`section-pad ${muted ? "bg-surface-muted" : ""}`}>
    <div className="container-site grid items-center gap-12 lg:grid-cols-2 lg:gap-16">
      <div data-reveal className={`min-w-0 ${flip ? "lg:order-2" : ""}`}>
        <SectionHeading id={id} title={title} lead={body} />
        {children ? <div className="mt-8">{children}</div> : null}
      </div>
      <div data-reveal className={`min-w-0 ${flip ? "lg:order-1" : ""}`}>
        {visual}
      </div>
    </div>
  </section>
);

export const CodeBlock = ({ code, label }: { code: string; label: string }) => {
  const { content } = useSite();
  return (
    <figure data-code-block className="rounded-card border-line bg-surface-sunken overflow-hidden border">
      <figcaption className="border-line text-small text-ink-700 flex items-center justify-between border-b px-4 py-2">
        <span>{label}</span>
        <button
          type="button"
          data-copy
          data-copied-label={content.common.copy.done}
          className="rounded-chip text-ink-700 inline-flex min-h-9 items-center gap-1.5 px-2 font-medium hover:bg-white"
          hidden>
          <Icon name="copy" size={16} />
          <span>{content.common.copy.button}</span>
        </button>
        <span data-copy-status role="status" aria-live="polite" className="sr-only" />
      </figcaption>
      <pre className="text-ink-900 overflow-x-auto p-4 text-[13px] leading-6">
        <code>{code}</code>
      </pre>
    </figure>
  );
};

export const CtaBand = () => {
  const { content } = useSite();
  const band = content.common.ctaBand;
  return (
    <section aria-labelledby="cta-band" className="section-pad">
      <div className="container-site">
        <div
          data-reveal
          className="rounded-panel bg-mint-50 flex flex-col items-start gap-6 px-6 py-12 sm:px-12 lg:flex-row lg:items-center lg:justify-between">
          <div className="measure">
            <h2 id="cta-band" className="text-h2 text-teal-900">
              {band.title}
            </h2>
            <p className="text-ink-700 mt-3">{SIGNUP_OPEN ? band.body.open : band.body.closed}</p>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <PrimaryCta />
            <SignInCta />
          </div>
        </div>
      </div>
    </section>
  );
};

/** The opening of every page but the home page: H1, sub, optional actions and a visual. */
export const PageHero = ({
  title,
  sub,
  visual,
  actions,
}: {
  title: string;
  sub: string;
  visual?: ReactNode;
  actions?: ReactNode;
}) => (
  <section className="relative overflow-hidden">
    {/* The glow sits behind the visual; without one it would light an empty half of the hero. */}
    {visual ? (
      <div
        className="hero-glow pointer-events-none absolute -top-40 right-0 h-[560px] w-[760px]"
        aria-hidden="true"
      />
    ) : null}
    <div
      className={`container-site relative grid items-center gap-12 pt-12 pb-16 lg:pt-20 lg:pb-24 ${visual ? "lg:grid-cols-2" : ""}`}>
      <div className="min-w-0">
        <h1 className="text-display measure">{title}</h1>
        <p className="measure text-ink-700 mt-6 text-lg leading-8">{sub}</p>
        {actions ? <div className="mt-8 flex flex-wrap items-center gap-3">{actions}</div> : null}
      </div>
      {visual ? <div className="min-w-0">{visual}</div> : null}
    </div>
  </section>
);
