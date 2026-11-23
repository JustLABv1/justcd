package gitops

import "testing"

func TestMatchAdvertisedRefFollowsFetchRules(t *testing.T) {
	const (
		head    = "1111111111111111111111111111111111111111"
		branch  = "2222222222222222222222222222222222222222"
		tagObj  = "3333333333333333333333333333333333333333"
		tagPeel = "4444444444444444444444444444444444444444"
		pr      = "5555555555555555555555555555555555555555"
	)
	output := head + "\tHEAD\n" +
		branch + "\trefs/heads/main\n" +
		branch + "\trefs/heads/release\n" +
		tagObj + "\trefs/tags/release\n" +
		tagPeel + "\trefs/tags/release^{}\n" +
		pr + "\trefs/pull/7/head\n" +
		tagObj + "\trefs/tags/unpeeled\n"
	for _, tc := range []struct {
		revision, want string
		found          bool
	}{
		{"HEAD", head, true},
		{"main", branch, true},
		// git fetch prefers a tag over a branch of the same name.
		{"release", tagPeel, true},
		{"refs/heads/release", branch, true},
		{"refs/pull/7/head", pr, true},
		{"unpeeled", "", false},
		{"missing", "", false},
	} {
		got, found := matchAdvertisedRef(output, tc.revision)
		if got != tc.want || found != tc.found {
			t.Errorf("%s: got %q %v, want %q %v", tc.revision, got, found, tc.want, tc.found)
		}
	}
}
