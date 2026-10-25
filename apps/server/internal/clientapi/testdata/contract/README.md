# Client API contract exchanges

Each `<case>.json` file is one HTTP exchange of what the SDK (`packages/js-core`), the survey runtime (`packages/surveys`) and respondents' browsers call: the client API (`/api/v1/client/*`, `/api/v2/client/*`), the public survey pages (`/s/{id}`, `/p/{slug}`), the OG image and the bundles under `/js/*`. `contract_test.go` replays the client API cases in the order their scenarios need, and `internal/linksurvey/contract_test.go` the page and OG image cases; both require the same status, the same listed headers and a body that equals the expected one once the placeholders below are matched. These answers are what SDK snippets already installed on customer sites decode, so a change to one is a change to a published contract; edit the case together with the code and say why in the change.

## Scenario

`seedRecording` in `contract_test.go` creates what the first request expects: a workspace with its `en-US` (default) and `de-DE` languages, the code action class `checkout`, the attribute key `plan`, and these surveys:

- `app`: in-progress app survey, one `openText` element `checkout_feedback`, ending `clendscreenappaaaaaaaaaa3`, display option `displayMultiple`, triggered by the `checkout` action class, with its private segment. It is the only survey in the environment state, because that state lists app surveys only.
- `link`: in-progress link survey in `en-US` (default) and `de-DE`, blocks with `feedback` (required `openText`) and `score` (`rating`, number scale, range 5), hidden fields `utm_source` and `campaign`, endings `clendscreenokaaaaaaaaaaa1` and `clendscreenquotaaaaaaaaa2`, slug `contract-feedback`, and one quota: limit 1, `feedback equals "quota"`, action `endSurvey` on `clendscreenquotaaaaaaaaa2`, partial submissions counted.
- `paused`, `archived` (archived and paused), `singleUse` (encrypted single-use links) and `upload` (a `fileUpload` element `attachment` allowing `png` and `pdf` up to 5 MB): one-element link surveys.

The cases run in the order `replayOrder` gives (identify before a contact's displays, a display before the response that references it, the first quota response before the one that finds the quota full). Client API requests carry `Origin: https://customer.example` and a fixed desktop Chrome `User-Agent`. The rate-limit cases come last: 101 requests within a minute from one address exhaust the shared `api:client` limit (100 per minute), after which every client route including the environment state answers 429. `v1-storage-rate-limited` is the sixth storage request within a minute (`storage:upload`, 5 per minute per address). Storage uploads are presigned against a dummy S3 configuration (`S3_ENDPOINT_URL=http://localhost:9000`, bucket `forma-uploads`, access key `contractfixtureaccess`, region `us-east-1`, path style); nothing is uploaded.

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
- `notes` appears only on the storage upload cases, where `decodedPolicy` is the base64 `Policy` field decoded and normalised, so the upload conditions (size range, content type, key) are visible.

## Placeholders

Values that differ on every run are placeholders that keep the JSON type (always a string where the value is a string) and, inside longer strings such as URLs and storage keys, the surrounding text. An id or contact token placeholder binds to the first value it meets and must name the same value wherever it appears again.

| Placeholder | Stands for |
| --- | --- |
| `<id:workspace>` | The workspace id. |
| `<id:survey:app>`, `<id:survey:link>`, `<id:survey:paused>`, `<id:survey:archived>`, `<id:survey:singleUse>`, `<id:survey:upload>` | The survey ids described above. |
| `<id:actionClass:checkout>` | The `checkout` code action class. |
| `<id:language:en-US>` | The workspace's default language row. |
| `<id:segment:app>` | The private segment of the app survey. |
| `<id:block:app>` | The app survey's block id (a 24-character cuid2). |
| `<id:quota>` | The link survey's quota. |
| `<contact-token:user1>`, `<contact-token:user2>` | The opaque tokens the client API names the contacts created by identifying `contract-user-1` and `contract-user-2` by, in place of their ids (`ct1.` and base64url). A contact always gets the same token, and the display and response routes accept it as `contactId`. |
| `<id:display:anonymous>`, `<id:display:contact>`, `<id:display:v1>`, `<id:display:anonymous-unused>` | Display ids returned by the display cases; `anonymous-unused` is a display created only to be referenced by `v2-responses-create-invalid-meta-action`. |
| `<id:response:...>` | Response ids returned by the response cases, named after the case that created them (`partial`, `finished`, `v1`, `contact`, `quota1`, `quota2`, `quota3`, `requiredOmitted`, `ratingOutOfRange`, `singleUse`). |
| `<id:singleUse>` | The canonical single-use id: the decrypted value of the link's `suId`, which the survey page hands to the runtime as `singleUseId`. |
| `<single-use-token>` | The encrypted `suId` query value of a single-use link: base64url of the id sealed under `ENCRYPTION_KEY`, bound to its survey. |
| `<timestamp>` | An ISO 8601 UTC timestamp, e.g. `2026-10-22T06:12:02.233Z` (`expiresAt`, `createdAt`, `updatedAt`; the policy `expiration` has no milliseconds). |
| `<uuid>` | A random UUID (the `--fid--` suffix of an uploaded file name). |
| `<webapp-url>` | `WEBAPP_URL` (and `PUBLIC_URL`, which defaults to it). |
| `<s3-endpoint>` | `S3_ENDPOINT_URL`. With path-style addressing the presigned POST URL is `<s3-endpoint>/<bucket>`. |
| `<s3:policy>`, `<s3:signature>`, `<s3:amz-date>` | The presigned POST `Policy` (base64 JSON, decoded in `notes.decodedPolicy`), `X-Amz-Signature` (64 hex characters) and `X-Amz-Date` (`YYYYMMDDTHHMMSSZ`). |
| `<yyyymmdd>` | The date segment of `X-Amz-Credential`. |

Ids that start with `cl` and end in `aaa…` followed by a digit (for example `clendscreenokaaaaaaaaaaa1`, `clunknownsurveyaaaaaaaaa1`) are literal: ending card ids, the quota condition id, and ids that deliberately match nothing. Free-text values (`contract-user-1`, `user1@example.com`, answers, names) are literal too.

## Behaviour the cases pin

- The identify state and the display creates never name a contact by its id, only by its token, and the state's `displays` and `responses` hold survey keys (`sdkidentity.SurveyKey`), never survey ids. The SDK writes `email` only on a verified identify (`v2-user-attributes-update` skips it with an error); the identity verification rules themselves are covered by `identity_test.go`, since the contract's workspace has no identity secret.
- The environment state's survey `segment` reports the workspace id as both `environmentId` and `workspaceId`.
- `v2-responses-create-required-element-omitted` and `v2-responses-create-rating-out-of-range` are accepted: only elements whose key is present in `data` are validated, and a rating is not checked against its range. `v2-responses-create-missing-required` shows the error for a required element sent empty, in the survey's default language for a response in `default`.
- `meta.action: null` is rejected on response create (`v2-responses-create-invalid-meta-action`); the key must be a string or absent.
- `/s/{id}` (including the 404 for an unknown id) sends `frame-ancestors *` and no `X-Frame-Options`; `/p/{slug}` with an unknown slug gets the default `frame-ancestors 'self'` and `X-Frame-Options: SAMEORIGIN`. Every HTML page's `Cache-Control` ends with `no-transform`.
