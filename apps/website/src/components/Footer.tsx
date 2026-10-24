import { GITHUB_URL, localePrefix, pagePath } from "../site";
import { LanguageLinks } from "./Header";
import { Icon } from "./icons";
import { LogoMark } from "./Logo";
import { useSite } from "./site-context";
import { SmartLink } from "./ui";

const Column = ({ title, links }: { title: string; links: { href: string; label: string }[] }) => (
  <div>
    <h2 className="text-small text-ink-900 font-semibold">{title}</h2>
    <ul className="mt-3 flex flex-col gap-2">
      {links.map((link) => (
        <li key={link.href}>
          <SmartLink
            href={link.href}
            className="text-ink-700 text-[15px] hover:text-teal-700 hover:underline">
            {link.label}
          </SmartLink>
        </li>
      ))}
    </ul>
  </div>
);

export const SiteFooter = () => {
  const { locale, content } = useSite();
  const nav = content.common.nav;
  const footer = content.common.footer;
  return (
    <footer aria-label={footer.label} className="border-line bg-surface-muted border-t">
      <div className="container-site grid grid-cols-2 gap-x-6 gap-y-10 py-12 lg:grid-cols-5 lg:py-16">
        <div className="col-span-2">
          <SmartLink
            href={localePrefix(locale)}
            aria-label={content.common.homeLabel}
            className="inline-flex items-center gap-2">
            <LogoMark size={28} />
            <span className="text-lg font-bold tracking-tight">Forma</span>
          </SmartLink>
          <div className="text-small text-ink-500 mt-6 flex items-center gap-2">
            <Icon name="globe" size={16} />
            <span>{nav.language}</span>
          </div>
          <LanguageLinks
            className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-[15px]"
            itemClassName="text-ink-700 hover:text-teal-700 hover:underline aria-[current]:font-semibold aria-[current]:text-ink-900"
          />
          <p className="text-small text-ink-500 mt-6">{footer.copyright}</p>
        </div>
        <Column
          title={footer.product}
          links={[
            { href: pagePath(locale, "formBuilder"), label: nav.linkSurveys },
            { href: pagePath(locale, "websiteSurvey"), label: nav.websiteSurveys },
            { href: pagePath(locale, "inAppSurvey"), label: nav.inAppSurveys },
            { href: pagePath(locale, "integrations"), label: nav.integrations },
          ]}
        />
        <Column
          title={footer.developers}
          links={[
            { href: pagePath(locale, "developers"), label: nav.developers },
            { href: pagePath(locale, "selfHosting"), label: nav.selfHosting },
          ]}
        />
        <Column
          title={footer.project}
          links={[
            { href: GITHUB_URL, label: nav.github },
            { href: `${GITHUB_URL}/issues`, label: footer.reportIssue },
            { href: pagePath(locale, "security"), label: footer.security },
          ]}
        />
      </div>
    </footer>
  );
};
