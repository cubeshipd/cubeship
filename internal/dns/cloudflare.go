package dns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// Cloudflare's API, which is JSON over REST and the same shape for
// everything: a `success` flag, an `errors` list, and a `result`.
//
// The credential is one API token. Cloudflare's older scheme — an email
// plus a global key that can do anything to the account — is not
// accepted here: a token is scoped to the zones and permissions someone
// chose, and storing the other kind would mean storing a credential that
// can close the account.

const cloudflareAPI = "https://api.cloudflare.com/client/v4"

// cfEnvelope is every Cloudflare response. The transport succeeding is
// not the call succeeding: a refusal comes back 200 with success false
// more often than it comes back as a status code.
type cfEnvelope struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	Result     json.RawMessage `json:"result"`
	ResultInfo struct {
		Page       int `json:"page"`
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

// errUnauthorized is a credential the provider would not accept, as
// opposed to a provider that could not be reached. Only the first is
// fixed by storing a new one, which is the whole point of the status
// column and of the settings screen behind it.
var errUnauthorized = fmt.Errorf("the provider refused this credential")

// cfCall runs one Cloudflare request and unwraps the envelope.
func cfCall(ctx context.Context, client *http.Client, c *Credential, method, path string, body, into any) error {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, cloudflareAPI+path, payload)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Password)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("reach Cloudflare: %w", err)
	}
	defer resp.Body.Close()

	var env cfEnvelope
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&env); err != nil {
		// A body that is not the envelope is not a Cloudflare answer at
		// all — a proxy in the way, most likely — and the status is the
		// only thing worth reporting.
		return fmt.Errorf("Cloudflare answered %s", resp.Status)
	}

	if !env.Success {
		refusal := cfRefusal(env)
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return fmt.Errorf("%w: Cloudflare said %q", errUnauthorized, refusal)
		}
		return fmt.Errorf("Cloudflare refused: %s", refusal)
	}
	if into == nil {
		return nil
	}
	return json.Unmarshal(env.Result, into)
}

// cfRefusal renders what Cloudflare said, which is a list rather than a
// sentence — and the list is the only thing that distinguishes a token
// that is wrong from one that is merely missing a permission.
func cfRefusal(env cfEnvelope) string {
	if len(env.Errors) == 0 {
		return "no reason given"
	}
	parts := make([]string, 0, len(env.Errors))
	for _, e := range env.Errors {
		parts = append(parts, e.Message)
	}
	return strings.Join(parts, "; ")
}

// cfPing is the status probe, and it asks for zones rather than
// verifying the token.
//
// /user/tokens/verify looks like the right call and is not: it is a
// *user* endpoint, and a token created at the account level — which is
// an ordinary token with Zone:DNS:Edit on it — is refused there while
// being perfectly able to do everything this screen needs. Probing it
// reported a working credential as unauthorized.
//
// Listing zones is what the detail screen does the moment you click the
// row, so it is what a status should answer for. An empty list is still
// available: a token scoped to a zone that has since gone is a working
// token with nothing to show.
func cfPing(ctx context.Context, client *http.Client, c *Credential) error {
	var zones []struct{}
	return cfCall(ctx, client, c, http.MethodGet, "/zones?per_page=1", nil, &zones)
}

// cfZones lists the zones the token can see.
func cfZones(ctx context.Context, client *http.Client, c *Credential) ([]Zone, error) {
	out := []Zone{}
	for page := 1; ; page++ {
		var batch []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		env, err := cfPage(ctx, client, c, fmt.Sprintf("/zones?per_page=50&page=%d", page), &batch)
		if err != nil {
			return nil, err
		}
		for _, z := range batch {
			out = append(out, Zone{ID: z.ID, Name: NormalizeName(z.Name)})
		}
		if page >= env.ResultInfo.TotalPages || len(batch) == 0 {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// cfPage is cfCall for the endpoints that paginate, which need the
// envelope's own page count rather than only its result.
func cfPage(ctx context.Context, client *http.Client, c *Credential, path string, into any) (cfEnvelope, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cloudflareAPI+path, nil)
	if err != nil {
		return cfEnvelope{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Password)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return cfEnvelope{}, fmt.Errorf("reach Cloudflare: %w", err)
	}
	defer resp.Body.Close()

	var env cfEnvelope
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&env); err != nil {
		return cfEnvelope{}, fmt.Errorf("Cloudflare answered %s", resp.Status)
	}
	if !env.Success {
		refusal := cfRefusal(env)
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return env, fmt.Errorf("%w: Cloudflare said %q", errUnauthorized, refusal)
		}
		return env, fmt.Errorf("Cloudflare refused: %s", refusal)
	}
	return env, json.Unmarshal(env.Result, into)
}

// cfRecord is one row as Cloudflare stores it. Cloudflare keeps one row
// per value, so two A records for a name are two rows here and one
// Record after they are folded together.
type cfRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
}

// cfRows reads every DNS row in a zone, following the pages.
func cfRows(ctx context.Context, client *http.Client, c *Credential, zoneID string) ([]cfRecord, error) {
	rows := []cfRecord{}
	for page := 1; ; page++ {
		var batch []cfRecord
		env, err := cfPage(ctx, client, c,
			fmt.Sprintf("/zones/%s/dns_records?per_page=100&page=%d", url.PathEscape(zoneID), page), &batch)
		if err != nil {
			return nil, err
		}
		rows = append(rows, batch...)
		if page >= env.ResultInfo.TotalPages || len(batch) == 0 {
			return rows, nil
		}
	}
}

// cfRecords lists a zone's records.
//
// Cloudflare's rows are returned one value at a time and folded into a
// Record per name and type, because that is what a person edits and what
// the other provider stores natively. The id of the first row is kept:
// with one value there is one row to address, and with several the
// caller edits by name and type anyway.
func cfRecords(ctx context.Context, client *http.Client, c *Credential, zoneID string) ([]Record, error) {
	rows, err := cfRows(ctx, client, c, zoneID)
	if err != nil {
		return nil, err
	}

	type key struct{ name, kind string }
	order := []key{}
	folded := map[key]*Record{}
	for _, row := range rows {
		k := key{NormalizeName(row.Name), row.Type}
		if existing, ok := folded[k]; ok {
			existing.Values = append(existing.Values, row.Content)
			continue
		}
		folded[k] = &Record{
			ID: row.ID, Name: k.name, Type: row.Type,
			Values: []string{row.Content}, TTL: row.TTL, Proxied: row.Proxied,
		}
		order = append(order, k)
	}

	out := make([]Record, 0, len(order))
	for _, k := range order {
		out = append(out, *folded[k])
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Type < out[j].Type
	})
	return out, nil
}

// cfPutRecord writes a record, replacing whatever was at that name and
// type. Cloudflare has no such operation — it addresses rows by id — so
// this is the read, the deletes and the creates that add up to one.
//
// Every row at that name and type goes first, however many values it
// had: a replace that left the old rows behind would leave the name
// resolving to both answers.
func cfPutRecord(ctx context.Context, client *http.Client, c *Credential, zoneID string, r Record) error {
	rows, err := cfRows(ctx, client, c, zoneID)
	if err != nil {
		return err
	}

	for _, row := range rows {
		if NormalizeName(row.Name) != r.Name || row.Type != r.Type {
			continue
		}
		if err := cfCall(ctx, client, c, http.MethodDelete,
			"/zones/"+url.PathEscape(zoneID)+"/dns_records/"+url.PathEscape(row.ID), nil, nil); err != nil {
			return err
		}
	}

	for _, value := range r.Values {
		body := map[string]any{
			"name": r.Name, "type": r.Type, "content": value, "ttl": r.TTL,
		}
		// Proxying only means anything for the types Cloudflare can sit
		// in front of, and sending it for the rest is refused.
		if r.Type == "A" || r.Type == "AAAA" || r.Type == "CNAME" {
			body["proxied"] = r.Proxied
		}
		if err := cfCall(ctx, client, c, http.MethodPost,
			"/zones/"+url.PathEscape(zoneID)+"/dns_records", body, nil); err != nil {
			return err
		}
	}
	return nil
}

// cfDeleteRecord removes every row at one name and type.
func cfDeleteRecord(ctx context.Context, client *http.Client, c *Credential, zoneID, name, kind string) error {
	rows, err := cfRows(ctx, client, c, zoneID)
	if err != nil {
		return err
	}

	found := false
	for _, row := range rows {
		if NormalizeName(row.Name) != name || row.Type != kind {
			continue
		}
		found = true
		if err := cfCall(ctx, client, c, http.MethodDelete,
			"/zones/"+url.PathEscape(zoneID)+"/dns_records/"+url.PathEscape(row.ID), nil, nil); err != nil {
			return err
		}
	}
	if !found {
		return ErrRecordNotFound
	}
	return nil
}
