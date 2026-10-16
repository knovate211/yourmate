package main

import (
	"strings"
	"testing"
)

func TestCampusDirectoryFile(t *testing.T) {
	entries, err := readDirectory()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 500 {
		t.Fatalf("got %d campuses, want the 500 from the master list", len(entries))
	}
	seen := map[string]bool{}
	byCountry := map[string]int{}
	for _, e := range entries {
		key := strings.ToLower(e.name)
		if seen[key] {
			t.Errorf("duplicate campus %q", e.name)
		}
		seen[key] = true
		byCountry[e.country]++
		if slugify(e.name) == "campus" {
			t.Errorf("%q has no usable slug", e.name)
		}
	}
	want := map[string]int{"India": 301, "Germany": 131, "United Kingdom": 68}
	for c, n := range want {
		if byCountry[c] != n {
			t.Errorf("%s: got %d campuses, want %d", c, byCountry[c], n)
		}
	}
}
