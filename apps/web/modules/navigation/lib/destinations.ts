import type { TFunction } from "i18next";

/**
 * Every place in the product a person can be sent, as data.
 *
 * The sidebars each know a slice of this: the main navigation knows the product areas, the settings
 * sidebar knows the settings pages, and the account menu knows the personal ones. Nothing knew all
 * of it, which is why the product reads as three items — its depth is real but two clicks down.
 *
 * Labels reuse the keys the sidebars already use, so this adds no strings to translate.
 */

export interface NavigationDestination {
  id: string;
  label: string;
  href: string;
  /** Which heading it sits under in the palette. */
  group: string;
  /** Extra words to match on, for destinations whose label is not what someone would type. */
  keywords?: string[];
}

interface DestinationInput {
  t: TFunction;
  workspaceId: string;
  organizationId: string;
  /** The billing role holds no product access, so it is offered only what it can open. */
  isBilling: boolean;
  isOwnerOrManager: boolean;
  isFormaCloud: boolean;
}

export const getNavigationDestinations = ({
  t,
  workspaceId,
  organizationId,
  isBilling,
  isOwnerOrManager,
  isFormaCloud,
}: DestinationInput): NavigationDestination[] => {
  const workspace = `/workspaces/${workspaceId}`;
  const workspaceSettings = `${workspace}/settings/workspace`;
  const organizationSettings = `/organizations/${organizationId}/settings`;

  const product: NavigationDestination[] = isBilling
    ? []
    : [
        {
          id: "surveys",
          label: t("common.surveys"),
          href: `${workspace}/surveys`,
          group: t("common.workspace"),
        },
        {
          id: "contacts",
          label: t("common.contacts"),
          href: `${workspace}/contacts`,
          group: t("common.workspace"),
        },
        {
          id: "workflows",
          label: t("common.workflows"),
          href: `${workspace}/workflows`,
          group: t("common.workspace"),
        },
        {
          id: "user-actions",
          label: t("common.user_actions"),
          href: `${workspace}/user-actions`,
          group: t("common.workspace"),
        },
        {
          id: "integrations",
          label: t("common.integrations"),
          href: `${workspace}/integrations`,
          group: t("common.workspace"),
          keywords: ["zapier", "webhook", "slack", "notion", "airtable", "google sheets", "n8n", "make"],
        },
        {
          id: "app-connection",
          label: t("common.web_and_mobile_sdk"),
          href: `${workspace}/integrations/app-connection`,
          group: t("common.workspace"),
          keywords: ["sdk", "javascript", "snippet", "embed"],
        },
        {
          id: "tags",
          label: t("common.tags"),
          href: `${workspace}/tags`,
          group: t("common.workspace"),
        },
      ];

  const workspaceSettingsItems: NavigationDestination[] = isBilling
    ? []
    : [
        {
          id: "ws-general",
          label: t("common.general"),
          href: `${workspaceSettings}/general`,
          group: t("common.settings"),
        },
        {
          id: "ws-look",
          label: t("common.appearance"),
          href: `${workspaceSettings}/look`,
          group: t("common.settings"),
        },
        {
          id: "ws-languages",
          label: t("common.survey_languages"),
          href: `${workspaceSettings}/languages`,
          group: t("common.settings"),
        },
        {
          id: "ws-teams",
          label: t("common.team_access"),
          href: `${workspaceSettings}/teams`,
          group: t("common.settings"),
        },
      ];

  const organizationItems: NavigationDestination[] = [
    {
      id: "org-general",
      label: t("common.general"),
      href: `${organizationSettings}/general`,
      group: t("common.organization"),
    },
    {
      id: "org-teams",
      label: t("common.teams"),
      href: `${organizationSettings}/teams`,
      group: t("common.organization"),
    },
    ...(isOwnerOrManager
      ? [
          {
            id: "org-api-keys",
            label: t("common.api_keys"),
            href: `${organizationSettings}/api-keys`,
            group: t("common.organization"),
          },
        ]
      : []),
    {
      id: "org-billing",
      label: isFormaCloud ? t("common.billing") : t("common.enterprise_license"),
      href: `${organizationSettings}/${isFormaCloud ? "billing" : "enterprise"}`,
      group: t("common.organization"),
    },
    ...(isFormaCloud
      ? []
      : [
          {
            id: "org-domain",
            label: t("common.domain"),
            href: `${organizationSettings}/domain`,
            group: t("common.organization"),
          },
        ]),
  ];

  const account: NavigationDestination[] = [
    {
      id: "account-profile",
      label: t("common.your_profile"),
      href: "/account/settings/profile",
      group: t("common.account"),
    },
    ...(isBilling
      ? []
      : [
          {
            id: "account-notifications",
            label: t("common.notifications"),
            href: "/account/settings/notifications",
            group: t("common.account"),
          },
        ]),
  ];

  return [...product, ...workspaceSettingsItems, ...organizationItems, ...account];
};
