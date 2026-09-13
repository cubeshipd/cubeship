import { randomUUID } from "node:crypto";
import { GetObjectCommand, PutObjectCommand, S3Client } from "@aws-sdk/client-s3";
import sharp from "sharp";
import { s3Config } from "./env";
import { HttpError } from "./http";

export const IMAGE_LIMIT = 2 * 1024 * 1024;
// One size, so every card in the catalog weighs the same.
const WIDTH = 1200;
const HEIGHT = 675;

export type ObjectClient = {
  put(key: string, body: Uint8Array, contentType: string): Promise<void>;
  get(key: string): Promise<{ body: Uint8Array; contentType: string }>;
};

export function s3Client(): ObjectClient {
  const config = s3Config();
  const client = new S3Client({
    endpoint: config.endpoint,
    region: config.region,
    forcePathStyle: config.pathStyle,
    credentials: { accessKeyId: config.accessKeyId, secretAccessKey: config.secretAccessKey },
  });

  return {
    async put(key, body, contentType) {
      await client.send(
        new PutObjectCommand({
          Bucket: config.bucket,
          Key: key,
          Body: body,
          ContentType: contentType,
        }),
      );
    },
    async get(key) {
      const response = await client.send(new GetObjectCommand({ Bucket: config.bucket, Key: key }));
      const body = new Uint8Array(
        await (
          response.Body as { transformToByteArray(): Promise<Uint8Array> }
        ).transformToByteArray(),
      );
      return { body, contentType: response.ContentType ?? "application/octet-stream" };
    },
  };
}

// Never stored as uploaded: an image taken verbatim is an attack surface
// and an unbounded payload.
export async function putImage(
  bytes: Uint8Array,
  client: ObjectClient = s3Client(),
): Promise<string> {
  if (bytes.byteLength > IMAGE_LIMIT)
    throw new HttpError(413, "too_large", "a photo is at most 2 MB");

  let webp: Buffer;
  try {
    webp = await sharp(bytes)
      .rotate()
      .resize(WIDTH, HEIGHT, { fit: "cover" })
      .webp({ quality: 82 })
      .toBuffer();
  } catch {
    throw new HttpError(415, "not_an_image", "that file is not an image this can read");
  }

  const key = `templates/${randomUUID()}.webp`;
  await client.put(key, webp, "image/webp");
  return key;
}

export async function readImage(key: string, client: ObjectClient = s3Client()) {
  return client.get(key);
}
