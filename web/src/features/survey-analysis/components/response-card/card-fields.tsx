import type { TLinkedEmbeddedField } from "@forma/types/embedded-data-resolver";
import type { TResponseData, TResponseVariables, TResponseWithQuotas } from "@forma/types/responses";
import { EyeOffIcon, FileDigitIcon, FileType2Icon, MailIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { TooltipRenderer } from "../../../../components/ui/tooltip";
import { ResponseBadges } from "../response-values/response-values";

// The response card's extra fields: forma2's HiddenFields, ResponseVariables, VerifiedEmail and ResponseCardQuotas.

/** The survey's ingested Embedded Data fields the response holds, by storage key. */
export const HiddenFields = ({
  hiddenFields,
  responseData,
}: {
  hiddenFields: TLinkedEmbeddedField[];
  responseData: TResponseData;
}) => {
  const { t } = useTranslation();
  const fields = hiddenFields.flatMap(({ field, link }) => {
    const value = responseData[link.storageKey];
    return value
      ? [{ storageKey: link.storageKey, label: field.name, value: typeof value === "string" ? value : "" }]
      : [];
  });
  if (fields.length === 0) return null;

  return (
    <div data-testid="main-hidden-fields-div" className="mt-6 flex flex-col gap-6">
      {fields.map((field) => (
        <div key={field.storageKey}>
          <div className="flex gap-x-2 text-sm text-slate-500">
            <p>{field.label}</p>
            <div className="flex items-center gap-x-2 rounded-full bg-slate-100 px-2">
              <TooltipRenderer tooltipContent={t("common.hidden_field")} className="max-w-[300px]">
                <EyeOffIcon className="size-4" />
              </TooltipRenderer>
            </div>
          </div>
          <p className="ph-no-capture mt-2 font-semibold text-slate-700">{field.value}</p>
        </div>
      ))}
    </div>
  );
};

/** The survey's computed Embedded Data fields (variables) the response holds. */
export const ResponseVariables = ({
  variables,
  variablesData,
}: {
  variables: TLinkedEmbeddedField[];
  variablesData: TResponseVariables;
}) => {
  const { t } = useTranslation();
  return (
    <div className="mt-6 flex flex-col gap-6">
      {variables.map(({ field, link }) => {
        const value = variablesData[link.storageKey];
        if (typeof value !== "string" && typeof value !== "number") return null;
        return (
          <div key={link.storageKey}>
            <div className="flex gap-x-2 text-sm text-slate-500">
              <p>{field.name}</p>
              <div className="flex items-center gap-x-2 rounded-full bg-slate-100 px-2">
                <TooltipRenderer tooltipContent={t("common.variable")} className="max-w-[300px]">
                  {field.dataType === "number" ? (
                    <FileDigitIcon className="size-4" />
                  ) : (
                    <FileType2Icon className="size-4" />
                  )}
                </TooltipRenderer>
              </div>
            </div>
            <p className="ph-no-capture mt-2 font-semibold text-slate-700">{value}</p>
          </div>
        );
      })}
    </div>
  );
};

/** The address the respondent verified before answering. */
export const VerifiedEmail = ({ responseData }: { responseData: TResponseData }) => {
  const { t } = useTranslation();
  const email = responseData.verifiedEmail;
  return (
    <div>
      <p className="flex items-center gap-x-2 text-sm text-slate-500">
        <MailIcon className="size-4" />
        <span>{t("common.verified_email")}</span>
      </p>
      <p className="ph-no-capture my-1 font-semibold text-slate-700">
        {typeof email === "string" ? email : ""}
      </p>
    </div>
  );
};

/** The quotas the response counts toward. */
export const ResponseCardQuotas = ({ quotas }: { quotas: TResponseWithQuotas["quotas"] }) => {
  const { t } = useTranslation();
  if (!quotas || quotas.length === 0) return null;
  return (
    <div data-testid="main-quotas-div" className="mt-6 flex flex-col gap-1">
      <p className="text-sm text-slate-500">{t("common.quotas")}</p>
      <div className="flex flex-wrap gap-2">
        {quotas.map((quota) => (
          <ResponseBadges key={quota.id} items={[{ value: quota.name }]} showId={false} />
        ))}
      </div>
    </div>
  );
};
