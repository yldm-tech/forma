# forma2 client API golden fixtures

Each `<case>.json` file is one HTTP exchange recorded against a running forma2 instance. They are the reference for the Go client API (`/api/v1/client/*`, `/api/v2/client/*`), the public survey pages (`/s/{id}`, `/p/{slug}`) and the SDK bundle under `/js/*`: for the request in a fixture, the rewrite must answer with the same status, the same recorded headers and a body that equals the recorded one once the placeholders below are matched.

## Source

- Image: `ghcr.io/yldm-tech/forma:latest`, digest `sha256:f26ba6bd0c01cbbb1d0207490f1f9af702b53b67a70745867707164230bbbd57` (linux/amd64), label `org.opencontainers.image.version=0.1.22`, built from `2f54f4df3ce7c7023627b5b4134be93abb645fa0`.
- That commit is in this repository's history, 15 commits before `3db326f` (the forma2 snapshot the rewrite ports from). Between the two, the only change under the client API and survey page code is the race-free contact creation in `modules/contacts/api/v1/client/[workspaceId]/user/lib/update-user.ts`, which does not change any response.
- Recorded on 2026-10-22.

## How they were produced

1. The stack from `docker/docker-compose.yml` at `3db326f` (PostgreSQL with pgvector, Valkey, the SpiceDB database bootstrap, `spicedb migrate`, SpiceDB, `forma-migrate`, `authzed-initialize` and the app; without the optional vLLM profile) ran in a throwaway directory with freshly generated secrets, `WEBAPP_URL=http://localhost:3999`, `EMAIL_VERIFICATION_DISABLED=1`, no rate-limit gateway (so the app applies the client API limits itself), no reCAPTCHA keys, and a dummy S3 configuration (`S3_ENDPOINT_URL=http://localhost:9000`, bucket `forma-uploads`, access key `contractfixtureaccess`, region `us-east-1`, path style). A presigned POST is signed locally, so no object store was running and nothing was uploaded.
2. Data was created through forma2's own surfaces: the first-user signup under `/setup` (which creates the organization, the default workspace and its `en-US` language), a workspace-scoped API key with `manage` permission from the organization settings, the `createLanguageAction` server action for `de-DE`, `POST /api/v1/management/action-classes` (code action `checkout`), `POST /api/v1/management/contact-attribute-keys` (`plan`), `POST /api/v3/surveys` for every survey, `POST /api/v3/surveys/{id}/archive`, `PUT /api/v1/management/surveys/{id}` for the slug and the single-use setting, `GET /api/v1/management/surveys/{id}/singleUseIds` for a single-use link, and the `createQuotaAction` server action for the quota. The only direct database write was setting the workspace's `legacyEnvironmentId` to `clegacyenvaaaaaaaaaaaaaa1`, because no forma2 surface assigns one (it is carried over from the environment rows of older installs).
3. The surveys:
   - `app`: in-progress app survey, one `openText` element `checkout_feedback`, ending `clendscreenappaaaaaaaaaa3`, display option `displayMultiple`, triggered by the `checkout` action class. It is the only survey in the environment state, because that state lists app surveys only.
   - `link`: in-progress link survey in `en-US` (default) and `de-DE`, blocks with `feedback` (required `openText`) and `score` (`rating`, number scale, range 5), hidden fields `utm_source` and `campaign`, endings `clendscreenokaaaaaaaaaaa1` and `clendscreenquotaaaaaaaaa2`, slug `contract-feedback`, and one quota: limit 1, `feedback equals "quota"`, action `endSurvey` on `clendscreenquotaaaaaaaaa2`, partial submissions counted.
   - `paused`, `archived` (archiving sets the status to `paused`), `singleUse` (encrypted single-use links) and `upload` (a `fileUpload` element `attachment` allowing `png` and `pdf` up to 5 MB): one-element link surveys.
4. Every exchange was sent with plain HTTP from the host in the order the files' scenarios need (identify before contact displays, display before the response that references it, the first quota response before the one that finds the quota full). Client API requests carry `Origin: https://customer.example` and a fixed desktop Chrome `User-Agent`. The rate-limit cases came last, apart from the two OG image cases, which were recorded after the limit window had passed: 101 requests within a minute from one IP exhaust the shared `api:client` limit (100 per minute), after which every client route including the environment state answers 429. `v1-storage-rate-limited` is the sixth storage request within a minute (`storage:upload`, 5 per minute per IP).
5. The raw exchanges were normalised (below) and the stack, its volumes, the images it pulled and the throwaway directory were removed.

## File format

```json
{
  "case": "file name without .json",
  "description": "what the exchange shows",
  "request": { "method": "...", "path": "...", "query": {}, "headers": {}, "body": null },
  "response": { "status": 200, "headers": {}, "bodyFormat": "json", "body": {} },
  "notes": {}
}
```

- `request.headers` keeps the headers that matter to the exchange: `origin`, `content-type`, `cache-control`, `accept`, `user-agent` and the `access-control-request-*` preflight headers. `request.body` is the JSON that was sent, or a string when the case sends a body that is not JSON.
- `response.headers` keeps `content-type`, `cache-control`, `vary`, `content-security-policy`, `x-frame-options`, `allow` and every `access-control-*` header, as forma2 sent them. Headers that are absent were not sent.
- `response.bodyFormat` says what `response.body` holds:
  - `json`: the parsed JSON body.
  - `html-head`: for HTML pages, only the document head: `title`, every `meta` tag except the charset as an attribute map, and every `link` that is not a stylesheet, preload or other asset hint. The page body is a Next.js render and is not part of the contract.
  - `png`: the image's dimensions; the bytes are not recorded.
  - `omitted`: the JavaScript bundle; only the headers are the contract.
  - `empty`: no body.
- `notes` appears only on the storage upload cases, where `decodedPolicy` is the base64 `Policy` field decoded and normalised, so the upload conditions (size range, content type, key) are visible.

## Placeholders

Values that differ on every run are replaced by placeholders that keep the JSON type (always a string where the original was a string) and, inside longer strings such as URLs and storage keys, the surrounding text.

| Placeholder | Stands for |
| --- | --- |
| `<id:workspace>` | The workspace id (a cuid). |
| `<id:survey:app>`, `<id:survey:link>`, `<id:survey:paused>`, `<id:survey:archived>`, `<id:survey:singleUse>`, `<id:survey:upload>` | The survey ids described above. |
| `<id:actionClass:checkout>` | The `checkout` code action class. |
| `<id:language:en-US>` | The workspace's default language row. |
| `<id:segment:app>` | The private segment forma2 creates for the app survey. |
| `<id:block:app>` | The block id the server generated for the app survey (a 24-character cuid2). |
| `<id:quota>` | The link survey's quota. |
| `<id:contact:user1>`, `<id:contact:user2>` | Contacts created by identifying `contract-user-1` and `contract-user-2`. |
| `<id:display:anonymous>`, `<id:display:contact>`, `<id:display:v1>`, `<id:display:anonymous-unused>` | Display ids returned by the display cases; `anonymous-unused` is a display created only to be referenced by `v2-responses-create-invalid-meta-action`. |
| `<id:response:...>` | Response ids returned by the response cases, named after the case that created them (`partial`, `finished`, `v1`, `contact`, `quota1`, `quota2`, `quota3`, `requiredOmitted`, `ratingOutOfRange`, `singleUse`). |
| `<id:singleUse>` | The canonical single-use id: the decrypted value of the link's `suId`, which the survey page hands to the runtime as `singleUseId`. |
| `<single-use-token>` | The encrypted `suId` query value of a single-use link (`<iv hex>%3A<ciphertext hex>%3A<tag hex>`). |
| `<timestamp>` | An ISO 8601 UTC timestamp, e.g. `2026-10-22T06:12:02.233Z` (the policy `expiration` has no milliseconds) (`expiresAt`, `createdAt`, `updatedAt`, the policy `expiration`). |
| `<uuid>` | A random v4 UUID (the `--fid--` suffix of an uploaded file name). |
| `<webapp-url>` | `WEBAPP_URL` (and `PUBLIC_URL`, which defaults to it): `http://localhost:3999` when recorded. |
| `<s3-endpoint>` | `S3_ENDPOINT_URL`: `http://localhost:9000` when recorded. With path-style addressing the presigned POST URL is `<s3-endpoint>/<bucket>`. |
| `<s3:policy>`, `<s3:signature>`, `<s3:amz-date>` | The presigned POST `Policy` (base64 JSON, decoded in `notes.decodedPolicy`), `X-Amz-Signature` (64 hex characters) and `X-Amz-Date` (`YYYYMMDDTHHMMSSZ`). |
| `<yyyymmdd>` | The date segment of `X-Amz-Credential`. |

Ids that start with `cl` and end in `aaa…` followed by a digit (for example `clendscreenokaaaaaaaaaaa1`, `clunknownsurveyaaaaaaaaa1`, `clegacyenvaaaaaaaaaaaaaa1`) were chosen by the recorder and are literal: ending card ids, the quota condition id, the legacy environment id, and ids that deliberately match nothing. Free-text values (`contract-user-1`, `user1@example.com`, answers, names) are literal too.

## Behaviour worth knowing when porting

- Because this workspace has a `legacyEnvironmentId`, the environment state's survey `segment` reports it as both `environmentId` and `workspaceId`.
- `v2-responses-create-required-element-omitted` and `v2-responses-create-rating-out-of-range` are accepted: forma2 only validates elements whose key is present in `data`, and does not check a rating against its range. `v2-responses-create-missing-required` shows the localized error for a required element sent empty.
- `meta.action: null` is rejected on response create (`v2-responses-create-invalid-meta-action`); the key must be a string or absent.
- `/s/{id}` (including the 404 for an unknown id) sends `frame-ancestors *` and no `X-Frame-Options`; `/p/{slug}` with an unknown slug falls through to the app's default `frame-ancestors 'self'` and `X-Frame-Options: SAMEORIGIN`.
- The image ships `/js/surveys.umd.cjs` but no `/js/forma.umd.cjs`, and an `OPTIONS` request to `/js/surveys.umd.cjs` returns a bare 500 from Next.js; neither is recorded as a contract.
