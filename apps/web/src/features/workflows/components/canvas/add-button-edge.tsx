import { BaseEdge, type EdgeProps, getStraightPath } from "@xyflow/react";

// The connector line between two nodes. Inserting a node mid-chain is not offered while a workflow holds one action after its trigger, so the `+` lives on the trigger card instead.
export const AddButtonEdge = ({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  markerEnd,
  style,
}: Readonly<EdgeProps>) => {
  const [edgePath] = getStraightPath({ sourceX, sourceY, targetX, targetY });
  return (
    <BaseEdge
      id={id}
      path={edgePath}
      markerEnd={markerEnd}
      style={{ stroke: "#cbd5e1", strokeWidth: 1.5, ...style }}
    />
  );
};
