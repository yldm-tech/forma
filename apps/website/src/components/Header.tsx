import { APP_LOGIN_PATH, GITHUB_URL, HTML_LANG, LOCALES, LOCALE_NAME, localePrefix, pagePath } from "../site";
import { Icon, type IconName } from "./icons";
import { LogoMark } from "./Logo";
import { useSite } from "./site-context";
import { PrimaryCta, SmartLink } from "./ui";

// Every control here works as a plain link without JavaScript except the three toggles (Product, language, mobile menu), which client/main.ts wires up; their destinations are all in the footer as well.

const useProductLinks = (): { href: string; label: string; icon: IconName }[] => {
  const { locale, content } = useSite();
  const nav = content.common.nav;
  return [
    { href: pagePath(locale, "formBuilder"), label: nav.linkSurveys, icon: "link" },
    { href: pagePath(locale, "websiteSurvey"), label: nav.websiteSurveys, icon: "monitor" },
    { href: pagePath(locale, "inAppSurvey"), label: nav.inAppSurveys, icon: "smartphone" },
  ];
};

const useTopLinks = (): { href: string; label: string }[] => {
  const { locale, content } = useSite();
  const nav = content.common.nav;
  return [
    { href: pagePath(locale, "integrations"), label: nav.integrations },
    { href: pagePath(locale, "developers"), label: nav.developers },
    { href: pagePath(locale, "selfHosting"), label: nav.selfHosting },
  ];
};

/** The same page in every locale, each label in its own language. On the 404 page, each locale's home. */
export const LanguageLinks = ({
  className,
  itemClassName,
}: {
  className?: string;
  itemClassName?: string;
}) => {
  const { locale: current, page } = useSite();
  return (
    <ul className={className}>
      {LOCALES.map((locale) => (
        <li key={locale}>
          <a
            href={page ? pagePath(locale, page) : localePrefix(locale)}
            lang={HTML_LANG[locale]}
            hrefLang={HTML_LANG[locale]}
            aria-current={locale === current ? "true" : undefined}
            className={itemClassName}>
            {LOCALE_NAME[locale]}
          </a>
        </li>
      ))}
    </ul>
  );
};

const LanguageSwitcher = () => {
  const { locale, content } = useSite();
  return (
    <div className="relative" data-disclosure-root>
      <button
        type="button"
        data-disclosure
        aria-expanded="false"
        aria-controls="language-menu"
        className="btn btn-ghost min-h-10 px-3 font-medium">
        <Icon name="globe" size={18} />
        <span className="sr-only">{content.common.nav.language}: </span>
        <span>{LOCALE_NAME[locale]}</span>
        <Icon name="chevronDown" size={16} />
      </button>
      <div id="language-menu" hidden className="card shadow-raised absolute right-0 mt-2 w-44 p-2">
        <LanguageLinks
          className="flex flex-col"
          itemClassName="block rounded-chip px-3 py-2 hover:bg-surface-sunken aria-[current]:font-semibold aria-[current]:text-teal-700"
        />
      </div>
    </div>
  );
};

export const SiteHeader = () => {
  const { locale, content } = useSite();
  const nav = content.common.nav;
  const product = useProductLinks();
  const top = useTopLinks();
  const navLink =
    "rounded-button px-3 py-2 font-medium text-ink-700 hover:bg-surface-sunken hover:text-ink-900 aria-[current=page]:text-teal-700";

  return (
    <>
      <header
        data-site-header
        className="site-header sticky top-0 z-40 border-b border-transparent bg-white/80 backdrop-blur-md transition-colors">
        <div className="container-site flex h-16 items-center gap-4 lg:gap-8">
          <SmartLink
            href={localePrefix(locale)}
            aria-label={content.common.homeLabel}
            className="flex items-center gap-2">
            <LogoMark size={30} />
            <span className="text-xl font-bold tracking-tight">Forma</span>
          </SmartLink>

          <nav aria-label={nav.label} className="hidden lg:block">
            <ul className="flex items-center gap-1 text-[15px]">
              <li className="relative" data-disclosure-root>
                <button
                  type="button"
                  data-disclosure
                  aria-expanded="false"
                  aria-controls="product-menu"
                  className={`${navLink} inline-flex items-center gap-1`}>
                  {nav.product}
                  <Icon name="chevronDown" size={16} />
                </button>
                <div id="product-menu" hidden className="card shadow-raised absolute left-0 mt-2 w-72 p-2">
                  <ul className="flex flex-col">
                    {product.map((item) => (
                      <li key={item.href}>
                        <SmartLink
                          href={item.href}
                          className="rounded-button hover:bg-surface-sunken flex items-center gap-3 px-3 py-2.5">
                          <span className="icon-tile size-9">
                            <Icon name={item.icon} size={18} />
                          </span>
                          <span className="font-medium">{item.label}</span>
                        </SmartLink>
                      </li>
                    ))}
                  </ul>
                </div>
              </li>
              {top.map((item) => (
                <li key={item.href}>
                  <SmartLink href={item.href} className={navLink}>
                    {item.label}
                  </SmartLink>
                </li>
              ))}
              <li>
                <a
                  href={GITHUB_URL}
                  className={`${navLink} inline-flex items-center`}
                  aria-label={nav.github}>
                  <Icon name="github" size={18} />
                </a>
              </li>
            </ul>
          </nav>

          <div className="ml-auto flex items-center gap-2">
            <div className="hidden md:block">
              <LanguageSwitcher />
            </div>
            <a href={APP_LOGIN_PATH} className="btn btn-ghost hidden min-h-10 md:inline-flex">
              {content.common.cta.signIn}
            </a>
            <PrimaryCta className="hidden min-h-10 sm:inline-flex" />
            <button
              type="button"
              data-sheet-open
              aria-expanded="false"
              aria-controls="mobile-sheet"
              className="btn btn-ghost min-h-11 px-2.5 lg:hidden">
              <Icon name="menu" size={22} />
              <span className="sr-only">{nav.openMenu}</span>
            </button>
          </div>
        </div>
      </header>

      {/* Outside the header: its backdrop-filter would make it the containing block of a fixed descendant, trapping the sheet inside the 64px bar. */}
      <div
        id="mobile-sheet"
        hidden
        role="dialog"
        aria-modal="true"
        aria-label={nav.label}
        className="fixed inset-0 z-50 overflow-y-auto bg-white lg:hidden">
        <div className="container-site flex h-16 items-center justify-between">
          <SmartLink
            href={localePrefix(locale)}
            aria-label={content.common.homeLabel}
            className="flex items-center gap-2">
            <LogoMark size={30} />
            <span className="text-xl font-bold tracking-tight">Forma</span>
          </SmartLink>
          <button type="button" data-sheet-close className="btn btn-ghost min-h-11 px-2.5">
            <Icon name="x" size={22} />
            <span className="sr-only">{nav.closeMenu}</span>
          </button>
        </div>
        <nav aria-label={nav.label} className="container-site pt-4 pb-10">
          <p className="text-small text-ink-500 font-semibold">{nav.product}</p>
          <ul className="mt-2 flex flex-col">
            {product.map((item) => (
              <li key={item.href}>
                <SmartLink href={item.href} className="flex items-center gap-3 py-3 text-lg font-medium">
                  <span className="icon-tile size-9">
                    <Icon name={item.icon} size={18} />
                  </span>
                  {item.label}
                </SmartLink>
              </li>
            ))}
          </ul>
          <ul className="border-line mt-4 flex flex-col border-t pt-4">
            {top.map((item) => (
              <li key={item.href}>
                <SmartLink href={item.href} className="block py-3 text-lg font-medium">
                  {item.label}
                </SmartLink>
              </li>
            ))}
            <li>
              <a href={GITHUB_URL} className="flex items-center gap-2 py-3 text-lg font-medium">
                <Icon name="github" size={20} />
                {nav.github}
              </a>
            </li>
          </ul>
          <div className="border-line mt-4 border-t pt-4">
            <p className="text-small text-ink-500 flex items-center gap-2 font-semibold">
              <Icon name="globe" size={16} />
              {nav.language}
            </p>
            <LanguageLinks
              className="mt-2 flex flex-wrap gap-2"
              itemClassName="block rounded-button border border-line px-3 py-2 aria-[current]:border-teal-600 aria-[current]:font-semibold aria-[current]:text-teal-700"
            />
          </div>
          <div className="mt-8 flex flex-col gap-3">
            <PrimaryCta />
            <a href={APP_LOGIN_PATH} className="btn btn-secondary">
              {content.common.cta.signIn}
            </a>
          </div>
        </nav>
      </div>
    </>
  );
};
