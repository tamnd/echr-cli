package echr

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const twoResults = `{
	"resultcount": 229707,
	"results": [
		{
			"columns": {
				"itemid": "001-248971",
				"docname": "CASE OF JONES v. UNITED KINGDOM",
				"judgementdate": "12/03/2026 00:00:00",
				"decisiondate": "",
				"appno": "12345/21",
				"respondent": "GBR",
				"importance": "3",
				"typedescription": "Judgment (Merits and Just Satisfaction)",
				"documentcollectionid": "JUDGMENTS"
			}
		},
		{
			"columns": {
				"itemid": "001-248900",
				"docname": "CASE OF MARTIN v. FRANCE",
				"judgementdate": "10/03/2026 00:00:00",
				"decisiondate": "",
				"appno": "99887/20",
				"respondent": "FRA",
				"importance": "2",
				"typedescription": "Judgment (Merits)",
				"documentcollectionid": "JUDGMENTS"
			}
		}
	]
}`

func TestSearchCases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(twoResults))
	}))
	defer srv.Close()

	c := NewClient()
	c.Rate = 0

	cases, total, err := searchCasesAt(context.Background(), c, srv.URL, "article 3", "FRA", 0, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 229707 {
		t.Errorf("total = %d, want 229707", total)
	}
	if len(cases) != 2 {
		t.Fatalf("len(cases) = %d, want 2", len(cases))
	}
	if cases[0].DocName != "CASE OF JONES v. UNITED KINGDOM" {
		t.Errorf("DocName = %q, want CASE OF JONES v. UNITED KINGDOM", cases[0].DocName)
	}
	if cases[0].Respondent != "GBR" {
		t.Errorf("Respondent = %q, want GBR", cases[0].Respondent)
	}
	if cases[1].DocName != "CASE OF MARTIN v. FRANCE" {
		t.Errorf("DocName = %q, want CASE OF MARTIN v. FRANCE", cases[1].DocName)
	}
	if cases[1].Respondent != "FRA" {
		t.Errorf("Respondent = %q, want FRA", cases[1].Respondent)
	}
}

const oneResult = `{
	"resultcount": 1,
	"results": [
		{
			"columns": {
				"itemid": "001-248971",
				"docname": "CASE OF JONES v. UNITED KINGDOM",
				"judgementdate": "12/03/2026 00:00:00",
				"decisiondate": "",
				"appno": "12345/21",
				"respondent": "GBR",
				"importance": "3",
				"typedescription": "Judgment (Merits and Just Satisfaction)",
				"documentcollectionid": "JUDGMENTS"
			}
		}
	]
}`

func TestGetCase(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(oneResult))
	}))
	defer srv.Close()

	c := NewClient()
	c.Rate = 0

	cas, err := getCaseAt(context.Background(), c, srv.URL, "001-248971")
	if err != nil {
		t.Fatal(err)
	}
	if cas.ID != "001-248971" {
		t.Errorf("ID = %q, want 001-248971", cas.ID)
	}
}

func TestRecentCases(t *testing.T) {
	var gotSort string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSort = r.URL.Query().Get("sort")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(twoResults))
	}))
	defer srv.Close()

	c := NewClient()
	c.Rate = 0

	_, err := recentCasesAt(context.Background(), c, srv.URL, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotSort, "judgementdate") || !strings.Contains(strings.ToLower(gotSort), "desc") {
		t.Errorf("sort = %q, want judgementdate Desc", gotSort)
	}
}

func TestRetryOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(oneResult))
	}))
	defer srv.Close()

	c := NewClient()
	c.Rate = 0
	c.Retries = 5

	start := time.Now()
	cases, _, err := searchCasesAt(context.Background(), c, srv.URL, "", "", 0, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if len(cases) != 1 {
		t.Errorf("len(cases) = %d, want 1", len(cases))
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}

// testable variants that accept an explicit base URL instead of BaseURL.

func searchCasesAt(ctx context.Context, c *Client, base, query, country string, importance, limit, offset int) ([]Case, int, error) {
	if limit <= 0 {
		limit = 20
	}
	u := buildURLAt(base, buildQuery(query, country, importance), limit, offset)
	body, err := c.get(ctx, u)
	if err != nil {
		return nil, 0, err
	}
	return parseResp(body)
}

func getCaseAt(ctx context.Context, c *Client, base, itemID string) (*Case, error) {
	import_url := buildURLAt(base, "itemid:"+itemID, 1, 0)
	body, err := c.get(ctx, import_url)
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

func recentCasesAt(ctx context.Context, c *Client, base string, limit int) ([]Case, error) {
	if limit <= 0 {
		limit = 20
	}
	u := buildURLAt(base, "contentsitename:ECHR", limit, 0)
	body, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}
	cases, _, err := parseResp(body)
	return cases, err
}

func buildURLAt(base, query string, limit, offset int) string {
	import_url := base +
		"?query=" + urlEncode(query) +
		"&select=" + urlEncode(selectFields) +
		"&sort=" + urlEncode("judgementdate Desc") +
		"&start=" + intStr(offset) +
		"&length=" + intStr(limit) +
		"&rankingModelId=11111111-0000-0000-0000-000000000000"
	return import_url
}

func urlEncode(s string) string {
	out := ""
	for _, b := range []byte(s) {
		switch {
		case b == ' ':
			out += "+"
		case (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') ||
			b == '-' || b == '_' || b == '.' || b == '~':
			out += string(rune(b))
		default:
			out += "%" + hexByte(b)
		}
	}
	return out
}

func hexByte(b byte) string {
	const hex = "0123456789ABCDEF"
	return string([]byte{hex[b>>4], hex[b&0xf]})
}

func intStr(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
