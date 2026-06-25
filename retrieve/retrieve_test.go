package retrieve

import "testing"

func bank(t *testing.T) *Bank {
	t.Helper()
	docs := []Document{
		{Name: "cats", Text: "The cat sat quietly on the warm windowsill and watched the small birds " +
			"flit between the garden branches while the morning sun slowly warmed its soft grey fur."},
		{Name: "cars", Text: "The engine roared to life as the driver pressed the pedal and the car " +
			"surged forward down the empty motorway, tyres gripping the cold tarmac through every sweeping bend."},
		{Name: "cooking", Text: "She chopped the onions and garlic finely, then let them soften slowly in " +
			"warm olive oil before adding the tomatoes and herbs to build the rich base of the evening sauce."},
	}
	return Build(docs, DefaultOptions())
}

func TestRetrieveTopical(t *testing.T) {
	b := bank(t)
	if len(b.Chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(b.Chunks))
	}
	res := b.Retrieve("a cat watching birds in the garden", 1)
	if len(res) != 1 {
		t.Fatalf("expected 1 result, got %d", len(res))
	}
	if res[0].Chunk.Source != "cats" {
		t.Errorf("top exemplar = %q, want cats", res[0].Chunk.Source)
	}
	if res[0].Score <= 0 || res[0].Score > 1.0001 {
		t.Errorf("score out of [0,1]: %v", res[0].Score)
	}
}

func TestRetrieveCarTopic(t *testing.T) {
	b := bank(t)
	res := b.Retrieve("driving the car fast down the motorway", 2)
	if len(res) == 0 || res[0].Chunk.Source != "cars" {
		t.Errorf("expected cars top, got %+v", res)
	}
}

func TestRetrieveNoOverlap(t *testing.T) {
	b := bank(t)
	if res := b.Retrieve("zzzqqq xyzzy plugh", 3); len(res) != 0 {
		t.Errorf("expected no results for non-overlapping query, got %d", len(res))
	}
}

func TestRetrieveK(t *testing.T) {
	b := bank(t)
	res := b.Retrieve("the warm garden and the rich sauce and the fast car", 2)
	if len(res) > 2 {
		t.Errorf("k=2 returned %d results", len(res))
	}
}

func TestMinWordsDropsShortChunks(t *testing.T) {
	docs := []Document{{Name: "tiny", Text: "Too short.\n\nAlso short here."}}
	b := Build(docs, Options{MinWords: 20})
	if len(b.Chunks) != 0 {
		t.Errorf("expected short paragraphs dropped, got %d chunks", len(b.Chunks))
	}
}

func TestSingleChunkRetrievable(t *testing.T) {
	// Regression for the idf=0 degenerate case: a one-chunk corpus must still be
	// retrievable (every term has df==n, which the +1 idf floor keeps positive).
	docs := []Document{{Name: "solo", Text: "The quiet river drifted slowly past the old " +
		"stone bridge as the evening light faded gently over the surrounding green hills and fields."}}
	b := Build(docs, DefaultOptions())
	if len(b.Chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(b.Chunks))
	}
	res := b.Retrieve("the river by the bridge", 1)
	if len(res) != 1 || res[0].Chunk.Source != "solo" {
		t.Errorf("single chunk not retrievable: %+v", res)
	}
}

func TestBuildDedupsIdenticalChunks(t *testing.T) {
	para := "This exact paragraph of boilerplate appears in two different documents and " +
		"should be indexed only once so it cannot crowd out the few-shot exemplar budget."
	docs := []Document{{Name: "a", Text: para}, {Name: "b", Text: para}}
	b := Build(docs, DefaultOptions())
	if len(b.Chunks) != 1 {
		t.Errorf("expected duplicate chunk deduped to 1, got %d", len(b.Chunks))
	}
}
