import { prisma } from "@forma/database";
import { Prisma } from "@forma/database/prisma";
import { DatabaseError, InvalidInputError, ResourceNotFoundError } from "@forma/types/errors";
import {
  TDisplayCreateInputV2,
  ZDisplayCreateInputV2,
} from "@/app/api/v2/client/[workspaceId]/displays/types/display";
import { validateInputs } from "@/lib/utils/validate";
import { doesContactExistInWorkspace } from "./contact";

export const createDisplay = async (displayInput: TDisplayCreateInputV2): Promise<{ id: string }> => {
  validateInputs([displayInput, ZDisplayCreateInputV2]);

  const { contactId, surveyId, workspaceId } = displayInput;

  try {
    const contactExists = contactId ? await doesContactExistInWorkspace(contactId, workspaceId) : false;

    const survey = await prisma.survey.findUnique({
      where: {
        id: surveyId,
        workspaceId,
      },
      // Only the status is read below, and the display is connected by the input's surveyId rather
      // than by anything off this row. Without the select every display creation pulls the whole
      // survey - blocks, endings, styling, every JSON column - out of the database.
      select: { status: true },
    });
    if (!survey) {
      throw new ResourceNotFoundError("Survey", surveyId);
    }

    if (survey.status !== "inProgress") {
      throw new InvalidInputError("Survey is not accepting submissions");
    }

    const display = await prisma.display.create({
      data: {
        survey: {
          connect: {
            id: surveyId,
          },
        },

        ...(contactExists && {
          contact: {
            connect: {
              id: contactId,
            },
          },
        }),
      },
      select: { id: true, contactId: true, surveyId: true },
    });

    return display;
  } catch (error) {
    if (error instanceof Prisma.PrismaClientKnownRequestError) {
      throw new DatabaseError(error.message);
    }

    throw error;
  }
};
