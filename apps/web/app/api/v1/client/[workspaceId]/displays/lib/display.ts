import { prisma } from "@forma/database";
import { Prisma } from "@forma/database/prisma";
import { TDisplayCreateInput, ZDisplayCreateInput } from "@forma/types/displays";
import { DatabaseError, InvalidInputError, ResourceNotFoundError } from "@forma/types/errors";
import { validateInputs } from "@/lib/utils/validate";
import { getContactByUserId } from "./contact";

export const createDisplay = async (displayInput: TDisplayCreateInput): Promise<{ id: string }> => {
  validateInputs([displayInput, ZDisplayCreateInput]);

  const { workspaceId, userId, surveyId } = displayInput;

  try {
    // Validate the survey before the contact write. This endpoint is unauthenticated and there is no transaction around the two, so creating the contact first left a Contact plus a ContactAttribute committed in the workspace on every request that then failed with a 404 or a 403.
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

    let contact: { id: string } | null = null;
    if (userId) {
      contact = await getContactByUserId(workspaceId, userId);
      if (!contact) {
        contact = await prisma.contact.create({
          data: {
            workspaceId,
            attributes: {
              create: {
                attributeKey: {
                  connect: { key_workspaceId: { key: "userId", workspaceId } },
                },
                value: userId,
              },
            },
          },
        });
      }
    }

    const display = await prisma.display.create({
      data: {
        survey: {
          connect: {
            id: surveyId,
          },
        },

        ...(contact && {
          contact: {
            connect: {
              id: contact.id,
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
