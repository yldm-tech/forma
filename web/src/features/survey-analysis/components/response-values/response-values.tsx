import { getOriginalFileNameFromUrl } from "@forma/survey-ui/storage";
import { DownloadIcon } from "lucide-react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Checkbox } from "../../../../components/ui/checkbox";
import { IdBadge } from "../../../../components/ui/id-badge";
import { cn } from "../../../../lib/cn";

// The ways the response card and table draw an answer: forma2's ArrayResponse, FileUploadResponse, PictureSelectionResponse, RankingResponse and ResponseBadges.

export const ArrayResponse = ({ value }: { value: string[] }) => (
  <div className="my-1 font-normal text-slate-700" dir="auto">
    {value.map(
      (item, index) =>
        item && (
          <div key={`${index}-${item}`}>
            {item}
            <br />
          </div>
        )
    )}
  </div>
);

export const FileUploadResponse = ({ selected }: { selected: string[] }) => {
  const { t } = useTranslation();
  if (selected.length === 0) return <div className="font-semibold text-slate-500">{t("common.skipped")}</div>;
  return (
    <div>
      {selected.map((fileUrl, index) => (
        <a
          href={fileUrl}
          // A respondent may upload the same file twice; the position tells the links apart.
          // oxlint-disable-next-line react/no-array-index-key -- see above
          key={`${index}-${fileUrl}`}
          target="_blank"
          rel="noopener noreferrer"
          className="group flex max-w-60 items-center justify-center rounded-lg bg-slate-200 px-2 py-1 hover:bg-slate-300">
          <p className="w-full overflow-hidden text-center text-ellipsis whitespace-nowrap text-slate-700 group-hover:text-slate-800">
            {getOriginalFileNameFromUrl(fileUrl) || "Download"}
          </p>
          <DownloadIcon className="p-0.5" strokeWidth={1.5} />
        </a>
      ))}
    </div>
  );
};

export const PictureSelectionResponse = ({
  choices,
  selected,
  isExpanded = true,
  showId,
}: {
  choices: { id: string; imageUrl: string }[];
  selected: unknown;
  isExpanded?: boolean;
  showId: boolean;
}) => {
  if (!Array.isArray(selected)) return null;
  const images = new Map(choices.map((choice) => [choice.id, choice.imageUrl]));
  return (
    <div className={cn("my-1 flex gap-x-5 gap-y-4", isExpanded && "flex-wrap", showId && "flex-col")}>
      {selected.map((id: unknown) => {
        if (typeof id !== "string") return null;
        const imageUrl = images.get(id);
        return (
          <div className="flex items-center gap-2" key={id}>
            {imageUrl && (
              <>
                <img
                  src={imageUrl}
                  alt={imageUrl.split("/").pop() || "Image"}
                  className="h-10 w-16 rounded-lg object-cover"
                />
                {showId && <IdBadge id={id} />}
              </>
            )}
          </div>
        );
      })}
    </div>
  );
};

export const RankingResponse = ({
  value,
  isExpanded,
  showId,
}: {
  value: { value: string; id: string | undefined }[];
  isExpanded: boolean;
  showId: boolean;
}) => (
  <div
    className={cn("text-slate-700", isExpanded ? "space-y-2" : "flex space-x-2", showId && "flex-col")}
    dir="auto">
    {value.map(
      (item, index) =>
        item.value && (
          <div key={item.value} className="flex items-center gap-x-2">
            <span className="text-slate-400">#{index + 1}</span>
            <div className="rounded-sm bg-slate-100 px-2 py-1 font-semibold">{item.value}</div>
            {item.id && showId && <IdBadge id={item.id} />}
          </div>
        )
    )}
  </div>
);

export const ResponseBadges = ({
  items,
  isExpanded = false,
  icon,
  showId,
}: {
  items: { value: string | number; id?: string }[];
  isExpanded?: boolean;
  icon?: ReactNode;
  showId: boolean;
}) => (
  <div className={cn("my-1 flex gap-2", isExpanded && "flex-wrap", showId && "flex-col")}>
    {items.map((item, index) => (
      // Badges are answers, which may repeat; the position tells them apart.
      // oxlint-disable-next-line react/no-array-index-key -- see above
      <div key={`${item.value}-${index}`} className="flex items-center gap-2">
        <span className="flex items-center rounded-md bg-slate-200 px-2 py-1 font-medium">
          {icon && <span className="mr-1.5">{icon}</span>}
          {item.value}
        </span>
        {item.id && showId && <IdBadge id={item.id} />}
      </div>
    ))}
  </div>
);

/** forma2's DecrementQuotasCheckbox: whether deleting responses also gives their quotas the places back. */
export const DecrementQuotasCheckbox = ({
  title,
  checked,
  onCheckedChange,
}: {
  title: string;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
}) => {
  const { t } = useTranslation();
  return (
    <div className="mt-4 flex flex-col gap-3">
      <p className="text-sm text-slate-900">{title}</p>
      <label htmlFor="decrementQuotas" className="flex cursor-pointer items-center">
        <Checkbox
          id="decrementQuotas"
          className="bg-white focus:ring-0"
          checked={checked}
          onCheckedChange={(value) => onCheckedChange(value === true)}
        />
        <span className="ml-2">{t("workspace.surveys.responses.decrement_quotas")}</span>
      </label>
    </div>
  );
};
