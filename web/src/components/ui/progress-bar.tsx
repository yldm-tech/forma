import { cn } from "../../lib/cn";

/** forma2's ProgressBar: a rounded bar filled to a share between 0 and 1. */
export const ProgressBar = ({
  progress,
  barColor,
  height = 5,
}: {
  progress: number;
  barColor: string;
  height?: 2 | 5;
}) => {
  const width = Math.floor(Math.max(0, Math.min(progress, 1)) * 100);
  return (
    <div className={cn(height === 2 ? "h-2" : "h-5", "w-full overflow-hidden rounded-full bg-slate-200")}>
      <div
        className={cn("h-full rounded-full", barColor)}
        style={{ width: `${width}%`, transition: "width 0.5s ease-out" }}
      />
    </div>
  );
};

/** forma2's HalfCircle: a gauge from -100 to 100, the NPS score's dial. */
export const HalfCircle = ({ value }: { value: number }) => {
  const normalizedValue = (value + 100) / 200;
  const rotation = `${normalizedValue * 180 - 180}deg`;
  return (
    <div className="w-fit">
      <div className="relative flex h-28 w-52 items-end justify-center overflow-hidden">
        <div className="absolute h-24 w-48 origin-bottom rounded-tl-full rounded-tr-full bg-slate-200" />
        <div
          className="bg-brand-dark absolute h-24 w-48 origin-bottom rounded-tl-full rounded-tr-full"
          style={{ rotate: rotation }}
        />
        <div className="absolute h-20 w-40 rounded-tl-full rounded-tr-full bg-white" />
      </div>
      <div className="flex justify-between text-sm leading-10 text-slate-600">
        <p>-100</p>
        <p className="text-2xl text-black md:text-4xl">{Math.round(value)}</p>
        <p>100</p>
      </div>
    </div>
  );
};
