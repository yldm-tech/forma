import { cn } from "@/lib/cn";
import { SettingsCard, type TSettingsCardWidth } from "@/modules/ui/components/settings-card";

export const LoadingCard = ({
  title,
  description,
  skeletonLines,
  width,
  className,
}: {
  title: string;
  description: string;
  skeletonLines: Array<{ classes: string }>;
  width?: TSettingsCardWidth;
  className?: string;
}) => {
  return (
    <SettingsCard title={title} description={description} width={width} className={className}>
      <div className="w-full space-y-4">
        {skeletonLines.map((line, index) => (
          <div key={index}>
            <div className={cn("animate-pulse rounded-full bg-slate-200", line.classes)}></div>
          </div>
        ))}
      </div>
    </SettingsCard>
  );
};
