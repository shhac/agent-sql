package snowflake

import (
	"net/url"
	"strings"
)

// ParsedURL is the structured form of a `snowflake://account/db/schema?param=value`
// connection URL. Extracted into its own type so both `connection add` (which
// stores the parsed components in config) and the ad-hoc `-c snowflake://...`
// path agree on the URL grammar.
type ParsedURL struct {
	Account   string
	Database  string
	Schema    string
	Warehouse string
	Role      string
	Options   map[string]string // other query params
}

// ParseURL parses a snowflake:// URL into structured components. The path
// is split as `/database/schema` (both optional). `warehouse` and `role`
// query parameters are lifted to first-class fields; everything else
// lands in Options. Returns an error if connStr is not a valid URL.
func ParseURL(connStr string) (ParsedURL, error) {
	u, err := url.Parse(connStr)
	if err != nil {
		return ParsedURL{}, err
	}
	p := ParsedURL{Account: u.Hostname()}
	pathParts := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 2)
	if len(pathParts) > 0 && pathParts[0] != "" {
		p.Database = pathParts[0]
	}
	if len(pathParts) > 1 && pathParts[1] != "" {
		p.Schema = pathParts[1]
	}
	for k, vs := range u.Query() {
		if len(vs) == 0 {
			continue
		}
		v := vs[0]
		switch strings.ToLower(k) {
		case "warehouse":
			p.Warehouse = v
		case "role":
			p.Role = v
		default:
			if p.Options == nil {
				p.Options = make(map[string]string)
			}
			p.Options[k] = v
		}
	}
	return p, nil
}
