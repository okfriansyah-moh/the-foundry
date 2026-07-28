package capability

import "sort"

// Eligible returns every Record that can serve profile and offers all of the
// required feature strings, in deterministic order (sorted by provider name).
// It is a pure function: no side effects, no I/O, no time dependence. Task 85's
// ExecutorSelector uses it as the capability gate before applying the policy
// allowlist and routing preferences.
//
// A record is eligible iff:
//   - Availability == AvailabilitySupported (unsupported stubs never match), and
//   - profile is not in ProfileDeny (deny always wins), and
//   - ProfileAllow is empty OR profile is listed in it, and
//   - every string in required appears in the record's Features.
//
// required is treated as a set; an empty required matches every otherwise-
// eligible record.
func (r Registry) Eligible(profile string, required []string) []Record {
	var out []Record
	for _, rec := range r.Executors {
		if rec.Availability != AvailabilitySupported {
			continue
		}
		if contains(rec.ProfileDeny, profile) {
			continue
		}
		if len(rec.ProfileAllow) > 0 && !contains(rec.ProfileAllow, profile) {
			continue
		}
		if !hasAll(rec.Features, required) {
			continue
		}
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Provider < out[j].Provider })
	return out
}

func contains(set []string, v string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

func hasAll(have, want []string) bool {
	for _, w := range want {
		if !contains(have, w) {
			return false
		}
	}
	return true
}
