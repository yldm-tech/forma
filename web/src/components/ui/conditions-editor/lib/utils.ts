import { TGenericCondition, TGenericConditionGroup } from "../types";

export const isConditionGroup = (
  condition: TGenericCondition | TGenericConditionGroup
): condition is TGenericConditionGroup => {
  return "conditions" in condition && Array.isArray((condition as TGenericConditionGroup).conditions);
};
