import { z } from "zod";

// The shape every locale's dictionary must have. Objects are strict and every string must be non-empty, so a missing, misspelt or extra key in any locale fails the build (src/prerender/main.ts parses every dictionary before rendering) and the test suite, rather than shipping a page with a hole in it.
//
// Text may carry `inline code` in backticks; <Rich> renders it as <code>.

const text = z.string().trim().min(1);
const obj = <T extends z.ZodRawShape>(shape: T) => z.strictObject(shape);

const meta = obj({ title: text, description: text });
const section = obj({ title: text, body: text });
const linkedSection = obj({ title: text, body: text, link: text });
/** Copy that depends on whether public sign-up is open (SIGNUP_OPEN). */
const signupVariant = obj({ closed: text, open: text });

const common = obj({
  skipToContent: text,
  homeLabel: text,
  nav: obj({
    label: text,
    product: text,
    linkSurveys: text,
    websiteSurveys: text,
    inAppSurveys: text,
    integrations: text,
    developers: text,
    selfHosting: text,
    github: text,
    openMenu: text,
    closeMenu: text,
    language: text,
  }),
  cta: obj({ primary: signupVariant, signIn: text }),
  beta: text,
  copy: obj({ button: text, done: text }),
  ctaBand: obj({ title: text, body: signupVariant }),
  footer: obj({
    label: text,
    product: text,
    developers: text,
    project: text,
    reportIssue: text,
    security: text,
    copyright: text,
  }),
  // Shown to a visitor whose browser language is this locale while they read another one, in this locale's language.
  localeHint: obj({ text: text, dismiss: text }),
  illustrations: obj({
    surveyTitle: text,
    ratingQuestion: text,
    npsQuestion: text,
    npsLow: text,
    npsHigh: text,
    textPlaceholder: text,
    submit: text,
    popoverQuestion: text,
    popoverYes: text,
    popoverNo: text,
    summaryTitle: text,
    summaryOptions: z.tuple([text, text, text, text]),
    workflowTrigger: text,
    workflowTriggerValue: text,
    workflowFilter: text,
    workflowFilterValue: text,
    workflowAction: text,
    workflowActionValue: text,
    segmentLabel: text,
    segmentValue: text,
    eventLabel: text,
    eventValue: text,
    newResponse: text,
  }),
});

const home = obj({
  meta,
  hero: obj({ eyebrow: text, title: text, sub: text, note: signupVariant }),
  ways: obj({ title: text, cards: z.tuple([section, section, section]) }),
  build: obj({ title: text, body: text, checklist: z.tuple([text, text, text]) }),
  understand: section,
  act: linkedSection,
  ai: section,
  developers: linkedSection,
  hosting: obj({
    title: text,
    hostedTitle: text,
    hosted: z.tuple([text, text, signupVariant]),
    selfHostedTitle: text,
    selfHosted: z.tuple([text, text, text]),
    footer: text,
  }),
});

const formBuilder = obj({
  meta,
  title: text,
  sub: text,
  share: obj({ title: text, items: z.tuple([text, text, text, text, text, text]) }),
  control: section,
  logic: section,
  languages: section,
  templates: section,
});

const websiteSurvey = obj({
  meta,
  title: text,
  sub: text,
  install: section,
  triggers: obj({ title: text, items: z.tuple([text, text, text, text, text]), body: text }),
  considerate: section,
  style: section,
});

const inAppSurvey = obj({
  meta,
  title: text,
  sub: text,
  identify: section,
  segments: section,
  codeActions: section,
  language: section,
});

const integrations = obj({
  meta,
  title: text,
  sub: text,
  native: section,
  webhooks: section,
  automation: section,
  workflows: obj({ title: text, body: text, badgeNote: text }),
});

/** A term of the developer reference and what it says about it. */
const fact = obj({ term: text, body: text });

const developers = obj({
  meta,
  title: text,
  sub: text,
  snippet: section,
  api: linkedSection,
  mcp: linkedSection,
  webhooks: linkedSection,
  // The short API and MCP reference on the page itself: there is no separate documentation site (/docs is reserved and retired).
  reference: obj({
    title: text,
    lead: text,
    rest: obj({
      title: text,
      facts: z.tuple([fact, fact, fact]),
      resourcesTitle: text,
      resources: z.tuple([text, text, text, text]),
    }),
    mcp: obj({ title: text, facts: z.tuple([fact, fact, fact]) }),
  }),
});

const selfHosting = obj({
  meta,
  title: text,
  sub: text,
  optionsTitle: text,
  docker: linkedSection,
  oneClick: linkedSection,
  kubernetes: linkedSection,
  requirements: section,
  signIn: section,
  ai: section,
  source: linkedSection,
});

const security = obj({
  meta,
  title: text,
  report: section,
  account: section,
  policyLink: text,
});

const notFound = obj({ meta, title: text, body: text, home: text });

export const siteContentSchema = obj({
  common,
  pages: obj({
    home,
    formBuilder,
    websiteSurvey,
    inAppSurvey,
    integrations,
    developers,
    selfHosting,
    security,
  }),
  notFound,
});

export type SiteContent = z.infer<typeof siteContentSchema>;
