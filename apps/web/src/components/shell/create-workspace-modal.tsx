import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { type FormEvent, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { untilAuthorized } from "../../lib/consistency";
import { errorMessage } from "../../lib/errors";
import { workspaceHomePath } from "../../lib/navigation";
import { organizationKeys } from "../../lib/queries/organizations";
import { teamsQuery } from "../../lib/queries/teams";
import { useCreateWorkspace, workspaceQuery } from "../../lib/queries/workspaces";
import { toast } from "../../lib/toast";
import { workspaceNameError } from "../../lib/validation";
import { Button } from "../ui/button";
import { Dialog, DialogBody, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "../ui/dialog";
import { FormError } from "../ui/form-error";
import { Input } from "../ui/input";
import { Label } from "../ui/label";
import { MultiSelect } from "../ui/multi-select";

/** forma2's CreateWorkspaceModal: a name, and optionally the teams that get access to the new workspace. A submission stays in progress from the click until the browser is on the new workspace, through the few seconds its permissions take to arrive after the POST (untilAuthorized); a button that came back during that wait sent a second POST. The ref ignores a second submit in the same tick, before the disabled button has rendered, and the dialog cannot be dismissed meanwhile, since the navigation would follow anyway. */
export const CreateWorkspaceModal = ({
  open,
  setOpen,
  organizationId,
}: {
  open: boolean;
  setOpen: (open: boolean) => void;
  organizationId: string;
}) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [teamIds, setTeamIds] = useState<string[]>([]);
  const [nameError, setNameError] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const submitting = useRef(false);
  const teams = useQuery({ ...teamsQuery(organizationId), enabled: open });
  const create = useCreateWorkspace(organizationId);

  const teamOptions = [...(teams.data?.userTeams ?? []), ...(teams.data?.otherTeams ?? [])].map((team) => ({
    label: team.name,
    value: team.id,
  }));

  const close = () => {
    setOpen(false);
    setName("");
    setTeamIds([]);
    setNameError("");
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (submitting.current) return;
    const invalid = workspaceNameError(name);
    if (invalid) {
      setNameError(invalid);
      return;
    }
    submitting.current = true;
    setIsSubmitting(true);
    try {
      const workspace = await create.mutateAsync({ name: name.trim(), teamIds });
      await untilAuthorized(() => queryClient.fetchQuery(workspaceQuery(workspace.id)));
      await queryClient.invalidateQueries({ queryKey: organizationKeys.detail(organizationId) });
      toast.success(t("common.workspace_created_successfully"));
      await navigate({ href: workspaceHomePath(workspace.id) });
      close();
    } catch (error) {
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
        else if (!submitting.current) close();
      }}>
      <DialogHeader>
        <DialogTitle>{t("common.create_workspace")}</DialogTitle>
        <DialogDescription>{t("common.workspace_creation_description")}</DialogDescription>
      </DialogHeader>
      <form onSubmit={submit} className="space-y-4" noValidate>
        <DialogBody className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="workspace-name">{t("common.workspace_name")}</Label>
            <Input
              id="workspace-name"
              value={name}
              onChange={(e) => {
                setName(e.target.value);
                setNameError("");
              }}
              placeholder={t("common.workspace_name_placeholder")}
              isInvalid={!!nameError}
              aria-describedby={nameError ? "workspace-name-error" : undefined}
            />
            <FormError id="workspace-name-error">{nameError}</FormError>
          </div>
          {teamOptions.length > 0 && (
            <div className="space-y-2">
              <Label htmlFor="workspace-teams">{t("common.team")}</Label>
              <MultiSelect
                id="workspace-teams"
                value={teamIds}
                options={teamOptions}
                onChange={setTeamIds}
                placeholder={t("common.select_teams")}
              />
            </div>
          )}
        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="secondary" onClick={close} disabled={isSubmitting}>
            {t("common.cancel")}
          </Button>
          <Button type="submit" loading={isSubmitting}>
            {t("common.create_workspace")}
          </Button>
        </DialogFooter>
      </form>
    </Dialog>
  );
};
