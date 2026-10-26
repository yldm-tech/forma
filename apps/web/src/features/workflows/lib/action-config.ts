import type { TWorkflowHttpRequestActionNode, TWorkflowSecretValue } from "../definition";

// Helpers of the step forms after send_email.

type THttpConfig = TWorkflowHttpRequestActionNode["config"];

/** A URL's scheme and host, lower-cased: what the server binds a sealed header or signing secret to. Empty for a URL that does not parse. */
export const originOf = (value: string): string => {
  try {
    const url = new URL(value);
    return `${url.protocol}//${url.host}`.toLowerCase();
  } catch {
    return "";
  }
};

const clearSealed = (secret: TWorkflowSecretValue): TWorkflowSecretValue => (secret.sealed ? {} : secret);

/** The config with url, and with every sealed secret emptied when the URL now points at another origin: the server would refuse them there (they were sealed for the old one), and a new host must not receive them without the author entering them again. cleared says whether any was. */
export const clearSecretsOnOriginChange = (
  config: THttpConfig,
  url: string
): { config: Partial<THttpConfig>; cleared: boolean } => {
  const hasSealed =
    config.headers.some((header) => Boolean(header.secret?.sealed)) || Boolean(config.signingSecret?.sealed);
  if (!hasSealed || originOf(url) === originOf(config.url)) return { config: { url }, cleared: false };
  return {
    config: {
      url,
      headers: config.headers.map((header) =>
        header.secret ? { name: header.name, secret: clearSealed(header.secret) } : header
      ),
      signingSecret: config.signingSecret ? clearSealed(config.signingSecret) : null,
    },
    cleared: true,
  };
};

const toBase64 = (bytes: Uint8Array): string => btoa(String.fromCharCode(...bytes));

/** A new Standard Webhooks signing secret, `whsec_` and the base64 of 32 random bytes, the shape webhooks are signed with. */
export const generateSigningSecret = (): string => {
  const bytes = new Uint8Array(32);
  crypto.getRandomValues(bytes);
  return `whsec_${toBase64(bytes)}`;
};
