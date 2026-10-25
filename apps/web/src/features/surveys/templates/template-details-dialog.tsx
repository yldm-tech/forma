import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { LayoutTemplateIcon, PencilIcon } from "lucide-react";
import { type FormEvent, type ReactNode, useId, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../../../components/ui/dialog";
import { FormError } from "../../../components/ui/form-error";
import { Input } from "../../../components/ui/input";
import { Label } from "../../../components/ui/label";
import { errorMessage } from "../../../lib/errors";
import { toast } from "../../../lib/toast";
import {
  type OrganizationTemplate,
  type OrganizationTemplateDetails,
  TEMPLATE_DESCRIPTION_MAX_LENGTH,
  TEMPLATE_NAME_MAX_LENGTH,
  organizationTemplateKeys,
  saveSurveyAsTemplate,
  updateOrganizationTemplate,
} from "./organization-templates";

/**
 * "Save as template", from the survey list and the editor: the survey as it is stored becomes a template of its organization under the name and description given here, which every workspace of the organization then offers in its template gallery. Offered to owners and managers only, who are the ones the server lets save.
 */
export const SaveAsTemplateDialog = ({
  open,
  setOpen,
  surveyId,
  surveyName,
  workspaceId,
  organizationId,
  notice,
}: {
  open: boolean;
  setOpen: (open: boolean) => void;
  surveyId: string;
  surveyName: string;
  workspaceId: string;
  organizationId: string;
  /** Shown above the fields, for what the caller knows about the survey being saved (the editor's unsaved changes). */
  notice?: ReactNode;
}) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [submitting, setSubmitting] = useState(false);
  const save = async (details: OrganizationTemplateDetails) => {
    await saveSurveyAsTemplate(surveyId, details);
    await queryClient.invalidateQueries({ queryKey: organizationTemplateKeys.list(organizationId) });
    toast.success(t("workspace.surveys.organization_templates.saved"), {
      action: {
        label: t("workspace.surveys.organization_templates.open_gallery"),
        onClick: () => void navigate({ href: `/workspaces/${workspaceId}/surveys/templates` }),
      },
    });
  };
  return (
    <Dialog open={open} onOpenChange={(next) => !submitting && setOpen(next)} width="narrow">
      <DialogHeader>
        <LayoutTemplateIcon />
        <DialogTitle>{t("workspace.surveys.organization_templates.save_as_template")}</DialogTitle>
        <DialogDescription>
          {t("workspace.surveys.organization_templates.save_as_template_description")}
        </DialogDescription>
      </DialogHeader>
      {/* Rendered only while open, so every opening starts from the survey's current name. */}
      {open && (
        <TemplateDetailsForm
          initial={{ name: surveyName, description: "" }}
          notice={notice}
          submitLabel={t("workspace.surveys.organization_templates.save_template")}
          submit={save}
          submitting={submitting}
          setSubmitting={setSubmitting}
          close={() => setOpen(false)}
        />
      )}
    </Dialog>
  );
};

/** Renames one of the organization's templates or changes its description; owners and managers only. */
export const EditOrganizationTemplateDialog = ({
  open,
  setOpen,
  template,
}: {
  open: boolean;
  setOpen: (open: boolean) => void;
  template: OrganizationTemplate;
}) => {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [submitting, setSubmitting] = useState(false);
  const save = async (details: OrganizationTemplateDetails) => {
    await updateOrganizationTemplate(template.id, details);
    await queryClient.invalidateQueries({ queryKey: organizationTemplateKeys.list(template.organizationId) });
    toast.success(t("workspace.surveys.organization_templates.updated"));
  };
  return (
    <Dialog open={open} onOpenChange={(next) => !submitting && setOpen(next)} width="narrow">
      <DialogHeader>
        <PencilIcon />
        <DialogTitle>{t("workspace.surveys.organization_templates.edit_template")}</DialogTitle>
        <DialogDescription>
          {t("workspace.surveys.organization_templates.edit_template_description")}
        </DialogDescription>
      </DialogHeader>
      {open && (
        <TemplateDetailsForm
          initial={{ name: template.name, description: template.description }}
          submitLabel={t("common.save")}
          submit={save}
          submitting={submitting}
          setSubmitting={setSubmitting}
          close={() => setOpen(false)}
        />
      )}
    </Dialog>
  );
};

const TemplateDetailsForm = ({
  initial,
  notice,
  submitLabel,
  submit,
  submitting,
  setSubmitting,
  close,
}: {
  initial: OrganizationTemplateDetails;
  notice?: ReactNode;
  submitLabel: string;
  submit: (details: OrganizationTemplateDetails) => Promise<void>;
  submitting: boolean;
  setSubmitting: (value: boolean) => void;
  close: () => void;
}) => {
  const { t } = useTranslation();
  const id = useId();
  const [name, setName] = useState(initial.name.slice(0, TEMPLATE_NAME_MAX_LENGTH));
  const [description, setDescription] = useState(initial.description);
  const [invalid, setInvalid] = useState(false);

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    const trimmed = name.trim();
    if (trimmed === "") {
      setInvalid(true);
      return;
    }
    setSubmitting(true);
    try {
      await submit({ name: trimmed, description: description.trim() });
      close();
    } catch (error) {
      toast.error(errorMessage(t, error));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
      <DialogBody className="space-y-4 p-1">
        {notice}
        <div className="space-y-2">
          <Label htmlFor={`${id}-name`}>{t("common.name")}</Label>
          <Input
            id={`${id}-name`}
            value={name}
            maxLength={TEMPLATE_NAME_MAX_LENGTH}
            onChange={(e) => {
              setName(e.target.value);
              setInvalid(false);
            }}
            isInvalid={invalid}
            aria-describedby={invalid ? `${id}-name-error` : undefined}
          />
          <FormError id={`${id}-name-error`}>
            {invalid ? t("workspace.surveys.organization_templates.name_required") : undefined}
          </FormError>
        </div>
        <div className="space-y-2">
          <Label htmlFor={`${id}-description`}>{t("common.description")}</Label>
          <textarea
            id={`${id}-description`}
            rows={3}
            maxLength={TEMPLATE_DESCRIPTION_MAX_LENGTH}
            placeholder={t("workspace.surveys.organization_templates.description_placeholder")}
            className="focus:border-ink focus:ring-ring flex w-full rounded-md border border-slate-300 bg-transparent px-3 py-2 text-sm text-slate-800 placeholder:text-slate-400 focus:ring-2 focus:ring-offset-2 focus:outline-hidden"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
        </div>
      </DialogBody>
      <DialogFooter>
        <Button type="button" variant="secondary" onClick={close} disabled={submitting}>
          {t("common.cancel")}
        </Button>
        <Button type="submit" loading={submitting}>
          {submitLabel}
        </Button>
      </DialogFooter>
    </form>
  );
};
