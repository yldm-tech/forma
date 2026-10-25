# Forma JavaScript SDK

The SDK that shows Forma's website and in-app surveys on your own pages. Every Forma instance serves it at `/js/forma.umd.js` (and, for snippets installed before that name existed, `/js/forma.umd.cjs`). It is not published on npm: load it with the script tag below. The public reference, in English, Chinese and Japanese, is at [forma.yldm.ai/developers/sdk/](https://forma.yldm.ai/developers/sdk/).

## Install

Copy the snippet from your workspace in Forma, under **Integrations → Web & Mobile SDK**, which fills in your workspace ID and your instance's URL. It looks like this:

```html
<!-- START Forma Surveys -->
<script type="text/javascript">
  !(function () {
    var appUrl = "https://forma.yldm.ai";
    var workspaceId = "<your-workspace-id>";
    var f = (window.forma = window.forma || {});
    if (f.setup || f.q) return;
    f.q = [];
    var l;
    "setUserId setEmail setAttribute setAttributes setLanguage track logout registerRouteChange setNonce setEmbeddedData clearEmbeddedData on off"
      .split(" ")
      .forEach(function (m) {
        f[m] = function () {
          if (l) return l[m].apply(l, arguments);
          f.q.push([m, arguments]);
        };
      });
    var t = document.createElement("script");
    ((t.type = "text/javascript"), (t.async = !0), (t.src = appUrl + "/js/forma.umd.js"));
    t.onload = function () {
      var s = window.forma,
        i = /^(on|off|setNonce|setEmbeddedData|clearEmbeddedData)$/,
        r = function (p) {
          f.q.forEach(function (c) {
            if (i.test(c[0]) === p) s[c[0]].apply(s, c[1]);
          });
        };
      r(!0);
      s.setup({ workspaceId: workspaceId, appUrl: appUrl });
      l = s;
      r(!1);
      f.q.length = 0;
    };
    var e = document.getElementsByTagName("script")[0];
    e.parentNode.insertBefore(t, e);
  })();
</script>
<!-- END Forma Surveys -->
```

- `workspaceId` is the ID shown on that settings page. Older snippets passed `environmentId`, which still works but is deprecated.
- `appUrl` is your Forma instance: `https://forma.yldm.ai`, or your own domain if you self-host (`PUBLIC_URL` if your instance serves surveys on a separate domain, otherwise `WEBAPP_URL`).
- Paste it into the `<head>` of every page. With Google Tag Manager, paste it into a Custom HTML tag that fires on All Pages; the snippet is plain ES5 so that GTM accepts it.
- React and Vue: paste it into `index.html`. Next.js: render it with `next/script`:

```tsx
import Script from "next/script";

export default function FormaScript() {
  return (
    <Script id="forma-sdk" strategy="beforeInteractive">
      {`...the JavaScript between the <script> tags above...`}
    </Script>
  );
}
```

App Router: render `<FormaScript />` in the root `app/layout.tsx`. Pages Router: Next runs a `beforeInteractive` script only from `pages/_document.tsx`, and only a `<Script>` placed there directly, not one inside your own component, so put the `<Script>` element itself in the `<Head>` of `pages/_document.tsx`. `beforeInteractive` puts the snippet in the server HTML ahead of hydration, so `window.forma` exists before any component's effect runs; with `afterInteractive` it is injected from an effect, and a call from a component rendered before it throws. It needs no route hooks: the SDK watches the history API itself.

### Calls before the SDK has loaded

The snippet defines `window.forma` straight away as a small stub. Calls to the methods it lists are queued, not thrown, and replayed in order once the SDK has loaded: `on`, `off`, `setNonce`, `setEmbeddedData` and `clearEmbeddedData` before `setup`, everything else after it. A queued call returns nothing, so do not `await` it or keep the function `on()` returns until the SDK is there. Once the SDK has loaded, the stub's methods forward to it, so a reference to `window.forma` or to one of its methods kept from before then still works.

Without the snippet's stub (for example a plain `<script src=".../js/forma.umd.js">` and your own `setup()` call), call `setup()` first: a call that reaches the SDK before setup has finished is dropped with a console warning that names it, for example `🧱 Forma - Setup not complete, track("upgrade_clicked") dropped.` The same warning follows every call after a `setup()` that failed.

### TypeScript

The SDK is a global, so declare it once, for example in `src/forma.d.ts`. The methods are named rather than declared through an index signature, which `noUncheckedIndexedAccess` would make possibly undefined:

```ts
interface Window {
  forma: Record<
    | "setUserId"
    | "setEmail"
    | "setAttribute"
    | "setAttributes"
    | "setLanguage"
    | "track"
    | "logout"
    | "registerRouteChange"
    | "setNonce"
    | "setEmbeddedData"
    | "clearEmbeddedData"
    | "on"
    | "off",
    (...args: unknown[]) => unknown
  >;
}
```

## Identify users

Surveys that target a segment, or that should be shown only once per person, need to know who the visitor is. Anonymous visitors still see surveys whose audience is everyone.

```js
// After sign-in
window.forma.setUserId("user-123");
window.forma.setAttributes({ plan: "pro", seats: 30, signedUpAt: new Date("2025-04-01") });
window.forma.setLanguage("de");

// On sign-out
window.forma.logout();
```

- `setUserId(userId, options?)` identifies the visitor as the contact with that ID, creating it on first use. IDs are up to 255 characters. Setting a different ID first clears the previous user's state, as `logout()` does. `options.userHash` verifies the ID, see [Verify identities](#verify-identities).
- `setAttribute(key, value)` and `setAttributes(attributes)` set contact attributes, which segments filter on. Call `setUserId` first: attributes without a user are rejected with a console error. A string, a number or a `Date` (or an ISO 8601 date string) gives the attribute its type the first time it is written; later values must have the same type, otherwise the server does not store that value. Updates are batched and sent together. Attributes marked as protected in Forma are never written by the SDK, and the email only for a verified user; the console error names each attribute that was skipped.
- `setEmail(email)` sets the `email` attribute. Forma stores it only when the user was identified with a valid `userHash`.
- `setLanguage(language)` sets the language surveys are shown in: a language code (`de`) or an alias you configured for the workspace's languages. It works for anonymous visitors too; for an identified user it is stored as the `language` attribute.
- `logout()` forgets the user on this browser. Call it on sign-out, so the next person on the same device is not treated as the previous one.

## Verify identities

The workspace ID in the snippet is public, so anyone can call the SDK's API with any user ID. Forma therefore trusts the browser only so far:

- The SDK never writes an attribute marked as protected. Mark the attributes your own systems own (a plan, a role, an account tier) in the **Identity verification** card of the workspace's **Web & Mobile SDK** page in Forma (under Integrations), and set them through the Management API, a CSV import or the contact page.
- The SDK writes the email only for a verified user.
- The user state the SDK receives names neither the contact nor any survey by ID.

To verify users, generate the workspace's identity secret on the same page. It is shown once; store it on your server like any other secret and never send it to the browser. For each signed-in user, your server computes the HMAC-SHA256 of the user ID under the secret, in hex, and your page passes it to `setUserId`:

```js
// Node.js, on your server
import { createHmac } from "node:crypto";
const userHash = createHmac("sha256", process.env.FORMA_IDENTITY_SECRET).update(user.id).digest("hex");
```

```python
# Python, on your server
import hashlib, hmac
user_hash = hmac.new(FORMA_IDENTITY_SECRET.encode(), user.id.encode(), hashlib.sha256).hexdigest()
```

```js
// In the page, with the userHash your server rendered or returned
window.forma.setUserId(user.id, { userHash });
```

The SDK keeps the hash with the user and sends it with every identify, so pass it wherever you call `setUserId`. A hash that does not match is logged as a console error and the user is identified without verification.

Once every page sends a `userHash`, turn on **Require verified identities**. Forma then refuses an identify without a valid hash, so nobody can identify as one of your users or read their state. Displays and responses are attached to a contact only when they come from a verified identify, or from the contact's personal link; the rest, including link surveys opened with `?userId=`, are recorded without a contact. Regenerating the secret invalidates every hash computed with the old one at once: while verification is required, a browser that still holds a user with an old hash is logged out on its next sync, and its user is identified again by the next `setUserId` call that brings a hash made with the new secret.

## Track actions

```js
window.forma.track("upgrade_clicked");
window.forma.track("checkout_completed", { hiddenFields: { orderId: "A-1001", total: 129 } });
```

- `track(code, properties?)` fires the code action with that key (set up under **Actions** in Forma). Surveys triggered by it may then show. `hiddenFields` fills the survey's hidden fields, so the values are stored with the response; this option is deprecated and will be removed in a future version.
- Page views, URL actions and click actions defined in Forma without code are detected by the SDK.
- `setEmbeddedData(data)` attaches data to future responses without triggering anything: values merge into what is already set, last write wins per key, `{ key: null }` removes a key. Only fields the survey declares as ingested embedded data are kept. `clearEmbeddedData(key?)` removes one key, or all of them with no argument. Both are synchronous and send nothing.

### Single-page apps

The SDK patches `history.pushState` and `history.replaceState` and listens to `popstate` and `hashchange`, so client-side navigation counts as a page view on its own: React Router, Next.js, Vue Router and similar need nothing extra. `registerRouteChange()` re-evaluates the current URL by hand, for a router that changes the page some other way.

## Events

```js
const off = window.forma.on("forma_response_submitted", ({ surveyId, responseId, finished }) => {
  if (finished) analytics.track("Survey completed", { surveyId, responseId });
});
```

`on(event, handler)` subscribes and returns a function that unsubscribes; `off(event, handler)` does the same. Handlers can be registered before `setup()`.

| Event                      | Payload                               | When                                                                                                   |
| -------------------------- | ------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| `forma_setup_successful`   | `{ workspaceId }`                     | Setup has finished and the SDK is ready.                                                               |
| `forma_setup_failed`       | `{ workspaceId, code, message }`      | Setup did not complete. `code` is `missing_field`, `invalid_config`, `network_error` or `error_state`. |
| `forma_action_tracked`     | `{ action }`                          | An action was tracked, by `track()` or detected on the page.                                           |
| `forma_survey_shown`       | `{ surveyId }`                        | A survey reached the screen.                                                                           |
| `forma_response_submitted` | `{ surveyId, responseId?, finished }` | On the first answer (`finished: false`), and again when the finished response has been saved (`true`). |
| `forma_survey_closed`      | `{ surveyId }`                        | The survey was dismissed or finished.                                                                  |

The same events are pushed to `window.dataLayer` for Google Tag Manager, as `{ event: "<name>", forma: { ... } }` with every payload key present (`null` where the event does not set it). Payloads carry IDs only, never answers.

## Content Security Policy

The SDK and the survey runtime load from your Forma instance and call its API, so allow `appUrl` in `script-src` and `connect-src`. Surveys add inline styles; with a nonce-based `style-src`, pass the nonce (without the `nonce-` prefix) before surveys render:

```js
window.forma.setNonce("<nonce>");
```

## Debugging

Add `?formaDebug=true` to the page's URL. The SDK then logs each step to the console with the `🧱 Forma` prefix, fetches the workspace state again instead of using its cache, and ignores a stored error state. Because the workspace state is cached, changes to surveys and actions in Forma can take several minutes to reach a page that has already loaded the SDK; debug mode skips that cache.

To find out why a survey does not show, read what debug mode logs about it. The state names surveys by id, which is the id in the survey's URL in Forma.

- Every survey the user cannot be shown is logged as `Survey <id> left out: <reason>` whenever the SDK works out which surveys can show (at setup, after `setUserId()` or an attribute change, after each display and close). The reasons: `shown once already` (Show only once), `already responded`, `shown 2/3 times` (the display limit), `workspace cooldown 7d, last survey shown 2d ago` or `survey cooldown ...` (the cooldown counts from the last display of any survey), `segment needs setUserId()` (an anonymous visitor never gets a targeted survey), and `user not in its segment`.
- When an action is tracked, each survey it triggers that is left out is logged again with its reason, and an action no survey uses says so: `No survey uses action "checkout"`.
- At the moment a triggered survey would show, it can still be left out because the user is outside its "show to X% of users" sample (fixed per user), the survey has no translation for the language set with `setLanguage()`, or another survey is on screen.
- The workspace state lists only the 100 newest in-progress app surveys. When there are more, debug mode logs how many older ones are left out, and the survey list in Forma names them.

A survey set to "Ignore Cooldown Period" that shows on every page view restarts the workspace cooldown each time it shows, so surveys that use the cooldown may never show; the survey editor warns about it.

## Development

`pnpm --filter @forma/js-core build` writes the UMD bundle and copies it to `apps/web/public/js/` as both `forma.umd.js` and `forma.umd.cjs`; the Go server embeds and serves them at `/js/*`. `pnpm --filter @forma/js-core test` runs the tests.
