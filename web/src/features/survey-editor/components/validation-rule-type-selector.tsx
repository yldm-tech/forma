import { TValidationRuleType } from "@forma/types/surveys/validation-rules";
import { capitalize } from "../../../lib/survey/object";
import {
  SelectMenu,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "../../../components/ui/select-menu";
import { cn } from "../../../lib/cn";
import { RULE_TYPE_CONFIG } from "../lib/validation-rules-config";

interface ValidationRuleTypeSelectorProps {
  value: TValidationRuleType;
  onChange: (value: TValidationRuleType) => void;
  availableTypes: TValidationRuleType[];
  ruleLabels: Record<string, string>;
  needsValue: boolean;
  className?: string;
}

export const ValidationRuleTypeSelector = ({
  value,
  onChange,
  availableTypes,
  ruleLabels,
  needsValue,
  className,
}: Readonly<ValidationRuleTypeSelectorProps>) => {
  return (
    <SelectMenu value={value} onValueChange={(val) => onChange(val as TValidationRuleType)}>
      {/* min-w-0 rather than a pixel floor: a floor here is what pushed the value group out of the
          row and under the delete and add buttons at laptop widths (ENG-3175). */}
      <SelectTrigger className={cn("bg-white", needsValue ? "min-w-0" : "flex-1", className)}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {availableTypes.map((type) => (
          <SelectItem key={type} value={type}>
            {capitalize(ruleLabels[RULE_TYPE_CONFIG[type].labelKey] ?? "")}
          </SelectItem>
        ))}
      </SelectContent>
    </SelectMenu>
  );
};
