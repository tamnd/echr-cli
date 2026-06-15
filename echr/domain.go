package echr

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// domain.go exposes echr as a kit Domain: a driver that a multi-domain
// host (ant) enables with a single blank import,
//
//	import _ "github.com/tamnd/echr-cli/echr"
//
// exactly as a database/sql program enables a driver with `import _
// "github.com/lib/pq"`. The init below registers it; the host then dereferences
// echr:// URIs by routing to the operations Register installs. The same
// Domain also builds the standalone echr binary (see cli.NewApp), so the
// binary and a host share one source of truth.
func init() { kit.Register(Domain{}) }

// Domain is the echr driver. It carries no state; the per-run client is
// built by the factory Register hands kit.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against, and
// the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "echr",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "echr",
			Short:  "A command line for the European Court of Human Rights HUDOC database.",
			Long: `A command line for the ECHR HUDOC database.

echr reads 229k+ public case judgments and decisions from the European Court of
Human Rights over plain HTTPS, shapes them into clean records, and prints output
that pipes into the rest of your tools. No API key, nothing to run alongside it.`,
			Site: Host,
			Repo: "https://github.com/tamnd/echr-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	kit.Handle(app, kit.OpMeta{Name: "search", Group: "read", List: true,
		Summary: "Search HUDOC by text or query",
		Args:    []kit.Arg{{Name: "query", Help: "search text or HUDOC query"}}}, searchCases)

	kit.Handle(app, kit.OpMeta{Name: "case", Group: "read", Single: true,
		Summary: "Get a single case by HUDOC item ID", URIType: "case", Resolver: true,
		Args: []kit.Arg{{Name: "item-id", Help: "HUDOC item ID (e.g. 001-248971)"}}}, getCase)

	kit.Handle(app, kit.OpMeta{Name: "recent", Group: "read", List: true,
		Summary: "List recently decided cases"}, recentCases)

	kit.Handle(app, kit.OpMeta{Name: "keycases", Group: "read", List: true,
		Summary: "List key cases (Grand Chamber, importance level 1)"}, keyCases)
}

// newClient builds the client from the host-resolved config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := NewClient()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.HTTP.Timeout = cfg.Timeout
	}
	return c, nil
}

// --- inputs ---

type searchInput struct {
	Query      string  `kit:"arg"          help:"search text or HUDOC query"`
	Country    string  `kit:"flag"         help:"filter by respondent country code (e.g. FRA)"`
	Importance int     `kit:"flag"         help:"filter by importance level (1-4)"`
	Limit      int     `kit:"flag,inherit" help:"max results"`
	Offset     int     `kit:"flag"         help:"result offset"`
	Client     *Client `kit:"inject"`
}

type caseInput struct {
	ItemID string  `kit:"arg"   help:"HUDOC item ID (e.g. 001-248971)"`
	Client *Client `kit:"inject"`
}

type recentInput struct {
	Limit  int     `kit:"flag,inherit" help:"max results"`
	Client *Client `kit:"inject"`
}

type keycasesInput struct {
	Country string  `kit:"flag"         help:"filter by respondent country code (e.g. FRA)"`
	Limit   int     `kit:"flag,inherit" help:"max results"`
	Client  *Client `kit:"inject"`
}

// --- handlers ---

func searchCases(ctx context.Context, in searchInput, emit func(*Case) error) error {
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	cases, _, err := in.Client.SearchCases(ctx, in.Query, in.Country, in.Importance, limit, in.Offset)
	if err != nil {
		return mapErr(err)
	}
	for i := range cases {
		if err := emit(&cases[i]); err != nil {
			return err
		}
	}
	return nil
}

func getCase(ctx context.Context, in caseInput, emit func(*Case) error) error {
	c, err := in.Client.GetCase(ctx, in.ItemID)
	if err != nil {
		return mapErr(err)
	}
	return emit(c)
}

func recentCases(ctx context.Context, in recentInput, emit func(*Case) error) error {
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	cases, err := in.Client.RecentCases(ctx, limit)
	if err != nil {
		return mapErr(err)
	}
	for i := range cases {
		if err := emit(&cases[i]); err != nil {
			return err
		}
	}
	return nil
}

func keyCases(ctx context.Context, in keycasesInput, emit func(*Case) error) error {
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	cases, _, err := in.Client.SearchCases(ctx, "", in.Country, 1, limit, 0)
	if err != nil {
		return mapErr(err)
	}
	for i := range cases {
		if err := emit(&cases[i]); err != nil {
			return err
		}
	}
	return nil
}

// --- Resolver: the URI-native string functions, pure and network-free ---

// itemIDRE matches HUDOC item IDs like "001-248971".
var itemIDRE = regexp.MustCompile(`^\d{3}-\d+$`)

// Classify turns any accepted input into the canonical (type, id).
func (Domain) Classify(input string) (uriType, id string, err error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", "", errs.Usage("HUDOC item ID required")
	}
	return "case", s, nil
}

// Locate is the inverse: the live https URL for a (type, id).
func (Domain) Locate(uriType, id string) (string, error) {
	if uriType != "case" {
		return "", errs.Usage("echr has no resource type %q", uriType)
	}
	return fmt.Sprintf(`https://hudoc.echr.coe.int/eng#{"itemid":["%s"]}`, id), nil
}

// mapErr converts a library error into the kit error kind that carries the right
// exit code.
func mapErr(err error) error {
	return err
}
