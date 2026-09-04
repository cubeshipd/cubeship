package dns

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"cubeship/internal/platform/awssig"
)

// Route 53's API, which is XML rather than JSON and global rather than
// regional.
//
// Two things about it shape everything below.
//
// **It has no region.** Route 53 is signed for us-east-1 wherever the
// account lives, and that is the service's own rule rather than a
// default chosen here — which is why, unlike ECR, a credential for it
// stores no region for someone to get wrong.
//
// **A record is a set.** Route 53 stores one record per name and type
// holding every value, where Cloudflare stores one row per value. That
// is the shape Record has, and it is Cloudflare that is folded to match
// rather than the other way round: a set is what a person edits.

const (
	route53Host   = "route53.amazonaws.com"
	route53Region = "us-east-1"
	// The API is versioned in the path, and the version decides which
	// elements the XML carries. Pinned rather than tracked: a newer one
	// is a document this code has not been read against.
	route53Version = "2013-04-01"
)

// r53Call signs and runs one Route 53 request.
func r53Call(ctx context.Context, client *http.Client, c *Credential, method, path string, body []byte, into any) error {
	full := "https://" + route53Host + "/" + route53Version + path
	var payload io.Reader
	if body != nil {
		payload = strings.NewReader(string(body))
	}

	req, err := http.NewRequestWithContext(ctx, method, full, payload)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/xml")
	}
	awssig.Sign(req, body, c.Username, c.Password, route53Region, "route53", time.Now())

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("reach Route 53: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("read Route 53's answer: %w", err)
	}

	if resp.StatusCode/100 != 2 {
		return r53Refusal(resp.StatusCode, raw)
	}
	if into == nil {
		return nil
	}
	return xml.Unmarshal(raw, into)
}

// r53Refusal turns an error document into an error, and decides whether
// it was the credential that was refused.
//
// AWS answers 403 for a signature it will not accept and 400 for a
// request it will not carry out, so the status alone nearly says it —
// but an expired key also arrives as 400 with the reason in the code,
// which is why the code is read too.
func r53Refusal(status int, raw []byte) error {
	var doc struct {
		Error struct {
			Code    string `xml:"Code"`
			Message string `xml:"Message"`
		} `xml:"Error"`
		Message string `xml:"Message"`
	}
	_ = xml.Unmarshal(raw, &doc)

	reason := doc.Error.Message
	if reason == "" {
		reason = doc.Message
	}
	if reason == "" {
		reason = http.StatusText(status)
	}

	if status == http.StatusForbidden || r53RejectedTheKey(doc.Error.Code) {
		return fmt.Errorf("%w: AWS said %q", errUnauthorized, reason)
	}
	if status == http.StatusNotFound {
		return ErrNotFound
	}
	return fmt.Errorf("AWS refused: %s", reason)
}

func r53RejectedTheKey(code string) bool {
	for _, name := range []string{
		"InvalidClientTokenId", "SignatureDoesNotMatch", "InvalidSignature",
		"UnrecognizedClientException", "ExpiredToken", "AccessDenied", "AccessDeniedException",
		"IncompleteSignature", "MissingAuthenticationToken",
	} {
		if strings.Contains(code, name) {
			return true
		}
	}
	return false
}

// r53Ping is the status probe: the cheapest call that proves the key
// signs and that the account has Route 53 at all.
func r53Ping(ctx context.Context, client *http.Client, c *Credential) error {
	var doc struct{}
	return r53Call(ctx, client, c, http.MethodGet, "/hostedzone?maxitems=1", nil, &doc)
}

// r53Zones lists the hosted zones on the account.
func r53Zones(ctx context.Context, client *http.Client, c *Credential) ([]Zone, error) {
	out := []Zone{}
	marker := ""
	for {
		path := "/hostedzone?maxitems=100"
		if marker != "" {
			path += "&marker=" + url.QueryEscape(marker)
		}

		var doc struct {
			HostedZones []struct {
				ID   string `xml:"Id"`
				Name string `xml:"Name"`
			} `xml:"HostedZones>HostedZone"`
			IsTruncated bool   `xml:"IsTruncated"`
			NextMarker  string `xml:"NextMarker"`
		}
		if err := r53Call(ctx, client, c, http.MethodGet, path, nil, &doc); err != nil {
			return nil, err
		}

		for _, z := range doc.HostedZones {
			// The id comes back as "/hostedzone/Z123". Everything that
			// takes one wants the bare id, so the prefix goes here
			// rather than at each call site.
			out = append(out, Zone{
				ID:   strings.TrimPrefix(z.ID, "/hostedzone/"),
				Name: NormalizeName(z.Name),
			})
		}
		if !doc.IsTruncated || doc.NextMarker == "" {
			break
		}
		marker = doc.NextMarker
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// r53Records lists a zone's record sets.
func r53Records(ctx context.Context, client *http.Client, c *Credential, zoneID string) ([]Record, error) {
	out := []Record{}
	nextName, nextType := "", ""
	for {
		path := "/hostedzone/" + url.PathEscape(zoneID) + "/rrset?maxitems=300"
		if nextName != "" {
			path += "&name=" + url.QueryEscape(nextName) + "&type=" + url.QueryEscape(nextType)
		}

		var doc struct {
			Sets []struct {
				Name    string `xml:"Name"`
				Type    string `xml:"Type"`
				TTL     int    `xml:"TTL"`
				Records []struct {
					Value string `xml:"Value"`
				} `xml:"ResourceRecords>ResourceRecord"`
				// An alias is Route 53's own thing: a record pointing at
				// an AWS resource, with no value and no TTL. It is
				// listed so the zone adds up, and it is not something
				// this can write.
				AliasTarget struct {
					DNSName string `xml:"DNSName"`
				} `xml:"AliasTarget"`
			} `xml:"ResourceRecordSets>ResourceRecordSet"`
			IsTruncated bool   `xml:"IsTruncated"`
			NextName    string `xml:"NextRecordName"`
			NextType    string `xml:"NextRecordType"`
		}
		if err := r53Call(ctx, client, c, http.MethodGet, path, nil, &doc); err != nil {
			return nil, err
		}

		for _, set := range doc.Sets {
			values := make([]string, 0, len(set.Records))
			for _, r := range set.Records {
				values = append(values, r.Value)
			}
			if len(values) == 0 && set.AliasTarget.DNSName != "" {
				values = append(values, "ALIAS "+NormalizeName(set.AliasTarget.DNSName))
			}
			out = append(out, Record{
				Name: NormalizeName(unescapeR53(set.Name)), Type: set.Type,
				Values: values, TTL: set.TTL,
			})
		}
		if !doc.IsTruncated || doc.NextName == "" {
			break
		}
		nextName, nextType = doc.NextName, doc.NextType
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Type < out[j].Type
	})
	return out, nil
}

// unescapeR53 turns Route 53's octal escapes back into characters. It
// writes a wildcard as \052, and a listing showing that is a listing
// nobody recognises their own record in.
func unescapeR53(name string) string {
	if !strings.Contains(name, `\0`) {
		return name
	}
	var b strings.Builder
	for i := 0; i < len(name); i++ {
		if name[i] == '\\' && i+3 < len(name) {
			var code int
			if _, err := fmt.Sscanf(name[i+1:i+4], "%o", &code); err == nil {
				b.WriteByte(byte(code))
				i += 3
				continue
			}
		}
		b.WriteByte(name[i])
	}
	return b.String()
}

// r53Change submits one change to a zone.
//
// UPSERT is what "write this record" means at Route 53: it creates the
// set or replaces it whole, which is the same promise cfPutRecord makes
// by deleting and re-creating. Here it is one atomic call, and that is
// the better of the two — a Cloudflare replace that fails half way
// leaves the name with fewer values than it started with.
func r53Change(ctx context.Context, client *http.Client, c *Credential, zoneID, action string, r Record) error {
	var values strings.Builder
	for _, v := range r.Values {
		values.WriteString("<ResourceRecord><Value>")
		values.WriteString(xmlEscape(v))
		values.WriteString("</Value></ResourceRecord>")
	}

	body := []byte(`<?xml version="1.0" encoding="UTF-8"?>` +
		`<ChangeResourceRecordSetsRequest xmlns="https://route53.amazonaws.com/doc/` + route53Version + `/">` +
		`<ChangeBatch><Changes><Change>` +
		`<Action>` + action + `</Action>` +
		`<ResourceRecordSet>` +
		`<Name>` + xmlEscape(r.Name) + `</Name>` +
		`<Type>` + xmlEscape(r.Type) + `</Type>` +
		fmt.Sprintf("<TTL>%d</TTL>", r.TTL) +
		`<ResourceRecords>` + values.String() + `</ResourceRecords>` +
		`</ResourceRecordSet>` +
		`</Change></Changes></ChangeBatch>` +
		`</ChangeResourceRecordSetsRequest>`)

	return r53Call(ctx, client, c, http.MethodPost,
		"/hostedzone/"+url.PathEscape(zoneID)+"/rrset/", body, nil)
}

// r53PutRecord writes a record set, creating or replacing it.
func r53PutRecord(ctx context.Context, client *http.Client, c *Credential, zoneID string, r Record) error {
	return r53Change(ctx, client, c, zoneID, "UPSERT", r)
}

// r53DeleteRecord removes a record set.
//
// Route 53 needs the set's current values to delete it — a DELETE that
// does not match what is there is refused — so the set is read back
// first rather than deleted from what a caller believed it held.
func r53DeleteRecord(ctx context.Context, client *http.Client, c *Credential, zoneID, name, kind string) error {
	records, err := r53Records(ctx, client, c, zoneID)
	if err != nil {
		return err
	}
	for _, r := range records {
		if r.Name != name || r.Type != kind {
			continue
		}
		return r53Change(ctx, client, c, zoneID, "DELETE", r)
	}
	return ErrRecordNotFound
}

// xmlEscape escapes a value for the documents above, which are written
// by hand rather than marshalled: they are three nested elements with
// one namespace, and a struct per request to say so is more to read than
// the document is.
func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
