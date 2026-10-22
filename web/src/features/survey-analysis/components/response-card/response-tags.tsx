import type { TResponse } from "@forma/types/responses";
import type { TTag } from "@forma/types/tags";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { IdBadge } from "../../../../components/ui/id-badge";
import { Tag } from "../../../../components/ui/tag";
import { ApiError } from "../../../../lib/api";
import { TAG_NAME_ALREADY_EXISTS } from "../../../../lib/queries/tags";
import { toast } from "../../../../lib/toast";
import { analysisApi } from "../../api";
import { ResponseCardMetadata } from "./response-card-metadata";
import { TagsCombobox } from "./tags-combobox";

interface TagRef {
  tagId: string;
  tagName: string;
}

/** forma2's ResponseTagsWrapper: the response's metadata, its tags and the picker that adds or creates one. */
export const ResponseTags = ({
  tags,
  workspaceId,
  responseId,
  environmentTags,
  onTagsChanged,
  isReadOnly,
  response,
  locale,
}: {
  tags: TagRef[];
  workspaceId: string;
  responseId: string;
  environmentTags: TTag[];
  /** Called after a tag was added or removed, to read the response and the workspace's tags again. */
  onTagsChanged: () => void;
  isReadOnly: boolean;
  response: TResponse;
  locale: string;
}) => {
  const { t } = useTranslation();
  const [searchValue, setSearchValue] = useState("");
  const [open, setOpen] = useState(false);
  const [tagsState, setTagsState] = useState(tags);
  const [tagIdToHighlight, setTagIdToHighlight] = useState("");
  const [isLoadingTagOperation, setIsLoadingTagOperation] = useState(false);

  useEffect(() => {
    if (!tagIdToHighlight) return;
    const timeoutId = setTimeout(() => setTagIdToHighlight(""), 2000);
    return () => clearTimeout(timeoutId);
  }, [tagIdToHighlight]);

  const onDelete = async (tagId: string) => {
    setIsLoadingTagOperation(true);
    // Tag drops the row optimistically before calling back; keep the list as it was to restore it on failure.
    const tagsBeforeDelete = tagsState;
    try {
      await analysisApi.removeTag(responseId, tagId);
      onTagsChanged();
    } catch {
      toast.error(t("workspace.surveys.responses.an_error_occurred_deleting_the_tag"));
      setTagsState((current) => (current.some((tag) => tag.tagId === tagId) ? current : tagsBeforeDelete));
    } finally {
      setIsLoadingTagOperation(false);
    }
  };

  const handleCreateTag = async (tagName: string) => {
    setIsLoadingTagOperation(true);
    try {
      let newTag: TTag;
      try {
        newTag = await analysisApi.createTag(workspaceId, tagName);
      } catch (error) {
        toast.error(
          error instanceof ApiError && error.code === TAG_NAME_ALREADY_EXISTS
            ? t("workspace.surveys.responses.tag_already_exists")
            : t("workspace.surveys.responses.an_error_occurred_creating_the_tag")
        );
        return;
      }
      await analysisApi.addTag(responseId, newTag.id);
      setTagsState((current) => [...current, { tagId: newTag.id, tagName: newTag.name }]);
      setTagIdToHighlight(newTag.id);
      onTagsChanged();
      setSearchValue("");
      setOpen(false);
    } catch {
      toast.error(t("workspace.surveys.responses.an_error_occurred_adding_the_tag"));
    } finally {
      setIsLoadingTagOperation(false);
    }
  };

  const handleAddTag = async (tagId: string) => {
    setIsLoadingTagOperation(true);
    setTagsState((current) => [
      ...current,
      { tagId, tagName: environmentTags.find((tag) => tag.id === tagId)?.name ?? "" },
    ]);
    try {
      await analysisApi.addTag(responseId, tagId);
      onTagsChanged();
      setSearchValue("");
      setOpen(false);
    } catch {
      toast.error(t("workspace.surveys.responses.an_error_occurred_adding_the_tag"));
      setTagsState((current) => current.filter((tag) => tag.tagId !== tagId));
    } finally {
      setIsLoadingTagOperation(false);
    }
  };

  return (
    <div className="flex items-center justify-between gap-4 border-t border-slate-200 px-6 py-3">
      <div className="flex flex-wrap items-center gap-2">
        <ResponseCardMetadata response={response} locale={locale} />
        {tagsState.map((tag) => (
          <Tag
            key={tag.tagId}
            onDelete={(tagId) => void onDelete(tagId)}
            tagId={tag.tagId}
            tagName={tag.tagName}
            tags={tagsState}
            setTagsState={setTagsState}
            highlight={tagIdToHighlight === tag.tagId}
            allowDelete={!isReadOnly && !isLoadingTagOperation}
          />
        ))}
        {!isReadOnly && (
          <TagsCombobox
            open={open && !isLoadingTagOperation}
            setOpen={setOpen}
            searchValue={searchValue}
            setSearchValue={setSearchValue}
            tags={environmentTags.map((tag) => ({ value: tag.id, label: tag.name }))}
            currentTags={tagsState.map((tag) => ({ value: tag.tagId, label: tag.tagName }))}
            createTag={(name) => void handleCreateTag(name)}
            addTag={(tagId) => void handleAddTag(tagId)}
          />
        )}
      </div>
      <IdBadge id={responseId} />
    </div>
  );
};
