"use server";

import { z } from "zod";
import { authenticatedActionClient } from "@/lib/utils/action-client";

const ZConvertHeicToJpegInput = z.object({
  file: z.instanceof(File),
});

export const convertHeicToJpegAction = authenticatedActionClient
  .inputSchema(ZConvertHeicToJpegInput)
  .action(async ({ parsedInput }) => {
    if (!parsedInput.file || !parsedInput.file.name.endsWith(".heic")) return parsedInput.file;

    const convert = (await import("heic-convert")).default;

    const arrayBuffer = await parsedInput.file.arrayBuffer();
    // `@types/heic-convert` used to declare `buffer` as ArrayBufferLike, which a Buffer is not, so
    // this needed a double cast to compile. The types now say Uint8Array — which Buffer is — and the
    // cast can go.
    const nodeBuffer = Buffer.from(arrayBuffer);

    const convertedBuffer = await convert({
      buffer: nodeBuffer,
      format: "JPEG",
      quality: 0.9,
    });

    // Re-wrap rather than pass through: the declared Uint8Array is generic over ArrayBufferLike,
    // which admits SharedArrayBuffer, and BlobPart does not.
    return new File([new Uint8Array(convertedBuffer)], parsedInput.file.name.replace(/\.heic$/, ".jpg"), {
      type: "image/jpeg",
    });
  });
