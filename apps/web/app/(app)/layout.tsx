import { PlainChat } from "@/app/plain/components/plain-chat";
import { getIsActiveCustomer } from "@/app/plain/lib/customer";
import { computePlainEmailHash } from "@/app/plain/lib/identity";
import { PostHogIdentify } from "@/app/posthog/PostHogIdentify";
import {
  IS_PLAIN_CHAT_CONFIGURED,
  PLAIN_ACTIVE_CUSTOMER_LABEL_TYPE_ID,
  PLAIN_APP_ID,
  POSTHOG_KEY,
} from "@/lib/constants";
import { getUser } from "@/lib/user/service";
import { I18nResources } from "@/lingodotdev/client";
import { getLocale } from "@/lingodotdev/language";
import { APP_I18N_NAMESPACES } from "@/lingodotdev/namespaces";
import { loadI18nResources } from "@/lingodotdev/resources";
import { getSession } from "@/modules/auth/lib/session";
import { ClientLogout } from "@/modules/ui/components/client-logout";
import { NoMobileOverlay } from "@/modules/ui/components/no-mobile-overlay";
import { ToasterClient } from "@/modules/ui/components/toaster-client";

const AppLayout = async ({ children }: Readonly<{ children: React.ReactNode }>) => {
  const session = await getSession();
  const user = session?.user?.id ? await getUser(session.user.id) : null;
  // If user account is deactivated, log them out instead of rendering the app
  if (user?.isActive === false) {
    return <ClientLogout />;
  }

  // 136 KB the public routes must never carry, preloaded here rather than fetched after hydration
  // so an authenticated page does not paint its own translation keys first. `getLocale` resolves
  // through the request-cached `getUserLocale`, so asking again after the root layout is free.
  const locale = await getLocale();
  const appResources = await loadI18nResources(locale, APP_I18N_NAMESPACES);

  // Resolve the paying-customer label server-side so Plain applies it to threads
  // from init time. Only queried when a label is configured to avoid extra work.
  const plainActiveCustomerLabelTypeId =
    IS_PLAIN_CHAT_CONFIGURED &&
    PLAIN_ACTIVE_CUSTOMER_LABEL_TYPE_ID &&
    user &&
    (await getIsActiveCustomer(user.id))
      ? PLAIN_ACTIVE_CUSTOMER_LABEL_TYPE_ID
      : null;

  return (
    <>
      {/* Before children: it merges the namespaces they translate against. Renders nothing. */}
      <I18nResources language={locale} resources={appResources} />
      <NoMobileOverlay />
      {POSTHOG_KEY && user && (
        <PostHogIdentify posthogKey={POSTHOG_KEY} userId={user.id} email={user.email} name={user.name} />
      )}
      {IS_PLAIN_CHAT_CONFIGURED && PLAIN_APP_ID && (
        <PlainChat
          appId={PLAIN_APP_ID}
          userEmail={user?.email}
          userName={user?.name}
          userId={user?.id}
          emailHash={user?.email ? computePlainEmailHash(user.email) : null}
          activeCustomerLabelTypeId={plainActiveCustomerLabelTypeId}
        />
      )}
      <ToasterClient />
      {children}
    </>
  );
};

export default AppLayout;
