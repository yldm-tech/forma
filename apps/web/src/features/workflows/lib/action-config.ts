import {
  isWorkflowChatAction,
  type TWorkflowChatMessageActionNode,
  type TWorkflowChildNode,
  type TWorkflowDefinition,
  type TWorkflowHttpRequestActionNode,
  type TWorkflowSecretValue,
  WORKFLOW_ACTIONS,
} from "../definition";

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

const adoptSecret = (
  current: TWorkflowSecretValue,
  sent: TWorkflowSecretValue | null | undefined,
  persisted: TWorkflowSecretValue | null | undefined
): TWorkflowSecretValue =>
  current.value !== undefined && sent?.value === current.value && persisted?.sealed ? persisted : current;

const isChatNode = (node: TWorkflowChildNode | undefined): node is TWorkflowChatMessageActionNode =>
  node?.type === "action" && isWorkflowChatAction(node.actionType);

const isHttpNode = (node: TWorkflowChildNode | undefined): node is TWorkflowHttpRequestActionNode =>
  node?.type === "action" && node.actionType === WORKFLOW_ACTIONS.HTTP_REQUEST;

const adoptNodeSecrets = (
  current: TWorkflowChildNode,
  sent: TWorkflowChildNode | undefined,
  persisted: TWorkflowChildNode | undefined
): TWorkflowChildNode => {
  if (isChatNode(current) && isChatNode(sent) && isChatNode(persisted)) {
    if (sent.actionType !== current.actionType || persisted.actionType !== current.actionType) return current;
    const webhookUrl = adoptSecret(
      current.config.webhookUrl,
      sent.config.webhookUrl,
      persisted.config.webhookUrl
    );
    return webhookUrl === current.config.webhookUrl
      ? current
      : { ...current, config: { ...current.config, webhookUrl } };
  }
  if (!isHttpNode(current) || !isHttpNode(sent) || !isHttpNode(persisted)) return current;
  // Sealed for the origin that was sent: once the URL has moved on, the typed value stays typed and the next save seals it for where the step now sends.
  if (originOf(current.config.url) !== originOf(sent.config.url)) return current;
  let changed = false;
  const headers = current.config.headers.map((header, index) => {
    const sentHeader = sent.config.headers[index];
    const persistedHeader = persisted.config.headers[index];
    if (!header.secret || sentHeader?.name !== header.name || persistedHeader?.name !== header.name)
      return header;
    const secret = adoptSecret(header.secret, sentHeader.secret, persistedHeader.secret);
    if (secret === header.secret) return header;
    changed = true;
    return { ...header, secret };
  });
  return changed ? { ...current, config: { ...current.config, headers } } : current;
};

/** current with every header secret and chat webhook URL it still holds as the value a save sent replaced by the sealed form the server answered with (persisted), so the builder shows it as saved and a later host change asks for it again, exactly as after a reload. A value typed since the save stays as typed. A signing secret stays in view: the author copies it into the receiver, and the receiver only ever gets signatures, never the secret. Unchanged nodes keep their identity, and current itself is returned when nothing was adopted. */
export const adoptSealedSecrets = (
  current: TWorkflowDefinition,
  sent: TWorkflowDefinition,
  persisted: TWorkflowDefinition
): TWorkflowDefinition => {
  const sentById = new Map(sent.nodes.map((node) => [node.id, node]));
  const persistedById = new Map(persisted.nodes.map((node) => [node.id, node]));
  let changed = false;
  const nodes = current.nodes.map((node) => {
    const next = adoptNodeSecrets(node, sentById.get(node.id), persistedById.get(node.id));
    if (next !== node) changed = true;
    return next;
  });
  return changed ? { ...current, nodes } : current;
};

const toBase64 = (bytes: Uint8Array): string => btoa(String.fromCharCode(...bytes));

/** A new Standard Webhooks signing secret, `whsec_` and the base64 of 32 random bytes, the shape webhooks are signed with. */
export const generateSigningSecret = (): string => {
  const bytes = new Uint8Array(32);
  crypto.getRandomValues(bytes);
  return `whsec_${toBase64(bytes)}`;
};
