import { HeroArt } from "../components/HeroArt";
import { Icon } from "../components/icons";
import { SummaryChartArt, SurveyCardArt, WorkflowChainArt } from "../components/Illustrations";
import { useSite } from "../components/site-context";
import {
  CheckList,
  CodeBlock,
  CtaBand,
  FeatureCard,
  IconTile,
  PrimaryCta,
  SectionHeading,
  SignInCta,
  SmartLink,
  SplitSection,
} from "../components/ui";
import { GITHUB_URL, JS_SNIPPET, SIGNUP_OPEN, pagePath } from "../site";

export const HomePage = () => {
  const { locale, content } = useSite();
  const home = content.pages.home;
  const [linkCard, websiteCard, inAppCard] = home.ways.cards;

  return (
    <>
      <section className="relative overflow-hidden">
        <div className="container-site grid items-center gap-12 pt-12 pb-16 lg:grid-cols-[1.05fr_1fr] lg:pt-20 lg:pb-24">
          <div>
            <p className="bg-crimson-50 text-small ring-crimson-100 text-crimson-900 inline-flex items-center gap-2 rounded-full px-3 py-1 font-semibold ring-1">
              <span className="bg-crimson-600 size-2 rounded-full" aria-hidden="true" />
              {home.hero.eyebrow}
            </p>
            <h1 className="text-display measure mt-6">{home.hero.title}</h1>
            <p className="measure text-ink-700 mt-6 text-lg leading-8">{home.hero.sub}</p>
            <div className="mt-8 flex flex-wrap items-center gap-3">
              <PrimaryCta />
              <SignInCta />
            </div>
            <p className="measure text-small text-ink-500 mt-6">
              {SIGNUP_OPEN ? home.hero.note.open : home.hero.note.closed}
            </p>
          </div>
          <HeroArt />
        </div>
      </section>

      <section aria-labelledby="ways" className="section-pad bg-surface-muted">
        <div className="container-site">
          <SectionHeading id="ways" title={home.ways.title} />
          <div className="mt-10 grid gap-6 md:grid-cols-3">
            <FeatureCard
              icon="link"
              title={linkCard.title}
              body={linkCard.body}
              href={pagePath(locale, "formBuilder")}
            />
            <FeatureCard
              icon="monitor"
              title={websiteCard.title}
              body={websiteCard.body}
              href={pagePath(locale, "websiteSurvey")}
            />
            <FeatureCard
              icon="smartphone"
              title={inAppCard.title}
              body={inAppCard.body}
              href={pagePath(locale, "inAppSurvey")}
            />
          </div>
        </div>
      </section>

      <SplitSection id="build" title={home.build.title} body={home.build.body} visual={<SurveyCardArt />}>
        <CheckList items={home.build.checklist} />
      </SplitSection>

      <SplitSection
        id="understand"
        title={home.understand.title}
        body={home.understand.body}
        visual={<SummaryChartArt />}
        flip
        muted
      />

      <SplitSection id="act" title={home.act.title} body={home.act.body} visual={<WorkflowChainArt />}>
        <SmartLink href={pagePath(locale, "integrations")} className="link">
          {home.act.link}
        </SmartLink>
      </SplitSection>

      <section aria-labelledby="ai" className="section-pad bg-surface-muted">
        <div className="container-site">
          <div data-reveal className="card flex flex-col gap-6 p-8 sm:flex-row sm:items-start lg:p-12">
            <IconTile icon="sparkle" />
            <SectionHeading id="ai" title={home.ai.title} lead={home.ai.body} />
          </div>
        </div>
      </section>

      <SplitSection
        id="developers"
        title={home.developers.title}
        body={home.developers.body}
        visual={<CodeBlock code={JS_SNIPPET} label="HTML" />}>
        <SmartLink href={pagePath(locale, "developers")} className="link">
          {home.developers.link}
        </SmartLink>
      </SplitSection>

      <section id="get-forma" aria-labelledby="hosting" className="section-pad bg-surface-muted">
        <div className="container-site">
          <SectionHeading id="hosting" title={home.hosting.title} />
          <div className="mt-10 grid gap-6 md:grid-cols-2">
            <div data-reveal className="card p-6 lg:p-8">
              <div className="flex items-center gap-3">
                <IconTile icon="globe" />
                <h3 className="text-h3">{home.hosting.hostedTitle}</h3>
              </div>
              <div className="mt-6">
                <CheckList
                  items={[
                    home.hosting.hosted[0],
                    home.hosting.hosted[1],
                    SIGNUP_OPEN ? home.hosting.hosted[2].open : home.hosting.hosted[2].closed,
                  ]}
                />
              </div>
            </div>
            <div data-reveal className="card p-6 lg:p-8">
              <div className="flex items-center gap-3">
                <IconTile icon="server" />
                <h3 className="text-h3">{home.hosting.selfHostedTitle}</h3>
              </div>
              <div className="mt-6">
                <CheckList items={home.hosting.selfHosted} />
              </div>
            </div>
          </div>
          <p className="text-ink-700 mt-8 flex items-center gap-2">
            <Icon name="github" size={18} />
            <a href={GITHUB_URL} className="link">
              {home.hosting.footer}
            </a>
          </p>
        </div>
      </section>

      <CtaBand />
    </>
  );
};
