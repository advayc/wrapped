package main

import (
	"database/sql"
	"path/filepath"
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

func TestNormalizeChatHandlesMergesSameContactAliases(t *testing.T) {
	contacts := map[string]string{"5551234567": "Ada", "ada@example.com": "Ada"}
	got := normalizeChatHandles([]string{"5551234567", "ada@example.com"}, contacts)
	if len(got) != 1 {
		t.Fatalf("got %v", got)
	}
}

func TestAttributedBodyFallbackText(t *testing.T) {
	body := []byte("bplist00 noise NSString\x01\x94\x84\x01+hello there NSDictionary")
	if got := messageText("", body); got != "hello there" {
		t.Fatalf("got %q", got)
	}
}

func TestLoadMessagesCountsOneToOneWithAliasHandlesAndAttributedBody(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "chat.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range []string{
		"CREATE TABLE message (ROWID INTEGER PRIMARY KEY, handle_id INTEGER, is_from_me INTEGER, text TEXT, attributedBody BLOB, date INTEGER, associated_message_type INTEGER)",
		"CREATE TABLE chat_message_join (chat_id INTEGER, message_id INTEGER)",
		"CREATE TABLE chat (ROWID INTEGER PRIMARY KEY, display_name TEXT)",
		"CREATE TABLE handle (ROWID INTEGER PRIMARY KEY, id TEXT)",
		"CREATE TABLE chat_handle_join (chat_id INTEGER, handle_id INTEGER)",
		"CREATE TABLE message_attachment_join (message_id INTEGER)",
		"INSERT INTO chat VALUES (1, '')",
		"INSERT INTO handle VALUES (1, '5551234567')",
		"INSERT INTO handle VALUES (2, 'ada@example.com')",
		"INSERT INTO chat_handle_join VALUES (1, 1)",
		"INSERT INTO chat_handle_join VALUES (1, 2)",
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Date(2026, 1, 2, 12, 0, 0, 0, time.Local)
	imsgDate := (at.Unix() - appleEpochOffset) * 1000000000
	body := []byte("bplist00 noise NSString\x01\x94\x84\x01+sent text NSDictionary")
	if _, err := db.Exec("INSERT INTO message VALUES (1, NULL, 1, '', ?, ?, 0)", body, imsgDate); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO message VALUES (2, 2, 0, 'received text', NULL, ?, 0)", imsgDate+1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO chat_message_join VALUES (1, 1), (1, 2)"); err != nil {
		t.Fatal(err)
	}

	old := messageDB
	messageDB = dbPath
	t.Cleanup(func() { messageDB = old })

	contacts := map[string]string{"5551234567": "Ada", "ada@example.com": "Ada"}
	msgs, participants, err := loadMessages(timeframe{Start: at.Add(-time.Hour), End: at.Add(time.Hour)}, contacts)
	if err != nil {
		t.Fatal(err)
	}
	if participants[1] != 1 {
		t.Fatalf("participants = %d", participants[1])
	}
	report := analyze(msgs, participants, timeframe{})
	if len(report.TopContacts) != 1 {
		t.Fatalf("contacts = %+v groups = %+v", report.TopContacts, report.TopGroups)
	}
	got := report.TopContacts[0]
	if got.Name != "Ada" || got.Messages != 2 || got.Sent != 1 || got.Received != 1 {
		t.Fatalf("got %+v", got)
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
		DurationLabel:    "365 days",
		TotalMessages:    10,
		SentMessages:     6,
		ReceivedMessages: 4,
		TopContacts:      []contactStat{{Name: "Ada", Messages: 10, Sent: 6, Received: 4, Monthly: map[string]int{"2026-01": 10}}},
		DailyCounts:      []dayCount{{Date: "2026-01-01", Count: 10}},
		Words:            []wordCount{{Text: "test", Count: 3}},
		Emojis:           []emojiCount{{Emoji: "😂", Count: 2}},
		Tapbacks:         []emojiCount{{Emoji: "Loved", Count: 1}},
		AvgResponseMin:   12.5,
		AvgTheirReplyMin: 7.5,
	}
	var b strings.Builder
	if err := pageTemplate.Execute(&b, map[string]any{"Report": report, "Data": `{}`}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "Save summary image") {
		t.Fatal("summary save button missing")
	}
}
