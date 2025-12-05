package refine

import "testing"

func TestIsLikelySussexCounty(t *testing.T) {
	ok, reason := IsLikelySussexCounty("24 Ruth Drive, Wantage, NJ")
	if !ok || reason == "uncertain" {
		t.Fatalf("expected Wantage address to be Sussex (ok=%v reason=%s)", ok, reason)
	}
	ok, reason = IsLikelySussexCounty("12 Main St, Phillipsburg, Warren County, NJ")
	if ok || reason == "uncertain" {
		t.Fatalf("expected Phillipsburg to be flagged as non-Sussex (ok=%v reason=%s)", ok, reason)
	}
}
