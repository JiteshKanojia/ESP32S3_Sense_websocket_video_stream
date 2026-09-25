package cameractl

import (
	"encoding/json"
	"testing"
)

func TestMergePartial(t *testing.T) {
	c := New()
	_, err := c.Merge(Settings{Quality: intPtr(12), Antibanding: intPtr(50)})
	if err != nil {
		t.Fatal(err)
	}
	data, err := c.Merge(Settings{Quality: intPtr(18)})
	if err != nil {
		t.Fatal(err)
	}
	var got Settings
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Quality == nil || *got.Quality != 18 {
		t.Fatalf("quality %v", got.Quality)
	}
	if got.Antibanding == nil || *got.Antibanding != 50 {
		t.Fatalf("antibanding %v", got.Antibanding)
	}
}

func intPtr(n int) *int { return &n }
