import { Icon } from "../components/icons";
import { BrowserPopoverArt, InAppArt, SurveyCardArt } from "../components/Illustrations";
import { useSite } from "../components/site-context";
import {
  CheckList,
  CodeBlock,
  CtaBand,
  DocsLink,
  FeatureCard,
  PageHero,
  PrimaryCta,
  SectionHeading,
  SignInCta,
  SplitSection,
} from "../components/ui";
import { JS_SNIPPET } from "../site";

const HeroActions = () => (
  <>
    <PrimaryCta />
    <SignInCta />
    <DocsLink className="ml-2" />
  </>
);

export const FormBuilderPage = () => {
  const { content } = useSite();
  const page = content.pages.formBuilder;
  return (
    <>
      <PageHero title={page.title} sub={page.sub} visual={<SurveyCardArt />} actions={<HeroActions />} />
      <section aria-labelledby="share" className="section-pad bg-surface-muted">
        <div className="container-site">
          <SectionHeading id="share" title={page.share.title} />
          <ul className="mt-10 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {page.share.items.map((item) => (
              <li key={item} data-reveal className="card flex items-center gap-3 p-5">
                <span className="bg-mint-100 inline-flex size-8 flex-none items-center justify-center rounded-full text-teal-900">
                  <Icon name="check" size={16} />
                </span>
                <span className="font-medium">{item}</span>
              </li>
            ))}
          </ul>
        </div>
      </section>
      <section aria-label={page.title} className="section-pad">
        <div className="container-site grid gap-6 md:grid-cols-2">
          <FeatureCard icon="lock" title={page.control.title} body={page.control.body} />
          <FeatureCard icon="gitBranch" title={page.logic.title} body={page.logic.body} />
          <FeatureCard icon="languages" title={page.languages.title} body={page.languages.body} />
          <FeatureCard icon="layoutTemplate" title={page.templates.title} body={page.templates.body} />
        </div>
      </section>
      <CtaBand />
    </>
  );
};

export const WebsiteSurveyPage = () => {
  const { content } = useSite();
  const page = content.pages.websiteSurvey;
  return (
    <>
      <PageHero title={page.title} sub={page.sub} visual={<BrowserPopoverArt />} actions={<HeroActions />} />
      <SplitSection
        id="install"
        title={page.install.title}
        body={page.install.body}
        visual={<CodeBlock code={JS_SNIPPET} label="HTML" />}
        muted
      />
      <section aria-labelledby="triggers" className="section-pad">
        <div className="container-site grid gap-12 lg:grid-cols-2">
          <div data-reveal>
            <SectionHeading id="triggers" title={page.triggers.title} lead={page.triggers.body} />
          </div>
          <div data-reveal>
            <CheckList items={page.triggers.items} />
          </div>
        </div>
      </section>
      <section aria-label={page.title} className="section-pad bg-surface-muted">
        <div className="container-site grid gap-6 md:grid-cols-2">
          <FeatureCard icon="clock" title={page.considerate.title} body={page.considerate.body} />
          <FeatureCard icon="palette" title={page.style.title} body={page.style.body} />
        </div>
      </section>
      <CtaBand />
    </>
  );
};

export const InAppSurveyPage = () => {
  const { content } = useSite();
  const page = content.pages.inAppSurvey;
  return (
    <>
      <PageHero title={page.title} sub={page.sub} visual={<InAppArt />} actions={<HeroActions />} />
      <section aria-label={page.title} className="section-pad bg-surface-muted">
        <div className="container-site grid gap-6 md:grid-cols-2">
          <FeatureCard icon="userCheck" title={page.identify.title} body={page.identify.body} />
          <FeatureCard icon="users" title={page.segments.title} body={page.segments.body} />
          <FeatureCard icon="code" title={page.codeActions.title} body={page.codeActions.body} />
          <FeatureCard icon="languages" title={page.language.title} body={page.language.body} />
        </div>
      </section>
      <CtaBand />
    </>
  );
};
