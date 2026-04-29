---
type: how-to
---

# Cookbook: JavaScript / TypeScript (AWS SDK for JS v3)

```ts
import {
  S3Client,
  ListBucketsCommand,
  GetObjectCommand,
  PutObjectCommand,
} from "@aws-sdk/client-s3";

const s3 = new S3Client({
  endpoint: "https://s3.stowage.example.com",
  region: "us-east-1",
  forcePathStyle: true,
  credentials: {
    accessKeyId: process.env.AWS_ACCESS_KEY_ID!,
    secretAccessKey: process.env.AWS_SECRET_ACCESS_KEY!,
  },
});
```

## List

```ts
const out = await s3.send(new ListBucketsCommand({}));
for (const b of out.Buckets ?? []) {
  console.log(b.Name);
}
```

## Get

```ts
const out = await s3.send(new GetObjectCommand({
  Bucket: "my-bucket",
  Key: "file.bin",
}));
const bytes = await out.Body!.transformToByteArray();
```

## Put

```ts
await s3.send(new PutObjectCommand({
  Bucket: "my-bucket",
  Key: "file.bin",
  Body: bytes,                          // Buffer | Uint8Array | stream | string
  ContentType: "application/octet-stream",
}));
```

## Multipart with the upload helper

```ts
import { Upload } from "@aws-sdk/lib-storage";

const upload = new Upload({
  client: s3,
  params: { Bucket: "my-bucket", Key: "big.bin", Body: stream },
  partSize: 16 * 1024 * 1024,           // 16 MiB
  queueSize: 4,                         // 4 parts in flight
});

upload.on("httpUploadProgress", (p) => console.log(p.loaded, "/", p.total));
await upload.done();
```

## Pre-signed URLs

```ts
import { getSignedUrl } from "@aws-sdk/s3-request-presigner";

const url = await getSignedUrl(s3, new GetObjectCommand({
  Bucket: "my-bucket",
  Key: "file.bin",
}), { expiresIn: 3600 });
```

## Browser usage

The SDK also works directly in the browser. Two important caveats:

1. **CORS** — the bucket must have a CORS rule that allows your
   origin. Set this in the dashboard's bucket settings.
2. **Don't ship long-lived credentials to the browser.** Use
   pre-signed URLs minted server-side, or short-TTL credentials your
   backend hands to the page.

## Handling 507 quota errors

```ts
try {
  await s3.send(new PutObjectCommand({ ... }));
} catch (e: any) {
  if (e.name === "EntityTooLarge") {
    // Bucket quota exceeded — surface to user.
  }
  throw e;
}
```
