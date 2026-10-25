import { DEFAULT_WORKSPACE_NAME } from "../survey-editor/lib/publish-warnings";

// The first-survey step asks what the product or company is called and names the workspace after it, because the templates write the workspace's name into their questions ("How likely are you to recommend <name> ...?"). The field starts from the workspace's own name once someone has chosen one, otherwise from the domain of a work address; a personal address suggests nothing, since its domain names the mail provider, not the user's company.

/** Personal and free mail providers: forma2's personal-email-domains.ts (the list the server refuses on Forma Cloud, apps/server/internal/auth/domains.go) plus the common Japanese, Korean and other regional providers, whose domains would otherwise be offered as a company name. */
const PERSONAL_EMAIL_DOMAINS = new Set([
  "gmail.com",
  "googlemail.com",
  "outlook.com",
  "outlook.jp",
  "hotmail.com",
  "hotmail.co.jp",
  "hotmail.fr",
  "live.com",
  "live.jp",
  "live.nl",
  "msn.com",
  "yahoo.com",
  "yahoo.co.jp",
  "yahoo.co.uk",
  "yahoo.fr",
  "ymail.com",
  "rocketmail.com",
  "aol.com",
  "icloud.com",
  "me.com",
  "mac.com",
  "privaterelay.appleid.com",
  "proton.me",
  "protonmail.com",
  "pm.me",
  "passmail.com",
  "mailbox.org",
  "posteo.de",
  "fastmail.com",
  "mozmail.com",
  "duck.com",
  "anonaddy.com",
  "gmx.com",
  "gmx.net",
  "gmx.de",
  "web.de",
  "t-online.de",
  "yandex.com",
  "yandex.ru",
  "mail.ru",
  "zoho.com",
  "zohomail.com",
  "mail.com",
  "qq.com",
  "foxmail.com",
  "163.com",
  "126.com",
  "139.com",
  "yeah.net",
  "sina.com",
  "aliyun.com",
  "naver.com",
  "daum.net",
  "hanmail.net",
  "docomo.ne.jp",
  "ezweb.ne.jp",
  "au.com",
  "softbank.ne.jp",
  "i.softbank.jp",
  "nifty.com",
  "biglobe.ne.jp",
  "sfr.fr",
  "orange.fr",
  "earthlink.net",
]);

/** Second-level labels under a two-letter country code that are not the registrant's own (acme.co.jp, acme.com.au, acme.ne.jp). */
const COUNTRY_SECOND_LEVELS = new Set([
  "co",
  "com",
  "ne",
  "or",
  "ac",
  "go",
  "gov",
  "edu",
  "org",
  "net",
  "ltd",
  "plc",
  "gr",
  "lg",
  "ed",
]);

/** The company name a work address suggests: the registrable label of its domain, capitalized ("ada@mail.acme.co.jp" gives "Acme"). Empty for a personal provider, a malformed address or an internationalized (punycode) label, which would show as "Xn--...". */
export const companyNameFromEmail = (email: string): string => {
  const at = email.lastIndexOf("@");
  if (at <= 0) return "";
  const domain = email
    .slice(at + 1)
    .trim()
    .toLowerCase()
    .replace(/\.+$/, "");
  if (PERSONAL_EMAIL_DOMAINS.has(domain)) return "";
  const labels = domain.split(".");
  if (labels.length < 2 || labels.some((label) => label === "")) return "";
  let index = labels.length - 2;
  const tld = labels[labels.length - 1] ?? "";
  if (labels.length >= 3 && tld.length === 2 && COUNTRY_SECOND_LEVELS.has(labels[index] ?? "")) index -= 1;
  const label = labels[index] ?? "";
  if (label === "" || label.startsWith("xn--")) return "";
  return label.charAt(0).toUpperCase() + label.slice(1);
};

/** What the product name field starts with: the workspace's name once it is no longer the default every organization starts with, otherwise what the user's address suggests. */
export const initialProductName = (workspaceName: string, email: string): string =>
  workspaceName.trim() !== DEFAULT_WORKSPACE_NAME ? workspaceName : companyNameFromEmail(email);
