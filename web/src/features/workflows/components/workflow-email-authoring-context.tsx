import { type ReactNode, createContext, useContext } from "react";
import type { TWorkflowEmailAuthoringContext } from "../lib/authoring-context";

const WorkflowEmailAuthoringCtx = createContext<TWorkflowEmailAuthoringContext | null>(null);

/** Gives the node inspector the bound survey, the addressable members and the sender, so the send_email form renders the follow-ups' controls (recall body, recipient options). */
export const WorkflowEmailAuthoringProvider = ({
  value,
  children,
}: Readonly<{ value: TWorkflowEmailAuthoringContext; children: ReactNode }>) => (
  <WorkflowEmailAuthoringCtx.Provider value={value}>{children}</WorkflowEmailAuthoringCtx.Provider>
);

/** The authoring context, or null outside the provider. */
export const useWorkflowEmailAuthoringContext = (): TWorkflowEmailAuthoringContext | null =>
  useContext(WorkflowEmailAuthoringCtx);
