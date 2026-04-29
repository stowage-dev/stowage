---
type: how-to
---

# Cookbook: Go (AWS SDK v2)

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func main() {
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			os.Getenv("AWS_ACCESS_KEY_ID"),
			os.Getenv("AWS_SECRET_ACCESS_KEY"),
			"",
		)),
	)
	if err != nil {
		log.Fatal(err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String("https://s3.stowage.example.com")
		o.UsePathStyle = true
	})

	out, err := client.ListBuckets(context.Background(), &s3.ListBucketsInput{})
	if err != nil {
		log.Fatal(err)
	}
	for _, b := range out.Buckets {
		fmt.Println(*b.Name)
	}
}
```

## Get an object

```go
out, err := client.GetObject(ctx, &s3.GetObjectInput{
    Bucket: aws.String("my-bucket"),
    Key:    aws.String("file.bin"),
})
if err != nil {
    return err
}
defer out.Body.Close()
```

## Put with content-type

```go
_, err := client.PutObject(ctx, &s3.PutObjectInput{
    Bucket:        aws.String("my-bucket"),
    Key:           aws.String("file.bin"),
    Body:          file,
    ContentType:   aws.String("application/octet-stream"),
})
```

## Multipart with the upload manager

```go
import "github.com/aws/aws-sdk-go-v2/feature/s3/manager"

uploader := manager.NewUploader(client, func(u *manager.Uploader) {
    u.PartSize = 16 * 1024 * 1024  // 16 MiB
    u.Concurrency = 4
})

_, err := uploader.Upload(ctx, &s3.PutObjectInput{
    Bucket: aws.String("my-bucket"),
    Key:    aws.String("big.bin"),
    Body:   bigReader,
})
```

## Pre-signed URLs

```go
psClient := s3.NewPresignClient(client)
out, err := psClient.PresignGetObject(ctx, &s3.GetObjectInput{
    Bucket: aws.String("my-bucket"),
    Key:    aws.String("file.bin"),
}, s3.WithPresignExpires(time.Hour))
fmt.Println(out.URL)
```

## Handling 507 quota errors

```go
import "github.com/aws/aws-sdk-go-v2/aws/awserr"

_, err := client.PutObject(ctx, in)
var apiErr smithy.APIError
if errors.As(err, &apiErr) && apiErr.ErrorCode() == "EntityTooLarge" {
    // Bucket quota exceeded — surface to user, do not retry.
}
```
