# Management API contract exchanges

Each `<case>.json` file is one HTTP exchange of the management APIs (`/api/v1/management/*`, `/api/v2/*` except `/api/v2/client/*`), the MCP endpoint (`/api/mcp`) and the OAuth discovery and registration surfaces MCP clients use (`/.well-known/*`, `/api/auth/jwks`, `/api/auth/oauth2/register`). They pin what integrations and customers' backends rely on: `contract_test.go` replays every request in the order its scenario needs and requires the same status, the same listed headers and a body that equals the expected one once the placeholders below are matched. A change to one of these answers is a change to a published API; edit the case together with the code and say why in the change.

## Scenario

`seedRecording` in `contract_test.go` creates what the first request expects: the organization `Contract Organization` with its owner `admin@example.com` (`Contract Admin`), the workspaces `My workspace` and `Other workspace`, and the API keys below. Steps whose name starts with `@` in `replayOrder` are setup that is not itself a case: contacts the client API would have identified (`contract-user-1` to `contract-user-3`), the public segment `Pro customers` (`plan equals pro`), the responses the client API would have created, and subjects the delete cases remove (a temporary survey, response, attribute key, team). Everything else is created by the cases themselves, in order.

The rate-limited cases fill the key's counter for the current window in Valkey (`fb:rate_limit:api:v1:<apiKeyId>:<windowStart>` = 100) and then send one request; `v1-storage-rate-limited` does the same with `storage:upload` at 5.

## API keys

Requests carry the key as `x-api-key` unless the case says `bearer` (then `Authorization: Bearer <key>`). Every key belongs to the one organization; `workspace` is `My workspace`, `workspace2` is `Other workspace`.

| Placeholder | Workspace permissions | Organization access (`accessControl`) |
| --- | --- | --- |
| `<api-key:manage>` | `manage` on `workspace` | read, write |
| `<api-key:write>` | `write` on `workspace` | none |
| `<api-key:read>` | `read` on `workspace` | read |
| `<api-key:other>` | `manage` on `workspace2` | none |
| `<api-key:orgonly>` | none | read, write |
| `<api-key:ratelimit>` | `read` on `workspace` | none |

`fbk_invalidcontractkeyaaaaaaaaaaaaaaaaaaaaaaaaa` and `fbk_querycredentialaaaaaaaaaaaaaaaaaaaaaaaaaaaa` are literal: well-formed keys that match nothing. The API keys were created by the organization owner, which matters to `POST /api/v2/organizations/{id}/users` (the key's creator role bounds the roles it may assign).

## File format

The same as the client API exchanges:

```json
{
  "case": "file name without .json",
  "description": "what the exchange shows",
  "request": { "method": "...", "path": "...", "query": {}, "headers": {}, "body": null },
  "response": { "status": 200, "headers": {}, "bodyFormat": "json", "body": {} },
  "notes": {}
}
```

- `request.headers` holds `origin`, `content-type`, `accept`, `x-api-key`, `authorization`, `mcp-protocol-version` and the `access-control-request-*` preflight headers. `request.body` is the JSON sent, or a string when the case sends a body that is not JSON. `request.query` values are strings as sent.
- `response.headers` lists the headers checked: `content-type`, `cache-control`, `vary`, `content-security-policy`, `x-frame-options`, `allow`, `www-authenticate`, `retry-after`, `location`, `mcp-session-id` and every `access-control-*` header. `Access-Control-Allow-Origin`, `X-Frame-Options`, `Allow` and `Cache-Control` must also be absent where a case does not list them. The global headers every response carries (`X-Content-Type-Options`, `Strict-Transport-Security`, `Referrer-Policy`, `Permissions-Policy`) are not listed per case.
- `response.bodyFormat`: `json` (the parsed body) or `empty` (no body).
- `notes` appears only where it adds something: `decodedPolicy` on the storage upload cases (the base64 `Policy` decoded and normalised), and `contactLinkToken` on the contact-link cases (the JWT header, the claim names and the lifetime, which the replay checks on every token in the answer).

## Placeholders

Values that differ on every run are placeholders that keep the surrounding text. They keep the JSON type of the value (a string where the value is a string), with one exception: `<unix-time>` stands for a JSON number. An id placeholder binds to the first id it meets and must name the same id wherever it appears again.

| Placeholder | Stands for |
| --- | --- |
| `<api-key:...>` | The API keys in the table above. |
| `<id:organization>`, `<id:workspace>`, `<id:workspace2>` | The organization and the two workspaces. |
| `<id:apiKey:...>` | API key row ids (they appear only where an answer echoes the key id). |
| `<id:user:admin>`, `<id:user:member>`, `<id:user:manager>` | The owner, and the users created by `v2-users-create` and `v2-users-create-inactive-manager`. |
| `<id:language:en-US>`, `<id:workspace2:language:en-US>` | The default language rows. |
| `<id:contactAttributeKey:<key>>`, `<id:workspace2:contactAttributeKey:<key>>` | Attribute keys by their `key`: the defaults (`userId`, `email`, `firstName`, `lastName`, `language`), and `plan`, `seats`, `company`, `signup_date`, `phone`, plus `temporary` and `temporary_v2`, which the delete cases remove. |
| `<id:contactAttribute:<contact>:<key>>` | Attribute value rows of a contact. |
| `<id:contact:user1>`, `<id:contact:user2>`, `<id:contact:user3>`, `<id:contact:jane>` | Contacts for `contract-user-1`, `contract-user-2`, `contract-user-3` (deleted by `v1-contacts-delete`) and `jane@example.com` (`contract-user-4`, created by `v2-contacts-create`). |
| `<id:actionClass:checkout>`, `<id:actionClass:pricing>`, `<id:actionClass:temporary>` | Action classes: the `checkout` code action, the `.pricing-cta` click action and the one the delete cases remove. |
| `<id:survey:link>`, `<id:survey:app>`, `<id:survey:singleUse>`, `<id:survey:other>`, `<id:survey:temporary>` | `Link Feedback Survey` (created from `questions`: required `openText` `feedback`, `rating` `score` range 5, hidden field `utm_source`, ending `clendscreenokaaaaaaaaaaa1`), `App Checkout Survey` (created from `blocks`, triggered by `checkout`), `Single Use Survey` (created through the `environmentId` alias), `Other Workspace Survey` (in `workspace2`) and the survey the delete cases remove. |
| `<id:block:<survey>:<n>>`, `<id:block:<n>>` | Block ids the server generates when it converts `questions` into blocks; `<id:block:1>` is the block regenerated when `v1-surveys-update-questions` replaces the questions of `survey:other`. |
| `<id:segment:app-private>`, `<id:segment:pro>` | The private segment created for the app survey, and the public `Pro customers` segment. |
| `<id:display:client>` | The display the client API would have created. |
| `<id:response:...>` | Responses, named after where they came from: `client` and `client-contact` (client API), `v1`, `v1-userid`, `v1-file-url`, `v2`, `v2-contact`, `v2-userid`, `v2-jane`, `v2-jane-userid`, and the `temporary` ones the delete cases remove. |
| `<id:webhook:contract>`, `<id:webhook:zapier>`, `<id:webhook:activepieces>` | Webhooks created by `v2-webhooks-create`, `v2-webhooks-create-environment-id` and `v2-webhooks-create-activepieces-source` (the step after it deletes the last one, so the later lists do not show it). |
| `<id:team:contract>`, `<id:team:temporary>` | Teams created by `v2-teams-create` and for `v2-teams-delete`. |
| `<timestamp>` | An ISO 8601 UTC timestamp the server generates (`createdAt`, `updatedAt`, `expiresAt`, the policy `expiration`). Timestamps a request sends (`createdAt` overrides, date filters) are literal. |
| `<uuid>` | A random UUID (the `--fid--` suffix of an uploaded file name). |
| `<webapp-url>` | `WEBAPP_URL` (and `PUBLIC_URL`). |
| `<s3-endpoint>`, `<s3:policy>`, `<s3:signature>`, `<s3:amz-date>`, `<yyyymmdd>` | The presigned POST values, exactly as in the client API exchanges. |
| `<sealed-single-use-id>` | An encrypted single-use id: base64url of the id sealed under `ENCRYPTION_KEY`, bound to its survey. |
| `<single-use-id>`, `<single-use-signature>` | An unencrypted single-use link: `suId` is a fresh cuid2 and `suToken` the hex HMAC-SHA256 of `forma.single-use.v1:<surveyId>:<suId>` keyed with `ENCRYPTION_KEY`. |
| `<contact-link-token>` | The JWT of a personal survey link (`/c/<token>`); its shape is in `notes.contactLinkToken`. |
| `<webhook-secret>` | The `whsec_...` signing secret, returned only by the create response. |
| `<oauth:client-id>`, `<unix-time>` | The registered OAuth client's `client_id` and `client_id_issued_at` (seconds, a JSON number). |
| `<jwk:x>`, `<jwk:kid>` | The public key and key id of the authorization server's signing key. |

Ids that start with `cl` and end in `aaa…` followed by a digit (`clendscreenokaaaaaaaaaaa1`, `clblockappaaaaaaaaaaaaaa1`, `clunknownsurveyaaaaaaaaa1`, ...) are literal: ending and block ids, and ids that deliberately match nothing. Free-text values (names, emails, answers) are literal too.

## Behaviour the cases pin

Authentication and authorization:

- v1 errors are `{"code", "message", "details"}`. A missing key, a key that matches nothing, and a key with no workspace permission on a workspace-scoped route all answer 401 `not_authenticated`; a valid key that lacks permission answers 401 `unauthorized` (not 403). Of the workspace-scoped routes only `GET /api/v1/management/surveys` accepts an organization-only key (it lists nothing for it).
- v2 errors are `{"error": {"code", "message", "details"}}`, and 401, 403 and 429 never carry `details`. A key without permission answers 401, except where the body names the workspace (`workspaceId`/`environmentId` in create bodies, resolved before the handler): that path answers 403 Forbidden.
- Permissions map from the HTTP method: GET needs `read`, POST and PUT need `write`, DELETE needs `manage` (every `*-delete-write-key` case is refused). `GET .../singleUseIds` needs `write`, because it is checked as POST.
- An API key is accepted as `x-api-key` or as `Authorization: Bearer fbk_...` everywhere except `GET /api/v1/management/me`, which reads only `x-api-key` and otherwise falls through to the session (401). `/me` answers 400 for a key that is not scoped to exactly one workspace, and reports the workspace id as the environment's `id`. `GET /api/v2/me` lists the workspace grants; its `environmentPermissions` is always empty.
- Organization routes (`/api/v2/organizations/{organizationId}/*`) answer 401 when the path names another organization, and need `accessControl.read` for GET and `accessControl.write` for writes. `GET /api/v2/me` needs `accessControl.read`.

Inputs and outputs:

- Body ids accept `workspaceId` or its alias `environmentId`. v1 survey, action class and response payloads carry `environmentId` (the workspace id) and `projectOverwrites` (an alias of `workspaceOverwrites`); the workspace-team routes accept `projectId` as an alias of `workspaceId`.
- v1 pagination differs by route: surveys read `limit`/`offset` (default and maximum 250), responses read `limit`/`skip`, both answer 400 `Invalid pagination parameters` with per-parameter details; contacts read `limit`/`skip` and silently ignore invalid values. v2 lists take `limit` (1–250, default 50), `skip`, `sortBy`, `order`, `startDate`, `endDate`, `filterDateField` and answer `meta: {total, limit, offset}`. Default attribute keys created together are listed in the order they were created.
- `POST /api/v1/management/contact-attribute-keys` requires `"type": "custom"`. The v2 create and update bodies require a `description` key (string or null). Default attribute keys cannot be updated or deleted; for `email` (default and unique) the default check wins.
- `POST /api/v2/management/responses` ignores `contactId` (the schema does not pick it); `userId` resolves the contact. `PUT /api/v2/management/responses/{id}` takes the whole response minus `id` and `surveyId`; a partial body is a 422, and a missing date reports `Invalid input: expected date, received Date`.
- `GET /api/v2/management/webhooks?surveyIds=...` always answers 422: the schema wants an array and a query string yields a string, so the filter cannot be used.
- `POST /api/v2/management/contacts` rejects attribute keys that do not exist (400, the issue text ends in `. `). A bulk contact upload answers 200 `Contacts bulk upload successful`, creating the attribute keys it names.
- A duplicate team name is 409, and deleting a workspace team that does not exist is 404.
- The webhook signing secret appears only in the create response. Webhook URLs that resolve to private addresses are refused with 400. Every webhook source, `activepieces` included, is accepted.
- `POST /api/v1/management/surveys` with `recaptcha.enabled` answers 403 when no reCAPTCHA keys are configured. No OpenAPI document is served at `/api/v2/openapi.json`; an unknown `/api` path answers the JSON 404.
- Rate limits are fixed 60-second windows aligned to the epoch, counted per API key: `api:v1` and `api:v2` 100 each, and `storage:upload` 5 for the management storage route, which counts instead of `api:v1`.
- CORS headers (`Access-Control-Allow-Origin: *` with the method and header lists) are per response, not per route: v2 sends them on 200 only (not on 201, 207 or errors); v1 sends them where the handler passes the CORS flag, which is the response create success and a scattering of 400/404 validation errors (every case shows which). A preflight (`OPTIONS`) answers 204 with `Allow: GET, HEAD, OPTIONS, POST` and no CORS headers.

OAuth and MCP:

- The authorization server metadata is served at `/.well-known/oauth-authorization-server`, at the issuer-suffixed `/.well-known/oauth-authorization-server/api/auth`, and the OpenID configuration at three paths; all carry `Cache-Control: public, max-age=15, stale-while-revalidate=15, stale-if-error=86400`. They advertise only what the server implements: no `client_credentials` grant, no `private_key_jwt` client authentication, no DPoP, back-channel logout or `claims` request parameter. The protected resource metadata is served at the root and at `/api/mcp`, and 404s (`{"error":"not_found"}`) for any other resource path. PKCE is `S256` only. JSON is labelled `application/json; charset=utf-8`, and each signing key names its use, `sig`.
- Dynamic client registration is open (no initial access token). It ignores the requested `scope` and registers the client for every supported scope, bound to the MCP resource (`resources`).
- `GET /api/mcp` answers 405. The MCP exchanges themselves (the 401 challenge, initialize, the tools and their input schemas) are tested in `internal/mcp`.
