import { HttpError, route } from "@/lib/http";
import { readImage } from "@/lib/storage";

// Served through here rather than from a public bucket: the bucket stays
// private and this is the only way in.
export const GET = route<{ key: string[] }>(async (_request, context) => {
  const key = (await context?.params)?.key ?? [];
  const path = key.join("/");
  if (!path.startsWith("templates/") || path.includes("..")) {
    throw new HttpError(404, "not_found", "no such image");
  }

  try {
    const { body, contentType } = await readImage(path);
    // The DOM body type wants a Uint8Array backed by a plain ArrayBuffer;
    // an S3 SDK read is typed more loosely than that.
    return new Response(new Uint8Array(body), {
      headers: {
        "content-type": contentType,
        "cache-control": "public, max-age=31536000, immutable",
      },
    });
  } catch {
    throw new HttpError(404, "not_found", "no such image");
  }
});
