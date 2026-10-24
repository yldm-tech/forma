// forma2's segment filter tree editing (apps/web/modules/contacts/segments/lib/utils.ts). The helpers change the tree they are given in place, so the editor applies them to a copy and keeps the copy.

import { createId } from "@paralleldrive/cuid2";
import type { TFunction } from "i18next";
import {
  DATE_OPERATORS,
  NUMBER_TYPE_OPERATORS,
  STRING_TYPE_OPERATORS,
  type TAllOperators,
  type TAttributeOperator,
  type TBaseFilter,
  type TBaseFilters,
  type TDeviceOperator,
  type TSegmentAttributeFilter,
  type TSegmentConnector,
  type TSegmentDeviceFilter,
  type TSegmentFilter,
  type TSegmentFilterValue,
  type TSegmentOperator,
  type TSegmentSegmentFilter,
  type TSegmentSurveyInteractionFilterValue,
  type TSurveyInteractionOperator,
  ZSegmentFilters,
} from "@forma/types/segment";

// type guard to check if a resource is a filter
export const isResourceFilter = (resource: TSegmentFilter | TBaseFilters): resource is TSegmentFilter => {
  return (resource as TSegmentFilter).root !== undefined;
};

export const convertOperatorToText = (operator: TAllOperators, t: TFunction) => {
  switch (operator) {
    case "equals":
      return "=";
    case "notEquals":
      return "!=";
    case "lessThan":
      return "<";
    case "lessEqual":
      return "<=";
    case "greaterThan":
      return ">";
    case "greaterEqual":
      return ">=";
    case "isSet":
      return t("workspace.segments.operator_is_set");
    case "isNotSet":
      return t("workspace.segments.operator_is_not_set");
    case "contains":
      return t("workspace.segments.operator_contains");
    case "doesNotContain":
      return t("workspace.segments.operator_does_not_contain");
    case "startsWith":
      return t("workspace.segments.operator_starts_with");
    case "endsWith":
      return t("workspace.segments.operator_ends_with");
    case "userIsIn":
      return t("workspace.segments.operator_user_is_in");
    case "userIsNotIn":
      return t("workspace.segments.operator_user_is_not_in");
    case "isOlderThan":
      return t("workspace.segments.operator_is_older_than");
    case "isNewerThan":
      return t("workspace.segments.operator_is_newer_than");
    case "isBefore":
      return t("workspace.segments.operator_is_before");
    case "isAfter":
      return t("workspace.segments.operator_is_after");
    case "isBetween":
      return t("workspace.segments.operator_is_between");
    case "isSameDay":
      return t("workspace.segments.operator_is_same_day");
    case "haveCompleted":
      return t("workspace.segments.operator_have_completed");
    case "haveNotCompleted":
      return t("workspace.segments.operator_have_not_completed");
    case "haveSeen":
      return t("workspace.segments.operator_have_seen");
    case "haveNotSeen":
      return t("workspace.segments.operator_have_not_seen");
    case "haveStartedRespondingTo":
      return t("workspace.segments.operator_have_started_responding_to");
    default:
      return operator;
  }
};

export const convertOperatorToTitle = (operator: TAllOperators, t: TFunction) => {
  switch (operator) {
    case "equals":
      return t("workspace.segments.operator_title_equals");
    case "notEquals":
      return t("workspace.segments.operator_title_not_equals");
    case "lessThan":
      return t("workspace.segments.operator_title_less_than");
    case "lessEqual":
      return t("workspace.segments.operator_title_less_equal");
    case "greaterThan":
      return t("workspace.segments.operator_title_greater_than");
    case "greaterEqual":
      return t("workspace.segments.operator_title_greater_equal");
    case "isSet":
      return t("workspace.segments.operator_title_is_set");
    case "isNotSet":
      return t("workspace.segments.operator_title_is_not_set");
    case "contains":
      return t("workspace.segments.operator_title_contains");
    case "doesNotContain":
      return t("workspace.segments.operator_title_does_not_contain");
    case "startsWith":
      return t("workspace.segments.operator_title_starts_with");
    case "endsWith":
      return t("workspace.segments.operator_title_ends_with");
    case "userIsIn":
      return t("workspace.segments.operator_title_user_is_in");
    case "userIsNotIn":
      return t("workspace.segments.operator_title_user_is_not_in");
    case "isOlderThan":
      return t("workspace.segments.operator_title_is_older_than");
    case "isNewerThan":
      return t("workspace.segments.operator_title_is_newer_than");
    case "isBefore":
      return t("workspace.segments.operator_title_is_before");
    case "isAfter":
      return t("workspace.segments.operator_title_is_after");
    case "isBetween":
      return t("workspace.segments.operator_title_is_between");
    case "isSameDay":
      return t("workspace.segments.operator_title_is_same_day");
    case "haveCompleted":
      return t("workspace.segments.operator_title_have_completed");
    case "haveNotCompleted":
      return t("workspace.segments.operator_title_have_not_completed");
    case "haveSeen":
      return t("workspace.segments.operator_title_have_seen");
    case "haveNotSeen":
      return t("workspace.segments.operator_title_have_not_seen");
    case "haveStartedRespondingTo":
      return t("workspace.segments.operator_title_have_started_responding_to");
    default:
      return operator;
  }
};

export const addFilterBelow = (group: TBaseFilters, resourceId: string, filter: TBaseFilter) => {
  for (let i = 0; i < group.length; i++) {
    const { resource } = group[i]!;

    if (isResourceFilter(resource)) {
      if (resource.id === resourceId) {
        group.splice(i + 1, 0, filter);
        break;
      }
    } else {
      if (group[i]!.id === resourceId) {
        group.splice(i + 1, 0, filter);
        break;
      } else {
        addFilterBelow(resource, resourceId, filter);
      }
    }
  }
};

export const createGroupFromResource = (group: TBaseFilters, resourceId: string) => {
  for (let i = 0; i < group.length; i++) {
    const filters = group[i]!;
    if (isResourceFilter(filters.resource)) {
      if (filters.resource.id === resourceId) {
        const newGroupToAdd: TBaseFilter = {
          id: createId(),
          connector: filters.connector,
          resource: [
            {
              ...filters,
              connector: null,
            },
          ],
        };

        group.splice(i, 1, newGroupToAdd);

        break;
      }
    } else {
      if (group[i]!.id === resourceId) {
        // make an outer group, wrap the current group in it and add a filter below it

        const outerGroup: TBaseFilter = {
          connector: filters.connector,
          id: createId(),
          resource: [{ ...filters, connector: null }],
        };

        group.splice(i, 1, outerGroup);

        break;
      } else {
        createGroupFromResource(filters.resource, resourceId);
      }
    }
  }
};

export const moveResourceUp = (group: TBaseFilters, i: number) => {
  if (i === 0) {
    return;
  }

  const previousTemp = group[i - 1]!;

  group[i - 1] = group[i]!;
  group[i] = previousTemp;

  if (i - 1 === 0) {
    const newConnector = group[i - 1]!.connector;

    group[i - 1]!.connector = null;
    group[i]!.connector = newConnector;
  }
};

export const moveResourceDown = (group: TBaseFilters, i: number) => {
  if (i === group.length - 1) {
    return;
  }

  const temp = group[i + 1]!;
  group[i + 1] = group[i]!;
  group[i] = temp;

  // after the swap, determine if the connector should be null or not
  if (i === 0) {
    const nextConnector = group[i]!.connector;

    group[i]!.connector = null;
    group[i + 1]!.connector = nextConnector;
  }
};

export const moveResource = (group: TBaseFilters, resourceId: string, direction: "up" | "down") => {
  for (let i = 0; i < group.length; i++) {
    const { resource } = group[i]!;

    if (isResourceFilter(resource)) {
      if (resource.id === resourceId) {
        if (direction === "up") {
          moveResourceUp(group, i);
          break;
        } else {
          moveResourceDown(group, i);
          break;
        }
      }
    } else {
      if (group[i]!.id === resourceId) {
        if (direction === "up") {
          moveResourceUp(group, i);
          break;
        } else {
          moveResourceDown(group, i);
          break;
        }
      }

      moveResource(resource, resourceId, direction);
    }
  }
};

export const deleteResource = (group: TBaseFilters, resourceId: string) => {
  for (let i = 0; i < group.length; i++) {
    const { resource } = group[i]!;

    if (isResourceFilter(resource) && resource.id === resourceId) {
      group.splice(i, 1);

      if (group.length) {
        group[0]!.connector = null;
      }

      break;
    } else if (!isResourceFilter(resource) && group[i]!.id === resourceId) {
      group.splice(i, 1);

      if (group.length) {
        group[0]!.connector = null;
      }

      break;
    } else if (!isResourceFilter(resource)) {
      deleteResource(resource, resourceId);
    }
  }

  // check and delete all empty groups
  deleteEmptyGroups(group);
};

export const deleteEmptyGroups = (group: TBaseFilters) => {
  // Iterate backward to safely remove items while iterating
  for (let i = group.length - 1; i >= 0; i--) {
    const { resource } = group[i]!;

    if (!isResourceFilter(resource)) {
      // Recursively delete empty groups within the current group first
      deleteEmptyGroups(resource);
      // After cleaning the inner group, check if it has become empty
      if (resource.length === 0) {
        group.splice(i, 1);
      }
    }
  }
};

export const addFilterInGroup = (group: TBaseFilters, groupId: string, filter: TBaseFilter) => {
  for (let i = 0; i < group.length; i++) {
    const { resource } = group[i]!;

    if (isResourceFilter(resource)) {
      continue;
    } else {
      if (group[i]!.id === groupId) {
        const { resource } = group[i]!;

        if (!isResourceFilter(resource)) {
          if (resource.length === 0) {
            resource.push({
              ...filter,
              connector: null,
            });
          } else {
            resource.push(filter);
          }
        }

        break;
      } else {
        addFilterInGroup(resource, groupId, filter);
      }
    }
  }
};

export const toggleGroupConnector = (
  group: TBaseFilters,
  groupId: string,
  newConnectorValue: TSegmentConnector
) => {
  for (let i = 0; i < group.length; i++) {
    const { resource } = group[i]!;
    if (!isResourceFilter(resource)) {
      if (group[i]!.id === groupId) {
        group[i]!.connector = newConnectorValue;
        break;
      } else {
        toggleGroupConnector(resource, groupId, newConnectorValue);
      }
    }
  }
};

export const toggleFilterConnector = (
  group: TBaseFilters,
  filterId: string,
  newConnectorValue: TSegmentConnector
) => {
  for (let i = 0; i < group.length; i++) {
    const { resource } = group[i]!;

    if (isResourceFilter(resource)) {
      if (resource.id === filterId) {
        group[i]!.connector = newConnectorValue;
      }
    } else {
      toggleFilterConnector(resource, filterId, newConnectorValue);
    }
  }
};

export const updateOperatorInFilter = (
  group: TBaseFilters,
  filterId: string,
  newOperator: TAttributeOperator | TSegmentOperator | TDeviceOperator | TSurveyInteractionOperator
) => {
  for (let i = 0; i < group.length; i++) {
    const { resource } = group[i]!;

    if (isResourceFilter(resource)) {
      if (resource.id === filterId) {
        resource.qualifier.operator = newOperator;
        break;
      }
    } else {
      updateOperatorInFilter(resource, filterId, newOperator);
    }
  }
};

export const updateContactAttributeKeyInFilter = (
  group: TBaseFilters,
  filterId: string,
  newContactAttributeKey: string
) => {
  for (let i = 0; i < group.length; i++) {
    const { resource } = group[i]!;

    if (isResourceFilter(resource)) {
      if (resource.id === filterId) {
        (resource as TSegmentAttributeFilter).root.contactAttributeKey = newContactAttributeKey;
        break;
      }
    } else {
      updateContactAttributeKeyInFilter(resource, filterId, newContactAttributeKey);
    }
  }
};

export const updateSegmentIdInFilter = (group: TBaseFilters, filterId: string, newSegmentId: string) => {
  for (let i = 0; i < group.length; i++) {
    const { resource } = group[i]!;

    if (isResourceFilter(resource)) {
      if (resource.id === filterId) {
        (resource as TSegmentSegmentFilter).root.segmentId = newSegmentId;
        resource.value = newSegmentId;
        break;
      }
    } else {
      updateSegmentIdInFilter(resource, filterId, newSegmentId);
    }
  }
};

export const updateFilterValue = (group: TBaseFilters, filterId: string, newValue: TSegmentFilterValue) => {
  for (let i = 0; i < group.length; i++) {
    const { resource } = group[i]!;

    if (isResourceFilter(resource)) {
      if (resource.id === filterId) {
        resource.value = newValue;

        break;
      }
    } else {
      updateFilterValue(resource, filterId, newValue);
    }
  }
};

export const updateDeviceTypeInFilter = (
  group: TBaseFilters,
  filterId: string,
  newDeviceType: "phone" | "desktop"
) => {
  for (let i = 0; i < group.length; i++) {
    const { resource } = group[i]!;

    if (isResourceFilter(resource)) {
      if (resource.id === filterId) {
        (resource as TSegmentDeviceFilter).root.deviceType = newDeviceType;
        resource.value = newDeviceType;
        break;
      }
    } else {
      updateDeviceTypeInFilter(resource, filterId, newDeviceType);
    }
  }
};

export const updateSurveyInteractionValueInFilter = (
  group: TBaseFilters,
  filterId: string,
  newValue: TSegmentSurveyInteractionFilterValue
) => {
  for (const { resource } of group) {
    if (isResourceFilter(resource)) {
      if (resource.id === filterId) {
        resource.value = newValue;
        break;
      }
    } else {
      updateSurveyInteractionValueInFilter(resource, filterId, newValue);
    }
  }
};

// ---- building filters and segments ----

export type NewFilterType = "attribute" | "segment" | "device" | "person" | "surveyInteraction";

/**
 * forma2's handleAddFilter without the modal: a new filter of the type, joined with "and", with the defaults forma2 gave it. An attribute filter needs its key (a date key starts as "is older than 1 day"), a segment filter its segment and a device filter its device type; without them there is nothing to add.
 */
export const newSegmentFilter = ({
  type,
  contactAttributeKey,
  attributeDataType,
  segmentId,
  deviceType,
}: {
  type: NewFilterType;
  contactAttributeKey?: string;
  attributeDataType?: "string" | "number" | "date";
  segmentId?: string;
  deviceType?: string;
}): TBaseFilter | null => {
  const wrap = (resource: TSegmentFilter): TBaseFilter => ({ id: createId(), connector: "and", resource });
  switch (type) {
    case "attribute": {
      if (!contactAttributeKey) return null;
      const isDate = attributeDataType === "date";
      return wrap({
        id: createId(),
        root: { type, contactAttributeKey },
        qualifier: { operator: isDate ? "isOlderThan" : "equals" },
        value: isDate ? { amount: 1, unit: "days" } : "",
      });
    }
    case "person":
      return wrap({
        id: createId(),
        root: { type: "person", personIdentifier: "userId" },
        qualifier: { operator: "equals" },
        value: "",
      });
    case "segment":
      if (!segmentId) return null;
      return wrap({
        id: createId(),
        root: { type, segmentId },
        qualifier: { operator: "userIsIn" },
        value: segmentId,
      });
    case "device":
      if (!deviceType) return null;
      return wrap({
        id: createId(),
        root: { type, deviceType },
        qualifier: { operator: "equals" },
        value: deviceType,
      });
    case "surveyInteraction":
      return wrap({
        id: createId(),
        root: { type: "surveyInteraction" },
        qualifier: { operator: "haveSeen" },
        value: { surveyScope: "any", surveyIds: [], within: { amount: 1, unit: "months" } },
      });
  }
};

/** Appends a filter at the top level of a tree; the first filter of a tree has no connector. */
export const appendFilter = (filters: TBaseFilters, filter: TBaseFilter): TBaseFilters =>
  filters.length === 0 ? [{ ...filter, connector: null }] : [...filters, filter];

/** The operators an attribute filter offers for its key's data type. */
export const operatorsForDataType = (dataType: "string" | "number" | "date") =>
  dataType === "date"
    ? DATE_OPERATORS
    : dataType === "number"
      ? NUMBER_TYPE_OPERATORS
      : STRING_TYPE_OPERATORS;

/** When an attribute filter changes key, its operator and value restart from the new key's type. */
export const changeAttributeKey = (
  group: TBaseFilters,
  filterId: string,
  key: string,
  dataType: "string" | "number" | "date"
) => {
  updateContactAttributeKeyInFilter(group, filterId, key);
  updateOperatorInFilter(group, filterId, dataType === "date" ? "isOlderThan" : "equals");
  updateFilterValue(group, filterId, dataType === "date" ? { amount: 1, unit: "days" } : "");
};

/** forma2's save rule for the segment forms: a title, at least one filter, and a tree ZSegmentFilters accepts. */
export const canSaveSegment = (title: string, filters: TBaseFilters): boolean =>
  title.trim() !== "" && filters.length > 0 && ZSegmentFilters.safeParse(filters).success;

// ---- segment activity (forma2's segment-activity-utils.ts) ----

/** Whether a tree has a "user is (not) in" filter on the segment, at any depth. */
export const doesSegmentReferenceSegment = (filters: TBaseFilters, targetSegmentId: string): boolean =>
  filters.some(({ resource }) =>
    isResourceFilter(resource)
      ? resource.root.type === "segment" && resource.root.segmentId === targetSegmentId
      : doesSegmentReferenceSegment(resource, targetSegmentId)
  );

interface SegmentWithSurveys {
  id: string;
  filters: TBaseFilters;
  activeSurveys: { id: string; name: string }[];
  inactiveSurveys: { id: string; name: string }[];
}

/** The other segments whose filters use the segment. */
export const getReferencingSegments = <S extends SegmentWithSurveys>(
  segments: S[],
  targetSegmentId: string
): S[] =>
  segments.filter(
    (segment) =>
      segment.id !== targetSegmentId && doesSegmentReferenceSegment(segment.filters, targetSegmentId)
  );

/** The surveys a segment reaches: its own, then those of the segments that use it, each survey once. */
export const segmentActivitySummary = (
  current: SegmentWithSurveys,
  segments: SegmentWithSurveys[]
): { activeSurveys: string[]; inactiveSurveys: string[] } => {
  const active = new Map(current.activeSurveys.map((s) => [s.id, s.name]));
  const inactive = new Map(current.inactiveSurveys.map((s) => [s.id, s.name]));
  const direct = new Set([...active.keys(), ...inactive.keys()]);
  for (const segment of getReferencingSegments(segments, current.id)) {
    for (const survey of segment.activeSurveys) {
      if (!direct.has(survey.id) && !active.has(survey.id)) active.set(survey.id, survey.name);
    }
    for (const survey of segment.inactiveSurveys) {
      if (!direct.has(survey.id) && !inactive.has(survey.id)) inactive.set(survey.id, survey.name);
    }
  }
  return { activeSurveys: [...active.values()], inactiveSurveys: [...inactive.values()] };
};

// ---- attribute key usage ----

/** Whether a tree has an attribute filter on one of the keys, at any depth. */
const usesAttributeKey = (filters: TBaseFilters, keys: Set<string>): boolean =>
  filters.some(({ resource }) =>
    isResourceFilter(resource)
      ? resource.root.type === "attribute" && keys.has(resource.root.contactAttributeKey)
      : usesAttributeKey(resource, keys)
  );

/**
 * What deleting attribute keys would break: the segments whose filters use one of them, directly or through a "user is (not) in" filter on such a segment, and the surveys those segments target. Public segments are listed by title; a survey's private segment is only listed through its survey. Each name appears once.
 */
export const attributeKeyUsage = (
  segments: (SegmentWithSurveys & { title: string; isPrivate: boolean })[],
  keys: string[]
): { segments: string[]; surveys: string[] } => {
  const keySet = new Set(keys);
  const affected = new Set(
    segments.filter((segment) => usesAttributeKey(segment.filters, keySet)).map((s) => s.id)
  );
  // A segment that filters on an affected segment matches differently too; repeat until no more are found.
  let grew = affected.size > 0;
  while (grew) {
    grew = false;
    for (const segment of segments) {
      if (affected.has(segment.id)) continue;
      if ([...affected].some((id) => doesSegmentReferenceSegment(segment.filters, id))) {
        affected.add(segment.id);
        grew = true;
      }
    }
  }
  const hit = segments.filter((segment) => affected.has(segment.id));
  const surveys = new Map<string, string>();
  for (const segment of hit) {
    for (const survey of [...segment.activeSurveys, ...segment.inactiveSurveys])
      surveys.set(survey.id, survey.name);
  }
  return {
    segments: hit.filter((segment) => !segment.isPrivate).map((segment) => segment.title),
    surveys: [...surveys.values()],
  };
};
