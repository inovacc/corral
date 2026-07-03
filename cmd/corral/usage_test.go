package main

import "testing"

func TestBar(t *testing.T) {
	cases := map[float64]string{
		0:   "[--------------------]",
		100: "[####################]",
		50:  "[##########----------]",
		5:   "[#-------------------]",
	}
	for pct, want := range cases {
		if got := bar(pct); got != want {
			t.Errorf("bar(%v) = %q, want %q", pct, got, want)
		}
	}
	if bar(-5) != bar(0) {
		t.Error("bar should clamp negatives to 0")
	}
	if bar(150) != bar(100) {
		t.Error("bar should clamp >100 to 100")
	}
}
