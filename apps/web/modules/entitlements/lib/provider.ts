import "server-only";
import { cache as reactCache } from "react";
import { IS_FORMA_CLOUD } from "@/lib/constants";
import { getCloudOrganizationEntitlementsContext } from "./cloud-provider";
import { getSelfHostedOrganizationEntitlementsContext } from "./self-hosted-provider";
import type { TOrganizationEntitlementsContext } from "./types";

export const getOrganizationEntitlementsContext = reactCache(
  async (organizationId: string): Promise<TOrganizationEntitlementsContext> => {
    if (IS_FORMA_CLOUD) {
      return getCloudOrganizationEntitlementsContext(organizationId);
    }

    return getSelfHostedOrganizationEntitlementsContext(organizationId);
  }
);
