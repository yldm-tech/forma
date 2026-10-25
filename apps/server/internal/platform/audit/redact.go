package audit

import (
	"net/url"
	"reflect"
	"strconv"
	"strings"
)

const redacted = "********"

// sensitiveKeys are matched against the lowercased key. forma2 listed "stripeCustomerId" and "fileName" in mixed case and compared them with the lowercased key, so it never redacted those two; they are lowercase here so it does.
var sensitiveKeys = map[string]bool{
	"email": true, "name": true, "password": true, "access_token": true, "refresh_token": true, "id_token": true,
	"twofactorsecret": true, "backupcodes": true, "session_state": true, "provideraccountid": true, "imageurl": true,
	"identityprovideraccountid": true, "locale": true, "token": true, "key": true, "secret": true, "code": true,
	"address": true, "phone": true, "hashedkey": true, "apikey": true, "createdby": true, "lastusedat": true,
	"expiresat": true, "acceptorid": true, "creatorid": true, "firstname": true, "lastname": true, "userid": true,
	"attributes": true, "pin": true, "image": true, "stripecustomerid": true, "filename": true, "state": true,
}

// keysOnlyTargets are the targets whose objects carry what respondents and contacts submitted. Their events record which keys of the submitted subtrees changed, never the values, so answers (free text may hold contact details) do not end up in log retention, where deleting a response cannot reach them.
var keysOnlyTargets = map[Target]bool{TargetResponse: true, TargetContact: true}

// submittedKeys are the subtrees, matched against the lowercased key at any depth, that keysOnlyTargets keep the keys of: a response's answers and hidden fields (data), the values its logic computed from them (variables), the contact attributes captured with it, and its request metadata (meta: URL, user agent, location).
var submittedKeys = map[string]bool{"data": true, "variables": true, "contactattributes": true, "meta": true}

// maskSubmitted replaces the value under every key of each submitted subtree with the redaction mark, at any depth of a generic JSON value. A subtree that is not an object keeps nothing but whether it is null.
func maskSubmitted(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if submittedKeys[strings.ToLower(k)] {
				out[k] = keysOf(val)
				continue
			}
			out[k] = maskSubmitted(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = maskSubmitted(val)
		}
		return out
	default:
		return v
	}
}

// keysOf is a submitted subtree with its keys kept and every value masked.
func keysOf(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case map[string]any:
		out := make(map[string]any, len(t))
		for k := range t {
			out[k] = redacted
		}
		return out
	default:
		return redacted
	}
}

// urlSensitiveParams are masked inside any string value that parses as an absolute URL.
var urlSensitiveParams = map[string]bool{"token": true, "code": true, "state": true}

// redact masks the value of every sensitive key at any depth and the sensitive query parameters of URL strings. v is a generic JSON value.
func redact(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if sensitiveKeys[strings.ToLower(k)] {
				out[k] = redacted
				continue
			}
			out[k] = redact(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = redact(val)
		}
		return out
	case string:
		if u, err := url.Parse(t); err == nil && u.Scheme != "" {
			return sanitizeURL(u)
		}
		return t
	default:
		return v
	}
}

// sanitizeURL keeps origin, path and query, drops credentials and fragment, and masks sensitive query values (the first occurrence; repeats are dropped, as URLSearchParams.set does).
func sanitizeURL(u *url.URL) string {
	prefix := u.Scheme + "://" + u.Host + u.EscapedPath()
	if u.Opaque != "" {
		prefix = u.Scheme + ":" + u.Opaque
	}
	if u.RawQuery == "" {
		return prefix
	}
	pairs := strings.Split(u.RawQuery, "&")
	masked := map[string]bool{}
	out := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		rawKey, _, _ := strings.Cut(pair, "=")
		key, err := url.QueryUnescape(rawKey)
		if err != nil || !urlSensitiveParams[key] {
			out = append(out, pair)
			continue
		}
		if !masked[key] {
			masked[key] = true
			out = append(out, rawKey+"="+redacted)
		}
	}
	return prefix + "?" + strings.Join(out, "&")
}

// deepDiff is forma2's diff of two generic JSON values: the parts of newV that differ from oldV, with removed keys left out. ok is false when nothing differs. Arrays are compared index by index and their diff is an object keyed by index, as JavaScript's Object.keys made it.
func deepDiff(oldV, newV any) (diff any, ok bool) {
	oldM, oldIsObj := asObject(oldV)
	newM, newIsObj := asObject(newV)
	if !oldIsObj || !newIsObj {
		if reflect.DeepEqual(oldV, newV) {
			return nil, false
		}
		return newV, true
	}
	out := map[string]any{}
	for k, nv := range newM {
		ov, inOld := oldM[k]
		if !inOld {
			out[k] = nv
			continue
		}
		if d, changed := deepDiff(ov, nv); changed {
			out[k] = d
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// asObject views a JSON object or array as a map; null and primitives are not objects.
func asObject(v any) (map[string]any, bool) {
	switch t := v.(type) {
	case map[string]any:
		return t, true
	case []any:
		m := make(map[string]any, len(t))
		for i, e := range t {
			m[strconv.Itoa(i)] = e
		}
		return m, true
	default:
		return nil, false
	}
}
