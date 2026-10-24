import type { TSurvey } from "@forma/types/surveys/types";
import { Copy, Trash2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../../components/ui/button";
import { DeleteDialog } from "../../../../components/ui/delete-dialog";
import { Input } from "../../../../components/ui/input";
import { Label } from "../../../../components/ui/label";
import { copyText } from "../../../../lib/clipboard";
import { errorMessage } from "../../../../lib/errors";
import { toast } from "../../../../lib/toast";
import { useSurveySlug } from "./use-survey-settings";

/** What the slug field keeps of a keystroke: lowercase letters, digits and hyphens, as forma2's PrettyUrlInput. */
export const normalizeSlugInput = (value: string): string =>
  value.toLowerCase().replaceAll(/[^a-z0-9-]/g, "");

/** forma2's PrettyUrlTab: the survey's readable link, /p/<slug>, set, changed, copied or removed. */
export const PrettyUrlTab = ({
  survey,
  publicDomain,
  isReadOnly,
}: {
  survey: TSurvey;
  publicDomain: string;
  isReadOnly: boolean;
}) => {
  const { t } = useTranslation();
  const slugMutation = useSurveySlug(survey.id);
  const [isEditing, setIsEditing] = useState(!survey.slug);
  const [slug, setSlug] = useState(survey.slug ?? "");
  const [showRemoveDialog, setShowRemoveDialog] = useState(false);

  // A slug saved or removed elsewhere resets the field to it.
  const [storedSlug, setStoredSlug] = useState(survey.slug);
  if (storedSlug !== survey.slug) {
    setStoredSlug(survey.slug);
    setIsEditing(!survey.slug);
    setSlug(survey.slug ?? "");
  }

  const onSubmit = async () => {
    if (!slug.trim()) {
      toast.error(t("workspace.surveys.share.pretty_url.slug_required"));
      return;
    }
    try {
      await slugMutation.mutateAsync(slug);
      toast.success(t("workspace.surveys.share.pretty_url.save_success"));
      setIsEditing(false);
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  const handleRemove = async () => {
    try {
      await slugMutation.mutateAsync(null);
      setShowRemoveDialog(false);
      setSlug("");
      setIsEditing(true);
      toast.success(t("workspace.surveys.share.pretty_url.remove_success"));
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  return (
    <div className="px-1">
      <form
        onSubmit={(event) => {
          event.preventDefault();
          void onSubmit();
        }}
        className="space-y-6">
        <div className="space-y-2">
          <Label htmlFor="pretty-url-slug">{t("workspace.surveys.share.pretty_url.slug_label")}</Label>
          <div className="flex items-center overflow-hidden rounded-md border border-slate-300 bg-white">
            <span className="shrink-0 border-r border-slate-300 bg-slate-50 px-3 py-2 text-sm text-slate-600">
              {publicDomain}/p/
            </span>
            <Input
              id="pretty-url-slug"
              type="text"
              value={slug}
              onChange={(e) => setSlug(normalizeSlugInput(e.target.value))}
              placeholder={t("workspace.surveys.share.pretty_url.slug_placeholder")}
              disabled={isReadOnly || !isEditing}
              className="border-0 bg-white focus-visible:ring-0 focus-visible:ring-offset-0"
            />
          </div>
          <p className="text-sm text-slate-500">{t("workspace.surveys.share.pretty_url.slug_help")}</p>
        </div>

        <div className="flex gap-2">
          {isEditing ? (
            <>
              <Button type="submit" disabled={isReadOnly || slugMutation.isPending}>
                {t("common.save")}
              </Button>
              {survey.slug && (
                <Button
                  type="button"
                  variant="secondary"
                  onClick={() => {
                    setSlug(survey.slug ?? "");
                    setIsEditing(false);
                  }}
                  disabled={slugMutation.isPending}>
                  {t("common.cancel")}
                </Button>
              )}
            </>
          ) : (
            <Button
              type="button"
              variant="secondary"
              onClick={() => setIsEditing(true)}
              disabled={isReadOnly}>
              {t("common.edit")}
            </Button>
          )}

          {survey.slug && !isEditing && (
            <>
              <Button
                type="button"
                onClick={() => void copyText(`${publicDomain}/p/${survey.slug}`, t)}
                disabled={isReadOnly}>
                <Copy className="mr-2 size-4" />
                {t("common.copy")} URL
              </Button>
              <Button
                type="button"
                variant="destructive"
                onClick={() => setShowRemoveDialog(true)}
                disabled={isReadOnly}>
                <Trash2 className="mr-2 size-4" />
                {t("common.remove")}
              </Button>
            </>
          )}
        </div>
      </form>

      <DeleteDialog
        open={showRemoveDialog}
        setOpen={setShowRemoveDialog}
        deleteWhat={t("workspace.surveys.share.pretty_url.title")}
        onDelete={() => void handleRemove()}
        isDeleting={slugMutation.isPending}
        text={t("workspace.surveys.share.pretty_url.remove_description")}
      />
    </div>
  );
};
