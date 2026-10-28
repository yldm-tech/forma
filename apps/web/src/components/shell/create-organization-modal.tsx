import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { PlusCircleIcon } from "lucide-react";
import { type FormEvent, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { untilAuthorized } from "../../lib/consistency";
import { errorMessage } from "../../lib/errors";
import { organizationKeys, organizationQuery, useCreateOrganization } from "../../lib/queries/organizations";
import { toast } from "../../lib/toast";
import { Button } from "../ui/button";
import { Dialog, DialogBody, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "../ui/dialog";
import { Input } from "../ui/input";
import { Label } from "../ui/label";

/**
 * forma2's CreateOrganizationModal, opened from the organization switcher, organization settings and the landing page for whoever may create an organization (organizationCreationQuery): anyone where public sign-up is open, only the owner of an organization on an invite-only instance.
 *
 * A submission stays in progress from the click until the browser is on the new organization: after the POST returns, the creator's permissions take a few seconds to arrive (untilAuthorized) and the navigation follows, and a button that came back during that wait could create a second organization, as names are not unique. The ref ignores a second submit in the same tick, before the disabled button has rendered; a dismissal meanwhile is held back, since the navigation would follow anyway, and takes effect if the create fails.
 */
export const CreateOrganizationModal = ({
  open,
  setOpen,
}: {
  open: boolean;
  setOpen: (open: boolean) => void;
}) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const submitting = useRef(false);
  const closeRequested = useRef(false);
  const create = useCreateOrganization();

  const close = () => {
    setOpen(false);
    setName("");
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (submitting.current || name.trim() === "") return;
    submitting.current = true;
    closeRequested.current = false;
    setIsSubmitting(true);
    try {
      const organization = await create.mutateAsync(name.trim());
      await untilAuthorized(() => queryClient.fetchQuery(organizationQuery(organization.id)));
      await queryClient.invalidateQueries({ queryKey: organizationKeys.all });
      toast.success(t("workspace.settings.general.organization_created_successfully"));
      await navigate({ href: `/organizations/${organization.id}/` });
      close();
    } catch (error) {
      // A dismissal held back while the create ran takes effect once it has failed. Chrome closes the native dialog on a repeated Escape however the cancel event is handled, and an open state left true then would keep the dialog from ever opening again.
      if (closeRequested.current) close();
      toast.error(errorMessage(t, error));
    } finally {
      submitting.current = false;
      setIsSubmitting(false);
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (next) setOpen(true);
        else if (submitting.current) closeRequested.current = true;
        else close();
      }}
      disableCloseOnOutsideClick>
      <DialogHeader>
        <PlusCircleIcon />
        <DialogTitle>{t("workspace.settings.general.create_new_organization")}</DialogTitle>
        <DialogDescription>
          {t("workspace.settings.general.create_new_organization_description")}
        </DialogDescription>
      </DialogHeader>
      <form onSubmit={submit} className="space-y-4">
        <DialogBody>
          <div className="grid w-full gap-y-2">
            <Label htmlFor="new-organization-name">{t("workspace.settings.general.organization_name")}</Label>
            <Input
              id="new-organization-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t("workspace.settings.general.organization_name_placeholder")}
            />
          </div>
        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="secondary" onClick={close} disabled={isSubmitting}>
            {t("common.cancel")}
          </Button>
          <Button type="submit" loading={isSubmitting} disabled={name.trim() === ""}>
            {t("workspace.settings.general.create_new_organization")}
          </Button>
        </DialogFooter>
      </form>
    </Dialog>
  );
};
