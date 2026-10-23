import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../components/ui/button";
import { FormError } from "../../components/ui/form-error";
import { Input } from "../../components/ui/input";
import { untilAuthorized } from "../../lib/consistency";
import { errorMessage } from "../../lib/errors";
import { organizationKeys, organizationQuery, useCreateOrganization } from "../../lib/queries/organizations";
import { toast } from "../../lib/toast";
import { organizationNameError } from "../../lib/validation";

/** forma2's setup step that creates the user's organization (with its default workspace), then asks them to invite others. */
export const CreateOrganization = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [touched, setTouched] = useState(false);
  const create = useCreateOrganization();
  const error = touched ? organizationNameError(name) : null;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (organizationNameError(name)) return;
    try {
      const organization = await create.mutateAsync(name.trim());
      await untilAuthorized(() => queryClient.fetchQuery(organizationQuery(organization.id)));
      await queryClient.invalidateQueries({ queryKey: organizationKeys.all });
      await navigate({ href: `/setup/organization/${organization.id}/invite` });
    } catch (err) {
      toast.error(errorMessage(t, err));
    }
  };

  return (
    <form onSubmit={submit} noValidate>
      <div className="flex flex-col items-center gap-y-4">
        <h1 className="text-2xl font-medium">{t("setup.organization.create.title")}</h1>
        <p>{t("setup.organization.create.description")}</p>
        <div>
          <Input
            value={name}
            onChange={(e) => {
              setName(e.target.value);
              setTouched(true);
            }}
            aria-label={t("workspace.settings.general.organization_name")}
            aria-describedby={error ? "organization-name-error" : undefined}
            isInvalid={!!error}
            placeholder="e.g., Acme Inc"
            className="w-80"
            required
          />
          <FormError id="organization-name-error" className="mt-1 text-left">
            {error}
          </FormError>
        </div>
        <Button
          type="submit"
          className="flex w-80 justify-center"
          loading={create.isPending}
          disabled={name.trim() === ""}>
          {t("setup.organization.create.continue")}
        </Button>
      </div>
    </form>
  );
};
