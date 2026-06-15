// Package echr is the library behind the echr command line:
// the HTTP client, request shaping, and the typed data models for the ECHR
// HUDOC database of 229k+ case judgments and decisions.
//
// The Client here is the spine every command shares. It sets a real
// User-Agent, paces requests so a busy session stays polite, and retries the
// transient failures (429 and 5xx) that any public site throws under load.
// Build your endpoint calls and JSON decoding on top of it.
package echr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Host is the site this client talks to, and the host the URI driver in
// domain.go claims.
const Host = "hudoc.echr.coe.int"

// BaseURL is the root every request is built from.
const BaseURL = "https://hudoc.echr.coe.int/app/query/results"

// DefaultUserAgent identifies the client to HUDOC. A real, honest
// User-Agent is both polite and the thing most likely to keep you unblocked.
const DefaultUserAgent = "echr-cli/0.1.0"

const selectFields = "itemid,docname,judgementdate,decisiondate,appno,respondent,importance,typedescription,documentcollectionid"

// Wire types — match the HUDOC JSON shapes exactly. Unexported.

type wireColumns struct {
	ItemID        string `json:"itemid"`
	DocName       string `json:"docname"`
	JudgementDate string `json:"judgementdate"` // note: "judgement" spelling
	DecisionDate  string `json:"decisiondate"`
	AppNo         string `json:"appno"`
	Respondent    string `json:"respondent"`
	Importance    string `json:"importance"`
	TypeDesc      string `json:"typedescription"`
	CollectionID  string `json:"documentcollectionid"`
}

type wireResult struct {
	Columns wireColumns `json:"columns"`
}

type wireResp struct {
	ResultCount int          `json:"resultcount"`
	Results     []wireResult `json:"results"`
}

// Case is the public record type: one ECHR case from HUDOC.
type Case struct {
	ID           string `json:"id"            kit:"id"` // itemid
	DocName      string `json:"doc_name"`
	JudgmentDate string `json:"judgment_date"`
	DecisionDate string `json:"decision_date"`
	AppNo        string `json:"app_no"`
	Respondent   string `json:"respondent"`
	Importance   string `json:"importance"`
	Type         string `json:"type"`
	Collection   string `json:"collection"`
}

func caseFromWire(r wireResult) *Case {
	c := r.Columns
	return &Case{
		ID:           c.ItemID,
		DocName:      c.DocName,
		JudgmentDate: c.JudgementDate,
		DecisionDate: c.DecisionDate,
		AppNo:        c.AppNo,
		Respondent:   c.Respondent,
		Importance:   c.Importance,
		Type:         c.TypeDesc,
		Collection:   c.CollectionID,
	}
}

// Client talks to HUDOC over HTTP.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	// Rate is the minimum gap between requests. Zero means no pacing.
	Rate    time.Duration
	Retries int

	last time.Time
}

// NewClient returns a Client with sensible defaults: a 30s timeout, a 500ms
// minimum gap between requests, and three retries on transient errors.
func NewClient() *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: 30 * time.Second},
		UserAgent: DefaultUserAgent,
		Rate:      500 * time.Millisecond,
		Retries:   3,
	}
}

// SearchCases queries HUDOC and returns matching cases plus the total count.
func (c *Client) SearchCases(ctx context.Context, query, country string, importance, limit, offset int) ([]Case, int, error) {
	if limit <= 0 {
		limit = 20
	}
	u := c.buildURL(buildQuery(query, country, importance), limit, offset)
	body, err := c.get(ctx, u)
	if err != nil {
		return nil, 0, err
	}
	return parseResp(body)
}

// GetCase fetches a single case by HUDOC item ID (e.g. "001-248971").
func (c *Client) GetCase(ctx context.Context, itemID string) (*Case, error) {
	q := "itemid:" + url.QueryEscape(itemID)
	u := c.buildURL(q, 1, 0)
	body, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}
	cases, _, err := parseResp(body)
	if err != nil {
		return nil, err
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("case %s: not found", itemID)
	}
	return &cases[0], nil
}

// RecentCases lists recently decided cases sorted by judgment date descending.
func (c *Client) RecentCases(ctx context.Context, limit int) ([]Case, error) {
	if limit <= 0 {
		limit = 20
	}
	u := c.buildURL("contentsitename:ECHR", limit, 0)
	body, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}
	cases, _, err := parseResp(body)
	return cases, err
}

// buildQuery assembles a HUDOC query string from the optional filters.
func buildQuery(query, country string, importance int) string {
	q := "contentsitename:ECHR"
	if country != "" {
		q += " AND respondent:" + strings.ToUpper(country)
	}
	if importance > 0 {
		q += " AND importance:" + strconv.Itoa(importance)
	}
	if query != "" {
		q += " AND " + query
	}
	return q
}

// buildURL assembles the full HUDOC URL with all standard params.
func (c *Client) buildURL(query string, limit, offset int) string {
	return BaseURL +
		"?query=" + url.QueryEscape(query) +
		"&select=" + url.QueryEscape(selectFields) +
		"&sort=" + url.QueryEscape("judgementdate Desc") +
		"&start=" + strconv.Itoa(offset) +
		"&length=" + strconv.Itoa(limit) +
		"&rankingModelId=11111111-0000-0000-0000-000000000000"
}

// get fetches a URL and returns the response body, with pacing and retries.
func (c *Client) get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) (body []byte, retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace blocks until at least Rate has passed since the previous request.
func (c *Client) pace() {
	if c.Rate <= 0 {
		return
	}
	if wait := c.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// parseResp decodes a raw HUDOC JSON response into cases + total count.
func parseResp(body []byte) ([]Case, int, error) {
	var resp wireResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, 0, fmt.Errorf("decode response: %w", err)
	}
	cases := make([]Case, 0, len(resp.Results))
	for _, r := range resp.Results {
		cases = append(cases, *caseFromWire(r))
	}
	return cases, resp.ResultCount, nil
}
