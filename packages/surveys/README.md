## Overview

The `@forma/surveys` package provides a complete survey rendering system built with Preact/React. It features automated translation management through Lingo.dev.

## Features

- **Survey Components**: Complete set of survey question types and UI components
- **Internationalization**: Built with i18next and react-i18next
- **Type Safety**: Full TypeScript support
- **Testing**: Comprehensive test coverage with Vitest
- **Lightweight**: Built with Preact for optimal bundle size
- **Multi-language Support**: Supports 10+ languages with automated translation generation

## Architecture

### File Structure

```text
packages/surveys/
├── locales/                 # Translation files
│   ├── en-US.json          # Source translations (English)
│   ├── de-DE.json          # Generated translations (German)
│   ├── fr-FR.json          # Generated translations (French)
│   └── ...                 # Other target languages
├── i18n.json               # lingo.dev configuration
├── src/
│   ├── components/
│   │   ├── buttons/        # Survey navigation buttons
│   │   ├── general/        # Core survey components
│   │   ├── i18n/
│   │   │   └── provider.tsx # i18n provider component
│   │   ├── icons/          # Icon components
│   │   ├── elements/       # Question type components
│   │   └── wrappers/       # Layout wrappers
│   ├── lib/
│   │   ├── i18n.config.ts  # i18next configuration
│   │   ├── i18n-utils.ts   # Utility functions
│   │   └── ...             # Other utilities
│   ├── styles/             # CSS styles
│   └── types/              # TypeScript types
└── package.json
```

## Saving a respondent's progress (link surveys)

A link survey keeps the respondent's progress in the browser's IndexedDB (`src/lib/offline-storage.ts`), keyed by the survey id: the answers given so far, the card they are on, and the ids of the response and display they belong to, plus every answer the server has not acknowledged yet. A reload, or a tab the phone discarded in the background, goes on where the respondent left off, in the same response and display, instead of starting over with a new partial response. Progress older than 24 hours is discarded, progress left by a different single-use link, personal link or user id for the same survey is discarded rather than continued, and progress is removed once the server has the final answer. Unsent answers of a visit that is not continued (another respondent's, or one whose progress was discarded) are still sent, each to the response it was given in, and never change the response of the page that found them. On a PIN-protected survey the page also keeps the PIN token in `sessionStorage` for the tab's session, so the reload does not ask for the PIN again, as long as the token has at least 15 minutes left; otherwise the PIN is asked for again and the fresh token lasts another hour.

It is on by default. The creator turns it off per survey (Settings, Response Options, "Save respondents' progress", the survey's `isProgressSavingEnabled`) for kiosks and shared devices, where the next person would otherwise see the previous one's answers. With it off, the survey removes the progress earlier respondents left in that browser, and sends their unsent answers once before removing them too. The link page passes it to the runtime as `persistProgress`.

The offline mode the `?offlineSupport=true` link parameter turns on keeps progress the same way, and adds what a respondent who only reloads does not need: answers wait while the browser reports it is offline, the page shows an offline banner, the ending shows while answers are still stored, and leaving a survey half answered asks for confirmation. Progress saving alone does none of these.

## One response per browser or per email (link surveys)

Two switches in Settings, Response Options limit duplicate submissions of a link survey (owner decision D-FG03); both are off by default.

"One response per browser" (`isSingleResponsePerBrowserEnabled`) is best effort, and the editor says so. Once the final answer reaches the server, the link page (`apps/web/src/survey-page/lib/browser-response.ts`, not this runtime) remembers in the browser that it answered: in `localStorage` and in a cookie under the survey's own path `/s/{surveyId}`, each restoring the other when only one was cleared. A later visit from that browser to `/s/{surveyId}` shows "already responded" instead of the survey, the PIN gate or the email gate. A private window, clearing all site data, another browser or another device answers again, a frame on another site may be refused both kinds of storage, and on a shared device the next person cannot answer. Previews are neither limited nor remembered, and personal links (`/c/...`) already take one response per contact.

"One response per email address" (`isSingleResponsePerEmailEnabled`) needs "Verify email before submission" and is enforced by the server. Creating or finishing a response for a verified address that already finished a response to the survey, compared without regard to letter case, is refused with 409 and `details.code` `response_email_already_used` (`RESPONSE_EMAIL_ALREADY_USED_ERROR_CODE` in `@forma/types/errors`); the runtime shows it as `TResponseErrorCodesEnum.ResponseEmailAlreadyUsed` and does not retry. Opening a verification link whose address has finished a response shows "already responded" instead of the survey. The endpoint that mails verification links sends one to every address alike, so it does not tell anyone which addresses have responded. Only finished responses count, so an unfinished one never locks its address out: a respondent who reloads mid-survey goes on in the response they started while progress saving keeps its id, and one who reloads a survey without progress saving, comes back after the saved progress expired (24 hours), or opens the link in another browser starts a new response there. Whichever of the address's responses finishes first is kept; finishing another one is refused with the same 409.

## Setting Up Automated Translations

### Prerequisites

- [Lingo.dev](https://Lingo.dev) API key
- Access to the Forma team on Lingo.dev

### Step-by-Step Setup

1. **Join the Forma Team**
   - Join the Forma team on Lingo.dev

2. **Get Your API Key**
   - In the sidebar, go to **Workspaces** and open the default workspace
   - Navigate to the **Settings** tab
   - Copy the API key

3. **Configure Environment Variables**

   Add the key to the `.env` file at the root of the repository, which `i18n:generate` reads:

   ```bash
   # .env
   LINGO_API_KEY=<YOUR_API_KEY>
   ```

4. **Generate Translations**

   Run the translation generation script:

   ```bash
   # From within packages/surveys (from the repository root: pnpm i18n:surveys:generate)
   pnpm run i18n:generate
   ```

This will execute the auto-translate script and update translation files if needed.

## Development Workflow

### Adding New Translation Keys

1. **Update Source File**: Add new keys to `packages/surveys/locales/en-US.json`
2. **Generate Translations**: Run `pnpm run i18n:generate`
3. **Update Components**: Use the new translation keys in your components with `useTranslation` hook
4. **Test**: Verify translations work across all supported languages

### Updating Existing Translations

1. **Update Target File**: Update the translation keys in the target language file (`packages/surveys/locales/<target-language>.json`)
2. **Test**: Verify translations work across all supported languages
3. You don't need to run the `i18n:generate` command as it is only required when the source language is updated.

### Adding New Languages

#### 1. Update lingo.dev Configuration

Edit `packages/surveys/i18n.json` to include new target languages:

```json
{
  "locale": {
    "source": "en-US",
    "targets": ["de-DE", "it-IT", ...otherLanguages, "new-lang"]
  }
}
```

#### 2. Register the Language

The build picks up every file in `locales/`: `vite.config.mts` gives each language other than English its own content-hashed file under `/js/surveys-locales/`, which the runtime fetches when a respondent needs it, so there is no import to add. Register the tag where the rest of the product reads the list of runtime languages:

- `SURVEY_RUNTIME_LANGUAGE_CODES` in `packages/i18n-utils/src/survey-runtime-languages.ts`, which the runtime uses as `supportedLngs` and the workspace's default-language picker uses to tell which languages have built-in strings (`src/lib/i18n.config.test.ts` fails when it drifts from `locales/`).
- `surveyRuntimeLanguages` in `apps/server/internal/langcode/langcode.go`, the server's copy of the same list (`TestRuntimeLanguagesMatchI18nUtils` keeps them identical).
- The server's validation messages in `apps/server/internal/response/answers/messages.json`, regenerated from `locales/` with `go test ./internal/response/answers -run TestMessagesMatchSurveyLocales -update` in `apps/server/`.

#### 3. Generate Translation Files

Run the translation generation command:

```bash
pnpm run i18n:generate
```

This will create new translation files in the `locales/` directory for each target language.

## Scripts

- `pnpm dev` - Start development build
- `pnpm build` - Build for production
- `pnpm test` - Run tests
- `pnpm test:coverage` - Run tests with coverage
- `pnpm i18n:generate` - Generate translations using Lingo.dev
