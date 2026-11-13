package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestS3Signature signs the PUT Object example of the Signature Version 4
// documentation of S3.
func TestS3Signature(t *testing.T) {
	endpoint, _ := url.Parse("https://s3.amazonaws.com")
	c := &s3Client{bucket: "examplebucket", region: "us-east-1", endpoint: endpoint,
		accessKey: "AKIAIOSFODNN7EXAMPLE", secretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"}
	req, err := http.NewRequest(http.MethodPut, c.objectURL("test$file.text"), strings.NewReader("Welcome to Amazon S3."))
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.Host != "examplebucket.s3.amazonaws.com" || req.URL.EscapedPath() != "/test%24file.text" {
		t.Fatalf("URL %s", req.URL)
	}
	req.Header.Set("Date", "Fri, 24 May 2013 00:00:00 GMT")
	req.Header.Set("X-Amz-Storage-Class", "REDUCED_REDUNDANCY")
	sum := sha256.Sum256([]byte("Welcome to Amazon S3."))
	c.sign(req, hex.EncodeToString(sum[:]), time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC))
	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, " +
		"SignedHeaders=date;host;x-amz-content-sha256;x-amz-date;x-amz-storage-class, " +
		"Signature=98ad721746da40c64f1a55b78f14c238d841ea1380cd77a1b5971af0ece108bd"
	if got := req.Header.Get("Authorization"); got != want {
		t.Errorf("Authorization:\n%s\nwant\n%s", got, want)
	}
}

func TestS3URLs(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "id")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "eu-west-1")
	no := false
	for _, tt := range []struct {
		s3   s3Config
		want string
	}{
		{s3Config{Bucket: "downloads", Prefix: "/my-app/"}, "https://downloads.s3.eu-west-1.amazonaws.com/my-app/My%20App%201.2.0.dmg"},
		{s3Config{Bucket: "dl.example.com", Region: "us-west-2"}, "https://s3.us-west-2.amazonaws.com/dl.example.com/My%20App%201.2.0.dmg"},
		{s3Config{Bucket: "downloads", Endpoint: "https://acc.r2.cloudflarestorage.com/"}, "https://acc.r2.cloudflarestorage.com/downloads/My%20App%201.2.0.dmg"},
		{s3Config{Bucket: "downloads", Endpoint: "https://oss-cn-hangzhou.aliyuncs.com", PathStyle: &no}, "https://downloads.oss-cn-hangzhou.aliyuncs.com/My%20App%201.2.0.dmg"},
	} {
		c, err := newS3Client(&tt.s3)
		if err != nil {
			t.Fatal(err)
		}
		if got := c.objectURL(c.key("My App 1.2.0.dmg")); got != tt.want {
			t.Errorf("%+v: %s, want %s", tt.s3, got, tt.want)
		}
	}
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	if _, err := newS3Client(&s3Config{Bucket: "downloads"}); err == nil {
		t.Error("no error without credentials")
	}
}
