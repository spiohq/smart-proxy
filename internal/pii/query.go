package pii

import "strings"

// DefaultPIIQueryParams lists query-parameter names whose values are PII
// regardless of which endpoint they appear on. Matching is case-insensitive;
// keys must be lower-cased here.
var DefaultPIIQueryParams = map[string]bool{
	"buyeremail": true,
	"buyername":  true,
}

// queryRedactedValue is the URL-encoded form of "[REDACTED]". Pre-encoding
// avoids depending on net/url and keeps the function allocation-light for
// every non-matching parameter.
const queryRedactedValue = "%5BREDACTED%5D"

// RedactQueryString returns rawQuery with values of PII-bearing parameters
// replaced by the URL-encoded marker "[REDACTED]". Parameter order is
// preserved. Empty input returns empty output. Unknown parameter names
// pass through untouched.
//
// extraLower contains additional PII parameter names. The map keys MUST
// already be lower-cased; the parameter name encodes the contract so callers
// cannot accidentally pass a mixed-case map. NewRegistryWithExtras and the
// SP_PROXY_PII_QUERY_PARAMS env-var path both lower-case before storing, so
// in-tree callers never have to think about it. External callers building
// the map by hand should call strings.ToLower on each key.
//
// The function does not parse-then-reserialize via net/url because that
// would lose duplicate keys and reorder parameters. Instead it walks the
// raw string by '&' splits.
func RedactQueryString(rawQuery string, extraLower map[string]bool) string {
	if rawQuery == "" {
		return ""
	}

	parts := strings.Split(rawQuery, "&")
	for i, part := range parts {
		eq := strings.IndexByte(part, '=')
		if eq < 0 {
			// No '=': key only, no value to redact.
			continue
		}
		key := part[:eq]
		lowered := strings.ToLower(key)
		if DefaultPIIQueryParams[lowered] || (extraLower != nil && extraLower[lowered]) {
			parts[i] = key + "=" + queryRedactedValue
		}
	}
	return strings.Join(parts, "&")
}
