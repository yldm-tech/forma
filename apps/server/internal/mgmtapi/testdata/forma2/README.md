# forma2 management API golden fixtures

Each `<case>.json` file is one HTTP exchange recorded against a running forma2 instance. They are the reference for the Go management APIs (`/api/v1/management/*`, `/api/v2/*` except `/api/v2/client/*`), the MCP endpoint (`/api/mcp`) and the OAuth discovery and registration surfaces MCP clients use (`/.well-known/*`, `/api/auth/jwks`, `/api/auth/oauth2/register`): for the request in a fixture, the rewrite must answer with the same status, the same recorded headers and a body that equals the recorded one once the placeholders below are matched. Fixtures with `notes.knownDefect` record a forma2 bug instead of a contract; the note says what forma2 meant to answer, and the rewrite should answer that.

## Source

- Image: `ghcr.io/yldm-tech/forma:latest`, digest `sha256:f26ba6bd0c01cbbb1d0207490f1f9af702b53b67a70745867707164230bbbd57` (linux/amd64, run under emulation on arm64), label `org.opencontainers.image.version=0.1.22`, built from `2f54f4df3ce7c7023627b5b4134be93abb645fa0`. This is the same image the client API fixtures in `server/internal/clientapi/testdata/forma2` were recorded from.
- That commit is 15 commits before `3db326f`, the forma2 snapshot the rewrite ports from. Between the two, the code behind these routes changed in four places, none of which changes a recorded response except the webhook source: `POST /api/v2/management/responses` no longer loads the organization's billing row (no response change); the webhook `source` enum is derived from the Prisma enum and so also accepts `activepieces`, which this image rejects (`v2-webhooks-create-activepieces-source`: 422 here, 201 at `3db326f`); `lastLoginAt` became nullable in the v2 user schema (an input-schema change only); and the MCP survey tools gained `archive_survey` and `restore_survey` (MCP could not be recorded, see below).
- Recorded on 2026-10-22 (UTC).

## How they were produced

1. The stack from `docker/docker-compose.yml` at `3db326f` (PostgreSQL with pgvector, Valkey, the SpiceDB database bootstrap, `spicedb migrate`, SpiceDB, `forma-migrate`, `authzed-initialize` and the app; without the optional vLLM profile) ran in a throwaway directory with freshly generated secrets, `WEBAPP_URL=http://localhost:3999`, `EMAIL_VERIFICATION_DISABLED=1`, no rate-limit gateway (so the app applies the API limits itself), and the same dummy S3 configuration as the client API recording (`S3_ENDPOINT_URL=http://localhost:9000`, bucket `forma-uploads`, access key `contractfixtureaccess`, region `us-east-1`, path style). Presigned POSTs are signed locally; no object store ran.
2. Data was created through forma2's own surfaces: the first-user signup under `/setup` (organization, workspace `My workspace` and its `en-US` language), the `createWorkspaceAction` server action for a second workspace `Other workspace`, the `createApiKeyAction` server action for the API keys below, the client API (`POST /api/v2/client/{workspaceId}/user` for contacts `contract-user-1`, `contract-user-2` and `contract-user-3`, a display and two responses), the `createSegmentAction` server action for the public segment `Pro customers` (`plan equals pro`), and otherwise the management APIs themselves, in the order the cases need. The only direct database write was setting the first workspace's `legacyEnvironmentId` to `clegacyenvaaaaaaaaaaaaaa1`, as in the client API recording; the second workspace has none, so both shapes of `environmentId` are covered.
3. Every exchange was sent with plain HTTP from the host. Requests that create something the recorder needed only as a fixture subject (a temporary survey, response, attribute key, team, webhook to delete) were sent but not kept as fixtures.
4. The rate-limited cases were not reached by brute force: this image verifies the bcrypt hash of the API key on every request and answers about one request per second under emulation, so 100 requests do not fit in one window. Instead the key's counter for the current window was set to the limit in Valkey (`fb:rate_limit:api:v1:<apiKeyId>:<windowStart>` = 100), then one request was sent. `v1-storage-rate-limited` was reached with real requests (`storage:upload`, 5 per minute per key).
5. The raw exchanges were normalised (below); then the stack, its volumes, the images it pulled, the throwaway directory and every file holding generated keys, passwords or cookies were removed.

## API keys

Requests carry the key as `x-api-key` unless the case says `bearer` (then `Authorization: Bearer <key>`). Every key belongs to the one organization; `workspace` is `My workspace` (with the legacy environment id), `workspace2` is `Other workspace`.

| Placeholder | Workspace permissions | Organization access (`accessControl`) |
| --- | --- | --- |
| `<api-key:manage>` | `manage` on `workspace` | read, write |
| `<api-key:write>` | `write` on `workspace` | none |
| `<api-key:read>` | `read` on `workspace` | read |
| `<api-key:other>` | `manage` on `workspace2` | none |
| `<api-key:orgonly>` | none | read, write |
| `<api-key:ratelimit>` | `read` on `workspace` | none |

`fbk_invalidcontractkeyaaaaaaaaaaaaaaaaaaaaaaaaa` and `fbk_querycredentialaaaaaaaaaaaaaaaaaaaaaaaaaaaa` are literal: well-formed v2 keys that match nothing. The API keys were created by the admin user `admin@example.com`, an organization owner, which matters to `POST /api/v2/organizations/{id}/users` (the key's creator role bounds the roles it may assign).

## File format

The same as the client API fixtures:

```json
{
  "case": "file name without .json",
  "description": "what the exchange shows",
  "request": { "method": "...", "path": "...", "query": {}, "headers": {}, "body": null },
  "response": { "status": 200, "headers": {}, "bodyFormat": "json", "body": {} },
  "notes": {}
}
```

- `request.headers` keeps `origin`, `content-type`, `accept`, `x-api-key`, `authorization`, `mcp-protocol-version` and the `access-control-request-*` preflight headers. `request.body` is the JSON that was sent, or a string when the case sends a body that is not JSON. `request.query` values are strings as sent.
- `response.headers` keeps `content-type`, `cache-control`, `vary`, `content-security-policy`, `x-frame-options`, `allow`, `www-authenticate`, `retry-after`, `location`, `mcp-session-id` and every `access-control-*` header, as forma2 sent them; headers that are absent were not sent. forma2 also sends `X-Content-Type-Options: nosniff`, `Strict-Transport-Security`, `Referrer-Policy` and `Permissions-Policy` on every response; they are global app headers and are not recorded per case.
- `response.bodyFormat`: `json` (the parsed body), `empty` (no body) or `omitted` (an HTML page that is not part of the contract, only in `v2-openapi-not-served`).
- `notes` appears only where it adds something: `decodedPolicy` on the storage upload cases (the base64 `Policy` decoded and normalised), `contactLinkToken` on the contact-link cases (the JWT header, the claim names and the lifetime), and `knownDefect` on the defect cases.

## Placeholders

Values that differ on every run are replaced by placeholders that keep the surrounding text. They keep the JSON type of the original value (a string where the original was a string), with one exception: `<unix-time>` stands for a JSON number.

| Placeholder | Stands for |
| --- | --- |
| `<api-key:...>` | The API keys in the table above. |
| `<id:organization>`, `<id:workspace>`, `<id:workspace2>` | The organization and the two workspaces. |
| `<id:apiKey:...>` | API key row ids (they appear only where forma2 echoes the key id). |
| `<id:user:admin>`, `<id:user:member>`, `<id:user:manager>` | The admin who signed up, and the users created by `v2-users-create` and `v2-users-create-inactive-manager`. |
| `<id:language:en-US>`, `<id:workspace2:language:en-US>` | The default language rows. |
| `<id:contactAttributeKey:<key>>`, `<id:workspace2:contactAttributeKey:<key>>` | Attribute keys by their `key`: the defaults (`userId`, `email`, `firstName`, `lastName`, `language`), and `plan`, `seats`, `company`, `signup_date`, `phone`, plus `temporary` and `temporary_v2`, which the delete cases remove. |
| `<id:contactAttribute:<contact>:<key>>` | Attribute value rows of a contact. |
| `<id:contact:user1>`, `<id:contact:user2>`, `<id:contact:user3>`, `<id:contact:jane>` | Contacts for `contract-user-1`, `contract-user-2`, `contract-user-3` (deleted by `v1-contacts-delete`) and `jane@example.com` (`contract-user-4`, created by `v2-contacts-create`). |
| `<id:actionClass:checkout>`, `<id:actionClass:pricing>`, `<id:actionClass:temporary>` | Action classes: the `checkout` code action, the `.pricing-cta` click action and the one the delete cases remove. |
| `<id:survey:link>`, `<id:survey:app>`, `<id:survey:singleUse>`, `<id:survey:other>`, `<id:survey:temporary>` | `Link Feedback Survey` (created from legacy `questions`: required `openText` `feedback`, `rating` `score` range 5, hidden field `utm_source`, ending `clendscreenokaaaaaaaaaaa1`), `App Checkout Survey` (created from `blocks`, triggered by `checkout`), `Single Use Survey` (created through the legacy `environmentId`), `Other Workspace Survey` (in `workspace2`) and the survey the delete cases remove. |
| `<id:block:<survey>:<n>>`, `<id:block:<n>>` | Block ids the server generated when it converted `questions` into blocks; `<id:block:1>` is the block regenerated when `v1-surveys-update-questions` replaced the questions of `survey:other`. |
| `<id:segment:app-private>`, `<id:segment:pro>` | The private segment forma2 creates for the app survey, and the public `Pro customers` segment. |
| `<id:display:client>` | The display created through the client API. |
| `<id:response:...>` | Responses, named after where they came from: `client` and `client-contact` (client API), `v1`, `v1-userid`, `v1-file-url`, `v2`, `v2-contact`, `v2-userid`, `v2-jane`, `v2-jane-userid`, and the `temporary` ones the delete cases remove. |
| `<id:webhook:contract>`, `<id:webhook:zapier>` | Webhooks created by `v2-webhooks-create` and `v2-webhooks-create-legacy-environment-id`. |
| `<id:team:contract>`, `<id:team:temporary>` | Teams created by `v2-teams-create` and for `v2-teams-delete`. |
| `<timestamp>` | An ISO 8601 UTC timestamp the server generated (`createdAt`, `updatedAt`, `expiresAt`, the policy `expiration`). Timestamps the recorder sent (`createdAt` overrides, date filters) are literal. |
| `<uuid>` | A random v4 UUID (the `--fid--` suffix of an uploaded file name). |
| `<webapp-url>` | `WEBAPP_URL` (and `PUBLIC_URL`): `http://localhost:3999` when recorded. |
| `<s3-endpoint>`, `<s3:policy>`, `<s3:signature>`, `<s3:amz-date>`, `<yyyymmdd>` | The presigned POST values, exactly as in the client API fixtures. |
| `<single-use-token>` | An encrypted single-use id, `<iv hex>%3A<ciphertext hex>%3A<tag hex>`. |
| `<single-use-id>`, `<single-use-signature>` | An unencrypted single-use link: `suId` is a fresh cuid2 and `suToken` the hex HMAC-SHA256 of `forma.single-use.v1:<surveyId>:<suId>` keyed with `ENCRYPTION_KEY`. |
| `<contact-link-token>` | The JWT of a personal survey link (`/c/<token>`); its shape is in `notes.contactLinkToken`. |
| `<webhook-secret>` | The `whsec_...` signing secret, returned only by the create response. |
| `<oauth:client-id>`, `<unix-time>` | The registered OAuth client's `client_id` and `client_id_issued_at` (seconds, a JSON number). |
| `<jwk:x>`, `<jwk:kid>` | The public key and key id of the authorization server's signing key. |

Ids that start with `cl` and end in `aaa…` followed by a digit (`clendscreenokaaaaaaaaaaa1`, `clblockappaaaaaaaaaaaaaa1`, `clegacyenvaaaaaaaaaaaaaa1`, `clunknownsurveyaaaaaaaaa1`, ...) were chosen by the recorder and are literal: ending and block ids, the legacy environment id, and ids that deliberately match nothing. Free-text values (names, emails, answers) are literal too.

## Behaviour worth knowing when porting

Authentication and authorization:

- v1 errors are `{"code", "message", "details"}`. A missing key, a key that matches nothing, and a key with no workspace permission on a workspace-scoped route all answer 401 `not_authenticated`; a valid key that lacks permission answers 401 `unauthorized` (not 403). Of the workspace-scoped routes only `GET /api/v1/management/surveys` accepts an organization-only key (it lists nothing for it).
- v2 errors are `{"error": {"code", "message", "details"}}`, and 401, 403 and 429 never carry `details`, even where the handler passes some. A key without permission answers 401, except where the body names the workspace (`workspaceId`/`environmentId` in create bodies, resolved before the handler): that path answers 403 Forbidden.
- Permissions map from the HTTP method: GET needs `read`, POST and PUT need `write`, DELETE needs `manage` (every `*-delete-write-key` case is refused). `GET .../singleUseIds` needs `write`, because it is checked as POST.
- An API key is accepted as `x-api-key` or as `Authorization: Bearer fbk_...` everywhere except `GET /api/v1/management/me`, which reads only `x-api-key` and otherwise falls through to the session (401). `/me` answers 400 for a key that is not scoped to exactly one workspace, and reports `id` as the legacy environment id when there is one.
- Organization routes (`/api/v2/organizations/{organizationId}/*`) answer 401 when the path names another organization, and need `accessControl.read` for GET and `accessControl.write` for writes. `GET /api/v2/me` needs `accessControl.read`.

Inputs and outputs:

- Body ids accept `workspaceId` or the legacy `environmentId`; the legacy id resolves to the workspace. v1 survey, action class and response payloads gain `environmentId` (the legacy id, else the workspace id) and `projectOverwrites` (an alias of `workspaceOverwrites`).
- v1 pagination differs by route: surveys read `limit`/`offset` (default and maximum 250), responses read `limit`/`skip`, both answer 400 `Invalid pagination parameters` with per-parameter details; contacts read `limit`/`skip` and silently ignore invalid values. v2 lists take `limit` (1–250, default 50), `skip`, `sortBy`, `order`, `startDate`, `endDate`, `filterDateField` and answer `meta: {total, limit, offset}`.
- `POST /api/v1/management/contact-attribute-keys` requires `"type": "custom"`. The v2 create and update bodies require a `description` key (string or null). Default attribute keys cannot be updated or deleted; for `email` (default and unique) the default check wins.
- `POST /api/v2/management/responses` ignores `contactId` (the schema does not pick it); `userId` resolves the contact. `PUT /api/v2/management/responses/{id}` takes the whole response minus `id` and `surveyId`; a partial body is a 422, and a missing date reports `Invalid input: expected date, received Date`.
- `GET /api/v2/management/webhooks?surveyIds=...` always answers 422: the schema wants an array and a query string yields a string, so the filter cannot be used.
- `POST /api/v2/management/contacts` rejects attribute keys that do not exist (400, the issue text ends in `. `).
- The webhook signing secret appears only in the create response. Webhook URLs that resolve to private addresses are refused with 400.
- `POST /api/v1/management/surveys` with `recaptcha.enabled` answers 403 because no reCAPTCHA keys were configured.
- Rate limits are fixed 60-second windows aligned to the epoch, counted per API key: `api:v1` and `api:v2` 100 each, and `storage:upload` 5 for the management storage route, which counts instead of `api:v1`.
- CORS headers (`Access-Control-Allow-Origin: *` with the method and header lists) are per response, not per route: v2 sends them on 200 only (not on 201, 207 or errors); v1 sends them where the handler passes the CORS flag, which is the response create success and a scattering of 400/404 validation errors (every fixture shows which). A preflight (`OPTIONS`) answers 204 with `Allow: GET, HEAD, OPTIONS, POST` and no CORS headers.

OAuth and MCP:

- The authorization server metadata is served at `/.well-known/oauth-authorization-server`, at the issuer-suffixed `/.well-known/oauth-authorization-server/api/auth`, and the OpenID configuration at three paths; all carry `Cache-Control: public, max-age=15, stale-while-revalidate=15, stale-if-error=86400`. The protected resource metadata is served at the root and at `/api/mcp`, and 404s (`{"error":"not_found"}`) for any other resource path. PKCE is `S256` only.
- Dynamic client registration is open (no initial access token). It ignores the requested `scope` and registers the client for every supported scope, bound to the MCP resource (`resources`).
- `/api/mcp` cannot be recorded from this image: every POST, authenticated or not, crashes with a bare 500 before authentication (see the `knownDefect` notes on the `mcp-*` cases); GET answers 405. The MCP contract (the 401 challenge, initialize, tool names and input schemas) has to come from forma2's source at `3db326f`: `modules/mcp/auth.ts`, `modules/mcp/server.ts`, `modules/mcp/tools/*.ts`.

Other forma2 defects recorded here, all marked with `notes.knownDefect`: every bulk contact upload that writes a contact fails with 500 (`v2-contacts-bulk*`), a duplicate team name is a 500 (`v2-teams-create-duplicate`), and deleting a workspace team that does not exist is a 500 (`v2-workspace-teams-delete-unknown`).
