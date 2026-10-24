import { useTranslation } from "react-i18next";
import { TSurveyOpenTextElementInputType } from "@forma/types/surveys/elements";
import {
  SelectMenu,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "../../../components/ui/select-menu";
import { cn } from "../../../lib/cn";

interface ValidationRuleInputTypeSelectorProps {
  value: TSurveyOpenTextElementInputType;
  onChange?: (value: TSurveyOpenTextElementInputType) => void;
  disabled?: boolean;
}

export const ValidationRuleInputTypeSelector = ({
  value,
  onChange,
  disabled = false,
}: Readonly<ValidationRuleInputTypeSelectorProps>) => {
  const { t } = useTranslation();

  return (
    <SelectMenu
      value={value}
      onValueChange={onChange ? (val) => onChange(val as TSurveyOpenTextElementInputType) : undefined}
      disabled={disabled}>
      {/* w-auto, not the trigger's default w-full: this only ever sits on a single-line rule row, where
          a basis of 100% would make the row split into equal thirds and starve the value group. */}
      <SelectTrigger
        className={cn("h-9 w-auto min-w-0 grow", disabled ? "cursor-not-allowed bg-slate-100" : "bg-white")}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="text">{t("common.text")}</SelectItem>
        <SelectItem value="email">{t("common.email")}</SelectItem>
        <SelectItem value="url">{t("common.url")}</SelectItem>
        <SelectItem value="phone">{t("common.phone")}</SelectItem>
        <SelectItem value="number">{t("common.number")}</SelectItem>
      </SelectContent>
    </SelectMenu>
  );
};
