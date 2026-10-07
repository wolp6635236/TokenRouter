package qualityprobe

import "testing"

func TestAutoGroupAllowed(t *testing.T) {
	if AutoGroupAllowed(nil, nil) {
		t.Fatal("provider with no groups should skip")
	}
	if !AutoGroupAllowed(nil, []int64{1}) {
		t.Fatal("empty selected should allow grouped providers")
	}
	if !AutoGroupAllowed([]int64{}, []int64{2}) {
		t.Fatal("empty slice should allow grouped providers")
	}
	if !AutoGroupAllowed([]int64{3, 1}, []int64{9, 1}) {
		t.Fatal("intersection should allow")
	}
	if AutoGroupAllowed([]int64{3}, []int64{1, 2}) {
		t.Fatal("no intersection should skip")
	}
	if AutoGroupAllowed([]int64{3}, nil) {
		t.Fatal("provider with no groups should skip when filter is set")
	}
}

func TestAutoScopeAllowedRequiresActiveGroup(t *testing.T) {
	active := map[int64]struct{}{2: {}}
	if AutoScopeAllowed(nil, []int64{1}, active) {
		t.Fatal("disabled-only membership should skip")
	}
	if !AutoScopeAllowed(nil, []int64{1, 2}, active) {
		t.Fatal("one active group should allow")
	}
	if AutoScopeAllowed([]int64{1}, []int64{1, 2}, active) {
		t.Fatal("selected group that is disabled should skip")
	}
}

func TestAccountAutoEligible(t *testing.T) {
	if !AccountAutoEligible(&Snapshot{}) {
		t.Fatal("empty status should treat as active")
	}
	if AccountAutoEligible(&Snapshot{Status: "error"}) {
		t.Fatal("error status should skip")
	}
	if AccountAutoEligible(&Snapshot{SchedulingOff: true}) {
		t.Fatal("scheduling off should skip")
	}
}

func TestNormalizeGroupIDs(t *testing.T) {
	got := NormalizeGroupIDs([]int64{3, 0, 1, 3, -2, 1})
	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("got %v", got)
	}
}
