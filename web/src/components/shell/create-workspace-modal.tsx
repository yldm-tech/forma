import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { errorMessage } from "../../lib/errors";
import { workspaceHomePath } from "../../lib/navigation";
import { teamsQuery } from "../../lib/queries/teams";
import { useCreateWorkspace } from "../../lib/queries/workspaces";
import { toast } from "../../lib/toast";
import { workspaceNameError } from "../../lib/validation";
import { Button } from "../ui/button";
import { Dialog, DialogBody, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "../ui/dialog";
import { FormError } from "../ui/form-error";
import { Input } from "../ui/input";
import { Label } from "../ui/label";
import { MultiSelect } from "../ui/multi-select";

/** forma2's CreateWorkspaceModal: a name, and optionally the teams that get access to the new workspace. */
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
  const [name, setName] = useState("");
  const [teamIds, setTeamIds] = useState<string[]>([]);
  const [nameError, setNameError] = useState("");
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
    const invalid = workspaceNameError(name);
    if (invalid) {
      setNameError(invalid);
      return;
    }
    try {
      const workspace = await create.mutateAsync({ name: name.trim(), teamIds });
      toast.success(t("common.workspace_created_successfully"));
      close();
      await navigate({ href: workspaceHomePath(workspace.id) });
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  return (
    <Dialog open={open} onOpenChange={(next) => (next ? setOpen(true) : close())}>
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
          <Button type="button" variant="secondary" onClick={close}>
            {t("common.cancel")}
          </Button>
          <Button type="submit" loading={create.isPending}>
            {t("common.create_workspace")}
          </Button>
        </DialogFooter>
      </form>
    </Dialog>
  );
};
