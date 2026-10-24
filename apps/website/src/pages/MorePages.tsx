import { WorkflowChainArt } from "../components/Illustrations";
import { useSite } from "../components/site-context";
import {
  BetaBadge,
  CodeBlock,
  CtaBand,
  FeatureCard,
  IconTile,
  PageHero,
  Rich,
  SectionHeading,
  SmartLink,
  SplitSection,
} from "../components/ui";
import {
  APP_LOGIN_PATH,
  DOCS,
  DOCS_PATH,
  GITHUB_URL,
  JS_SNIPPET,
  MCP_URL,
  SECURITY_EMAIL,
  SECURITY_POLICY_URL,
  localePrefix,
  pagePath,
} from "../site";

const NATIVE_INTEGRATIONS = ["Slack", "Notion", "Airtable", "Google Sheets"] as const;

export const IntegrationsPage = () => {
  const { content } = useSite();
  const page = content.pages.integrations;
  return (
    <>
      <PageHero title={page.title} sub={page.sub} visual={<WorkflowChainArt />} />
      <section aria-labelledby="native" className="section-pad bg-surface-muted">
        <div className="container-site">
          <SectionHeading id="native" title={page.native.title} lead={page.native.body} />
          <ul className="mt-10 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            {NATIVE_INTEGRATIONS.map((name) => (
              <li key={name} data-reveal className="card flex items-center gap-3 p-5">
                <span className="bg-mint-100 inline-flex size-10 flex-none items-center justify-center rounded-[22%] font-bold text-teal-900">
                  {name.charAt(0)}
                </span>
                <span className="font-semibold">{name}</span>
              </li>
            ))}
          </ul>
        </div>
      </section>
      <section aria-label={page.title} className="section-pad">
        <div className="container-site grid gap-6 md:grid-cols-2">
          <FeatureCard icon="webhook" title={page.webhooks.title} body={page.webhooks.body} />
          <FeatureCard
            icon="plug"
            title={page.automation.title}
            body={page.automation.body}
            href={DOCS.integrations}
            linkLabel={page.automation.link}
          />
        </div>
      </section>
      <SplitSection
        id="workflows"
        title={page.workflows.title}
        body={page.workflows.body}
        visual={<WorkflowChainArt />}
        muted>
        <p className="text-small text-ink-700 flex flex-wrap items-center gap-3">
          <BetaBadge />
          {page.workflows.badgeNote}
        </p>
      </SplitSection>
      <CtaBand />
    </>
  );
};

export const DevelopersPage = () => {
  const { locale, content } = useSite();
  const page = content.pages.developers;
  return (
    <>
      <PageHero
        title={page.title}
        sub={page.sub}
        actions={
          <>
            <a href={DOCS_PATH} className="btn btn-primary">
              {content.common.cta.docs}
            </a>
            <a href={GITHUB_URL} className="btn btn-secondary">
              {content.common.nav.github}
            </a>
          </>
        }
      />
      <SplitSection
        id="snippet"
        title={page.snippet.title}
        body={page.snippet.body}
        visual={<CodeBlock code={JS_SNIPPET} label="HTML" />}
        muted
      />
      <section aria-label={page.title} className="section-pad">
        <div className="container-site grid gap-6 lg:grid-cols-3">
          <FeatureCard
            icon="terminal"
            title={page.api.title}
            body={page.api.body}
            href={DOCS.apiV3}
            linkLabel={page.api.link}
          />
          <FeatureCard
            icon="sparkle"
            title={page.mcp.title}
            body={page.mcp.body}
            href={DOCS.mcp}
            linkLabel={page.mcp.link}
          />
          <FeatureCard
            icon="webhook"
            title={page.webhooks.title}
            body={page.webhooks.body}
            href={pagePath(locale, "integrations")}
            linkLabel={page.webhooks.link}
          />
        </div>
        <div className="container-site mt-10">
          <CodeBlock code={MCP_URL} label="MCP" />
        </div>
      </section>
      <CtaBand />
    </>
  );
};

export const SelfHostingPage = () => {
  const { content } = useSite();
  const page = content.pages.selfHosting;
  return (
    <>
      <PageHero
        title={page.title}
        sub={page.sub}
        actions={
          <>
            <a href={DOCS.selfHosting} className="btn btn-primary">
              {content.common.cta.docs}
            </a>
            <a href={GITHUB_URL} className="btn btn-secondary">
              {content.common.nav.github}
            </a>
          </>
        }
      />
      <section aria-labelledby="options" className="section-pad bg-surface-muted">
        <div className="container-site">
          <SectionHeading id="options" title={page.optionsTitle} />
          <div className="mt-10 grid gap-6 lg:grid-cols-3">
            <FeatureCard
              icon="box"
              title={page.docker.title}
              body={page.docker.body}
              href={DOCS.docker}
              linkLabel={page.docker.link}
            />
            <FeatureCard
              icon="terminal"
              title={page.oneClick.title}
              body={page.oneClick.body}
              href={DOCS.oneClick}
              linkLabel={page.oneClick.link}
            />
            <FeatureCard
              icon="server"
              title={page.kubernetes.title}
              body={page.kubernetes.body}
              href={DOCS.kubernetes}
              linkLabel={page.kubernetes.link}
            />
          </div>
        </div>
      </section>
      <section aria-label={page.title} className="section-pad">
        <div className="container-site grid gap-6 md:grid-cols-2">
          <FeatureCard icon="cpu" title={page.requirements.title} body={page.requirements.body} />
          <FeatureCard icon="key" title={page.signIn.title} body={page.signIn.body} />
          <FeatureCard icon="sparkle" title={page.ai.title} body={page.ai.body} />
          <FeatureCard
            icon="github"
            title={page.source.title}
            body={page.source.body}
            href={GITHUB_URL}
            linkLabel={page.source.link}
          />
        </div>
      </section>
      <CtaBand />
    </>
  );
};

export const SecurityPage = () => {
  const { content } = useSite();
  const page = content.pages.security;
  return (
    <>
      <PageHero title={page.title} sub={page.meta.description} />
      <section className="section-pad bg-surface-muted">
        <div className="container-site grid gap-6 md:grid-cols-2">
          {/* The app links here with this fragment (apps/web/src/features/organization/general.tsx). */}
          <div id="stay-informed-with-forma-security-updates" className="card flex flex-col gap-4 p-6 lg:p-8">
            <IconTile icon="shieldCheck" />
            <h2 className="text-h3">{page.report.title}</h2>
            <p className="text-ink-700">
              <Rich text={page.report.body} />
            </p>
            <p>
              <a href={`mailto:${SECURITY_EMAIL}`} className="link">
                {SECURITY_EMAIL}
              </a>
            </p>
          </div>
          <div className="card flex flex-col gap-4 p-6 lg:p-8">
            <IconTile icon="lock" />
            <h2 className="text-h3">{page.account.title}</h2>
            <p className="text-ink-700">{page.account.body}</p>
          </div>
        </div>
        <div className="container-site mt-8">
          <a href={SECURITY_POLICY_URL} className="link">
            {page.policyLink}
          </a>
        </div>
      </section>
    </>
  );
};

export const NotFoundPage = () => {
  const { locale, content } = useSite();
  const page = content.notFound;
  return (
    <section className="section-pad">
      <div className="container-site">
        <p className="text-small font-mono font-semibold text-teal-700">404</p>
        <h1 className="text-display mt-4">{page.title}</h1>
        <p className="measure text-ink-700 mt-6 text-lg leading-8">{page.body}</p>
        <div className="mt-8 flex flex-wrap gap-3">
          <SmartLink href={localePrefix(locale)} className="btn btn-primary">
            {page.home}
          </SmartLink>
          <a href={APP_LOGIN_PATH} className="btn btn-secondary">
            {content.common.cta.signIn}
          </a>
          <a href={DOCS_PATH} className="btn btn-ghost">
            {content.common.cta.docs}
          </a>
        </div>
      </div>
    </section>
  );
};
