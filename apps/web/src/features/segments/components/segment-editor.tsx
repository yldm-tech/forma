import { ArrowDownIcon, ArrowUpIcon, MoreVertical, Trash2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { TContactAttributeKey } from "@forma/types/contact-attribute-key";
import type { TBaseFilter, TBaseFilters, TSegment, TSegmentConnector } from "@forma/types/segment";
import { cn } from "../../../lib/cn";
import {
  addFilterBelow,
  addFilterInGroup,
  createGroupFromResource,
  deleteResource,
  isResourceFilter,
  moveResource,
  toggleGroupConnector,
} from "../lib/utils";
import { Button } from "../../../components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "../../../components/ui/dropdown-menu";
import { AddFilterModal } from "./add-filter-modal";
import { SegmentConnector } from "./segment-connector";
import { SegmentFilter } from "./segment-filter";

interface TSegmentEditorProps {
  group: TBaseFilters;
  segment: TSegment;
  segments: TSegment[];
  contactAttributeKeys: TContactAttributeKey[];
  setSegment: React.Dispatch<React.SetStateAction<TSegment | null>>;
  viewOnly?: boolean;
}

export function SegmentEditor({
  group,
  setSegment,
  segment,
  contactAttributeKeys,
  segments,
  viewOnly = false,
}: TSegmentEditorProps) {
  const { t } = useTranslation();
  // The group whose add-filter modal is open, and whether the filter goes inside the group or below it. One per editor level, so a level with several groups opens the modal of the group asked, not of every group.
  const [addFilterTarget, setAddFilterTarget] = useState<{ groupId: string; below: boolean } | null>(null);

  const handleAddFilterBelow = (resourceId: string, filter: TBaseFilter) => {
    const localSegmentCopy = structuredClone(segment);

    if (localSegmentCopy.filters) {
      addFilterBelow(localSegmentCopy.filters, resourceId, filter);
    }

    setSegment(localSegmentCopy);
  };

  const handleCreateGroup = (resourceId: string) => {
    const localSegmentCopy = structuredClone(segment);
    if (localSegmentCopy.filters) {
      createGroupFromResource(localSegmentCopy.filters, resourceId);
    }

    setSegment(localSegmentCopy);
  };

  const handleMoveResource = (resourceId: string, direction: "up" | "down") => {
    const localSegmentCopy = structuredClone(segment);
    if (localSegmentCopy.filters) {
      moveResource(localSegmentCopy.filters, resourceId, direction);
    }

    setSegment(localSegmentCopy);
  };

  const handleDeleteResource = (resourceId: string) => {
    const localSegmentCopy = structuredClone(segment);

    if (localSegmentCopy.filters) {
      deleteResource(localSegmentCopy.filters, resourceId);
    }

    setSegment(localSegmentCopy);
  };

  const handleToggleGroupConnector = (groupId: string, newConnectorValue: TSegmentConnector) => {
    const localSegmentCopy = structuredClone(segment);
    if (localSegmentCopy.filters) {
      toggleGroupConnector(localSegmentCopy.filters, groupId, newConnectorValue);
    }

    setSegment(localSegmentCopy);
  };

  const handleAddFilterInGroup = (groupId: string, filter: TBaseFilter) => {
    const localSegmentCopy = structuredClone(segment);

    if (localSegmentCopy.filters) {
      addFilterInGroup(localSegmentCopy.filters, groupId, filter);
    }
    setSegment(localSegmentCopy);
  };

  return (
    <div className="flex flex-col gap-4 rounded-lg">
      {group.map((groupItem) => {
        const { connector, resource, id: groupId } = groupItem;

        if (isResourceFilter(resource)) {
          return (
            <SegmentFilter
              contactAttributeKeys={contactAttributeKeys}
              connector={connector}
              handleAddFilterBelow={handleAddFilterBelow}
              key={groupId}
              onCreateGroup={(filterId: string) => {
                handleCreateGroup(filterId);
              }}
              onDeleteFilter={(filterId: string) => {
                handleDeleteResource(filterId);
              }}
              onMoveFilter={(filterId: string, direction: "up" | "down") => {
                handleMoveResource(filterId, direction);
              }}
              resource={resource}
              segment={segment}
              segments={segments}
              setSegment={setSegment}
              viewOnly={viewOnly}
            />
          );
        }
        return (
          <div key={groupId}>
            <div className="flex items-start gap-2">
              <SegmentConnector
                connector={connector}
                viewOnly={viewOnly}
                onToggle={(next) => handleToggleGroupConnector(groupId, next)}
              />

              <div className="rounded-lg border-2 border-slate-300 bg-white p-4">
                <SegmentEditor
                  contactAttributeKeys={contactAttributeKeys}
                  group={resource}
                  segment={segment}
                  segments={segments}
                  setSegment={setSegment}
                  viewOnly={viewOnly}
                />

                <div className="mt-4">
                  <Button
                    data-testid="add-filter-button"
                    disabled={viewOnly}
                    onClick={() => {
                      if (viewOnly) return;
                      setAddFilterTarget({ groupId, below: false });
                    }}
                    size="sm"
                    variant="secondary">
                    {t("common.add_filter")}
                  </Button>
                </div>

                <AddFilterModal
                  contactAttributeKeys={contactAttributeKeys}
                  onAddFilter={(filter) => {
                    if (addFilterTarget?.below) {
                      handleAddFilterBelow(groupId, filter);
                    } else {
                      handleAddFilterInGroup(groupId, filter);
                    }
                    setAddFilterTarget(null);
                  }}
                  open={addFilterTarget?.groupId === groupId}
                  segments={segments}
                  setOpen={(open) => {
                    if (!open) setAddFilterTarget(null);
                  }}
                />
              </div>

              <div className="flex items-center gap-2 p-4">
                <DropdownMenu>
                  <DropdownMenuTrigger asChild disabled={viewOnly}>
                    <Button
                      variant="outline"
                      size="icon"
                      aria-label={t("common.actions")}
                      data-testid="segment-editor-group-menu-trigger">
                      <MoreVertical className="size-4" />
                    </Button>
                  </DropdownMenuTrigger>

                  <DropdownMenuContent>
                    <DropdownMenuItem
                      onClick={() => {
                        setAddFilterTarget({ groupId, below: true });
                      }}>
                      {t("workspace.segments.add_filter_below")}
                    </DropdownMenuItem>

                    <DropdownMenuItem
                      onClick={() => {
                        handleCreateGroup(groupId);
                      }}>
                      {t("workspace.segments.create_group")}
                    </DropdownMenuItem>

                    <DropdownMenuItem
                      onClick={() => {
                        handleMoveResource(groupId, "up");
                      }}
                      icon={<ArrowUpIcon className="size-4" />}>
                      {t("common.move_up")}
                    </DropdownMenuItem>

                    <DropdownMenuItem
                      onClick={() => {
                        if (viewOnly) return;
                        handleMoveResource(groupId, "down");
                      }}
                      icon={<ArrowDownIcon className="size-4" />}>
                      {t("common.move_down")}
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>

                <Button
                  size="icon"
                  aria-label={t("common.delete")}
                  data-testid="delete-resource"
                  disabled={viewOnly}
                  onClick={() => {
                    if (viewOnly) return;
                    handleDeleteResource(groupId);
                  }}
                  variant="outline">
                  <Trash2 className={cn("h-4 w-4 cursor-pointer", viewOnly && "cursor-not-allowed")} />
                </Button>
              </div>
            </div>
          </div>
        );
      })}
    </div>
  );
}
