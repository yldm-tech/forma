import { createId } from "@paralleldrive/cuid2";
import { TFunction } from "i18next";
import {
  TAllOperators,
  TAttributeOperator,
  TBaseFilter,
  TBaseFilters,
  TDeviceOperator,
  TSegmentAttributeFilter,
  TSegmentConnector,
  TSegmentDeviceFilter,
  TSegmentFilter,
  TSegmentFilterValue,
  TSegmentOperator,
  TSegmentPersonFilter,
  TSegmentSegmentFilter,
  TSegmentSurveyInteractionFilterValue,
  TSurveyInteractionOperator,
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
    const item = group[i];
    if (!item) continue;
    const { resource } = item;

    if (isResourceFilter(resource)) {
      if (resource.id === resourceId) {
        group.splice(i + 1, 0, filter);
        break;
      }
    } else {
      if (item.id === resourceId) {
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
    const filters = group[i];
    if (!filters) continue;
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
      if (filters.id === resourceId) {
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
  const current = group[i];
  const previous = group[i - 1];
  if (i === 0 || !current || !previous) {
    return;
  }

  group[i - 1] = current;
  group[i] = previous;

  if (i - 1 === 0) {
    const newConnector = current.connector;

    current.connector = null;
    previous.connector = newConnector;
  }
};

export const moveResourceDown = (group: TBaseFilters, i: number) => {
  const current = group[i];
  const next = group[i + 1];
  if (i === group.length - 1 || !current || !next) {
    return;
  }

  group[i + 1] = current;
  group[i] = next;

  // after the swap, determine if the connector should be null or not
  if (i === 0) {
    const nextConnector = next.connector;

    next.connector = null;
    current.connector = nextConnector;
  }
};

export const moveResource = (group: TBaseFilters, resourceId: string, direction: "up" | "down") => {
  for (let i = 0; i < group.length; i++) {
    const item = group[i];
    if (!item) continue;
    const { resource } = item;

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
      if (item.id === resourceId) {
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
    const item = group[i];
    if (!item) continue;
    const { resource } = item;

    if (isResourceFilter(resource) && resource.id === resourceId) {
      group.splice(i, 1);

      const first = group[0];
      if (first) {
        first.connector = null;
      }

      break;
    } else if (!isResourceFilter(resource) && item.id === resourceId) {
      group.splice(i, 1);

      const first = group[0];
      if (first) {
        first.connector = null;
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
    const item = group[i];
    if (!item) continue;
    const { resource } = item;

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
    const item = group[i];
    if (!item) continue;
    const { resource } = item;

    if (isResourceFilter(resource)) {
      continue;
    } else {
      if (item.id === groupId) {
        const item = group[i];
        if (!item) continue;
        const { resource } = item;

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
    const item = group[i];
    if (!item) continue;
    const { resource } = item;
    if (!isResourceFilter(resource)) {
      if (item.id === groupId) {
        item.connector = newConnectorValue;
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
    const item = group[i];
    if (!item) continue;
    const { resource } = item;

    if (isResourceFilter(resource)) {
      if (resource.id === filterId) {
        item.connector = newConnectorValue;
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
    const item = group[i];
    if (!item) continue;
    const { resource } = item;

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
    const item = group[i];
    if (!item) continue;
    const { resource } = item;

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

export const updatePersonIdentifierInFilter = (
  group: TBaseFilters,
  filterId: string,
  newPersonIdentifier: string
) => {
  for (let i = 0; i < group.length; i++) {
    const item = group[i];
    if (!item) continue;
    const { resource } = item;

    if (isResourceFilter(resource)) {
      if (resource.id === filterId) {
        (resource as TSegmentPersonFilter).root.personIdentifier = newPersonIdentifier;
      }
    } else {
      updatePersonIdentifierInFilter(resource, filterId, newPersonIdentifier);
    }
  }
};

export const updateSegmentIdInFilter = (group: TBaseFilters, filterId: string, newSegmentId: string) => {
  for (let i = 0; i < group.length; i++) {
    const item = group[i];
    if (!item) continue;
    const { resource } = item;

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
    const item = group[i];
    if (!item) continue;
    const { resource } = item;

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
    const item = group[i];
    if (!item) continue;
    const { resource } = item;

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

export const searchForAttributeKeyInSegment = (filters: TBaseFilters, attributeKey: string): boolean => {
  for (let filter of filters) {
    const { resource } = filter;

    if (isResourceFilter(resource)) {
      const { root } = resource;
      const { type } = root;

      if (type === "attribute") {
        const { contactAttributeKey: key } = root;
        if (key === attributeKey) {
          return true;
        }
      }
    } else {
      const found = searchForAttributeKeyInSegment(resource, attributeKey);
      if (found) {
        return true;
      }
    }
  }

  return false;
};
