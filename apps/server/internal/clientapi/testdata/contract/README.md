# Client API contract exchanges

Each `<case>.json` file is one HTTP exchange of what the SDK (`packages/js-core`), the survey runtime (`packages/surveys`) and respondents' browsers call: the client API (`/api/v1/client/*`, `/api/v2/client/*`), the public survey pages (`/s/{id}`, `/p/{slug}`), the OG image and the bundles under `/js/*`. `contract_test.go` replays the client API cases in the order their scenarios need, and `internal/linksurvey/contract_test.go` the page and OG image cases; both require the same status, the same listed headers and a body that equals the expected one once the placeholders below are matched. These answers are what SDK snippets already installed on customer sites decode, so a change to one is a change to a published contract; edit the case together with the code and say why in the change.

## Scenario

`seedRecording` in `contract_test.go` creates what the first request expects: a workspace with its `en-US` (default) and `de-DE` languages, the code action class `checkout`, the attribute key `plan`, and these surveys:

- `app`: in-progress app survey, one `openText` element `checkout_feedback`, ending `clendscreenappaaaaaaaaaa3`, display option `displayMultiple`, triggered by the `checkout` action class, with its private segment. It is the only survey in the environment state, because that state lists app surveys only.
- `link`: in-progress link survey in `en-US` (default) and `de-DE`, blocks with `feedback` (required `openText`) and `score` (`rating`, number scale, range 5), hidden fields `utm_source` and `campaign`, endings `clendscreenokaaaaaaaaaaa1` and `clendscreenquotaaaaaaaaa2`, slug `contract-feedback`, and one quota: limit 1, `feedback equals "quota"`, action `endSurvey` on `clendscreenquotaaaaaaaaa2`, partial submissions counted.
- `paused`, `archived` (archived and paused), `singleUse` (encrypted single-use links) and `upload` (a `fileUpload` element `attachment` allowing `png` and `pdf` up to 5 MB): one-element link surveys.
- `verified`: a link survey with one `openText` element `q1` that requires email verification.

The cases run in the order `replayOrder` gives (identify before a contact's displays, a display before the response that references it, the first quota response before the one that finds the quota full). Client API requests carry `Origin: https://customer.example` and a fixed desktop Chrome `User-Agent`. The rate-limit cases come last: 101 requests within a minute from one address exhaust the shared `api:client` limit (100 per minute), after which every client route including the environment state answers 429. `v1-storage-rate-limited` is the sixth storage request within a minute (`storage:upload`, 5 per minute per address). Storage uploads are presigned against a dummy S3 configuration (`S3_ENDPOINT_URL=http://localhost:9000`, bucket `forma-uploads`, access key `contractfixtureaccess`, region `us-east-1`, path style); nothing is uploaded. The cases in `putReplayOrder` (`v1-storage-upload-init-put`, `v2-storage-upload-init-put`, `v1-storage-upload-put-without-file-size`) are replayed by `TestContractExchangesPutUploads` against a second server on the same scenario whose store is configured with `STORAGE_UPLOAD_METHOD=put`; every other case runs against the default, `post`.

### Turnstile instance

The cases whose name contains `-turnstile` are replayed by `TestContractExchangesTurnstile`, in its own order, against a second instance whose spam protection provider is Cloudflare Turnstile (`TURNSTILE_SITE_KEY` is Cloudflare's always-pass test key `1x00000000000000000000AA`). It seeds the same data and one more survey, `spamProtected` (`<id:survey:spamProtected>`): an in-progress link survey with spam protection on (`recaptcha.enabled`) and one optional `openText` element `q1`. A stand-in for Cloudflare's siteverify accepts the token `turnstile-pass`, fails with Cloudflare's `internal-error` for `turnstile-down` and refuses every other token. The responses these cases create are `<id:response:turnstile>` and `<id:response:turnstileOff>`.

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

- `request.headers` holds the headers that matter to the exchange: `origin`, `content-type`, `cache-control`, `accept`, `user-agent` and the `access-control-request-*` preflight headers. `request.body` is the JSON sent, or a string when the case sends a body that is not JSON.
- `response.headers` lists the headers checked: `content-type`, `cache-control`, `vary`, `content-security-policy`, `x-frame-options`, `allow` and every `access-control-*` header. `Access-Control-Allow-Origin`, `X-Frame-Options` and `Allow` must also be absent where a case does not list them.
- `response.bodyFormat` says what `response.body` holds:
  - `json`: the parsed JSON body.
  - `html-head`: for HTML pages, only the document head: `title`, every `meta` tag except the charset as an attribute map, and every `link` that is not a stylesheet, preload or other asset hint. The page body is the survey runtime's and is not part of the contract.
  - `png`: the image's dimensions.
  - `omitted`: the JavaScript bundle; only the headers are the contract.
  - `empty`: no body.
- `notes` appears only on the storage upload cases, where `decodedPolicy` is the base64 `Policy` field decoded and normalised, so the upload conditions (size range, content type, key) are visible, and `signedHeaders` lists the headers a presigned PUT (`uploadUrl`) is signed with and their values.

## Placeholders

Values that differ on every run are placeholders that keep the JSON type (always a string where the value is a string) and, inside longer strings such as URLs and storage keys, the surrounding text. An id or contact token placeholder binds to the first value it meets and must name the same value wherever it appears again.

| Placeholder | Stands for |
| --- | --- |
| `<id:workspace>` | The workspace id. |
| `<id:survey:app>`, `<id:survey:link>`, `<id:survey:paused>`, `<id:survey:archived>`, `<id:survey:singleUse>`, `<id:survey:upload>`, `<id:survey:verified>` | The survey ids described above. |
| `<id:actionClass:checkout>` | The `checkout` code action class. |
| `<id:language:en-US>` | The workspace's default language row. |
| `<id:segment:app>` | The private segment of the app survey. |
| `<id:block:app>` | The app survey's block id (a 24-character cuid2). |
| `<id:quota>` | The link survey's quota. |
| `<contact-token:user1>`, `<contact-token:user2>` | The opaque tokens the client API names the contacts created by identifying `contract-user-1` and `contract-user-2` by, in place of their ids (`ct1.` and base64url). A contact always gets the same token, and the display and response routes accept it as `contactId`. |
| `<id:display:anonymous>`, `<id:display:contact>`, `<id:display:v1>`, `<id:display:anonymous-unused>` | Display ids returned by the display cases; `anonymous-unused` is a display created only to be referenced by `v2-responses-create-invalid-meta-action`. |
| `<id:response:...>` | Response ids returned by the response cases, named after the case that created them (`partial`, `finished`, `v1`, `contact`, `quota1`, `quota2`, `quota3`, `requiredOmitted`, `ratingOutOfRange`, `singleUse`, `verified`). |
| `<id:singleUse>` | The canonical single-use id: the decrypted value of the link's `suId`, which the survey page hands to the runtime as `singleUseId`. |
| `<verify-token:a>`, `<verify-token:b>` | Email verification tokens for the `verified` survey proving `a@example.com` and `b@example.com`, as the verification email's link carries them in `verify`: a JWT signed with the email verification secret whose address is encrypted under `ENCRYPTION_KEY`. |
| `<single-use-token>` | The encrypted `suId` query value of a single-use link: base64url of the id sealed under `ENCRYPTION_KEY`, bound to its survey. |
| `<timestamp>` | An ISO 8601 UTC timestamp, e.g. `2026-10-22T06:12:02.233Z` (`expiresAt`, `createdAt`, `updatedAt`; the policy `expiration` has no milliseconds). |
| `<uuid>` | A random UUID (the `--fid--` suffix of an uploaded file name). |
| `<webapp-url>` | `WEBAPP_URL` (and `PUBLIC_URL`, which defaults to it). |
| `<s3-endpoint>` | `S3_ENDPOINT_URL`. With path-style addressing the presigned POST URL is `<s3-endpoint>/<bucket>`. |
| `<s3:policy>`, `<s3:signature>`, `<s3:amz-date>` | The presigned POST `Policy` (base64 JSON, decoded in `notes.decodedPolicy`), `X-Amz-Signature` (64 hex characters) and `X-Amz-Date` (`YYYYMMDDTHHMMSSZ`), the last two also as query values of a presigned PUT's `uploadUrl`. |
| `<yyyymmdd>` | The date segment of `X-Amz-Credential`. |

Ids that start with `cl` and end in `aaa…` followed by a digit (for example `clendscreenokaaaaaaaaaaa1`, `clunknownsurveyaaaaaaaaa1`) are literal: ending card ids, the quota condition id, and ids that deliberately match nothing. Free-text values (`contract-user-1`, `user1@example.com`, answers, names) are literal too.

## Behaviour the cases pin

- The identify state and the display creates never name a contact by its id, only by its token, and the state's `displays` and `responses` hold survey keys (`sdkidentity.SurveyKey`), never survey ids. The SDK writes `email` only on a verified identify (`v2-user-attributes-update` skips it with an error); the identity verification rules themselves are covered by `identity_test.go`, since the contract's workspace has no identity secret.
- The environment state's survey `segment` reports the workspace id as both `environmentId` and `workspaceId`.
- `v2-responses-create-required-element-omitted` and `v2-responses-create-rating-out-of-range` are accepted: only elements whose key is present in `data` are validated, and a rating is not checked against its range. `v2-responses-create-missing-required` shows the error for a required element sent empty, in the survey's default language for a response in `default`.
- The response update takes an optional `verifyToken`, added for NF-01 (a second verified respondent going on with the first one's saved progress in a shared browser): on a survey that requires email verification, a token that proves another address than the one the response was created with is refused with 403 (`v1-responses-update-verified-email-other-address`), while an update without one is taken as before (`v1-responses-update-verified-email-no-token`).
- Response create takes an optional `turnstileToken`, added for Turnstile spam protection. On a Turnstile instance a survey with spam protection on needs one Cloudflare accepts (`v2-responses-create-turnstile`); without one, or with one Cloudflare refuses, the create is refused with 400 and `details.code` `recaptcha_verification_failed`, the code the survey runtime already shows its spam protection error for; while Cloudflare gives no verdict it is refused with 503 `service_unavailable` (`details.code` `spam_protection_unavailable`) instead of being taken unchecked. The update that finishes the response needs no token (`v1-responses-update-turnstile-no-token`), and a survey with spam protection off needs none either (`v2-responses-create-turnstile-off`). The environment state then names `turnstileSiteKey` in place of `recaptchaSiteKey` (`v2-environment-turnstile`).
- `meta.action: null` is rejected on response create (`v2-responses-create-invalid-meta-action`); the key must be a string or absent.
- `/s/{id}` (including the 404 for an unknown id) sends `frame-ancestors *` and no `X-Frame-Options`; `/p/{slug}` with an unknown slug gets the default `frame-ancestors 'self'` and `X-Frame-Options: SAMEORIGIN`. Every HTML page's `Cache-Control` ends with `no-transform`.
- The storage request takes an optional `fileSize` (bytes, `z.number().int().nonnegative()`), added for presigned PUT uploads (stores without POST uploads, such as Cloudflare R2). A size over the element's limit is refused before anything is signed, with `fileSize` and `maxFileSize` in the details, which the survey runtime reads as "file too large" (`v1-storage-file-too-large`); a malformed one is a field error (`v1-storage-invalid-file-size`). With the default `STORAGE_UPLOAD_METHOD=post` a size within the limit leaves the answer as it was (`v1-storage-upload-init-file-size`). With `put` the answer keeps the presigned POST and adds `uploadMethod: "put"`, `uploadUrl` (a presigned PUT of the same key, valid for two minutes, with `content-length;content-type;host` signed) and `uploadHeaders` (the `Content-Type` the client sends; the browser sets `Content-Length` from the body), which the runtime uploads with (`v1-storage-upload-init-put`, `v2-storage-upload-init-put`); a request without a size, from a runtime that predates it, gets the POST alone (`v1-storage-upload-put-without-file-size`).
