import { useReactFlow } from "@xyflow/react";
import {
  HandIcon,
  type LucideIcon,
  MousePointerClickIcon,
  WandSparklesIcon,
  ZoomInIcon,
  ZoomOutIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button, type ButtonProps } from "../../../../components/ui/button";
import { TooltipRenderer } from "../../../../components/ui/tooltip";

interface CanvasControlsProps {
  canMutate: boolean;
  /** Pan mode pans and browses only (nodes inert); pointer mode selects and inspects nodes, and edits them when allowed. */
  isPanMode: boolean;
  onAutoLayout: () => void;
  onPanMode: () => void;
  onPointerMode: () => void;
}

interface ControlDescriptor {
  key: string;
  Icon: LucideIcon;
  label: string;
  variant: ButtonProps["variant"];
  ariaPressed?: boolean;
  disabled?: boolean;
  onClick: () => void;
}

/** The canvas's toolbar: zoom, pan and pointer mode, and the auto-layout. */
export const CanvasControls = ({
  canMutate,
  isPanMode,
  onAutoLayout,
  onPanMode,
  onPointerMode,
}: Readonly<CanvasControlsProps>) => {
  const { t } = useTranslation();
  const { zoomIn, zoomOut } = useReactFlow();
  const controls: ControlDescriptor[] = [
    {
      key: "zoom-in",
      Icon: ZoomInIcon,
      label: t("workspace.workflows.zoom_in"),
      variant: "outline",
      onClick: () => void zoomIn(),
    },
    {
      key: "zoom-out",
      Icon: ZoomOutIcon,
      label: t("workspace.workflows.zoom_out"),
      variant: "outline",
      onClick: () => void zoomOut(),
    },
    {
      key: "pan-mode",
      Icon: HandIcon,
      label: t("workspace.workflows.pan_mode"),
      variant: isPanMode ? "default" : "outline",
      ariaPressed: isPanMode,
      onClick: onPanMode,
    },
    {
      key: "pointer-mode",
      Icon: MousePointerClickIcon,
      label: t("workspace.workflows.pointer_mode"),
      variant: isPanMode ? "outline" : "default",
      ariaPressed: !isPanMode,
      onClick: onPointerMode,
    },
    {
      key: "auto-layout",
      Icon: WandSparklesIcon,
      label: t("workspace.workflows.auto_layout"),
      variant: "outline",
      disabled: !canMutate,
      onClick: onAutoLayout,
    },
  ];
  return (
    <div className="pointer-events-none absolute inset-x-0 bottom-4 z-10 flex justify-center">
      <div className="pointer-events-auto flex items-center gap-2">
        {controls.map(({ key, Icon, label, variant, ariaPressed, disabled, onClick }) => (
          <TooltipRenderer key={key} tooltipContent={label}>
            <Button
              type="button"
              variant={variant}
              size="icon"
              aria-label={label}
              aria-pressed={ariaPressed}
              disabled={disabled}
              onClick={onClick}>
              <Icon />
            </Button>
          </TooltipRenderer>
        ))}
      </div>
    </div>
  );
};
