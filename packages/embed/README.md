# Forma embed script

The script that puts a Forma link survey on any web page with one tag and no SDK setup: inline at the height of its content, or as a popup, slider or side tab. Every Forma instance serves it at `/js/embed.js`, with the same cache and CORS headers as the SDK. It is not published on npm.

The share modal of a link survey (**Share → Website embed**) writes the snippets below with the survey's link and the instance's URL filled in, and previews them on a sample page.

For a survey that pops up for the visitors and actions its targeting picks, use an in-app survey and the [JavaScript SDK](../js-core/README.md) instead; this script shows a link survey to whoever opens it.

## Layouts

Each layout is one data attribute whose value is the survey's link. Everything the link carries (`?lang=`, hidden fields, prefill parameters) reaches the survey. Load the script once per page, anywhere; it renders every element it finds when the page has loaded.

### Standard

The survey sits in the page and its frame grows and shrinks with the current card, so the page never shows a scrollbar inside a scrollbar.

```html
<div data-forma-embed="https://forma.example.com/s/<survey-id>" style="width:100%"></div>
<script src="https://forma.example.com/js/embed.js" async></script>
```

Whatever the element holds is replaced by the survey, so a plain link inside it is a fallback for visitors without JavaScript. The frame loads lazily: a survey below the fold loads, and counts a display, when the visitor scrolls to it.

### Popup

A dialog over the page, opened by a click on the element:

```html
<button type="button" data-forma-popup="https://forma.example.com/s/<survey-id>">Take the survey</button>
<script src="https://forma.example.com/js/embed.js" async></script>
```

### Slider

A panel that slides in from the edge of the window and is as tall as the window:

```html
<button type="button" data-forma-slider="https://forma.example.com/s/<survey-id>" data-forma-position="left">
  Take the survey
</button>
<script src="https://forma.example.com/js/embed.js" async></script>
```

### Side tab

A tab on the edge of the window, created by the script; clicking it opens the survey beside it, and clicking it again closes it. The element itself stays empty.

```html
<div data-forma-side-tab="https://forma.example.com/s/<survey-id>" data-forma-label="Feedback"></div>
<script src="https://forma.example.com/js/embed.js" async></script>
```

Popups, sliders and side tabs load the survey when they first open, and keep it when they close, so a respondent who reopens one continues where they left off. Escape closes them, whether focus is on the page or inside the survey, unless the survey uses the key itself (to close an open dropdown). Popups and sliders are modal: the page behind does not scroll and focus stays in the dialog until it closes, then returns to the element that opened it.

## Options

| Attribute                 | Layouts                 | Meaning                                                                                                                                                                                                      |
| ------------------------- | ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `data-forma-title`        | all                     | The frame's accessible name, and the dialog's. Default `Survey`.                                                                                                                                             |
| `data-forma-height`       | standard                | The frame's height in pixels until the survey reports its own. Default `500`.                                                                                                                                |
| `data-forma-open-delay`   | popup, slider, side tab | Open on its own this many seconds after the page loads. It opens at most once per browser session (remembered in `sessionStorage`), so a visitor who closes it is not asked again on every page of the site. |
| `data-forma-auto-close`   | popup, slider, side tab | Close this many seconds after the survey is completed.                                                                                                                                                       |
| `data-forma-position`     | slider, side tab        | `right` (default) or `left`.                                                                                                                                                                                 |
| `data-forma-label`        | side tab                | The text on the tab. Default `Feedback`.                                                                                                                                                                     |
| `data-forma-button-color` | side tab                | The tab's background, any CSS colour. Default `#0f172a`.                                                                                                                                                     |
| `data-forma-close-label`  | popup, slider           | The close button's accessible name, for pages in another language. Default `Close`.                                                                                                                          |

## From your own code

The script defines `window.formaEmbed`:

```js
// Render embed elements added after the script ran (a single-page app's route change).
window.formaEmbed.scan();
// Open a survey from your own button or event; mode is "popup" (default), "slider" or "side-tab".
window.formaEmbed.open("https://forma.example.com/s/<survey-id>", { mode: "slider", position: "left" });
window.formaEmbed.close();
```

When a survey is completed, its embed element (the one carrying `data-forma-embed`, `data-forma-popup`, `data-forma-slider` or `data-forma-side-tab`) dispatches a `forma:survey-completed` event that bubbles, with the survey's frame URL in `event.detail.url`. A survey opened with `formaEmbed.open()` has no such element; its event comes from the element the script adds to the end of `<body>` for it, and bubbles to `document` all the same:

```js
document.addEventListener("forma:survey-completed", (event) => {
  console.log("Survey completed", event.detail.url);
});
```

## How the frame follows the survey

The script adds `embed=true&autoHeight=true` to the survey's link. With both, the survey page drops its page chrome, lays the survey out at the height of its content instead of filling the frame, and posts `{ type: "forma:embed-resize", height }` to the page around it whenever that height changes. The height includes an open dropdown's options, which are drawn outside the survey's own layout, so the frame grows while a dropdown is open instead of cutting its options off. With `embed=true` alone (the share modal's plain iframe snippet) the survey keeps filling its frame and still posts its height. With both parameters, the survey also posts `{ type: "forma:embed-escape" }` when the respondent presses Escape and nothing in the survey took the key; the script closes the popup, slider or side tab whose frame sent it. The script believes a message only from its own frame's window and the survey's origin. It also listens for the `"formaSurveyCompleted"` message the survey runtime has always posted when the last answer reaches the server.

The names in `src/protocol.ts` (data attributes, query parameters, messages, the event) are read by pages that pasted a snippet, so they are a published contract: add to them, never rename one.

## Development

```sh
pnpm --filter @forma/embed build   # dist/index.js, copied to apps/web/public/js/embed.js
pnpm --filter @forma/embed test
```

The build is checked against a gzip budget with the SDK and the survey runtime (`tooling/scripts/check-bundle-budget.mjs`): it runs on customer pages.
