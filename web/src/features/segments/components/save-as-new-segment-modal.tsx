import { UsersIcon } from "lucide-react";
import { useState } from "react";
import { SubmitHandler, useForm } from "react-hook-form";
import { errorMessage } from "../../../lib/errors";
import { toast } from "../../../lib/toast";
import { useTranslation } from "react-i18next";
import { TSegment, TSegmentCreateInput, TSegmentUpdateInput } from "@forma/types/segment";
import { TSurvey } from "@forma/types/surveys/types";
import { Button } from "../../../components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../../../components/ui/dialog";
import { Input } from "../../../components/ui/input";

interface SaveAsNewSegmentModalProps {
  open: boolean;
  setOpen: (open: boolean) => void;
  localSurvey: TSurvey;
  segment: TSegment;
  setSegment: (segment: TSegment) => void;
  setIsSegmentEditorOpen: (isOpen: boolean) => void;
  onCreateSegment: (data: TSegmentCreateInput) => Promise<TSegment>;
  onUpdateSegment: (segmentId: string, data: TSegmentUpdateInput) => Promise<TSegment>;
}

type SaveAsNewSegmentModalForm = {
  title: string;
  description?: string;
};

export const SaveAsNewSegmentModal = ({
  open,
  setOpen,
  localSurvey,
  segment,
  setSegment,
  setIsSegmentEditorOpen,
  onCreateSegment,
  onUpdateSegment,
}: SaveAsNewSegmentModalProps) => {
  const {
    register,
    formState: { errors },
    handleSubmit,
    setValue,
  } = useForm<SaveAsNewSegmentModalForm>();

  const [isLoading, setIsLoading] = useState(false);
  const { t } = useTranslation();
  const handleReset = () => {
    setValue("title", "");
    setValue("description", "");
    setOpen(false);
  };

  const handleSaveSegment: SubmitHandler<SaveAsNewSegmentModalForm> = async (data) => {
    if (!segment || !segment?.filters.length) return;

    const createSegment = async () => {
      setIsLoading(true);
      const createdSegment = await onCreateSegment({
        workspaceId: localSurvey.workspaceId,
        surveyId: localSurvey.id,
        title: data.title,
        description: data.description ?? "",
        isPrivate: false,
        filters: segment?.filters,
      });

      setSegment(createdSegment);

      setIsSegmentEditorOpen(false);
      setIsLoading(false);
      toast.success(t("workspace.segments.segment_created_successfully"));
      handleReset();
    };

    const updateSegment = async () => {
      if (!!segment && segment?.isPrivate) {
        const updatedSegment = await onUpdateSegment(segment.id, {
          ...segment,
          title: data.title,
          description: data.description,
          isPrivate: false,
          filters: segment?.filters,
        });

        toast.success(t("workspace.segments.segment_updated_successfully"));
        setSegment(updatedSegment);

        setIsSegmentEditorOpen(false);
        handleReset();
      }
    };

    try {
      // if the segment is private, update it to add title, description and make it public
      // otherwise, create a new segment

      setIsLoading(true);

      if (segment) {
        if (segment.id === "temp") {
          await createSegment();
        } else {
          await updateSegment();
        }

        return;
      }

      await createSegment();
      return;
    } catch (err) {
      toast.error(errorMessage(t, err));
      setIsLoading(false);
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(open) => {
        if (!open) {
          handleReset();
        }
      }}>
      <DialogHeader>
        <UsersIcon className="size-5" />
        <DialogTitle>{t("workspace.segments.save_as_new_segment")}</DialogTitle>
        <DialogDescription>
          {t("workspace.segments.save_your_filters_as_a_segment_to_use_it_in_other_surveys")}
        </DialogDescription>
      </DialogHeader>

      <form className="flex flex-col gap-4" onSubmit={handleSubmit(handleSaveSegment)}>
        <DialogBody className="space-y-4">
          <div>
            <label htmlFor="title" className="text-sm font-medium text-slate-700">
              {t("common.name")}
            </label>

            <Input
              {...register("title", {
                required: {
                  value: true,
                  message: t("workspace.segments.title_is_required"),
                },
              })}
              type="text"
              placeholder={t("workspace.segments.ex_power_users")}
              className="w-full p-2"
            />
            {errors?.title?.message && <p className="text-xs text-red-500">{errors?.title?.message}</p>}
          </div>

          <div>
            <label htmlFor="description" className="text-sm font-medium text-slate-700">
              {t("common.description")}
            </label>
            <Input
              {...register("description")}
              type="text"
              placeholder={t("workspace.segments.most_active_users_in_the_last_30_days")}
              className="w-full p-2"
            />
            {errors?.title?.message && <p className="text-xs text-red-500">{errors?.title?.message}</p>}
          </div>
        </DialogBody>

        <DialogFooter>
          <Button
            type="button"
            variant="secondary"
            onClick={() => {
              handleReset();
            }}>
            {t("common.cancel")}
          </Button>
          <Button type="submit" loading={isLoading}>
            {t("common.save")}
          </Button>
        </DialogFooter>
      </form>
    </Dialog>
  );
};
