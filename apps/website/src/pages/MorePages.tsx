import { DestinationsArt, WorkflowChainArt } from "../components/Illustrations";
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
  appLoginHref,
  INTEGRATION_LOGO,
  NATIVE_INTEGRATIONS,
  GITHUB_URL,
  JS_SNIPPET,
  SDK_EVENTS_SNIPPET,
  SDK_IDENTIFY_SNIPPET,
  SELF_HOSTING_SOURCES,
  SECURITY_EMAIL,
  SECURITY_POLICY_URL,
  localePrefix,
  pagePath,
} from "../site";

export const IntegrationsPage = () => {
  const { content } = useSite();
  const page = content.pages.integrations;
  return (
    <>
      <PageHero title={page.title} sub={page.sub} visual={<DestinationsArt />} />
      <section aria-labelledby="native" className="section-pad bg-surface-muted">
        <div className="container-site">
          <SectionHeading id="native" title={page.native.title} lead={page.native.body} />
          <ul className="mt-10 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            {NATIVE_INTEGRATIONS.map((name) => (
              <li key={name} data-reveal className="card flex items-center gap-3 p-5">
                <span className="border-line inline-flex size-10 flex-none items-center justify-center rounded-[22%] border bg-white">
                  <img
                    src={INTEGRATION_LOGO[name]}
                    alt=""
                    width={24}
                    height={24}
                    className="size-6 object-contain"
                  />
                </span>
                <span className="font-semibold">{name}</span>
              </li>
            ))}
          </ul>
        </div>
      </section>
      <section className="section-pad">
        <div className="container-site grid gap-6 md:grid-cols-2">
          <FeatureCard icon="webhook" title={page.webhooks.title} body={page.webhooks.body} />
          <FeatureCard icon="plug" title={page.automation.title} body={page.automation.body} />
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

/** The terms of the developer reference. Paths and headers are long unbroken strings, so they may wrap anywhere rather than widen a phone's card. */
const ReferenceFacts = ({ facts }: { facts: readonly { term: string; body: string }[] }) => (
  <dl className="mt-6 flex flex-col gap-5">
    {facts.map((fact) => (
      <div key={fact.term}>
        <dt className="font-semibold">{fact.term}</dt>
        <dd className="text-ink-700 mt-1 [overflow-wrap:anywhere]">
          <Rich text={fact.body} />
        </dd>
      </div>
    ))}
  </dl>
);

export const DevelopersPage = () => {
  const { locale, content } = useSite();
  const page = content.pages.developers;
  const reference = page.reference;
  return (
    <>
      <PageHero
        title={page.title}
        sub={page.sub}
        actions={
          <a href={GITHUB_URL} className="btn btn-primary">
            {content.common.nav.github}
          </a>
        }
      />
      <SplitSection
        id="snippet"
        title={page.snippet.title}
        body={page.snippet.body}
        visual={<CodeBlock code={JS_SNIPPET} label="HTML" />}
        muted>
        <SmartLink href={pagePath(locale, "sdk")} className="link">
          {page.snippet.link}
        </SmartLink>
      </SplitSection>
      <section className="section-pad">
        <div className="container-site grid gap-6 lg:grid-cols-3">
          <FeatureCard
            icon="terminal"
            title={page.api.title}
            body={page.api.body}
            href="#rest-api"
            linkLabel={page.api.link}
          />
          <FeatureCard
            icon="sparkle"
            title={page.mcp.title}
            body={page.mcp.body}
            href="#mcp"
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
      </section>
      <section aria-labelledby="reference" className="section-pad bg-surface-muted">
        <div className="container-site">
          <SectionHeading id="reference" title={reference.title} lead={reference.lead} />
          <div className="mt-10 grid items-start gap-6 lg:grid-cols-2">
            <div data-reveal className="card p-6 lg:p-8">
              <h3 id="rest-api" className="text-h3">
                {reference.rest.title}
              </h3>
              <ReferenceFacts facts={reference.rest.facts} />
              <h4 className="mt-8 font-semibold">{reference.rest.resourcesTitle}</h4>
              <ul className="mt-3 flex flex-col gap-3">
                {reference.rest.resources.map((resource) => (
                  <li key={resource} className="text-ink-700 [overflow-wrap:anywhere]">
                    <Rich text={resource} />
                  </li>
                ))}
              </ul>
            </div>
            <div data-reveal className="card p-6 lg:p-8">
              <h3 id="mcp" className="text-h3">
                {reference.mcp.title}
              </h3>
              <ReferenceFacts facts={reference.mcp.facts} />
            </div>
          </div>
        </div>
      </section>
      <CtaBand />
    </>
  );
};

/** One card of the SDK reference: a group of methods, and optionally an example below them. */
const ReferenceCard = ({
  id,
  title,
  body,
  facts,
  example,
}: {
  id: string;
  title: string;
  body?: string;
  facts: readonly { term: string; body: string }[];
  example?: string;
}) => (
  <div data-reveal className="card min-w-0 p-6 lg:p-8">
    <h3 id={id} className="text-h3">
      {title}
    </h3>
    {body ? (
      <p className="text-ink-700 mt-4">
        <Rich text={body} />
      </p>
    ) : null}
    <ReferenceFacts facts={facts} />
    {example ? (
      <div className="mt-6">
        <CodeBlock code={example} label="JavaScript" />
      </div>
    ) : null}
  </div>
);

export const SdkPage = () => {
  const { content } = useSite();
  const page = content.pages.sdk;
  const reference = page.reference;
  return (
    <>
      <PageHero title={page.title} sub={page.sub} />
      <SplitSection
        id="install"
        title={page.install.title}
        body={page.install.body}
        visual={<CodeBlock code={JS_SNIPPET} label="HTML" />}
        muted>
        <div className="text-ink-700 flex flex-col gap-4">
          <p>
            <Rich text={page.install.frameworks} />
          </p>
          <p className="[overflow-wrap:anywhere]">
            <Rich text={page.install.queue} />
          </p>
        </div>
      </SplitSection>
      <section aria-labelledby="methods" className="section-pad">
        <div className="container-site">
          <SectionHeading id="methods" title={reference.title} lead={reference.lead} />
          <div className="mt-10 grid items-start gap-6 lg:grid-cols-2">
            <ReferenceCard
              id="identify"
              title={reference.identify.title}
              facts={reference.identify.facts}
              example={SDK_IDENTIFY_SNIPPET}
            />
            <ReferenceCard id="actions" title={reference.actions.title} facts={reference.actions.facts} />
            <ReferenceCard
              id="events"
              title={reference.events.title}
              body={reference.events.body}
              facts={reference.events.facts}
              example={SDK_EVENTS_SNIPPET}
            />
            <ReferenceCard
              id="debugging"
              title={reference.debugging.title}
              facts={reference.debugging.facts}
            />
          </div>
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
          <a href={GITHUB_URL} className="btn btn-primary">
            {content.common.nav.github}
          </a>
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
              href={SELF_HOSTING_SOURCES.docker}
              linkLabel={page.docker.link}
            />
            <FeatureCard
              icon="terminal"
              title={page.oneClick.title}
              body={page.oneClick.body}
              href={SELF_HOSTING_SOURCES.oneClick}
              linkLabel={page.oneClick.link}
            />
            <FeatureCard
              icon="server"
              title={page.kubernetes.title}
              body={page.kubernetes.body}
              href={SELF_HOSTING_SOURCES.kubernetes}
              linkLabel={page.kubernetes.link}
            />
          </div>
        </div>
      </section>
      <section className="section-pad">
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
          <div className="card flex flex-col gap-4 p-6 md:col-span-2 lg:p-8">
            <IconTile icon="terminal" />
            <h2 className="text-h3">{page.audit.title}</h2>
            <p className="text-ink-700">
              <Rich text={page.audit.body} />
            </p>
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
        <p className="text-small text-crimson-700 font-mono font-semibold">404</p>
        <h1 className="text-display mt-4">{page.title}</h1>
        <p className="measure text-ink-700 mt-6 text-lg leading-8">{page.body}</p>
        <div className="mt-8 flex flex-wrap gap-3">
          <SmartLink href={localePrefix(locale)} className="btn btn-primary">
            {page.home}
          </SmartLink>
          <a href={appLoginHref(locale)} className="btn btn-secondary">
            {content.common.cta.signIn}
          </a>
        </div>
      </div>
    </section>
  );
};
