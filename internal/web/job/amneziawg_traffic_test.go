package job

import "testing"

func TestPruneAmneziaWGBaselinesPreservesUnavailableInbound(t *testing.T) {
	key := amneziaWGTrafficKey{inboundID: 7, email: "user@example.com"}
	last := map[amneziaWGTrafficKey]amneziaWGTrafficSample{
		key: {rx: 100, tx: 200},
	}

	pruneAmneziaWGBaselines(
		last,
		map[int]struct{}{7: {}},
		map[int]struct{}{},
		map[amneziaWGTrafficKey]struct{}{},
	)

	if _, ok := last[key]; !ok {
		t.Fatal("baseline was removed while diagnostics were unavailable")
	}
}

func TestPruneAmneziaWGBaselinesRemovesMissingClientAfterSuccessfulDiagnostics(t *testing.T) {
	missing := amneziaWGTrafficKey{inboundID: 7, email: "removed@example.com"}
	present := amneziaWGTrafficKey{inboundID: 7, email: "present@example.com"}
	last := map[amneziaWGTrafficKey]amneziaWGTrafficSample{
		missing: {rx: 100, tx: 200},
		present: {rx: 300, tx: 400},
	}

	pruneAmneziaWGBaselines(
		last,
		map[int]struct{}{7: {}},
		map[int]struct{}{7: {}},
		map[amneziaWGTrafficKey]struct{}{present: {}},
	)

	if _, ok := last[missing]; ok {
		t.Fatal("baseline for removed client was retained after successful diagnostics")
	}
	if _, ok := last[present]; !ok {
		t.Fatal("baseline for present client was removed")
	}
}

func TestPruneAmneziaWGBaselinesRemovesDeletedInbound(t *testing.T) {
	deleted := amneziaWGTrafficKey{inboundID: 7, email: "user@example.com"}
	last := map[amneziaWGTrafficKey]amneziaWGTrafficSample{
		deleted: {rx: 100, tx: 200},
	}

	pruneAmneziaWGBaselines(
		last,
		map[int]struct{}{},
		map[int]struct{}{},
		map[amneziaWGTrafficKey]struct{}{},
	)

	if len(last) != 0 {
		t.Fatalf("expected deleted inbound baseline to be removed, got %d entries", len(last))
	}
}
