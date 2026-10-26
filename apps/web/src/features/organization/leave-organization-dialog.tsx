import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Button } from "../../components/ui/button";
import {
  Dialog,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../../components/ui/dialog";
import { errorMessage } from "../../lib/errors";
import { organizationKeys, organizationsQuery, useLeaveOrganization } from "../../lib/queries/organizations";
import { forgetWorkspace } from "../../lib/storage";
import { toast } from "../../lib/toast";

/** forma2's confirmation to leave an organization, from the members page and from the guide to a two-factor requirement: a user cannot leave their only organization; once left, the browser goes to the root page. Owners are never offered it. */
export const LeaveOrganizationDialog = ({
  organizationId,
  open,
  onOpenChange,
}: {
  organizationId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const organizations = useQuery({ ...organizationsQuery, enabled: open });
  const leave = useLeaveOrganization(organizationId);
  const isOnlyOrganization = (organizations.data?.length ?? 0) <= 1;

  const leaveOrganization = async () => {
    try {
      await leave.mutateAsync();
    } catch (error) {
      toast.error(errorMessage(t, error));
      return;
    }
    toast.success(t("workspace.settings.general.member_deleted_successfully"));
    forgetWorkspace();
    onOpenChange(false);
    queryClient.removeQueries({ queryKey: organizationKeys.detail(organizationId) });
    await queryClient.invalidateQueries({ queryKey: organizationKeys.all });
    await navigate({ to: "/" });
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogHeader>
        <DialogTitle>{t("workspace.settings.general.leave_organization_title")}</DialogTitle>
        <DialogDescription>
          {t("workspace.settings.general.leave_organization_description")}
        </DialogDescription>
      </DialogHeader>
      {organizations.isSuccess && isOnlyOrganization && (
        <p className="mt-2 text-sm text-red-700">
          {t("workspace.settings.general.cannot_leave_only_organization")}
        </p>
      )}
      <DialogFooter>
        <Button variant="secondary" onClick={() => onOpenChange(false)}>
          {t("common.cancel")}
        </Button>
        <Button
          variant="destructive"
          onClick={() => void leaveOrganization()}
          loading={leave.isPending}
          disabled={!organizations.isSuccess || isOnlyOrganization}>
          {t("workspace.settings.general.leave_organization_ok_btn_text")}
        </Button>
      </DialogFooter>
    </Dialog>
  );
};
