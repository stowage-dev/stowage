---
type: how-to
---

# Cookbook: Python (boto3)

```python
import os
import boto3
from botocore.config import Config

s3 = boto3.client(
    "s3",
    endpoint_url="https://s3.stowage.example.com",
    region_name="us-east-1",
    aws_access_key_id=os.environ["AWS_ACCESS_KEY_ID"],
    aws_secret_access_key=os.environ["AWS_SECRET_ACCESS_KEY"],
    config=Config(
        s3={"addressing_style": "path"},
        signature_version="s3v4",
    ),
)
```

## List

```python
for b in s3.list_buckets()["Buckets"]:
    print(b["Name"])

for o in s3.get_paginator("list_objects_v2").paginate(Bucket="my-bucket"):
    for item in o.get("Contents", []):
        print(item["Key"], item["Size"])
```

## Get

```python
resp = s3.get_object(Bucket="my-bucket", Key="file.bin")
data = resp["Body"].read()
```

## Put

```python
with open("file.bin", "rb") as f:
    s3.put_object(
        Bucket="my-bucket",
        Key="file.bin",
        Body=f,
        ContentType="application/octet-stream",
    )
```

## Upload large files (managed transfer)

```python
from boto3.s3.transfer import TransferConfig

transfer = TransferConfig(
    multipart_threshold=64 * 1024 * 1024,
    multipart_chunksize=16 * 1024 * 1024,
    max_concurrency=4,
)
s3.upload_file(
    "big.bin", "my-bucket", "big.bin",
    Config=transfer,
)
```

## Pre-signed URLs

```python
url = s3.generate_presigned_url(
    "get_object",
    Params={"Bucket": "my-bucket", "Key": "file.bin"},
    ExpiresIn=3600,
)
```

## Handling 507 quota errors

```python
from botocore.exceptions import ClientError

try:
    s3.put_object(Bucket="my-bucket", Key="x", Body=b"...")
except ClientError as e:
    if e.response["Error"]["Code"] == "EntityTooLarge":
        # Bucket quota exceeded — do not retry blindly.
        raise SystemExit("bucket is full")
    raise
```
