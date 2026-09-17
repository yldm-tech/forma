import { redirect } from "next/navigation";
import { IS_FORMA_CLOUD } from "@/lib/constants";
import { getBillingFallbackPath } from "@/lib/membership/navigation";
import { getWorkspaceAuth } from "@/modules/workspaces/lib/utils";

/**
 * Sends the billing role away from a workspace subtree it holds no access to. Renders no shell of its own — the only thing it does is the redirect, which is why it can sit over settings, integrations and user actions alike.
 */
export const BillingRoleGuardLayout = async (props: {
  params: Promise<{ workspaceId: string }>;
  children: React.ReactNode;
}) => {
  const params = await props.params;
  const { children } = props;

  const { isBilling, organization } = await getWorkspaceAuth(params.workspaceId);

  if (isBilling) {
    return redirect(getBillingFallbackPath(organization.id, IS_FORMA_CLOUD));
  }

  return children;
};
