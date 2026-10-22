import {
  ConfusedFace,
  FrowningFace,
  GrinningFaceWithSmilingEyes,
  GrinningSquintingFace,
  NeutralFace,
  PerseveringFace,
  SlightlySmilingFace,
  SmilingFaceWithSmilingEyes,
  TiredFace,
  WearyFace,
} from "@forma/survey-ui/smileys";
import type { JSX } from "react";

const FACES = [
  TiredFace,
  WearyFace,
  PerseveringFace,
  FrowningFace,
  ConfusedFace,
  NeutralFace,
  SlightlySmilingFace,
  SmilingFaceWithSmilingEyes,
  GrinningFaceWithSmilingEyes,
  GrinningSquintingFace,
];

/** The faces a scale of the given length uses, forma2's iconsIdx: always running from unhappy to happy. */
const FACES_BY_RANGE: Record<number, number[]> = {
  10: [0, 1, 2, 3, 4, 5, 6, 7, 8, 9],
  7: [1, 3, 4, 5, 6, 8, 9],
  6: [0, 2, 4, 5, 7, 9],
  5: [3, 4, 5, 6, 7],
  4: [4, 5, 6, 7],
  3: [4, 5, 7],
};

/** forma2's smiley colours: the happiest faces green, the middle ones orange, the rest red. */
const getSmileyColor = (range: number, idx: number) => {
  if (range > 5) {
    if (range - idx < 3) return "fill-emerald-100";
    if (range - idx < 5) return "fill-orange-100";
    return "fill-rose-100";
  }
  if (range < 5) {
    if (range - idx < 2) return "fill-emerald-100";
    if (range - idx < 3) return "fill-orange-100";
    return "fill-rose-100";
  }
  if (range - idx < 3) return "fill-emerald-100";
  if (range - idx < 4) return "fill-orange-100";
  return "fill-rose-100";
};

/** forma2's RatingSmiley: the face of a smiley rating answer, drawn with the survey runtime's own faces. */
export const RatingSmiley = ({
  active,
  idx,
  range,
  addColors = false,
}: {
  active: boolean;
  idx: number;
  range: number;
  addColors?: boolean;
}): JSX.Element | null => {
  const faceIndex = FACES_BY_RANGE[range]?.[idx];
  const Face = faceIndex === undefined ? undefined : FACES[faceIndex];
  if (!Face) return null;
  const color = active ? "fill-yellow-200" : addColors ? getSmileyColor(range, idx) : "fill-none";
  return (
    <span className="inline-flex size-10 items-center justify-center text-slate-700">
      <Face className={color} />
    </span>
  );
};
