import {
  ARITHMETIC_OPERATORS,
  DEVICE_OPERATORS,
  PERSON_OPERATORS,
  SURVEY_INTERACTION_OPERATORS,
  SURVEY_INTERACTION_TIME_UNITS,
  type TAllOperators,
  type TArithmeticOperator,
  type TBaseFilter,
  type TBaseFilters,
  type TDateOperator,
  type TSegmentAttributeFilter,
  type TSegmentConnector,
  type TSegmentDeviceFilter,
  type TSegmentFilter,
  type TSegmentFilterValue,
  type TSegmentPersonFilter,
  type TSegmentSegmentFilter,
  type TSegmentSurveyInteractionFilter,
  type TSurveyInteractionTimeUnit,
  type TTimeUnit,
  isDateOperator,
} from "@forma/types/segment";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowDownIcon,
  ArrowUpIcon,
  CalendarIcon,
  FingerprintIcon,
  HashIcon,
  MonitorSmartphoneIcon,
  MoreVertical,
  MousePointerClickIcon,
  TagIcon,
  Trash2,
  Users2Icon,
} from "lucide-react";
import { type ReactNode, useId, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../components/ui/button";
import { Dialog, DialogBody, DialogHeader, DialogTitle } from "../../components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "../../components/ui/dropdown-menu";
import { Input } from "../../components/ui/input";
import { MultiSelect } from "../../components/ui/multi-select";
import { Select } from "../../components/ui/select";
import { type AttributeKey, attributeKeyLabel } from "../../lib/attribute-keys";
import { cn } from "../../lib/cn";
import { dayToStoredValue, storedDayForInput } from "../../lib/contact-attributes-form";
import { type Segment, attributeValuesQuery, segmentSurveysQuery } from "../../lib/queries/contacts";
import {
  addFilterBelow,
  addFilterInGroup,
  changeAttributeKey,
  convertOperatorToText,
  convertOperatorToTitle,
  createGroupFromResource,
  deleteResource,
  isResourceFilter,
  moveResource,
  newSegmentFilter,
  operatorsForDataType,
  toggleFilterConnector,
  toggleGroupConnector,
  updateDeviceTypeInFilter,
  updateFilterValue,
  updateOperatorInFilter,
  updateSegmentIdInFilter,
  updateSurveyInteractionValueInFilter,
} from "../../lib/segment-filters";

const DATA_TYPE_ICONS = { date: CalendarIcon, number: HashIcon, string: TagIcon } as const;

/** Everything a node of the editor needs: the workspace's keys and segments, the segment's linked surveys, and a way to change the tree. */
interface EditorContext {
  workspaceId: string;
  attributeKeys: AttributeKey[];
  segments: Segment[];
  /** The surveys the edited segment gates, which its survey-interaction filters may not name. */
  linkedSurveyIds: string[];
  /** Applies a change to a copy of the whole tree and keeps the copy. */
  edit: (change: (tree: TBaseFilters) => void) => void;
  viewOnly: boolean;
}

/** forma2's SegmentEditor: a group's filters and nested groups, each with its connector and actions. */
export const SegmentEditor = ({ group, context }: { group: TBaseFilters; context: EditorContext }) => {
  const { edit, viewOnly } = context;
  return (
    <div className="flex flex-col gap-4 rounded-lg">
      {group.map((node) => {
        const { connector, resource, id: nodeId } = node;
        if (isResourceFilter(resource)) {
          return (
            <SegmentFilterRow key={nodeId} connector={connector} resource={resource} context={context} />
          );
        }
        return (
          <div key={nodeId} className="flex items-start gap-2">
            <ConnectorToggle
              connector={connector}
              viewOnly={viewOnly}
              onToggle={(next) => edit((tree) => toggleGroupConnector(tree, nodeId, next))}
            />
            <div className="rounded-lg border-2 border-slate-300 bg-white p-4">
              <SegmentEditor group={resource} context={context} />
              <div className="mt-4">
                <AddFilterButton
                  context={context}
                  onAdd={(filter) => edit((tree) => addFilterInGroup(tree, nodeId, filter))}
                />
              </div>
            </div>
            <div className="flex items-center gap-2 p-4">
              <NodeActions resourceId={nodeId} context={context} />
            </div>
          </div>
        );
      })}
    </div>
  );
};

/** The add-filter button and its modal. */
export const AddFilterButton = ({
  context,
  onAdd,
  className,
}: {
  context: Pick<EditorContext, "attributeKeys" | "segments" | "viewOnly">;
  onAdd: (filter: TBaseFilter) => void;
  className?: string;
}) => {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button
        size="sm"
        variant="secondary"
        className={className}
        disabled={context.viewOnly}
        onClick={() => setOpen(true)}>
        {t("common.add_filter")}
      </Button>
      {open && (
        <AddFilterModal
          attributeKeys={context.attributeKeys}
          segments={context.segments}
          setOpen={setOpen}
          onAddFilter={(filter) => {
            onAdd(filter);
            setOpen(false);
          }}
        />
      )}
    </>
  );
};

/** A node's connector: "where" for the first, else and/or, switched by a click. */
const ConnectorToggle = ({
  connector,
  viewOnly,
  onToggle,
}: {
  connector: TSegmentConnector;
  viewOnly: boolean;
  onToggle: (next: TSegmentConnector) => void;
}) => {
  const { t } = useTranslation();
  return (
    <div className="w-[40px] text-sm">
      <button
        type="button"
        aria-label={connector ?? t("workspace.segments.where")}
        className={cn(Boolean(connector) && "cursor-pointer underline", viewOnly && "cursor-not-allowed")}
        onClick={() => {
          if (viewOnly || !connector) return;
          onToggle(connector === "and" ? "or" : "and");
        }}>
        {connector ?? t("workspace.segments.where")}
      </button>
    </div>
  );
};

/** A node's menu (add a filter below, wrap in a group, move) and its delete button. */
const NodeActions = ({ resourceId, context }: { resourceId: string; context: EditorContext }) => {
  const { t } = useTranslation();
  const { edit, viewOnly } = context;
  const [addOpen, setAddOpen] = useState(false);
  return (
    <div className="flex items-center gap-2">
      <DropdownMenu>
        <DropdownMenuTrigger asChild disabled={viewOnly}>
          <Button variant="secondary" size="icon" aria-label={t("common.actions")}>
            <MoreVertical className="size-4" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent>
          <DropdownMenuItem onClick={() => setAddOpen(true)}>
            {t("workspace.segments.add_filter_below")}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => edit((tree) => createGroupFromResource(tree, resourceId))}>
            {t("workspace.segments.create_group")}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => edit((tree) => moveResource(tree, resourceId, "up"))}>
            <ArrowUpIcon className="size-4" />
            {t("common.move_up")}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => edit((tree) => moveResource(tree, resourceId, "down"))}>
            <ArrowDownIcon className="size-4" />
            {t("common.move_down")}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <Button
        size="icon"
        variant="secondary"
        aria-label={t("common.delete")}
        disabled={viewOnly}
        onClick={() => edit((tree) => deleteResource(tree, resourceId))}>
        <Trash2 className="size-4" />
      </Button>
      {addOpen && (
        <AddFilterModal
          attributeKeys={context.attributeKeys}
          segments={context.segments}
          setOpen={setAddOpen}
          onAddFilter={(filter) => {
            edit((tree) => addFilterBelow(tree, resourceId, filter));
            setAddOpen(false);
          }}
        />
      )}
    </div>
  );
};

const OperatorSelect = <O extends TAllOperators>({
  value,
  operators,
  onChange,
  disabled,
  label,
}: {
  value: O;
  operators: readonly O[];
  onChange: (operator: O) => void;
  disabled: boolean;
  label: string;
}) => {
  const { t } = useTranslation();
  return (
    <Select
      aria-label={label}
      className="h-9 w-auto bg-white"
      disabled={disabled}
      value={value}
      title={convertOperatorToTitle(value, t)}
      onChange={(e) => onChange(e.target.value as O)}>
      {operators.map((operator) => (
        <option key={operator} value={operator} title={convertOperatorToTitle(operator, t)}>
          {convertOperatorToText(operator, t)}
        </option>
      ))}
    </Select>
  );
};

/** forma2's text and number value field: empty is refused, an arithmetic operator needs a number, which is stored as one. */
const ValueInput = ({
  resource,
  context,
  suggestions,
}: {
  resource: TSegmentAttributeFilter | TSegmentPersonFilter;
  context: EditorContext;
  suggestions?: string[];
}) => {
  const { t } = useTranslation();
  const listId = useId();
  // forma2 flagged an empty value once it was edited, and a non-number for an arithmetic operator at any time.
  const [touched, setTouched] = useState(false);
  const isArithmetic = ARITHMETIC_OPERATORS.includes(resource.qualifier.operator as TArithmeticOperator);
  const raw = resource.value as string | number;
  const error =
    touched && raw === ""
      ? t("workspace.segments.value_cannot_be_empty")
      : isArithmetic && Number.isNaN(Number(raw))
        ? t("workspace.segments.value_must_be_a_number")
        : "";
  return (
    <div className="relative flex flex-col gap-1">
      <Input
        aria-label={t("workspace.segments.number")}
        className={cn("h-9 w-auto bg-white", error && "border border-red-500 focus:border-red-500")}
        disabled={context.viewOnly}
        list={suggestions ? listId : undefined}
        value={raw}
        onChange={(e) => {
          setTouched(true);
          const { value } = e.target;
          const next: TSegmentFilterValue =
            isArithmetic && value !== "" && !Number.isNaN(Number(value)) ? Number.parseInt(value, 10) : value;
          context.edit((tree) => updateFilterValue(tree, resource.id, next));
        }}
      />
      {suggestions && (
        <datalist id={listId}>
          {suggestions.map((value) => (
            <option key={value} value={value} />
          ))}
        </datalist>
      )}
      {error && (
        <p className="absolute top-full right-2 -mt-1 rounded-md bg-white px-2 text-xs text-red-500">
          {error}
        </p>
      )}
    </div>
  );
};

/** forma2's DateFilterValue: an amount and unit for relative operators, two days for "between", one day otherwise. */
const DateValueInput = ({
  operator,
  value,
  onChange,
  viewOnly,
}: {
  operator: TDateOperator;
  value: TSegmentFilterValue;
  onChange: (value: TSegmentFilterValue) => void;
  viewOnly: boolean;
}) => {
  const { t } = useTranslation();
  const [error, setError] = useState("");
  if (operator === "isOlderThan" || operator === "isNewerThan") {
    const relative =
      typeof value === "object" && value !== null && "amount" in value && "unit" in value
        ? (value as { amount: number; unit: TTimeUnit })
        : { amount: 1, unit: "days" as TTimeUnit };
    return (
      <div className="flex items-center gap-2">
        <Input
          type="number"
          min={1}
          aria-label={t("workspace.segments.number")}
          className={cn("h-9 w-20 bg-white", error && "border border-red-500")}
          disabled={viewOnly}
          value={relative.amount}
          onChange={(e) => {
            const amount = Number.parseInt(e.target.value, 10);
            if (Number.isNaN(amount) || amount < 1) {
              setError(t("workspace.segments.value_must_be_positive"));
              return;
            }
            setError("");
            onChange({ amount, unit: relative.unit });
          }}
        />
        <Select
          aria-label={t("workspace.segments.period")}
          className="h-9 w-auto bg-white"
          disabled={viewOnly}
          value={relative.unit}
          onChange={(e) => onChange({ amount: relative.amount, unit: e.target.value as TTimeUnit })}>
          <option value="days">{t("common.days")}</option>
          <option value="weeks">{t("common.weeks")}</option>
          <option value="months">{t("common.months")}</option>
          <option value="years">{t("common.years")}</option>
        </Select>
        {error && <p className="text-xs text-red-500">{error}</p>}
      </div>
    );
  }
  if (operator === "isBetween") {
    const between =
      Array.isArray(value) && value.length === 2
        ? (value as [string, string])
        : (["", ""] as [string, string]);
    return (
      <div className="flex items-center gap-2">
        <Input
          type="date"
          aria-label={t("workspace.segments.operator_is_after")}
          className="h-9 w-[180px] bg-white"
          disabled={viewOnly}
          value={storedDayForInput(between[0])}
          onChange={(e) => onChange([dayToStoredValue(e.target.value), between[1]])}
        />
        <span className="text-sm text-slate-600">{t("common.and")}</span>
        <Input
          type="date"
          aria-label={t("workspace.segments.operator_is_before")}
          className="h-9 w-[180px] bg-white"
          disabled={viewOnly}
          value={storedDayForInput(between[1])}
          onChange={(e) => onChange([between[0], dayToStoredValue(e.target.value)])}
        />
      </div>
    );
  }
  return (
    <Input
      type="date"
      aria-label={convertOperatorToText(operator, t)}
      className="h-9 w-[180px] bg-white"
      disabled={viewOnly}
      value={storedDayForInput(typeof value === "string" ? value : "")}
      onChange={(e) => onChange(dayToStoredValue(e.target.value))}
    />
  );
};

/** One filter of the tree, laid out for its kind. */
const SegmentFilterRow = ({
  connector,
  resource,
  context,
}: {
  connector: TSegmentConnector;
  resource: TSegmentFilter;
  context: EditorContext;
}) => {
  const { t } = useTranslation();
  const { edit, viewOnly } = context;
  const connectorToggle = (
    <ConnectorToggle
      connector={connector}
      viewOnly={viewOnly}
      onToggle={(next) => edit((tree) => toggleFilterConnector(tree, resource.id, next))}
    />
  );
  const actions = <NodeActions resourceId={resource.id} context={context} />;
  const setOperator = (operator: TAllOperators) =>
    edit((tree) =>
      updateOperatorInFilter(tree, resource.id, operator as Parameters<typeof updateOperatorInFilter>[2])
    );

  switch (resource.root.type) {
    case "attribute":
      return (
        <AttributeFilterRow
          resource={resource as TSegmentAttributeFilter}
          context={context}
          connectorToggle={connectorToggle}
          actions={actions}
        />
      );
    case "person": {
      const person = resource as TSegmentPersonFilter;
      return (
        <div className="flex items-center gap-2 text-sm">
          {connectorToggle}
          <div className="flex h-9 items-center gap-1 rounded-md border border-slate-300 bg-white px-3 lowercase">
            <FingerprintIcon className="size-4" />
            <span>{person.root.personIdentifier}</span>
          </div>
          <OperatorSelect
            label={t("common.select")}
            value={person.qualifier.operator}
            operators={PERSON_OPERATORS}
            disabled={viewOnly}
            onChange={setOperator}
          />
          {!["isSet", "isNotSet"].includes(person.qualifier.operator) && (
            <ValueInput resource={person} context={context} />
          )}
          {actions}
        </div>
      );
    }
    case "segment": {
      const segmentFilter = resource as TSegmentSegmentFilter;
      const operatorText = convertOperatorToText(segmentFilter.qualifier.operator, t);
      return (
        <div className="flex items-center gap-2 text-sm">
          {connectorToggle}
          <button
            type="button"
            aria-label={operatorText}
            className={cn("cursor-pointer underline", viewOnly && "cursor-not-allowed")}
            onClick={() => {
              if (viewOnly) return;
              setOperator(segmentFilter.qualifier.operator === "userIsIn" ? "userIsNotIn" : "userIsIn");
            }}>
            {operatorText}
          </button>
          <div className="flex items-center gap-1">
            <Users2Icon className="size-4" />
            <Select
              aria-label={t("common.segments")}
              className="h-9 w-auto bg-white"
              disabled={viewOnly}
              value={segmentFilter.root.segmentId}
              onChange={(e) => edit((tree) => updateSegmentIdInFilter(tree, resource.id, e.target.value))}>
              {context.segments
                .filter((segment) => !segment.isPrivate)
                .map((segment) => (
                  <option key={segment.id} value={segment.id}>
                    {segment.title}
                  </option>
                ))}
            </Select>
          </div>
          {actions}
        </div>
      );
    }
    case "device": {
      const device = resource as TSegmentDeviceFilter;
      return (
        <div className="flex items-center gap-2 text-sm">
          {connectorToggle}
          <div className="flex h-9 items-center gap-2 rounded-lg border border-slate-300 bg-white px-3">
            <MonitorSmartphoneIcon className="size-4" />
            <p>Device</p>
          </div>
          <OperatorSelect
            label={t("common.select")}
            value={device.qualifier.operator}
            operators={DEVICE_OPERATORS}
            disabled={viewOnly}
            onChange={setOperator}
          />
          <Select
            aria-label={t("workspace.segments.devices")}
            className="h-9 w-auto bg-white"
            disabled={viewOnly}
            value={device.value as string}
            onChange={(e) =>
              edit((tree) =>
                updateDeviceTypeInFilter(tree, resource.id, e.target.value as "phone" | "desktop")
              )
            }>
            <option value="desktop">{t("workspace.segments.desktop")}</option>
            <option value="phone">{t("workspace.segments.phone")}</option>
          </Select>
          {actions}
        </div>
      );
    }
    case "surveyInteraction":
      return (
        <SurveyInteractionRow
          resource={resource as TSegmentSurveyInteractionFilter}
          context={context}
          connectorToggle={connectorToggle}
          actions={actions}
        />
      );
    default:
      return <div>{t("workspace.segments.unknown_filter_type")}</div>;
  }
};

const AttributeFilterRow = ({
  resource,
  context,
  connectorToggle,
  actions,
}: {
  resource: TSegmentAttributeFilter;
  context: EditorContext;
  connectorToggle: ReactNode;
  actions: ReactNode;
}) => {
  const { t } = useTranslation();
  const { edit, viewOnly, attributeKeys } = context;
  const key = attributeKeys.find((attributeKey) => attributeKey.key === resource.root.contactAttributeKey);
  const dataType = key?.dataType ?? "string";
  const values = useQuery({
    ...attributeValuesQuery(key?.id ?? ""),
    enabled: dataType === "string" && !!key?.id,
  });
  const { operator } = resource.qualifier;
  return (
    <div className="flex items-center gap-2 text-sm">
      {connectorToggle}
      <Select
        aria-label={t("common.attributes")}
        className="h-9 w-auto bg-white"
        disabled={viewOnly}
        value={resource.root.contactAttributeKey}
        onChange={(e) => {
          const next = attributeKeys.find((attributeKey) => attributeKey.key === e.target.value);
          edit((tree) => changeAttributeKey(tree, resource.id, e.target.value, next?.dataType ?? "string"));
        }}>
        {!key && (
          <option value={resource.root.contactAttributeKey}>{resource.root.contactAttributeKey}</option>
        )}
        {attributeKeys.map((attributeKey) => (
          <option key={attributeKey.id} value={attributeKey.key}>
            {attributeKeyLabel(attributeKey)}
          </option>
        ))}
      </Select>
      <OperatorSelect
        label={t("common.select")}
        value={operator}
        operators={operatorsForDataType(dataType) as readonly (typeof operator)[]}
        disabled={viewOnly}
        onChange={(next) => edit((tree) => updateOperatorInFilter(tree, resource.id, next))}
      />
      {!["isSet", "isNotSet"].includes(operator) &&
        (dataType === "date" && isDateOperator(operator) ? (
          <DateValueInput
            operator={operator}
            value={resource.value}
            viewOnly={viewOnly}
            onChange={(value) => edit((tree) => updateFilterValue(tree, resource.id, value))}
          />
        ) : (
          <ValueInput
            resource={resource}
            context={context}
            suggestions={dataType === "string" ? values.data : undefined}
          />
        ))}
      {actions}
    </div>
  );
};

/** forma2's SurveyInteractionFilter: what the contact did with which surveys, within how long. */
const SurveyInteractionRow = ({
  resource,
  context,
  connectorToggle,
  actions,
}: {
  resource: TSegmentSurveyInteractionFilter;
  context: EditorContext;
  connectorToggle: ReactNode;
  actions: ReactNode;
}) => {
  const { t } = useTranslation();
  const { edit, viewOnly } = context;
  const value = resource.value;
  const surveys = useQuery(segmentSurveysQuery(context.workspaceId));
  const commit = (next: typeof value) =>
    edit((tree) => updateSurveyInteractionValueInFilter(tree, resource.id, next));
  const unitLabel = (unit: TSurveyInteractionTimeUnit, amount: number) => {
    const singular = amount === 1;
    if (unit === "days")
      return singular ? t("workspace.segments.time_unit_day") : t("workspace.segments.time_unit_days");
    if (unit === "weeks")
      return singular ? t("workspace.segments.time_unit_week") : t("workspace.segments.time_unit_weeks");
    return singular ? t("workspace.segments.time_unit_month") : t("workspace.segments.time_unit_months");
  };
  // A segment cannot target by interaction with a survey it gates itself.
  const options = useMemo(
    () =>
      (surveys.data ?? [])
        .filter((survey) => !context.linkedSurveyIds.includes(survey.id))
        .map((survey) => ({ value: survey.id, label: survey.name || survey.id })),
    [surveys.data, context.linkedSurveyIds]
  );
  return (
    <div className="flex flex-col gap-2 text-sm">
      <div className="flex items-center gap-2">
        {connectorToggle}
        <OperatorSelect
          label={t("workspace.segments.survey_interaction")}
          value={resource.qualifier.operator}
          operators={SURVEY_INTERACTION_OPERATORS}
          disabled={viewOnly}
          onChange={(operator) => edit((tree) => updateOperatorInFilter(tree, resource.id, operator))}
        />
        <Select
          aria-label={t("common.surveys")}
          className="h-9 w-auto bg-white"
          disabled={viewOnly}
          value={value.surveyScope}
          onChange={(e) => commit({ ...value, surveyScope: e.target.value as "any" | "specific" })}>
          <option value="any">{t("workspace.segments.any_survey")}</option>
          <option value="specific">{t("workspace.segments.specific_surveys")}</option>
        </Select>
        <p className="whitespace-nowrap text-slate-600">{t("workspace.segments.within_last")}</p>
        <Input
          aria-label={t("workspace.segments.number")}
          className="h-9 w-16 bg-white"
          disabled={viewOnly}
          type="number"
          min={1}
          max={999}
          step={1}
          value={value.within.amount}
          onChange={(e) => {
            const parsed = Number.parseInt(e.target.value, 10);
            if (Number.isNaN(parsed)) return;
            commit({ ...value, within: { ...value.within, amount: Math.min(999, Math.max(1, parsed)) } });
          }}
        />
        <Select
          aria-label={t("workspace.segments.period")}
          className="h-9 w-auto bg-white"
          disabled={viewOnly}
          value={value.within.unit}
          onChange={(e) =>
            commit({
              ...value,
              within: { ...value.within, unit: e.target.value as TSurveyInteractionTimeUnit },
            })
          }>
          {SURVEY_INTERACTION_TIME_UNITS.map((unit) => (
            <option key={unit} value={unit}>
              {unitLabel(unit, value.within.amount)}
            </option>
          ))}
        </Select>
        {actions}
      </div>
      {value.surveyScope === "specific" && (
        <div className="ml-[48px] flex flex-col gap-1 rounded-lg border border-slate-300 bg-white p-3">
          <p className="text-xs font-medium text-slate-500">{t("common.surveys")}</p>
          <MultiSelect
            disabled={viewOnly}
            value={value.surveyIds}
            options={options}
            placeholder={t("common.select")}
            onChange={(surveyIds) => commit({ ...value, surveyIds })}
          />
          {value.surveyIds.length === 0 && (
            <p className="text-xs text-red-500">{t("workspace.segments.select_at_least_one_survey")}</p>
          )}
        </div>
      )}
    </div>
  );
};

type FilterTab = "all" | "attributes" | "segments" | "devices";

/** forma2's AddFilterModal: the filters that can be added, searchable and grouped in tabs. */
const AddFilterModal = ({
  attributeKeys,
  segments,
  setOpen,
  onAddFilter,
}: {
  attributeKeys: AttributeKey[];
  segments: Segment[];
  setOpen: (open: boolean) => void;
  onAddFilter: (filter: TBaseFilter) => void;
}) => {
  const { t } = useTranslation();
  const [tab, setTab] = useState<FilterTab>("all");
  const [search, setSearch] = useState("");
  const query = search.toLowerCase();
  const add = (options: Parameters<typeof newSegmentFilter>[0]) => {
    const filter = newSegmentFilter(options);
    if (filter) onAddFilter(filter);
  };
  // The userId key is offered as the person filter.
  const keys = attributeKeys
    .filter((key) => key.key !== "userId")
    .filter((key) => !search || attributeKeyLabel(key).toLowerCase().includes(query));
  const people = [{ id: "userId", label: t("common.user_id") }].filter(
    (person) => person.id.toLowerCase().includes(query) || person.label.toLowerCase().includes(query)
  );
  const publicSegments = segments
    .filter((segment) => !segment.isPrivate)
    .filter((segment) => !search || segment.title.toLowerCase().includes(query));
  const devices = [
    { id: "phone", name: t("workspace.segments.phone") },
    { id: "desktop", name: t("workspace.segments.desktop") },
  ].filter((device) => !search || device.name.toLowerCase().includes(query));
  const surveyInteractionLabel = t("workspace.segments.survey_interaction");
  const showSurveyInteraction = !search || surveyInteractionLabel.toLowerCase().includes(query);

  const keyButtons = keys.map((key) => {
    const Icon = DATA_TYPE_ICONS[key.dataType];
    return (
      <FilterButton
        key={key.id}
        icon={<Icon className="size-4" />}
        label={attributeKeyLabel(key)}
        onClick={() =>
          add({ type: "attribute", contactAttributeKey: key.key, attributeDataType: key.dataType })
        }
      />
    );
  });
  const personButtons = people.map((person) => (
    <FilterButton
      key={person.id}
      icon={<FingerprintIcon className="size-4" />}
      label={person.label}
      onClick={() => add({ type: "person" })}
    />
  ));
  const segmentButtons = publicSegments.map((segment) => (
    <FilterButton
      key={segment.id}
      icon={<Users2Icon className="size-4" />}
      label={segment.title}
      onClick={() => add({ type: "segment", segmentId: segment.id })}
    />
  ));
  const deviceButtons = devices.map((device) => (
    <FilterButton
      key={device.id}
      icon={<MonitorSmartphoneIcon className="size-4" />}
      label={device.name}
      onClick={() => add({ type: "device", deviceType: device.id })}
    />
  ));
  const empty = (message: string) => (
    <div className="flex w-full items-center justify-center rounded-lg px-2 py-1 text-sm">
      <p>{message}</p>
    </div>
  );

  let content: ReactNode;
  if (tab === "attributes") {
    content =
      keyButtons.length + personButtons.length === 0
        ? empty(t("workspace.segments.no_attributes_yet"))
        : [...keyButtons, ...personButtons];
  } else if (tab === "segments") {
    content = segmentButtons.length === 0 ? empty(t("workspace.segments.no_segments_yet")) : segmentButtons;
  } else if (tab === "devices") {
    content = deviceButtons;
  } else {
    const all = [
      ...keyButtons,
      ...personButtons,
      ...(showSurveyInteraction
        ? [
            <FilterButton
              key="surveyInteraction"
              icon={<MousePointerClickIcon className="size-4" />}
              label={surveyInteractionLabel}
              onClick={() => add({ type: "surveyInteraction" })}
            />,
          ]
        : []),
      ...segmentButtons,
      ...deviceButtons,
    ];
    content = all.length === 0 ? empty(t("workspace.segments.no_filters_yet")) : all;
  }

  const tabs: { id: FilterTab; label: string }[] = [
    { id: "all", label: t("common.all") },
    { id: "attributes", label: t("workspace.segments.person_and_attributes") },
    { id: "segments", label: t("common.segments") },
    { id: "devices", label: t("workspace.segments.devices") },
  ];

  return (
    <Dialog open onOpenChange={setOpen} width="narrow" disableCloseOnOutsideClick>
      <DialogHeader>
        <DialogTitle>{t("common.add_filter")}</DialogTitle>
      </DialogHeader>
      <DialogBody>
        <div className="flex flex-col">
          <Input
            aria-label={t("common.search")}
            placeholder="Browse filters..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          <div role="tablist" className="mt-2 flex gap-2 border-b border-slate-200">
            {tabs.map((item) => (
              <button
                type="button"
                role="tab"
                key={item.id}
                aria-selected={tab === item.id}
                onClick={() => setTab(item.id)}
                className={cn(
                  "px-2 pb-2 text-sm",
                  tab === item.id
                    ? "border-brand-dark border-b-2 font-semibold text-slate-900"
                    : "text-slate-500 hover:text-slate-700"
                )}>
                {item.label}
              </button>
            ))}
          </div>
          <div role="tabpanel" className="mt-2 flex max-h-96 flex-col gap-1 overflow-y-auto">
            {content}
          </div>
        </div>
      </DialogBody>
    </Dialog>
  );
};

const FilterButton = ({ icon, label, onClick }: { icon: ReactNode; label: string; onClick: () => void }) => (
  <button
    type="button"
    onClick={onClick}
    className="flex w-full cursor-pointer items-center gap-4 rounded-lg px-2 py-1 text-left text-sm hover:bg-slate-50">
    {icon}
    <p>{label}</p>
  </button>
);
