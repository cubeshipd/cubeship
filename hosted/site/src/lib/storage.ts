import { GetObjectCommand, S3Client } from "@aws-sdk/client-s3";
import { s3Config } from "./env";

// Read only: hosted/discovery writes the icons.
export type ObjectClient = {
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

export async function readImage(key: string, client: ObjectClient = s3Client()) {
  return client.get(key);
}
