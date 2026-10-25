import type { SiteContent } from "./schema";

export const en: SiteContent = {
  common: {
    skipToContent: "Skip to content",
    homeLabel: "Forma home",
    nav: {
      label: "Main",
      product: "Product",
      linkSurveys: "Link surveys",
      websiteSurveys: "Website surveys",
      inAppSurveys: "In-app surveys",
      integrations: "Integrations",
      developers: "Developers",
      sdk: "SDK reference",
      selfHosting: "Self-hosting",
      github: "GitHub",
      openMenu: "Open menu",
      closeMenu: "Close menu",
      language: "Language",
    },
    cta: {
      primary: { closed: "Self-host Forma", open: "Get started" },
      signIn: "Sign in",
    },
    beta: "Beta",
    copy: { button: "Copy", done: "Copied" },
    ctaBand: {
      title: "Start with one question",
      body: {
        closed:
          "Self-host Forma in minutes with Docker Compose, or sign in if your team already uses forma.yldm.ai.",
        open: "Create an account or self-host Forma.",
      },
    },
    footer: {
      label: "Footer",
      product: "Product",
      developers: "Developers",
      project: "Project",
      reportIssue: "Report an issue",
      security: "Security",
      copyright: "© 2026 YLDM",
    },
    localeHint: { text: "This page is also available in English.", dismiss: "Dismiss" },
    illustrations: {
      surveyTitle: "Customer feedback",
      ratingQuestion: "How would you rate your experience?",
      npsQuestion: "How likely are you to recommend us?",
      npsLow: "Not likely",
      npsHigh: "Very likely",
      textPlaceholder: "Tell us more…",
      submit: "Submit",
      popoverQuestion: "Did you find what you were looking for?",
      popoverYes: "Yes",
      popoverNo: "Not yet",
      summaryTitle: "What do you value most?",
      summaryOptions: ["Ease of use", "Speed", "Integrations", "Support"],
      workflowTrigger: "Trigger",
      workflowTriggerValue: "Response completed",
      workflowFilter: "Filter",
      workflowFilterValue: "Ending: Thank you",
      workflowAction: "Action",
      workflowActionValue: "Send email",
      segmentLabel: "Segment",
      segmentValue: "Plan is Trial",
      eventLabel: "Action",
      eventValue: "Exported a report",
      newResponse: "New response",
    },
  },
  pages: {
    home: {
      meta: {
        title: "Forma: link, website and in-app surveys",
        description:
          "Build surveys in a no-code editor, target who answers, read the results and send them to a webhook, or to Slack, Notion and Google Sheets on self-hosted instances.",
      },
      hero: {
        eyebrow: "Surveys and feedback",
        title: "Ask the right people. Act on what they tell you.",
        sub: "Forma is a survey platform for link, website and in-app surveys. Build in a no-code editor, choose exactly who should answer, read the results, and send them to the tools your team already uses.",
        note: {
          closed:
            "Use the hosted service at forma.yldm.ai (invitation only for now), or run Forma on your own servers.",
          open: "Create an account at forma.yldm.ai, or run Forma on your own servers.",
        },
      },
      ways: {
        title: "Three ways to ask",
        cards: [
          {
            title: "Link surveys",
            body: "Share a link, embed it on a page, or send personal and single-use links. Add a PIN or email verification when answers need to be trusted.",
          },
          {
            title: "Website surveys",
            body: "Show a survey to visitors when they click, scroll, stay on a page or are about to leave. Once the snippet is installed, you set the triggers in Forma, not in your code.",
          },
          {
            title: "In-app surveys",
            body: "Identify signed-in users, attach attributes, and ask exactly the segment you care about, at the moment it matters.",
          },
        ],
      },
      build: {
        title: "Build the survey you actually need",
        body: "Start from one of more than 50 templates or from a blank survey. Combine 17 question types with conditional logic, hidden fields, variables and recall, and style it to match your brand.",
        checklist: [
          "Free text, single and multiple choice, picture choice, rating, NPS, CSAT, CES, ranking, matrix, date, address, contact info, file upload, consent, statement, and meeting scheduling with Cal.com",
          "One survey in many languages",
          "Partial submissions, quotas, scheduling and response limits",
        ],
      },
      understand: {
        title: "Read the results where they were collected",
        body: "Every survey has its own summary and response views. Filter responses, tag them, export to CSV or Excel, or share the results with a public link.",
      },
      act: {
        title: "Turn responses into next steps",
        body: "Send responses to any endpoint with webhooks, and on self-hosted instances to Slack, Notion, Airtable or Google Sheets. Workflows (Beta) send an email when a response is completed, optionally only for the endings you choose.",
        link: "See integrations →",
      },
      ai: {
        title: "AI, only when you turn it on",
        body: "Describe a survey and get an editable draft, translate a survey into your enabled languages, or generate example responses to preview the analysis. AI stays off until an owner or manager enables it for the organization.",
      },
      developers: {
        title: "Built to be wired in",
        body: "Add one script tag to show website and in-app surveys. Manage surveys, responses and workflows over the v3 REST API, or connect an AI agent to the MCP server with your own Forma login.",
        link: "Developer overview →",
      },
      hosting: {
        title: "On our servers or yours",
        hostedTitle: "Hosted",
        hosted: [
          "forma.yldm.ai",
          "We run the service and its upgrades",
          { closed: "Invitation only for now", open: "Sign up with email" },
        ],
        selfHostedTitle: "Self-hosted",
        selfHosted: [
          "Docker Compose, a one-click script for Ubuntu, or the Helm chart",
          "Your database, your object storage",
          "SSO with Google, Microsoft Entra ID, OIDC or SAML",
        ],
        footer: "The source code is public on GitHub.",
      },
    },
    formBuilder: {
      meta: {
        title: "Link surveys and forms · Forma",
        description:
          "Create link surveys with 17 question types, conditional logic, hidden fields and multiple languages. Share by link, embed on a page or send single-use links.",
      },
      title: "Link surveys and forms",
      sub: "Build a survey once and send it anywhere a link can go: email, chat, QR code, or embedded on your own page.",
      share: {
        title: "Share it your way",
        items: [
          "Custom URL slugs",
          "Embed with an iframe",
          "Personal links per contact",
          "Single-use links",
          "Source tracking via URL parameters",
          "Prefill answers from the URL",
        ],
      },
      control: {
        title: "Control who answers",
        body: "Protect a survey with a PIN, require email verification before it starts, and cap or schedule submissions. Self-hosted instances can add reCAPTCHA spam protection.",
      },
      logic: {
        title: "Logic and data",
        body: "Branch with conditional logic, carry context in hidden fields and variables, quote earlier answers with recall, and keep partial submissions.",
      },
      languages: {
        title: "One survey, many languages",
        body: "Add translations to a single survey instead of copying it. Respondents pick a language or arrive with `?lang=`, and every answer lands in the same results.",
      },
      templates: {
        title: "Templates",
        body: "More than 50 templates, available in English, Japanese, and Simplified and Traditional Chinese.",
      },
    },
    websiteSurvey: {
      meta: {
        title: "Website surveys · Forma",
        description:
          "Show surveys to visitors on click, page view, scroll, time on page or exit intent, configured without code after one script tag.",
      },
      title: "Surveys on your website",
      sub: "Ask visitors at the right moment, without asking them to leave the page.",
      install: {
        title: "Install once",
        body: "Paste one script tag into your site, or add it through Google Tag Manager. Everything else is configured in Forma.",
      },
      triggers: {
        title: "No-code triggers",
        items: [
          "Click on an element (by text or CSS selector)",
          "Page view",
          "Exit intent",
          "50% scroll",
          "Time on page",
        ],
        body: "Limit any trigger to URLs that match, contain, start or end with a value, or a regular expression.",
      },
      considerate: {
        title: "Be considerate",
        body: "Set how often the same visitor can be asked, add a cooldown between surveys, and show a survey to only a percentage of visitors.",
      },
      style: {
        title: "Match your site",
        body: "Workspace themes, per-survey style overrides and custom CSS.",
      },
    },
    inAppSurvey: {
      meta: {
        title: "In-app surveys for known users · Forma",
        description:
          "Identify users, add attributes, build segments and trigger surveys from actions in your product.",
      },
      title: "In-app surveys for known users",
      sub: "Identify users, enrich them with attributes, and ask the people whose answers matter for the decision in front of you.",
      identify: {
        title: "Identify and enrich",
        body: "Set a user ID and attributes from your app through the SDK. Contacts and their attributes live in Forma.",
      },
      segments: {
        title: "Target with segments",
        body: "Build segments from attributes and show a survey only to that segment, triggered by actions in your product.",
      },
      codeActions: {
        title: "Code actions",
        body: "Track any event from your code and use it as a trigger, alongside the no-code actions.",
      },
      language: {
        title: "Language",
        body: "Set the user's language through the SDK. If a multi-language survey has no translation for it, Forma does not show that survey to the user.",
      },
    },
    integrations: {
      meta: {
        title: "Integrations and Workflows · Forma",
        description:
          "Send responses to webhooks, or to Slack, Notion, Airtable and Google Sheets on self-hosted instances, and email on completed responses with Workflows (Beta).",
      },
      title: "Integrations and Workflows",
      sub: "Responses are only useful once someone sees them. Send them where your team works.",
      native: {
        title: "Native integrations",
        body: "On a self-hosted instance with the service's OAuth app configured, connect an account and choose where new responses go.",
      },
      webhooks: {
        title: "Webhooks",
        body: "Get an HTTP request at your endpoint when responses are created, updated or finished.",
      },
      automation: {
        title: "Automation tools",
        body: "Zapier, Make, n8n and Activepieces subscribe to responses through the webhooks API.",
      },
      workflows: {
        title: "Workflows",
        body: "Trigger: a response is completed. Filter: only the endings you choose. Action: send an email. Every run is recorded with its status and step logs.",
        badgeNote: "Beta: the set of triggers and actions will grow.",
      },
    },
    developers: {
      meta: {
        title: "Forma for developers",
        description:
          "A JavaScript snippet for in-product surveys, the v3 REST API and an MCP server for AI agents.",
      },
      title: "For developers",
      sub: "A script tag for surveys in your product, a REST API for everything else.",
      snippet: {
        title: "JavaScript snippet",
        body: "Loads `/js/forma.umd.js` from your Forma instance and renders surveys on your pages. Calls made before it has loaded are queued.",
        link: "SDK reference →",
      },
      api: {
        title: "REST API v3",
        body: "Manage surveys and workflows with an API key. Responses and webhooks have management endpoints that take the same key.",
        link: "REST API reference →",
      },
      mcp: {
        title: "MCP server",
        body: "Point an MCP client at `https://forma.yldm.ai/api/mcp`. You sign in with your own Forma account over OAuth, approve the scopes, and the agent can list, create and edit surveys and workflows.",
        link: "Connect a client →",
      },
      webhooks: {
        title: "Webhooks",
        body: "Receive responses at your own endpoint as they are created, updated and finished.",
        link: "See integrations →",
      },
      reference: {
        title: "API reference",
        lead: "Paths are relative to your Forma instance: `https://forma.yldm.ai`, or your own domain if you self-host. Requests and responses are JSON.",
        rest: {
          title: "REST API",
          facts: [
            {
              term: "Base path",
              body: "`/api/v3`. Errors come back as RFC 9457 problem documents (`application/problem+json`).",
            },
            {
              term: "Authentication",
              body: "Send the key in the `X-Api-Key` header, or as `Authorization: Bearer fbk_…`. Keep keys out of URLs.",
            },
            {
              term: "API keys",
              body: "Organization owners and managers create keys in the app under Organization Settings → API Keys → Add API key. Grant read, write or manage access one workspace at a time, then copy the key: it is shown only once.",
            },
          ],
          resourcesTitle: "Main resources",
          resources: [
            "`/api/v3/surveys`: list, create, validate, read, update, archive, restore and delete surveys.",
            "`/api/v3/workflows`: create and edit workflows, enable, disable or test them, and list their runs.",
            "`/api/v3/action-classes` and `/api/v3/contact-attribute-keys`: the actions and attributes in-app surveys target, read-only.",
            "`/api/v2/management/responses` and `/api/v2/management/webhooks`: responses and webhook subscriptions, with the same key.",
          ],
        },
        mcp: {
          title: "MCP server",
          facts: [
            {
              term: "Endpoint",
              body: "`/api/mcp` over Streamable HTTP, for example `https://forma.yldm.ai/api/mcp`.",
            },
            {
              term: "OAuth",
              body: "A client that supports OAuth finds the sign-in on its own: you sign in with your Forma account and approve the scopes it asks for.",
            },
            {
              term: "API key",
              body: "A client without OAuth sends an API key in a header, as with the REST API. A read key can list and read; a write or manage key can also create and edit.",
            },
          ],
        },
      },
    },
    sdk: {
      meta: {
        title: "Forma JavaScript SDK reference",
        description:
          "Install the Forma SDK with a script tag, identify users, set attributes, track actions, listen to events and debug the integration.",
      },
      title: "JavaScript SDK reference",
      sub: "One script tag shows your website and in-app surveys. Everything else is a call on `window.forma`.",
      install: {
        title: "Install",
        body: "Copy the snippet from Integrations → Web & Mobile SDK in Forma, which fills in your workspace ID and your instance's URL, and paste it into the `<head>` of every page. With Google Tag Manager, paste it into a Custom HTML tag that fires on All Pages.",
        frameworks:
          'React and Vue: paste it into `index.html`. Next.js: render the same JavaScript with `next/script` and `strategy="beforeInteractive"`, so `window.forma` exists before your components\' effects run: in the root `app/layout.tsx` with the App Router, or directly in `pages/_document.tsx` with the Pages Router. No route hooks are needed. The SDK is not published on npm; mobile SDKs and a WordPress plugin are not available yet.',
        queue:
          'Until the script arrives, `window.forma` is a stub: calls are queued, not thrown, and replayed in order once the SDK has loaded and set itself up. A queued call returns nothing, so do not await it. Once the SDK has loaded, the stub forwards to it, so a reference kept from before then still works. In TypeScript, declare the global once, naming the methods you call rather than using an index signature, which `noUncheckedIndexedAccess` makes possibly undefined: `interface Window { forma: Record<"setUserId" | "track" | "logout", (...args: unknown[]) => unknown> }`.',
      },
      reference: {
        title: "Methods",
        lead: "Every method is called on `window.forma`. The snippet calls `setup({ workspaceId, appUrl })` for you; call it yourself only if you load `/js/forma.umd.js` without the snippet, and before anything else.",
        identify: {
          title: "Identify users",
          facts: [
            {
              term: "setUserId(userId)",
              body: "Identifies the visitor as the contact with that ID, created on first use, so segment targeting and show-once rules apply to the person. IDs are up to 255 characters; a different ID first clears the previous user's state.",
            },
            {
              term: "setAttributes(attributes), setAttribute(key, value)",
              body: "Set contact attributes for segments. Call `setUserId` first. A string, a number or a `Date` gives a new attribute its type; later values must have the same type, or the server does not store them.",
            },
            {
              term: "setLanguage(language)",
              body: "Shows surveys in that language: a language code such as `de`, or an alias you configured. Works for anonymous visitors too.",
            },
            {
              term: "logout()",
              body: "Forgets the user on this browser. Call it on sign-out, so the next person on the device is not treated as the previous one.",
            },
          ],
        },
        actions: {
          title: "Actions and single-page apps",
          facts: [
            {
              term: "track(code, { hiddenFields })",
              body: "Fires the code action with that key (set up under Actions in Forma), which may show a survey. `hiddenFields` fills the survey's hidden fields and is stored with the response; it is deprecated and will be removed in a future version.",
            },
            {
              term: "Page views and routing",
              body: "The SDK patches `history.pushState` and `replaceState` and listens to `popstate` and `hashchange`, so client-side navigation is a page view on its own. `registerRouteChange()` re-checks the URL by hand for a router that does neither.",
            },
            {
              term: "setEmbeddedData(data), clearEmbeddedData(key?)",
              body: "Attach data to future responses without triggering anything; `{ key: null }` removes a key, and only fields the survey declares as ingested embedded data are kept. Synchronous, no network.",
            },
          ],
        },
        events: {
          title: "Events",
          body: "`on(event, handler)` returns a function that unsubscribes, as does `off(event, handler)`. The same events are pushed to `window.dataLayer` for Google Tag Manager under a `forma` key. Payloads carry IDs only, never answers.",
          facts: [
            {
              term: "forma_setup_successful",
              body: "`{ workspaceId }`: setup has finished and the SDK is ready.",
            },
            {
              term: "forma_setup_failed",
              body: "`{ workspaceId, code, message }`: setup did not complete. `code` is `missing_field`, `invalid_config`, `network_error` or `error_state`.",
            },
            {
              term: "forma_action_tracked",
              body: "`{ action }`: an action was tracked, by `track()` or on the page.",
            },
            { term: "forma_survey_shown", body: "`{ surveyId }`: a survey reached the screen." },
            {
              term: "forma_response_submitted",
              body: "`{ surveyId, responseId, finished }`: on the first answer (`finished: false`), and again when the finished response has been saved.",
            },
            { term: "forma_survey_closed", body: "`{ surveyId }`: the survey was dismissed or finished." },
          ],
        },
        debugging: {
          title: "Debugging and security",
          facts: [
            {
              term: "?formaDebug=true",
              body: "Add it to the page's URL: the SDK logs each step to the console with the `🧱 Forma` prefix, fetches the workspace state again instead of using its cache, and ignores a stored error state.",
            },
            {
              term: "setNonce(nonce)",
              body: "With a nonce-based Content Security Policy for styles, pass the nonce (without `nonce-`) before surveys render. Allow your Forma instance in `script-src` and `connect-src`.",
            },
            {
              term: "Caching",
              body: "The workspace state is cached, so changes to surveys and actions can take several minutes to reach a page that has already loaded the SDK. Debug mode skips the cache.",
            },
          ],
        },
      },
    },
    selfHosting: {
      meta: {
        title: "Self-host Forma · Docker, Helm",
        description:
          "Run Forma with Docker Compose, a one-click Ubuntu script or the Helm chart. Requirements and setup options.",
      },
      title: "Run Forma on your own infrastructure",
      sub: "One Go binary in a container, PostgreSQL, Valkey and SpiceDB. Pick the setup that fits your team.",
      optionsTitle: "Choose a setup",
      docker: {
        title: "Docker Compose",
        body: "Download the Compose file, create `.env`, run `docker compose up`.",
        link: "Compose file on GitHub →",
      },
      oneClick: {
        title: "One-click (Ubuntu)",
        body: "A shell script that sets up Forma with Traefik and HTTPS certificates.",
        link: "Install script on GitHub →",
      },
      kubernetes: {
        title: "Kubernetes",
        body: "The official Helm chart, with managed PostgreSQL, Redis and S3-compatible storage when you need them.",
        link: "Helm chart on GitHub →",
      },
      requirements: {
        title: "Requirements",
        body: "For evaluation: 2 vCPU, 4 GB memory, 20 GB SSD. For low load on one server: 4 vCPU, 8 GB, 50 GB SSD. HTTPS is required.",
      },
      signIn: {
        title: "Sign-in options",
        body: "Email and password with two-factor authentication, plus Google, Microsoft Entra ID, OpenID Connect and SAML single sign-on.",
      },
      ai: {
        title: "Optional AI",
        body: "AI features need a model endpoint you configure. Leave it unset and Forma runs without AI.",
      },
      source: {
        title: "Source code",
        body: "The full source is on GitHub.",
        link: "View on GitHub →",
      },
    },
    security: {
      meta: {
        title: "Security · Forma",
        description: "How to report a vulnerability and the account security features Forma provides.",
      },
      title: "Security",
      report: {
        title: "Report a vulnerability",
        body: "Email security@yldm.ai instead of opening a public issue. We do not run a bug bounty; public credit is available on request.",
      },
      account: {
        title: "Account security",
        body: "Two-factor authentication, organizations with teams and per-workspace roles, audit logging, and SSO on self-hosted instances.",
      },
      policyLink: "Read the full policy on GitHub →",
    },
  },
  notFound: {
    meta: { title: "Page not found · Forma", description: "The page may have moved." },
    title: "Page not found",
    body: "The page may have moved. If you were looking for the app, it now lives at /app.",
    home: "Home",
  },
};
