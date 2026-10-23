import { StarIcon } from "lucide-react";
import type { ReactNode } from "react";
import { RatingSmiley } from "./rating-smiley";

type Variant = "default" | "individual" | "aggregated" | "scale";

const NumberBox = ({ answer, size }: { answer: number; size: "small" | "medium" | "large" }) => {
  const sizeClasses = { small: "h-6 w-6 text-xs", medium: "h-7 w-7 text-xs", large: "h-12 w-12 text-base" };
  return (
    <div
      className={`flex items-center justify-center rounded-md bg-slate-100 font-semibold text-slate-700 ${sizeClasses[size]}`}>
      {answer}
    </div>
  );
};

const renderStarScale = (answer: number, variant: Variant, range: number) => {
  if (variant === "scale") {
    return answer === 1 ? (
      <StarIcon className="size-6 text-slate-300" />
    ) : (
      <StarIcon fill="rgb(250 204 21)" className="size-6 text-yellow-400" />
    );
  }
  if (variant === "aggregated") return <NumberBox answer={answer} size="medium" />;
  if (variant === "individual" && range > 5) {
    return (
      <div className="flex items-center gap-x-2">
        <StarIcon className="size-5 text-slate-300" />
        <span className="text-base font-semibold text-slate-700">{answer}</span>
        <StarIcon fill="rgb(250 204 21)" className="size-5 text-yellow-400" />
      </div>
    );
  }
  return (
    <div className="flex">
      {Array.from({ length: range }, (_, i) => (
        <StarIcon
          key={i}
          fill={i < answer ? "rgb(250 204 21)" : "none"}
          className={`h-7 ${i < answer ? "text-yellow-400" : "text-slate-300"}`}
        />
      ))}
    </div>
  );
};

const renderSmileyScale = (answer: number, variant: Variant, range: number, addColors: boolean) => {
  if (variant === "scale") {
    return (
      <div className="flex size-6 items-center justify-center">
        <RatingSmiley active={false} idx={answer - 1} range={range} addColors={addColors} />
      </div>
    );
  }
  if (variant === "aggregated") return <NumberBox answer={answer} size="medium" />;
  return <RatingSmiley active={false} idx={answer - 1} range={range} addColors={addColors} />;
};

/** forma2's RatingResponse: a rating answer drawn on its scale, as stars, a face or a number. */
export const RatingResponse = ({
  scale,
  range,
  answer,
  addColors = false,
  variant = "default",
}: {
  scale?: "number" | "star" | "smiley";
  range?: number;
  answer: unknown;
  addColors?: boolean;
  variant?: Variant;
}): ReactNode => {
  if (typeof answer !== "number") return null;
  if (scale === undefined || range === undefined) return answer;
  if (scale === "star") return renderStarScale(answer, variant, range);
  if (scale === "smiley") return renderSmileyScale(answer, variant, range, addColors);
  if (variant === "scale") return <NumberBox answer={answer} size="small" />;
  if (variant === "aggregated") return <NumberBox answer={answer} size="medium" />;
  return <NumberBox answer={answer} size="large" />;
};
