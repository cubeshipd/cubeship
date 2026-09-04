// Package awssig signs requests to AWS with Signature Version 4.
//
// It is a few dozen lines against a published algorithm, and it is here
// rather than behind the AWS SDK because the SDK is a very large
// dependency to take on for two services — ECR's authorization token and
// Route 53's records — reached through a handful of calls each.
//
// The signature is derived from the request as it stands, headers
// included, so a caller sets what it needs and signs last. Anything set
// afterwards is not signed, and AWS will refuse the request.
package awssig

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Sign adds the headers that authenticate a request to one AWS service.
//
// body must be exactly what will be sent — the signature covers its
// hash, so a stream read twice or a body rebuilt afterwards produces a
// signature AWS computes differently and rejects.
func Sign(req *http.Request, body []byte, accessKeyID, secret, region, service string, now time.Time) {
	amzDate := now.UTC().Format("20060102T150405Z")
	dateStamp := now.UTC().Format("20060102")

	host := req.URL.Host
	req.Host = host
	req.Header.Set("X-Amz-Date", amzDate)

	sum := sha256.Sum256(body)
	hashedPayload := hex.EncodeToString(sum[:])
	req.Header.Set("X-Amz-Content-Sha256", hashedPayload)

	// The signed headers are whatever the request carries, rather than a
	// fixed list: one service needs X-Amz-Target and another needs none,
	// and a list that named a header the request does not have signs a
	// request AWS will not recognise.
	signed, canonical := canonicalHeaders(req, host)

	path := req.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	canonicalRequest := strings.Join([]string{
		req.Method, path, req.URL.Query().Encode(), canonical, signed, hashedPayload,
	}, "\n")

	requestSum := sha256.Sum256([]byte(canonicalRequest))
	scope := strings.Join([]string{dateStamp, region, service, "aws4_request"}, "/")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256", amzDate, scope, hex.EncodeToString(requestSum[:]),
	}, "\n")

	key := hmacSHA256([]byte("AWS4"+secret), dateStamp)
	key = hmacSHA256(key, region)
	key = hmacSHA256(key, service)
	key = hmacSHA256(key, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(key, stringToSign))

	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		accessKeyID, scope, signed, signature))
}

// canonicalHeaders renders the headers to sign, and the list naming
// them. Both are lowercase and sorted, because AWS rebuilds them the
// same way and compares the result byte for byte.
func canonicalHeaders(req *http.Request, host string) (signed, canonical string) {
	values := map[string]string{"host": host}
	for name, vs := range req.Header {
		lower := strings.ToLower(name)
		switch lower {
		case "content-type", "x-amz-date", "x-amz-content-sha256", "x-amz-target":
			if len(vs) > 0 && vs[0] != "" {
				values[lower] = strings.TrimSpace(vs[0])
			}
		}
	}

	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	for _, name := range names {
		b.WriteString(name)
		b.WriteByte(':')
		b.WriteString(values[name])
		b.WriteByte('\n')
	}
	return strings.Join(names, ";"), b.String()
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}
