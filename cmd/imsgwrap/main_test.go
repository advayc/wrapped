package main

import (
	"strings"
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

func TestContactForBlankHandle(t *testing.T) {
	key, name := contactFor("   ", map[string]string{})
	if key != "" || name != "" {
		t.Fatalf("got %q %q", key, name)
	}
}

func TestResolveHandleUsesSingleParticipant(t *testing.T) {
	got := resolveHandle("", []string{"+1 (555) 123-4567"})
	if got != "+1 (555) 123-4567" {
		t.Fatalf("got %q", got)
	}
}

func TestGroupNameUsesParticipants(t *testing.T) {
	contacts := map[string]string{"5551234567": "Ada", "5552223333": "Bob"}
	got := groupName("", []string{"5551234567", "5552223333", "5554445555"}, contacts)
	if got == "" || strings.HasPrefix(got, "Group ") || got == "Group chat" {
		t.Fatalf("got %q", got)
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

func TestCollectEmojiKeepsVariationSelector(t *testing.T) {
	counts := map[string]int{}
	collectEmoji("❤️ ❤️ 😂", counts)
	if counts["❤️"] != 2 || counts["😂"] != 1 {
		t.Fatalf("unexpected emoji counts: %+v", counts)
	}
}

func TestHTMLTemplateRenders(t *testing.T) {
	report := analysis{
		Timeframe:        timeframe{Label: "2026", Start: time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local), End: time.Date(2026, 12, 31, 0, 0, 0, 0, time.Local)},
		TotalMessages:    10,
		SentMessages:     6,
		ReceivedMessages: 4,
		TopContacts:      []contactStat{{Name: "Ada", Messages: 10, Sent: 6, Received: 4, Monthly: map[string]int{"2026-01": 10}}},
		DailyCounts:      []dayCount{{Date: "2026-01-01", Count: 10}},
		ConversationHeat: []hourDayCount{{Day: 4, Hour: 12, Count: 10}},
		Words:            []wordCount{{Text: "test", Count: 3}},
		Emojis:           []emojiCount{{Emoji: "😂", Count: 2}},
		EmojiSignature:   "😂",
	}
	var b strings.Builder
	if err := pageTemplate.Execute(&b, map[string]any{"Report": report, "Data": `{}`}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "Save summary image") {
		t.Fatal("summary save button missing")
	}
}
