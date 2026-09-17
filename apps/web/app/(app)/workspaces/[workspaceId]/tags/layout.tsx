import { type ReactNode } from "react";
import { BillingRoleGuardLayout } from "@/modules/workspaces/lib/billing-role-guard-layout";
import { TagsQueryClientProvider } from "./query-client-provider";

const TagsLayout = async (
  props: Readonly<{ params: Promise<{ workspaceId: string }>; children: ReactNode }>
) => {
  return (
    <BillingRoleGuardLayout params={props.params}>
      <TagsQueryClientProvider>{props.children}</TagsQueryClientProvider>
    </BillingRoleGuardLayout>
  );
};

export default TagsLayout;
