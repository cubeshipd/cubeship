// Every variable is read through a function. Reading one at import time
// would make `next build`, which has none of them, fail.
function need(name: string): string {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is not set`);
  return value;
}

export function databaseUrl(): string {
  return need("DATABASE_URL");
}

export function s3Config(): {
  endpoint: string;
  region: string;
  bucket: string;
  accessKeyId: string;
  secretAccessKey: string;
  pathStyle: boolean;
} {
  return {
    endpoint: need("S3_ENDPOINT"),
    region: process.env.S3_REGION || "us-east-1",
    bucket: need("S3_BUCKET"),
    accessKeyId: need("S3_ACCESS_KEY_ID"),
    secretAccessKey: need("S3_SECRET_ACCESS_KEY"),
    // MinIO needs path style; a bucket-per-host provider does not.
    pathStyle: (process.env.S3_PATH_STYLE ?? "true") !== "false",
  };
}

export function publicUrl(): string {
  return process.env.SITE_URL || "http://localhost:3002";
}
