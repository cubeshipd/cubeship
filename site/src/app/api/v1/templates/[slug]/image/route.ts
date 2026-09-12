import { requireUser } from "@/lib/auth/guard";
import { HttpError, json, route } from "@/lib/http";
import { putImage } from "@/lib/storage";
import { ownedTemplate, updateMetadata } from "@/lib/templates/publish";

export const POST = route<{ slug: string }>(async (request, context) => {
  const user = await requireUser();
  const { slug } = (await context?.params) ?? { slug: "" };
  await ownedTemplate(user, slug);

  const form = await request.formData();
  const file = form.get("file");
  if (!(file instanceof File)) {
    throw new HttpError(422, "invalid_body", 'attach the photo as "file"');
  }

  const key = await putImage(new Uint8Array(await file.arrayBuffer()));
  await updateMetadata(user, slug, { imageKey: key });

  return json({ key, url: `/i/${key}` });
});
