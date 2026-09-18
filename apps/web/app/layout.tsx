import { Metadata } from "next";
import React from "react";
import { NoScriptWarning } from "@/app/components/NoScriptWarning";
import { DEFAULT_LOCALE } from "@/lib/constants";
import { SentryClientConfigScript } from "@/lib/sentry/SentryClientConfigScript";
import { I18nProvider } from "@/lingodotdev/client";
import { getLocale } from "@/lingodotdev/language";
import { PUBLIC_I18N_NAMESPACES } from "@/lingodotdev/namespaces";
import { loadI18nResources } from "@/lingodotdev/resources";
import "../modules/ui/globals.css";

export const metadata: Metadata = {
  title: {
    template: "%s | Forma",
    default: "Forma",
  },
  description: "Open-Source Survey Suite",
};

const RootLayout = async ({ children }: { children: React.ReactNode }) => {
  const locale = await getLocale();
  // Read here so the provider can initialise during render instead of in an effect — which is what
  // lets the server produce translated markup at all. Only the public namespaces: `(app)` adds its
  // own, and a respondent must not download the admin catalogue.
  const resources = await loadI18nResources(locale, PUBLIC_I18N_NAMESPACES);

  return (
    <html lang={locale} translate="no">
      <body className="flex h-dvh flex-col transition-all ease-in-out">
        {/* First in the document so instrumentation-client.ts can start Sentry as early as possible. */}
        <SentryClientConfigScript />
        <NoScriptWarning locale={locale} />
        <I18nProvider language={locale} defaultLanguage={DEFAULT_LOCALE} resources={resources}>
          {children}
        </I18nProvider>
      </body>
    </html>
  );
};

export default RootLayout;
