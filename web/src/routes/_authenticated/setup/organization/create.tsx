import { createFileRoute } from "@tanstack/react-router";
import { SetupLayout } from "../../../../components/shell/setup-layout";
import { CreateOrganization } from "../../../../features/setup/create-organization";

// Multiple organizations are always enabled, so any signed-in user may create one here (forma2's create page with its license gates passing).
export const Route = createFileRoute("/_authenticated/setup/organization/create")({
  component: () => (
    <SetupLayout>
      <CreateOrganization />
    </SetupLayout>
  ),
});
