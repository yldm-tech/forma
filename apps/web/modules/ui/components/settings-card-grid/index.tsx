import { cn } from "@/lib/cn";

interface SettingsCardGridProps {
  children: React.ReactNode;
  className?: string;
}

/**
 * Lays a settings page's cards out in two columns once there is room for them.
 *
 * A settings form does not get better by growing: its fields cap at max-w-sm, so widening a card
 * only adds gutter inside it. Pairing the cards up instead is what uses a desktop's width, and it
 * keeps each card at a comfortable reading size. Below `xl` there is not room for two, so the
 * stack stays single-column.
 *
 * Cards carry their own `my-4` for the single-column case; inside the grid that fights `gap-6`, so
 * it is cleared here rather than made conditional on each card.
 *
 * A card that should not be paired off — a destructive action, or anything that reads as the end of
 * the page — takes `xl:col-span-2` and spans the row on its own.
 */
export const SettingsCardGrid = ({ children, className }: Readonly<SettingsCardGridProps>) => {
  return (
    <div className={cn("grid grid-cols-1 items-start gap-6 xl:grid-cols-2 [&>*]:my-0", className)}>
      {children}
    </div>
  );
};
