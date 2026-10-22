import { useQuery } from "@tanstack/react-query";
import { createColumnHelper } from "@tanstack/react-table";
import { Loader2 } from "lucide-react";
import { type FocusEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../components/ui/button";
import { DataTable } from "../../components/ui/data-table";
import { DeleteDialog } from "../../components/ui/delete-dialog";
import { Input } from "../../components/ui/input";
import { PageContentWrapper, PageHeader, SettingsCard } from "../../components/ui/page";
import { Popover, PopoverContent, PopoverTrigger } from "../../components/ui/popover";
import { cn } from "../../lib/cn";
import { errorMessage } from "../../lib/errors";
import {
  type Tag,
  isDuplicateTagNameError,
  tagsQuery,
  useDeleteTag,
  useMergeTags,
  useRenameTag,
} from "../../lib/queries/tags";
import { toast } from "../../lib/toast";

const column = createColumnHelper<Tag>();

/** forma2's tags page: the workspace's response tags with their counts, renamed inline, merged into one another or deleted. */
export const TagsPage = ({ workspaceId, isReadOnly }: { workspaceId: string; isReadOnly: boolean }) => {
  const { t } = useTranslation();
  const tags = useQuery(tagsQuery(workspaceId));
  const rows = tags.data ?? [];
  const columns = [
    column.accessor("name", {
      header: t("workspace.tags.tag"),
      meta: { headerClassName: "w-[50%]" },
      cell: (info) => (
        <TagNameInput
          tagId={info.row.original.id}
          tagName={info.getValue()}
          workspaceId={workspaceId}
          isReadOnly={isReadOnly}
        />
      ),
    }),
    column.accessor("count", {
      header: t("workspace.tags.count"),
      meta: {
        headerClassName: "w-[15%]",
        align: "center",
        cellClassName: "whitespace-nowrap text-slate-900",
      },
    }),
    ...(isReadOnly
      ? []
      : [
          column.display({
            id: "actions",
            header: t("common.actions"),
            meta: { headerClassName: "w-[35%]", align: "center" },
            cell: (info) => (
              <TagRowActions
                tag={info.row.original}
                workspaceId={workspaceId}
                mergeableTags={rows.filter((candidate) => candidate.id !== info.row.original.id)}
              />
            ),
          }),
        ]),
  ];

  return (
    <PageContentWrapper>
      <PageHeader pageTitle={t("common.tags")} />
      <SettingsCard
        title={t("workspace.tags.manage_tags")}
        description={t("workspace.tags.manage_tags_description")}
        bodyVariant="flush"
        width="full">
        {/* A failed load must not read as a workspace without tags. */}
        {tags.isError && rows.length === 0 ? (
          <div className="flex w-full flex-col items-center justify-center gap-4 py-16 text-slate-600">
            <p>{errorMessage(t, tags.error)}</p>
            <Button variant="secondary" size="sm" onClick={() => void tags.refetch()}>
              {t("common.try_again")}
            </Button>
          </div>
        ) : (
          <DataTable
            columns={columns}
            data={rows}
            getRowId={(tag) => tag.id}
            isLoading={tags.isPending}
            emptyMessage={t("workspace.tags.no_tag_found")}
            aria-label={t("common.tags")}
          />
        )}
      </SettingsCard>
    </PageContentWrapper>
  );
};

/** The tag's name, renamed when the field loses focus with a different trimmed value. */
const TagNameInput = ({
  tagId,
  tagName,
  workspaceId,
  isReadOnly,
}: {
  tagId: string;
  tagName: string;
  workspaceId: string;
  isReadOnly: boolean;
}) => {
  const { t } = useTranslation();
  const [hasError, setHasError] = useState(false);
  const rename = useRenameTag(workspaceId);

  const onBlur = async (event: FocusEvent<HTMLInputElement>) => {
    const name = event.target.value.trim();
    // Leaving the field without editing is not a rename.
    if (name === tagName) return;
    try {
      await rename.mutateAsync({ tagId, name });
      setHasError(false);
      toast.success(t("workspace.tags.tag_updated"));
    } catch (error) {
      setHasError(true);
      toast.error(
        isDuplicateTagNameError(error) ? t("workspace.tags.tag_already_exists") : errorMessage(t, error)
      );
    }
  };

  return (
    <Input
      // Disabled while a rename is in flight, so a second blur cannot race it.
      disabled={isReadOnly || rename.isPending}
      aria-label={t("workspace.tags.tag")}
      className={cn(
        "w-full border font-medium text-slate-900",
        hasError ? "border-red-500 focus:border-red-500" : "border-slate-200 focus:border-slate-500"
      )}
      defaultValue={tagName}
      onBlur={(e) => void onBlur(e)}
    />
  );
};

const TagRowActions = ({
  tag,
  workspaceId,
  mergeableTags,
}: {
  tag: Tag;
  workspaceId: string;
  mergeableTags: Tag[];
}) => {
  const { t } = useTranslation();
  const [confirmDelete, setConfirmDelete] = useState(false);
  const remove = useDeleteTag(workspaceId);
  const merge = useMergeTags(workspaceId);

  const deleteTag = async () => {
    try {
      await remove.mutateAsync(tag.id);
      toast.success(t("workspace.tags.tag_deleted"));
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  const mergeInto = async (newTagId: string) => {
    try {
      await merge.mutateAsync({ tagId: tag.id, newTagId });
      toast.success(t("workspace.tags.tags_merged"));
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  return (
    <div className="flex items-center justify-center gap-2">
      {merge.isPending ? (
        <div className="flex w-24 justify-center">
          <Loader2 className="size-4 animate-spin text-slate-500" aria-label={t("common.loading")} />
        </div>
      ) : (
        <MergeTagsCombobox tags={mergeableTags} onSelect={(id) => void mergeInto(id)} />
      )}
      <Button
        variant="destructive"
        size="sm"
        loading={remove.isPending}
        onClick={() => setConfirmDelete(true)}>
        {t("common.delete")}
      </Button>
      <DeleteDialog
        open={confirmDelete}
        setOpen={setConfirmDelete}
        deleteWhat={tag.name}
        text={t("workspace.tags.delete_tag_confirmation")}
        onDelete={() => void deleteTag()}
        isDeleting={remove.isPending}
      />
    </div>
  );
};

/** forma2's MergeTagsCombobox: a searchable list of the other tags; choosing one merges this tag into it. */
const MergeTagsCombobox = ({ tags, onSelect }: { tags: Tag[]; onSelect: (tagId: string) => void }) => {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const matches = tags.filter((tag) => tag.name.toLowerCase().includes(search.trim().toLowerCase()));
  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setSearch("");
      }}>
      <PopoverTrigger asChild>
        <Button variant="secondary" size="sm" className="font-medium text-slate-900">
          {t("workspace.tags.merge")}
        </Button>
      </PopoverTrigger>
      <PopoverContent className="max-h-60 w-[200px] overflow-y-auto p-1">
        <Input
          aria-label={t("workspace.tags.search_tags")}
          placeholder={t("workspace.tags.search_tags")}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="mb-1 border-none shadow-none"
        />
        {matches.length === 0 ? (
          <div className="p-2 text-sm text-slate-500">{t("workspace.tags.no_tag_found")}</div>
        ) : (
          <ul>
            {matches.map((tag) => (
              <li key={tag.id}>
                <button
                  type="button"
                  className="w-full cursor-pointer rounded-sm px-2 py-1.5 text-left text-sm text-slate-700 hover:bg-slate-100"
                  onClick={() => {
                    setOpen(false);
                    onSelect(tag.id);
                  }}>
                  {tag.name}
                </button>
              </li>
            ))}
          </ul>
        )}
      </PopoverContent>
    </Popover>
  );
};
