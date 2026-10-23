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

#### 2. Update i18n Configuration

Add the language tag to `SURVEY_RUNTIME_LANGUAGE_CODES` in `packages/i18n-utils/src/survey-runtime-languages.ts`, which the runtime uses as `supportedLngs`, then register the bundle in `packages/surveys/src/lib/i18n.config.ts`:

```tsx
// Add new import
import newLangTranslations from "../../locales/new-lang.json";

i18n
  .use(ICU)
  .use(initReactI18next)
  .init({
    fallbackLng: resolveFallbackBundles,
    supportedLngs: [...SURVEY_RUNTIME_LANGUAGE_CODES],
    resources: {
      // ... existing resources
      "new-lang": { translation: newLangTranslations },
    },
  });
```

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
