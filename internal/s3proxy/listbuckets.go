// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"encoding/xml"
	"net/http"
	"time"
)

type listAllMyBucketsResult struct {
	XMLName xml.Name `xml:"ListAllMyBucketsResult"`
	XMLNS   string   `xml:"xmlns,attr"`
	Owner   bucketOwner
	Buckets bucketContainer
}

type bucketOwner struct {
	XMLName     xml.Name `xml:"Owner"`
	ID          string   `xml:"ID"`
	DisplayName string   `xml:"DisplayName"`
}

type bucketContainer struct {
	XMLName xml.Name    `xml:"Buckets"`
	Bucket  []oneBucket `xml:"Bucket"`
}

type oneBucket struct {
	Name         string `xml:"Name"`
	CreationDate string `xml:"CreationDate"`
}

// WriteSynthesizedListBuckets responds with a ListAllMyBuckets body enumerating
// every bucket the credential is scoped to. For a legacy 1:1 credential this
// is a single-entry list; for an N:1 grant it enumerates all N buckets.
// CreationDate is a shared timestamp — the credential doesn't carry per-bucket
// times, and ListBuckets callers that care about per-bucket metadata should
// use GetBucketLocation / HeadBucket against each name instead.
func WriteSynthesizedListBuckets(w http.ResponseWriter, bucketNames []string, created time.Time) {
	creationDate := created.UTC().Format("2006-01-02T15:04:05.000Z")
	entries := make([]oneBucket, 0, len(bucketNames))
	for _, name := range bucketNames {
		entries = append(entries, oneBucket{Name: name, CreationDate: creationDate})
	}
	res := listAllMyBucketsResult{
		XMLNS:   "http://s3.amazonaws.com/doc/2006-03-01/",
		Owner:   bucketOwner{ID: "stowage", DisplayName: "stowage"},
		Buckets: bucketContainer{Bucket: entries},
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(&res)
}
