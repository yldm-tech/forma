import { Workspace } from "@forma/database/prisma";

export interface TUserWorkspace extends Pick<Workspace, "id" | "name"> {}
