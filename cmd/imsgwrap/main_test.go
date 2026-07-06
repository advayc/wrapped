package main

import (
	"testing"
	"time"
)

func TestParseLastDays(t *testing.T) {
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.Local)
	got, err := parseLast("90d", now)
	if err != nil {
		t.Fatal(err)
	}
	want := now.AddDate(0, 0, -90)
	if !got.Equal(want) {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestContactPhoneNormalization(t *testing.T) {
	contacts := map[string]string{}
	addPhoneKeys(contacts, "+1 (555) 123-4567", "Ada")
	_, name := contactFor("5551234567", contacts)
	if name != "Ada" {
		t.Fatalf("got %q", name)
	}
}

func TestLongestStreak(t *testing.T) {
	contacts := map[string]*contactStat{"a": {Name: "Ada"}}
	days := map[string]map[string]bool{"a": {"2026-01-01": true, "2026-01-02": true, "2026-01-04": true}}
	got := longestStreak(days, contacts)
	if got.Days != 2 || got.Name != "Ada" {
		t.Fatalf("got %+v", got)
	}
}

func TestCollectWordsSkipsStopwords(t *testing.T) {
	counts := map[string]int{}
	collectWords("the best best conversation", counts, stopwords())
	if counts["the"] != 0 || counts["best"] != 2 || counts["conversation"] != 1 {
		t.Fatalf("unexpected counts: %+v", counts)
	}
}
