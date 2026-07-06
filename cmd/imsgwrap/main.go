package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	_ "modernc.org/sqlite"
)

const (
	appleEpochOffset = int64(978307200)
	defaultOutDir    = "imsgwrap-output"
	starterGap       = 8 * time.Hour
)

var (
	messageDB      = filepath.Join(homeDir(), "Library", "Messages", "chat.db")
	addressBookDir = filepath.Join(homeDir(), "Library", "Application Support", "AddressBook")
	green          = lipgloss.Color("#216e39")
	blue           = lipgloss.Color("#0969da")
	muted          = lipgloss.Color("#6e7781")
	selectedStyle  = lipgloss.NewStyle().Foreground(green).Bold(true)
	helpStyle      = lipgloss.NewStyle().Foreground(muted)
	titleStyle     = lipgloss.NewStyle().Foreground(blue).Bold(true)
	checkStyle     = lipgloss.NewStyle().Foreground(green).Bold(true)
	phoneRE        = regexp.MustCompile(`\D`)
)

type timeframe struct {
	Label string    `json:"label"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Years []int     `json:"years,omitempty"`
}

type message struct {
	ID              int64
	ChatID          int64
	ChatName        string
	Handle          string
	ContactKey      string
	ContactName     string
	Text            string
	IsFromMe        bool
	At              time.Time
	IsGroup         bool
	IsTapback       bool
	AttachmentCount int
	IsCountable     bool
}

type contactStat struct {
	Key          string         `json:"key"`
	Name         string         `json:"name"`
	Messages     int            `json:"messages"`
	Sent         int            `json:"sent"`
	Received     int            `json:"received"`
	AvgSentChars float64        `json:"avgSentChars"`
	AvgRecvChars float64        `json:"avgRecvChars"`
	Monthly      map[string]int `json:"monthly"`
	ReplyMinutes float64        `json:"replyMinutes,omitempty"`
	TheyMinutes  float64        `json:"theyMinutes,omitempty"`
	DoubleTexts  int            `json:"doubleTexts"`
	StartsYou    int            `json:"startsYou"`
	StartsThem   int            `json:"startsThem"`
	BurstShort   int            `json:"burstShort"`
	BurstLong    int            `json:"burstLong"`
}

type groupStat struct {
	ChatID       int64  `json:"chatId"`
	Name         string `json:"name"`
	Messages     int    `json:"messages"`
	Sent         int    `json:"sent"`
	Received     int    `json:"received"`
	Participants int    `json:"participants"`
}

type dayCount struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type hourDayCount struct {
	Day   int `json:"day"`
	Hour  int `json:"hour"`
	Count int `json:"count"`
}

type wordCount struct {
	Text  string `json:"text"`
	Count int    `json:"count"`
}

type emojiCount struct {
	Emoji string `json:"emoji"`
	Count int    `json:"count"`
}

type streakStat struct {
	Name  string `json:"name"`
	Days  int    `json:"days"`
	Start string `json:"start"`
	End   string `json:"end"`
}

type busiestDay struct {
	Date     string        `json:"date"`
	Messages int           `json:"messages"`
	Top      []contactStat `json:"top"`
}

type analysis struct {
	GeneratedAt        string        `json:"generatedAt"`
	Timeframe          timeframe     `json:"timeframe"`
	DurationLabel      string        `json:"durationLabel"`
	TotalMessages      int           `json:"totalMessages"`
	SentMessages       int           `json:"sentMessages"`
	ReceivedMessages   int           `json:"receivedMessages"`
	AttachmentMessages int           `json:"attachmentMessages"`
	ReactionCount      int           `json:"reactionCount"`
	UniqueContacts     int           `json:"uniqueContacts"`
	TopContacts        []contactStat `json:"topContacts"`
	TopGroups          []groupStat   `json:"topGroups"`
	DailyCounts        []dayCount    `json:"dailyCounts"`
	GoldenHour         string        `json:"goldenHour"`
	GoldenDay          string        `json:"goldenDay"`
	BusiestDay         busiestDay    `json:"busiestDay"`
	LongestStreak      streakStat    `json:"longestStreak"`
	Words              []wordCount   `json:"words"`
	Emojis             []emojiCount  `json:"emojis"`
	Tapbacks           []emojiCount  `json:"tapbacks"`
	StarterYouPct      int           `json:"starterYouPct"`
	AvgResponseMin     float64       `json:"avgResponseMin"`
	AvgTheirReplyMin   float64       `json:"avgTheirReplyMin"`
	DoubleTexts        int           `json:"doubleTexts"`
	BurstShort         int           `json:"burstShort"`
	BurstLong          int           `json:"burstLong"`
	RelationshipLines  []contactStat `json:"relationshipLines"`
}

type options struct {
	outDir    string
	noOpen    bool
	jsonOnly  bool
	redact    bool
	flagAll   bool
	flagYear  int
	flagYears string
	flagFrom  string
	flagTo    string
	flagLast  string
}

func main() {
	var opt options
	flag.StringVar(&opt.outDir, "out", defaultOutDir, "output directory")
	flag.BoolVar(&opt.noOpen, "no-open", false, "do not open the generated site")
	flag.BoolVar(&opt.jsonOnly, "json-only", false, "write data.json without HTML")
	flag.BoolVar(&opt.redact, "redact", false, "hide contact names in output")
	flag.BoolVar(&opt.flagAll, "all", false, "analyze all available messages")
	flag.IntVar(&opt.flagYear, "year", 0, "analyze one year")
	flag.StringVar(&opt.flagYears, "years", "", "analyze comma-separated years, e.g. 2022,2023,2024")
	flag.StringVar(&opt.flagFrom, "from", "", "custom start date YYYY-MM-DD")
	flag.StringVar(&opt.flagTo, "to", "", "custom end date YYYY-MM-DD")
	flag.StringVar(&opt.flagLast, "last", "", "relative duration, e.g. 30d, 90d, 12m")
	flag.Parse()

	fmt.Println(titleStyle.Render("imsgwrap") + helpStyle.Render(" local iMessage Wrapped"))

	tf, err := resolveTimeframe(opt)
	if err != nil {
		fatal(err)
	}

	if err := checkAccess(); err != nil {
		fatal(err)
	}
	fmt.Println(checkStyle.Render("✓") + " Messages database access OK")

	contacts := extractContacts()
	fmt.Printf("%s Indexed %d contact handles\n", checkStyle.Render("✓"), len(contacts))

	msgs, participants, err := loadMessages(tf, contacts)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("%s Loaded %d rows from %s\n", checkStyle.Render("✓"), len(msgs), tf.Label)

	report := analyze(msgs, participants, tf)
	if opt.redact {
		redact(&report)
	}

	if err := writeOutput(report, opt); err != nil {
		fatal(err)
	}

	fmt.Printf("%s Wrote %s\n", checkStyle.Render("✓"), opt.outDir)
	if !opt.noOpen && !opt.jsonOnly {
		_ = exec.Command("open", filepath.Join(opt.outDir, "index.html")).Start()
	}
}

func resolveTimeframe(opt options) (timeframe, error) {
	now := time.Now()
	if opt.flagAll {
		return timeframe{Label: "All Time", Start: time.Unix(0, 0), End: now}, nil
	}
	if opt.flagYear > 0 {
		return yearFrame(opt.flagYear), nil
	}
	if opt.flagYears != "" {
		parts := strings.Split(opt.flagYears, ",")
		years := make([]int, 0, len(parts))
		for _, p := range parts {
			y, err := strconv.Atoi(strings.TrimSpace(p))
			if err != nil {
				return timeframe{}, fmt.Errorf("invalid year %q", p)
			}
			years = append(years, y)
		}
		return yearsFrame(years)
	}
	if opt.flagLast != "" {
		start, err := parseLast(opt.flagLast, now)
		if err != nil {
			return timeframe{}, err
		}
		return timeframe{Label: "Last " + opt.flagLast, Start: start, End: now}, nil
	}
	if opt.flagFrom != "" || opt.flagTo != "" {
		if opt.flagFrom == "" || opt.flagTo == "" {
			return timeframe{}, errors.New("--from and --to must be used together")
		}
		start, err := time.ParseInLocation("2006-01-02", opt.flagFrom, time.Local)
		if err != nil {
			return timeframe{}, fmt.Errorf("invalid --from date")
		}
		end, err := time.ParseInLocation("2006-01-02", opt.flagTo, time.Local)
		if err != nil {
			return timeframe{}, fmt.Errorf("invalid --to date")
		}
		return timeframe{Label: start.Format("Jan 2, 2006") + " - " + end.Format("Jan 2, 2006"), Start: start, End: end.Add(24*time.Hour - time.Nanosecond)}, nil
	}

	years, _ := discoverYears()
	return interactiveFrame(years)
}

func interactiveFrame(years []int) (timeframe, error) {
	now := time.Now()
	choices := []string{"This year (Jan 1 - now)", "All time", "One year", "Combined years", "Recent duration", "Custom date range"}
	idx, err := runMenu("Choose timeframe", choices)
	if err != nil {
		return timeframe{}, err
	}
	switch idx {
	case 0:
		return timeframe{Label: fmt.Sprintf("%d So Far", now.Year()), Start: time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.Local), End: now, Years: []int{now.Year()}}, nil
	case 1:
		return timeframe{Label: "All Time", Start: time.Unix(0, 0), End: now}, nil
	case 2:
		if len(years) == 0 {
			years = []int{now.Year()}
		}
		labels := make([]string, len(years))
		for i, y := range years {
			labels[i] = strconv.Itoa(y)
		}
		pick, err := runMenu("Choose year", labels)
		if err != nil {
			return timeframe{}, err
		}
		return yearFrame(years[pick]), nil
	case 3:
		if len(years) == 0 {
			years = []int{now.Year()}
		}
		labels := make([]string, len(years))
		for i, y := range years {
			labels[i] = strconv.Itoa(y)
		}
		picks, err := runMulti("Choose years (space to toggle)", labels)
		if err != nil {
			return timeframe{}, err
		}
		if len(picks) == 0 {
			return timeframe{}, errors.New("no years selected")
		}
		selected := make([]int, 0, len(picks))
		for _, p := range picks {
			selected = append(selected, years[p])
		}
		return yearsFrame(selected)
	case 4:
		labels := []string{"Last 30 days", "Last 90 days", "Last 180 days", "Last 365 days"}
		pick, err := runMenu("Choose duration", labels)
		if err != nil {
			return timeframe{}, err
		}
		days := []int{30, 90, 180, 365}[pick]
		return timeframe{Label: fmt.Sprintf("Last %d Days", days), Start: now.AddDate(0, 0, -days), End: now}, nil
	default:
		fmt.Println(helpStyle.Render("Type dates only for custom ranges."))
		var from, to string
		fmt.Print("From (YYYY-MM-DD): ")
		_, _ = fmt.Scanln(&from)
		fmt.Print("To   (YYYY-MM-DD): ")
		_, _ = fmt.Scanln(&to)
		return resolveTimeframe(options{flagFrom: from, flagTo: to})
	}
}

type menuModel struct {
	title     string
	choices   []string
	cursor    int
	selected  map[int]bool
	multi     bool
	done      bool
	cancelled bool
}

func (m menuModel) Init() tea.Cmd { return nil }
func (m menuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.cancelled = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.choices)-1 {
				m.cursor++
			}
		case " ":
			if m.multi {
				m.selected[m.cursor] = !m.selected[m.cursor]
			}
		case "enter":
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}
func (m menuModel) View() string {
	var b strings.Builder
	b.WriteString("\n" + titleStyle.Render(m.title) + "\n")
	for i, c := range m.choices {
		prefix := "  "
		mark := " "
		if m.cursor == i {
			prefix = selectedStyle.Render("› ")
		}
		if m.multi && m.selected[i] {
			mark = selectedStyle.Render("●")
		}
		line := fmt.Sprintf("%s%s %s", prefix, mark, c)
		if m.cursor == i {
			line = selectedStyle.Render(line)
		}
		b.WriteString(line + "\n")
	}
	b.WriteString(helpStyle.Render("↑/↓ move  enter select  q quit") + "\n")
	return b.String()
}
func runMenu(title string, choices []string) (int, error) {
	m := menuModel{title: title, choices: choices, selected: map[int]bool{}}
	res, err := tea.NewProgram(m).Run()
	if err != nil {
		return 0, err
	}
	r := res.(menuModel)
	if r.cancelled {
		return 0, errors.New("cancelled")
	}
	return r.cursor, nil
}
func runMulti(title string, choices []string) ([]int, error) {
	m := menuModel{title: title, choices: choices, selected: map[int]bool{}, multi: true}
	res, err := tea.NewProgram(m).Run()
	if err != nil {
		return nil, err
	}
	r := res.(menuModel)
	if r.cancelled {
		return nil, errors.New("cancelled")
	}
	var picks []int
	for i := range choices {
		if r.selected[i] {
			picks = append(picks, i)
		}
	}
	return picks, nil
}

func yearFrame(year int) timeframe {
	return timeframe{Label: strconv.Itoa(year), Start: time.Date(year, 1, 1, 0, 0, 0, 0, time.Local), End: time.Date(year, 12, 31, 23, 59, 59, int(time.Second-time.Nanosecond), time.Local), Years: []int{year}}
}
func yearsFrame(years []int) (timeframe, error) {
	if len(years) == 0 {
		return timeframe{}, errors.New("no years selected")
	}
	sort.Ints(years)
	start := time.Date(years[0], 1, 1, 0, 0, 0, 0, time.Local)
	endYear := years[len(years)-1]
	end := time.Date(endYear, 12, 31, 23, 59, 59, int(time.Second-time.Nanosecond), time.Local)
	return timeframe{Label: fmt.Sprintf("%d-%d", years[0], endYear), Start: start, End: end, Years: years}, nil
}
func parseLast(s string, now time.Time) (time.Time, error) {
	if len(s) < 2 {
		return time.Time{}, errors.New("invalid --last duration")
	}
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil {
		return time.Time{}, err
	}
	switch s[len(s)-1] {
	case 'd':
		return now.AddDate(0, 0, -n), nil
	case 'm':
		return now.AddDate(0, -n, 0), nil
	case 'y':
		return now.AddDate(-n, 0, 0), nil
	}
	return time.Time{}, errors.New("--last supports d, m, or y")
}

func checkAccess() error {
	if _, err := os.Stat(messageDB); err != nil {
		return fmt.Errorf("Messages DB not found at %s; imsgwrap only works on macOS with Messages", messageDB)
	}
	db, err := sql.Open("sqlite", "file:"+messageDB+"?mode=ro&immutable=1")
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec("SELECT 1 FROM message LIMIT 1"); err != nil {
		_ = exec.Command("open", "x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles").Start()
		return errors.New("Full Disk Access denied. Add Terminal/iTerm to System Settings > Privacy & Security > Full Disk Access, then run again")
	}
	return nil
}

func discoverYears() ([]int, error) {
	db, err := sql.Open("sqlite", "file:"+messageDB+"?mode=ro&immutable=1")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query("SELECT DISTINCT CAST(strftime('%Y', datetime((date/1000000000+978307200),'unixepoch','localtime')) AS INT) y FROM message WHERE date IS NOT NULL ORDER BY y DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var years []int
	for rows.Next() {
		var y int
		if rows.Scan(&y) == nil && y > 2000 {
			years = append(years, y)
		}
	}
	return years, nil
}

func extractContacts() map[string]string {
	contacts := map[string]string{}
	paths, _ := filepath.Glob(filepath.Join(addressBookDir, "Sources", "*", "AddressBook-v22.abcddb"))
	main := filepath.Join(addressBookDir, "AddressBook-v22.abcddb")
	if _, err := os.Stat(main); err == nil {
		paths = append(paths, main)
	}
	for _, p := range paths {
		db, err := sql.Open("sqlite", "file:"+p+"?mode=ro&immutable=1")
		if err != nil {
			continue
		}
		people := map[int64]string{}
		rows, err := db.Query("SELECT ROWID, COALESCE(ZFIRSTNAME,''), COALESCE(ZLASTNAME,'') FROM ZABCDRECORD WHERE ZFIRSTNAME IS NOT NULL OR ZLASTNAME IS NOT NULL")
		if err == nil {
			for rows.Next() {
				var id int64
				var first, last string
				if rows.Scan(&id, &first, &last) == nil {
					name := strings.TrimSpace(first + " " + last)
					if name != "" {
						people[id] = name
					}
				}
			}
			rows.Close()
		}
		rows, err = db.Query("SELECT ZOWNER, ZFULLNUMBER FROM ZABCDPHONENUMBER WHERE ZFULLNUMBER IS NOT NULL")
		if err == nil {
			for rows.Next() {
				var owner int64
				var phone string
				if rows.Scan(&owner, &phone) == nil {
					if name := people[owner]; name != "" {
						addPhoneKeys(contacts, phone, name)
					}
				}
			}
			rows.Close()
		}
		rows, err = db.Query("SELECT ZOWNER, ZADDRESS FROM ZABCDEMAILADDRESS WHERE ZADDRESS IS NOT NULL")
		if err == nil {
			for rows.Next() {
				var owner int64
				var email string
				if rows.Scan(&owner, &email) == nil {
					if name := people[owner]; name != "" {
						contacts[strings.ToLower(strings.TrimSpace(email))] = name
					}
				}
			}
			rows.Close()
		}
		_ = db.Close()
	}
	return contacts
}
func addPhoneKeys(m map[string]string, phone, name string) {
	digits := phoneRE.ReplaceAllString(phone, "")
	if digits == "" {
		return
	}
	m[digits] = name
	if len(digits) >= 10 {
		m[digits[len(digits)-10:]] = name
	}
	if len(digits) >= 7 {
		m[digits[len(digits)-7:]] = name
	}
	if len(digits) == 11 && strings.HasPrefix(digits, "1") {
		m[digits[1:]] = name
	}
}
func contactFor(handle string, contacts map[string]string) (string, string) {
	lower := strings.ToLower(strings.TrimSpace(handle))
	if lower == "" {
		return "", ""
	}
	if strings.Contains(lower, "@") {
		if n := contacts[lower]; n != "" {
			return "name:" + strings.ToLower(n), n
		}
		return "email:" + lower, strings.Split(lower, "@")[0]
	}
	d := phoneRE.ReplaceAllString(handle, "")
	for _, k := range []string{d, trimUS(d), lastN(d, 10), lastN(d, 7)} {
		if k != "" {
			if n := contacts[k]; n != "" {
				return "name:" + strings.ToLower(n), n
			}
		}
	}
	if d != "" {
		return "phone:" + lastN(d, 10), handle
	}
	return "raw:" + lower, handle
}
func trimUS(s string) string {
	if len(s) == 11 && strings.HasPrefix(s, "1") {
		return s[1:]
	}
	return ""
}
func lastN(s string, n int) string {
	if len(s) >= n {
		return s[len(s)-n:]
	}
	return ""
}

func loadMessages(tf timeframe, contacts map[string]string) ([]message, map[int64]int, error) {
	db, err := sql.Open("sqlite", "file:"+messageDB+"?mode=ro&immutable=1")
	if err != nil {
		return nil, nil, err
	}
	defer db.Close()
	participants := map[int64]int{}
	chatHandles := map[int64][]string{}
	rows, err := db.Query("SELECT chat_id, COUNT(*) FROM chat_handle_join GROUP BY chat_id")
	if err == nil {
		for rows.Next() {
			var cid int64
			var c int
			if rows.Scan(&cid, &c) == nil {
				participants[cid] = c
			}
		}
		rows.Close()
	}
	rows, err = db.Query("SELECT chj.chat_id, COALESCE(h.id,'') FROM chat_handle_join chj LEFT JOIN handle h ON chj.handle_id=h.ROWID ORDER BY chj.chat_id")
	if err == nil {
		for rows.Next() {
			var cid int64
			var handle string
			if rows.Scan(&cid, &handle) == nil {
				handle = strings.TrimSpace(handle)
				if handle == "" {
					continue
				}
				chatHandles[cid] = append(chatHandles[cid], handle)
			}
		}
		rows.Close()
	}

	hasAssoc := hasColumn(db, "message", "associated_message_type")
	assocExpr := "0"
	if hasAssoc {
		assocExpr = "COALESCE(m.associated_message_type,0)"
	}
	startUnix, endUnix := tf.Start.Unix(), tf.End.Unix()
	q := fmt.Sprintf(`
SELECT m.ROWID, COALESCE(cmj.chat_id,0), COALESCE(c.display_name,''), COALESCE(h.id,''), COALESCE(m.is_from_me,0), COALESCE(m.text,''),
       CAST((m.date/1000000000+978307200) AS INTEGER), %s,
       (SELECT COUNT(*) FROM message_attachment_join maj WHERE maj.message_id=m.ROWID)
FROM message m
LEFT JOIN chat_message_join cmj ON m.ROWID=cmj.message_id
LEFT JOIN chat c ON cmj.chat_id=c.ROWID
LEFT JOIN handle h ON m.handle_id=h.ROWID
WHERE (m.date/1000000000+978307200) >= ? AND (m.date/1000000000+978307200) <= ?
ORDER BY m.date`, assocExpr)
	rows, err = db.Query(q, startUnix, endUnix)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []message
	for rows.Next() {
		var m message
		var fromMe int
		var unix int64
		var assoc int
		if err := rows.Scan(&m.ID, &m.ChatID, &m.ChatName, &m.Handle, &fromMe, &m.Text, &unix, &assoc, &m.AttachmentCount); err != nil {
			return nil, nil, err
		}
		m.IsFromMe = fromMe == 1
		m.At = time.Unix(unix, 0)
		m.IsGroup = participants[m.ChatID] >= 2
		m.IsTapback = assoc != 0 || looksTapback(m.Text)
		m.IsCountable = !m.IsTapback && (strings.TrimSpace(m.Text) != "" || m.AttachmentCount > 0)
		if !m.IsGroup {
			m.Handle = resolveHandle(m.Handle, chatHandles[m.ChatID])
		}
		m.ContactKey, m.ContactName = contactFor(m.Handle, contacts)
		if m.IsGroup {
			m.ContactKey = ""
			m.ContactName = groupName(m.ChatName, chatHandles[m.ChatID], contacts)
		}
		out = append(out, m)
	}
	return out, participants, nil
}
func hasColumn(db *sql.DB, table, column string) bool {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dflt any
		var pk int
		if rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk) == nil && name == column {
			return true
		}
	}
	return false
}
func looksTapback(s string) bool {
	return tapbackLabel(s) != ""
}

func tapbackLabel(s string) string {
	for _, p := range []struct{ prefix, label string }{
		{"Loved \"", "Loved"},
		{"Liked \"", "Liked"},
		{"Disliked \"", "Disliked"},
		{"Laughed at \"", "Laughed"},
		{"Emphasized \"", "Emphasized"},
		{"Questioned \"", "Questioned"},
	} {
		if strings.HasPrefix(s, p.prefix) {
			return p.label
		}
	}
	return ""
}
func resolveHandle(handle string, chatHandles []string) string {
	if strings.TrimSpace(handle) != "" {
		return handle
	}
	if len(chatHandles) == 1 {
		return chatHandles[0]
	}
	return handle
}

func groupName(displayName string, handles []string, contacts map[string]string) string {
	if strings.TrimSpace(displayName) != "" {
		return displayName
	}
	names := make([]string, 0, 2)
	seen := map[string]bool{}
	for _, handle := range handles {
		_, name := contactFor(handle, contacts)
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		names = append(names, name)
		if len(names) == 2 {
			break
		}
	}
	if len(names) == 0 {
		return "Group chat"
	}
	if extra := len(handles) - len(names); extra > 0 {
		return fmt.Sprintf("%s +%d", strings.Join(names, ", "), extra)
	}
	return strings.Join(names, ", ")
}

func analyze(msgs []message, participants map[int64]int, tf timeframe) analysis {
	contactMap := map[string]*contactStat{}
	groupMap := map[int64]*groupStat{}
	daily := map[string]int{}
	words := map[string]int{}
	emojis := map[string]int{}
	tapbacks := map[string]int{}
	contactDays := map[string]map[string]bool{}
	busiestContacts := map[string]map[string]int{}
	var total, sent, recv, attach, reactions, doubleTexts, burstShort, burstLong int
	var yourReplySamples, theirReplySamples []float64
	byContact := map[string][]message{}
	byGroup := map[int64][]message{}
	wordStop := stopwords()

	for _, m := range msgs {
		if m.IsTapback {
			reactions++
			if label := tapbackLabel(m.Text); label != "" {
				tapbacks[label]++
			}
			continue
		}
		if !m.IsCountable {
			continue
		}
		total++
		if m.IsFromMe {
			sent++
		} else {
			recv++
		}
		if m.AttachmentCount > 0 && strings.TrimSpace(m.Text) == "" {
			attach++
		}
		day := m.At.Format("2006-01-02")
		daily[day]++
		if m.IsGroup {
			g := groupMap[m.ChatID]
			if g == nil {
				name := m.ChatName
				if name == "" {
					name = fmt.Sprintf("Group %d", m.ChatID)
				}
				g = &groupStat{ChatID: m.ChatID, Name: name, Participants: participants[m.ChatID]}
				groupMap[m.ChatID] = g
			}
			g.Messages++
			if m.IsFromMe {
				g.Sent++
			} else {
				g.Received++
			}
			byGroup[m.ChatID] = append(byGroup[m.ChatID], m)
			continue
		}
		cs := contactMap[m.ContactKey]
		if cs == nil {
			cs = &contactStat{Key: m.ContactKey, Name: m.ContactName, Monthly: map[string]int{}}
			contactMap[m.ContactKey] = cs
		}
		cs.Messages++
		if m.IsFromMe {
			cs.Sent++
		} else {
			cs.Received++
		}
		cs.Monthly[m.At.Format("2006-01")]++
		if _, ok := contactDays[m.ContactKey]; !ok {
			contactDays[m.ContactKey] = map[string]bool{}
		}
		contactDays[m.ContactKey][day] = true
		if _, ok := busiestContacts[day]; !ok {
			busiestContacts[day] = map[string]int{}
		}
		busiestContacts[day][m.ContactKey]++
		byContact[m.ContactKey] = append(byContact[m.ContactKey], m)
		if strings.TrimSpace(m.Text) != "" {
			collectWords(m.Text, words, wordStop)
			collectEmoji(m.Text, emojis)
		}
	}

	for key, arr := range byContact {
		cs := contactMap[key]
		var sentChars, recvChars, sentN, recvN int
		var prev *message
		ownRun := 0
		for i := range arr {
			m := arr[i]
			chars := len([]rune(m.Text))
			if m.IsFromMe {
				sentChars += chars
				sentN++
			} else {
				recvChars += chars
				recvN++
			}
			if prev != nil {
				gap := m.At.Sub(prev.At)
				if gap > starterGap {
					if m.IsFromMe {
						cs.StartsYou++
					} else {
						cs.StartsThem++
					}
				}
				if gap > 10*time.Second && gap < 24*time.Hour && m.IsFromMe != prev.IsFromMe {
					mins := gap.Minutes()
					if m.IsFromMe {
						yourReplySamples = append(yourReplySamples, mins)
						cs.ReplyMinutes += mins
					} else {
						theirReplySamples = append(theirReplySamples, mins)
						cs.TheyMinutes += mins
					}
				}
			}
			if prev == nil {
				if m.IsFromMe {
					cs.StartsYou++
				} else {
					cs.StartsThem++
				}
			}
			if m.IsFromMe {
				ownRun++
				if ownRun >= 2 {
					doubleTexts++
					cs.DoubleTexts++
				}
			} else {
				ownRun = 0
			}
			if m.IsFromMe && strings.TrimSpace(m.Text) != "" {
				if chars <= 35 {
					burstShort++
					cs.BurstShort++
				}
				if chars >= 180 {
					burstLong++
					cs.BurstLong++
				}
			}
			prev = &arr[i]
		}
		if sentN > 0 {
			cs.AvgSentChars = float64(sentChars) / float64(sentN)
		}
		if recvN > 0 {
			cs.AvgRecvChars = float64(recvChars) / float64(recvN)
		}
		replies := max(1, transitions(arr, false, true))
		theirs := max(1, transitions(arr, true, false))
		cs.ReplyMinutes /= float64(replies)
		cs.TheyMinutes /= float64(theirs)
	}

	contacts := ptrContacts(contactMap)
	sort.Slice(contacts, func(i, j int) bool { return contacts[i].Messages > contacts[j].Messages })
	groups := ptrGroups(groupMap)
	sort.Slice(groups, func(i, j int) bool { return groups[i].Messages > groups[j].Messages })
	days := make([]dayCount, 0, len(daily))
	var busy busiestDay
	for d, c := range daily {
		days = append(days, dayCount{d, c})
		if c > busy.Messages {
			busy = busiestDay{Date: d, Messages: c}
		}
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })
	if busy.Date != "" {
		busy.Top = topForDay(busiestContacts[busy.Date], contactMap)
	}
	streak := longestStreak(contactDays, contactMap)
	starterPct := 0
	startsYou, startsThem := 0, 0
	for _, c := range contacts {
		startsYou += c.StartsYou
		startsThem += c.StartsThem
	}
	if startsYou+startsThem > 0 {
		starterPct = int(math.Round(float64(startsYou) * 100 / float64(startsYou+startsThem)))
	}
	return analysis{GeneratedAt: time.Now().Format(time.RFC3339), Timeframe: tf, DurationLabel: fmt.Sprintf("%d days", int(tf.End.Sub(tf.Start).Hours()/24)+1), TotalMessages: total, SentMessages: sent, ReceivedMessages: recv, AttachmentMessages: attach, ReactionCount: reactions, UniqueContacts: len(contactMap), TopContacts: limitContacts(contacts, 20), TopGroups: limitGroups(groups, 5), DailyCounts: days, GoldenHour: "", GoldenDay: "", BusiestDay: busy, LongestStreak: streak, Words: topWords(words, 60), Emojis: topEmojis(emojis, 12), Tapbacks: topEmojis(tapbacks, 6), StarterYouPct: starterPct, AvgResponseMin: avg(yourReplySamples), AvgTheirReplyMin: avg(theirReplySamples), DoubleTexts: doubleTexts, BurstShort: burstShort, BurstLong: burstLong, RelationshipLines: limitContacts(contacts, 5)}
}

func transitions(arr []message, from, to bool) int {
	n := 0
	for i := 1; i < len(arr); i++ {
		if arr[i-1].IsFromMe == from && arr[i].IsFromMe == to {
			n++
		}
	}
	return n
}
func ptrContacts(m map[string]*contactStat) []contactStat {
	out := make([]contactStat, 0, len(m))
	for _, v := range m {
		out = append(out, *v)
	}
	return out
}
func ptrGroups(m map[int64]*groupStat) []groupStat {
	out := make([]groupStat, 0, len(m))
	for _, v := range m {
		out = append(out, *v)
	}
	return out
}
func limitContacts(in []contactStat, n int) []contactStat {
	if len(in) < n {
		return in
	}
	return in[:n]
}
func limitGroups(in []groupStat, n int) []groupStat {
	if len(in) < n {
		return in
	}
	return in[:n]
}
func topForDay(m map[string]int, contacts map[string]*contactStat) []contactStat {
	var out []contactStat
	for k, c := range m {
		if base := contacts[k]; base != nil {
			x := *base
			x.Messages = c
			out = append(out, x)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Messages > out[j].Messages })
	return limitContacts(out, 5)
}
func longestStreak(days map[string]map[string]bool, contacts map[string]*contactStat) streakStat {
	best := streakStat{}
	for k, set := range days {
		var ds []string
		for d := range set {
			ds = append(ds, d)
		}
		sort.Strings(ds)
		cur, start := 0, ""
		var prev time.Time
		for _, s := range ds {
			d, _ := time.Parse("2006-01-02", s)
			if cur == 0 || d.Sub(prev) == 24*time.Hour {
				cur++
				if start == "" {
					start = s
				}
			} else {
				cur = 1
				start = s
			}
			if cur > best.Days {
				best = streakStat{Name: contacts[k].Name, Days: cur, Start: start, End: s}
			}
			prev = d
		}
	}
	return best
}
func topWords(m map[string]int, n int) []wordCount {
	out := make([]wordCount, 0, len(m))
	for w, c := range m {
		out = append(out, wordCount{w, c})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	if len(out) > n {
		out = out[:n]
	}
	return out
}
func topEmojis(m map[string]int, n int) []emojiCount {
	out := make([]emojiCount, 0, len(m))
	for e, c := range m {
		out = append(out, emojiCount{e, c})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	if len(out) > n {
		out = out[:n]
	}
	return out
}
func firstEmoji(m map[string]int) string {
	e := topEmojis(m, 1)
	if len(e) == 0 {
		return ""
	}
	return e[0].Emoji
}
func avg(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var s float64
	for _, x := range xs {
		s += x
	}
	return math.Round(s/float64(len(xs))*10) / 10
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func formatHour(h int) string {
	t := time.Date(2000, 1, 1, h, 0, 0, 0, time.Local)
	return strings.ToLower(t.Format("3 PM"))
}
func weekdayName(d int) string {
	return []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}[d%7]
}

func collectWords(text string, counts map[string]int, stop map[string]bool) {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	for _, f := range fields {
		if strings.HasPrefix(f, "http") || strings.Contains(f, "www") {
			continue
		}
		if len([]rune(f)) >= 3 && !stop[f] {
			counts[f]++
		}
	}
}
func collectEmoji(text string, counts map[string]int) {
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		if !isEmojiBase(runes[i]) {
			continue
		}
		seq := []rune{runes[i]}
		for i+1 < len(runes) && isEmojiContinuation(runes[i+1]) {
			i++
			seq = append(seq, runes[i])
			if runes[i] == '\u200d' && i+1 < len(runes) && isEmojiBase(runes[i+1]) {
				i++
				seq = append(seq, runes[i])
			}
		}
		counts[string(seq)]++
	}
}
func isEmojiBase(r rune) bool {
	return (r >= 0x1F000 && r <= 0x1FAFF) || (r >= 0x2600 && r <= 0x27BF)
}
func isEmojiContinuation(r rune) bool {
	return r == '\uFE0F' || r == '\u200D' || (r >= 0x1F3FB && r <= 0x1F3FF) || (r >= 0xE0020 && r <= 0xE007F)
}
func stopwords() map[string]bool {
	words := strings.Fields("the and for you that this with have but not are was from just like what when your all can out get got she him her they them then than there here about would could should because its it's im i'm dont don't yeah yes lol lmao too very also into did does how why who been our their his has had were will gonna wanna one two okay ok really actually still back make made say said see know think time good much more some only even right sure need want going yeahh haha hahah")
	m := map[string]bool{}
	for _, w := range words {
		m[w] = true
	}
	return m
}

func redact(a *analysis) {
	for i := range a.TopContacts {
		a.TopContacts[i].Name = fmt.Sprintf("Contact %d", i+1)
	}
	for i := range a.RelationshipLines {
		a.RelationshipLines[i].Name = fmt.Sprintf("Contact %d", i+1)
	}
	for i := range a.BusiestDay.Top {
		a.BusiestDay.Top[i].Name = fmt.Sprintf("Contact %d", i+1)
	}
	if a.LongestStreak.Name != "" {
		a.LongestStreak.Name = "Contact"
	}
	for i := range a.TopGroups {
		a.TopGroups[i].Name = fmt.Sprintf("Group %d", i+1)
	}
}

func writeOutput(a analysis, opt options) error {
	if err := os.MkdirAll(opt.outDir, 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(opt.outDir, "data.json"), b, 0644); err != nil {
		return err
	}
	if opt.jsonOnly {
		return nil
	}
	f, err := os.Create(filepath.Join(opt.outDir, "index.html"))
	if err != nil {
		return err
	}
	defer f.Close()
	dataJSON, _ := json.Marshal(a)
	return pageTemplate.Execute(f, map[string]any{"Report": a, "Data": template.JS(dataJSON)})
}

func homeDir() string { h, _ := os.UserHomeDir(); return h }
func fatal(err error) {
	fmt.Fprintln(os.Stderr, lipgloss.NewStyle().Foreground(lipgloss.Color("#cf222e")).Bold(true).Render("error:"), err)
	os.Exit(1)
}

var pageTemplate = template.Must(template.New("page").Parse(htmlPage))

const htmlPage = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>iMessage Wrapped</title><link rel="preconnect" href="https://fonts.googleapis.com"><link rel="preconnect" href="https://fonts.gstatic.com" crossorigin><link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700;800;900&display=swap" rel="stylesheet"><script src="https://cdnjs.cloudflare.com/ajax/libs/html2canvas/1.4.1/html2canvas.min.js"></script><style>
:root{--ink:#071016;--muted:#637083;--line:#e2e8f0;--soft:#f8fafc;--blue:#2563eb;--cyan:#06b6d4;--pink:#ec4899;--amber:#f59e0b;--violet:#7c3aed;--gold:#d6a11d;--silver:#9ca3af;--bronze:#b45309;--green0:#ebedf0;--green1:#9be9a8;--green2:#40c463;--green3:#30a14e;--green4:#216e39}*{box-sizing:border-box}body{margin:0;font-family:Inter,system-ui,sans-serif;color:var(--ink);background:radial-gradient(circle at 10% 10%,#e0f2fe 0,transparent 28%),radial-gradient(circle at 90% 20%,#fce7f3 0,transparent 30%),linear-gradient(135deg,#fff 0%,#f8fafc 100%);overflow:hidden}.slides{height:100vh;display:flex;transition:transform .45s cubic-bezier(.22,1,.36,1)}.slide{min-width:100vw;height:100vh;padding:54px 72px;display:flex;flex-direction:column;justify-content:center;gap:22px}.kicker{font-size:13px;font-weight:900;letter-spacing:.14em;text-transform:uppercase;color:var(--blue)}.hero{font-size:clamp(58px,9vw,132px);line-height:.88;letter-spacing:-.085em;font-weight:900}.sub{font-size:clamp(17px,2vw,26px);color:var(--muted);max-width:840px}.grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:16px}.card{border:1px solid rgba(148,163,184,.35);border-radius:28px;padding:24px;background:rgba(255,255,255,.82);box-shadow:0 16px 50px rgba(15,23,42,.08);backdrop-filter:blur(18px)}.card.blue{background:linear-gradient(135deg,#eff6ff,#fff)}.card.pink{background:linear-gradient(135deg,#fdf2f8,#fff)}.card.green{background:linear-gradient(135deg,#ecfdf5,#fff)}.num{font-size:clamp(42px,6vw,86px);font-weight:900;letter-spacing:-.065em}.label{color:var(--muted);font-weight:700}.rank{display:flex;align-items:center;justify-content:space-between;padding:13px 14px;border:1px solid transparent;border-radius:18px;gap:16px;margin-bottom:8px;background:rgba(255,255,255,.68)}.rank.gold{background:linear-gradient(90deg,rgba(214,161,29,.22),rgba(255,255,255,.86));border-color:rgba(214,161,29,.35)}.rank.silver{background:linear-gradient(90deg,rgba(156,163,175,.22),rgba(255,255,255,.86));border-color:rgba(156,163,175,.35)}.rank.bronze{background:linear-gradient(90deg,rgba(180,83,9,.2),rgba(255,255,255,.86));border-color:rgba(180,83,9,.32)}.rank-main{display:flex;align-items:center;gap:12px;min-width:0}.rank-name{font-size:18px;font-weight:800;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.rank-meta{font-size:12px;color:var(--muted);font-weight:700}.rank-count{font-weight:900;font-size:20px}.medal{font-size:22px}.pill{display:inline-flex;border:1px solid var(--line);border-radius:999px;padding:9px 13px;color:var(--muted);font-weight:800;background:white}.section-title{font-size:24px;font-weight:900;letter-spacing:-.03em}.chart-title{font-size:18px;font-weight:900;margin-bottom:6px}.chart-note{font-size:13px;color:var(--muted);font-weight:700;margin-bottom:12px}.heatwrap{overflow:auto;padding:12px;border:1px solid rgba(148,163,184,.32);border-radius:28px;background:rgba(255,255,255,.78);box-shadow:0 16px 40px rgba(15,23,42,.06)}.year-block{display:grid;grid-template-columns:54px 1fr;gap:12px;margin:0 0 18px}.year-label{font-weight:900;color:var(--blue);padding-top:24px}.months{height:18px;position:relative;margin-left:38px;color:var(--muted);font-size:11px;font-weight:800}.heat{display:flex;gap:4px}.days{width:32px;display:grid;grid-template-rows:repeat(7,13px);gap:4px;color:var(--muted);font-size:10px;font-weight:800}.week{display:grid;grid-template-rows:repeat(7,13px);gap:4px}.cell{width:13px;height:13px;border-radius:3px;background:var(--green0);transition:transform .12s ease,box-shadow .12s ease}.cell:hover,.mcell:hover{transform:scale(1.55);box-shadow:0 8px 20px rgba(15,23,42,.22);z-index:3}.l1{background:var(--green1)}.l2{background:var(--green2)}.l3{background:var(--green3)}.l4{background:var(--green4)}.legend{display:flex;align-items:center;gap:5px;color:var(--muted);font-size:12px;font-weight:800;margin-top:8px}.matrix-wrap{overflow:auto;border:1px solid rgba(148,163,184,.32);border-radius:28px;background:rgba(255,255,255,.78);padding:18px}.matrix{display:grid;grid-template-columns:62px repeat(24,24px);gap:5px;align-items:center;min-width:700px}.axis{font-size:11px;color:var(--muted);font-weight:800;text-align:center}.day-axis{text-align:right;padding-right:6px}.mcell{height:22px;border-radius:6px;background:var(--green0);transition:transform .12s ease,box-shadow .12s ease}.cloud{display:flex;flex-wrap:wrap;gap:12px 16px;align-items:center}.emoji-row{display:flex;gap:14px;flex-wrap:wrap}.emoji-chip{font-size:34px;background:white;border:1px solid var(--line);border-radius:22px;padding:9px 12px;box-shadow:0 10px 28px rgba(15,23,42,.06)}.chart-card{border:1px solid rgba(148,163,184,.32);border-radius:28px;background:rgba(255,255,255,.82);padding:22px;box-shadow:0 16px 44px rgba(15,23,42,.06)}.scatter{width:100%;height:390px}.bars{height:300px;display:flex;align-items:end;gap:12px;border-left:2px solid #cbd5e1;border-bottom:2px solid #cbd5e1;padding:16px 16px 0}.bar{flex:1;background:linear-gradient(180deg,var(--pink),var(--blue));border-radius:12px 12px 0 0;min-height:4px;position:relative}.bar-label{writing-mode:vertical-rl;transform:rotate(180deg);font-size:10px;color:var(--muted);position:absolute;bottom:-54px;left:50%;translate:-50% 0;font-weight:800}.tooltip{position:fixed;display:none;pointer-events:none;z-index:20;background:#0f172a;color:white;border-radius:12px;padding:9px 11px;font-size:12px;font-weight:800;box-shadow:0 16px 42px rgba(15,23,42,.28)}.summary-card{width:min(520px,88vw);background:linear-gradient(145deg,#ffffff,#eff6ff 55%,#fdf2f8);border:1px solid rgba(148,163,184,.4);border-radius:36px;padding:30px;box-shadow:0 22px 70px rgba(15,23,42,.16)}.summary-top{border-bottom:1px solid rgba(148,163,184,.35);padding-bottom:16px;margin-bottom:20px}.summary-title{font-weight:900;letter-spacing:-.04em;font-size:25px}.summary-grid{display:grid;grid-template-columns:repeat(2,1fr);gap:12px}.summary-stat{background:rgba(255,255,255,.7);border:1px solid rgba(148,163,184,.26);border-radius:20px;padding:16px}.save-btn{border:0;border-radius:999px;background:#111827;color:white;padding:13px 18px;font-weight:900;cursor:pointer;width:max-content}.nav{position:fixed;bottom:22px;left:50%;transform:translateX(-50%);display:flex;gap:8px;align-items:center}.dot{width:8px;height:8px;border-radius:99px;background:#cbd5e1}.dot.active{width:28px;background:#111827}.arrow{position:fixed;top:50%;transform:translateY(-50%);border:0;background:rgba(255,255,255,.75);font-size:42px;cursor:pointer;border-radius:20px}.prev{left:18px}.next{right:18px}@media(max-width:850px){.slide{padding:34px 22px}.grid{grid-template-columns:1fr}.hero{font-size:58px}.arrow{display:none}.rank-name{font-size:15px}.matrix{grid-template-columns:44px repeat(24,18px)}.mcell{height:18px}.year-block{grid-template-columns:42px 1fr}}
 </style><style>body{background:#f6f4ee}.card,.card.blue,.card.pink,.card.green,.summary-card,.heatwrap,.matrix-wrap,.chart-card{background:#fff}.section-title{font-size:clamp(30px,4.2vw,56px);line-height:.92;letter-spacing:-.08em;font-weight:900;max-width:840px}.section-subtitle{font-size:clamp(16px,1.75vw,21px);line-height:1.45;color:var(--muted);max-width:760px}.leaderboard-shell{display:grid;gap:18px;padding-top:10px}.leaderboard-spotlight{display:flex;align-items:flex-end;justify-content:space-between;gap:18px;padding:26px 28px;border-radius:30px;background:linear-gradient(135deg,rgba(214,161,29,.16),rgba(255,255,255,.95));border:1px solid rgba(214,161,29,.28);box-shadow:0 16px 50px rgba(15,23,42,.08)}.leaderboard-spotlight .rank-main{align-items:center}.leaderboard-spotlight .medal{width:54px;height:54px;border-radius:18px;font-size:24px}.leaderboard-spotlight .rank-name{font-size:clamp(28px,3vw,42px)}.leaderboard-spotlight .rank-meta{font-size:15px}.leaderboard-spotlight .rank-count{font-size:clamp(46px,6vw,82px)}.rank-list{display:grid;gap:8px}.rank{display:flex;align-items:center;justify-content:space-between;padding:16px 18px;border:1px solid transparent;border-radius:18px;gap:16px;margin-bottom:0;background:rgba(255,255,255,.68)}.rank.gold{background:linear-gradient(90deg,rgba(214,161,29,.22),rgba(255,255,255,.86));border-color:rgba(214,161,29,.35)}.rank.silver{background:linear-gradient(90deg,rgba(156,163,175,.22),rgba(255,255,255,.86));border-color:rgba(156,163,175,.25)}.rank.bronze{background:linear-gradient(90deg,rgba(180,83,9,.14),rgba(255,255,255,.86));border-color:rgba(180,83,9,.25)}.rank.reveal{animation:rankRise .45s cubic-bezier(.22,1,.36,1) both}@keyframes rankRise{from{opacity:0;transform:translateY(16px) scale(.985)}to{opacity:1;transform:translateY(0) scale(1)}}.bar{background:#2563eb}.arrow{display:none}.dot{cursor:pointer}.save-btn{transition:transform .18s ease,box-shadow .18s ease,background .18s ease}.save-btn:hover{transform:translateX(-50%) translateY(-1px);box-shadow:0 14px 30px rgba(17,24,39,.28);background:#0f172a}.summary-card{padding:36px}.summary-top{margin-bottom:24px}.summary-grid{gap:14px;margin-top:22px}.summary-footer{margin-top:22px}.year-label{line-height:1.1}</style></head><body><main class="slides" id="slides">
<section class="slide"><div class="kicker">iMessage Wrapped</div><div class="hero">{{.Report.Timeframe.Label}}</div><div class="sub">A local-only Wrapped for your Messages data. Nothing uploaded. {{.Report.TotalMessages}} countable messages analyzed.</div><span class="pill">click or arrow keys to move</span></section>
<section class="slide"><div class="kicker">Total history</div><div class="grid"><div class="card blue"><div class="num">{{.Report.TotalMessages}}</div><div class="label">messages</div></div><div class="card green"><div class="num">{{.Report.SentMessages}}</div><div class="label">sent by you</div></div><div class="card pink"><div class="num">{{.Report.ReceivedMessages}}</div><div class="label">received</div></div></div><div class="sub">Attachments-only messages count for activity. Tapbacks are tracked separately as {{.Report.ReactionCount}} reactions.</div></section>
 <section class="slide"><div class="kicker">Top Contact Leaderboard</div><div class="section-title">Your message podium</div><div class="section-subtitle">First the number one contact, then the gold, silver, bronze lineup and the rest of the leaderboard.</div><div id="contacts"></div></section>
<section class="slide"><div class="kicker">Group chats</div><div class="section-title">Top group rooms, kept separate from personal contacts.</div><div id="groups"></div></section>
 <section class="slide"><div class="kicker">GitHub-style activity</div><div class="chart-title">Messages per day</div><div class="chart-note">Hover any square for the date and message count. Year totals sit on the right.</div><div class="heatwrap"><div id="heatmap"></div><div class="legend"><span>Less</span><span class="cell"></span><span class="cell l1"></span><span class="cell l2"></span><span class="cell l3"></span><span class="cell l4"></span><span>More</span></div></div><div class="sub">Busiest day: <b>{{.Report.BusiestDay.Date}}</b> with <b>{{.Report.BusiestDay.Messages}}</b> messages.</div></section>
<section class="slide"><div class="kicker">Response Timing</div><div class="grid"><div class="card blue"><div class="num">{{printf "%.1f" .Report.AvgResponseMin}}</div><div class="label">avg response time</div></div><div class="card green"><div class="num">{{printf "%.1f" .Report.AvgTheirReplyMin}}</div><div class="label">avg time for them to reply</div></div><div class="card pink"><div class="num">{{.Report.StarterYouPct}}%</div><div class="label">started by you after 8h silence</div></div></div><div class="sub">This replaces the old day/hour conversation heatmap and keeps the focus on reply speed.</div></section>
<section class="slide"><div class="kicker">Streak + Starters</div><div class="grid"><div class="card blue"><div class="num">{{.Report.LongestStreak.Days}}</div><div class="label">day streak with {{.Report.LongestStreak.Name}}</div></div><div class="card pink"><div class="num">{{.Report.StarterYouPct}}%</div><div class="label">started by you after 8h silence</div></div><div class="card green"><div class="num">{{printf "%.1f" .Report.AvgResponseMin}}</div><div class="label">avg minutes for you to reply</div></div></div></section>
<section class="slide"><div class="kicker">Double Text Trigger</div><div class="grid"><div class="card pink"><div class="num">{{.Report.DoubleTexts}}</div><div class="label">consecutive sends without a reply</div></div><div class="card blue"><div class="num">{{.Report.BurstShort}}</div><div class="label">short-message bursts</div></div><div class="card green"><div class="num">{{.Report.BurstLong}}</div><div class="label">long paragraph sends</div></div></div></section>
<section class="slide"><div class="kicker">Reply Stats</div><div class="grid"><div class="card blue"><div class="num">{{printf "%.1f" .Report.AvgResponseMin}}</div><div class="label">avg response time</div></div><div class="card pink"><div class="num">{{.Report.ReactionCount}}</div><div class="label">tapbacks found</div></div><div class="card green"><div class="num">{{.Report.DurationLabel}}</div><div class="label">report duration</div></div></div><div class="sub">Top emoji and tapback appear in the summary card.</div></section>
<section class="slide"><div class="kicker">Relationship Evolution</div><div class="chart-card"><div class="chart-title">Monthly message volume with top contacts</div><div class="chart-note">Total monthly volume across your top contacts in this report.</div><div class="bars" id="evolution"></div></div></section>
<section class="slide"><div class="kicker">Receipt Breakdown</div><div class="hero">{{.Report.ReactionCount}}</div><div class="sub">Tapbacks/reactions found. They are not counted as normal messages.</div></section>
<section class="slide"><div class="kicker">Share Card</div><div class="summary-card" id="summaryCard"><div class="summary-top"><div><div class="summary-title">iMessage Wrapped</div><div class="label">{{.Report.Timeframe.Label}}</div><div class="label">{{.Report.DurationLabel}}</div></div></div><div class="hero" style="font-size:72px">{{.Report.TotalMessages}}</div><div class="label">messages analyzed</div><div class="summary-grid"><div class="summary-stat"><div class="num" style="font-size:36px">{{.Report.SentMessages}}</div><div class="label">sent</div></div><div class="summary-stat"><div class="num" style="font-size:36px">{{.Report.ReceivedMessages}}</div><div class="label">received</div></div><div class="summary-stat"><div class="num" style="font-size:36px">{{printf "%.1f" .Report.AvgResponseMin}}</div><div class="label">avg response time</div></div><div class="summary-stat"><div class="num" style="font-size:36px">{{.Report.StarterYouPct}}%</div><div class="label">starter ratio</div></div></div><div class="summary-footer" id="summaryTop"></div></div><button class="save-btn" onclick="saveSummary(event)">Save summary image</button></section>
  </main><div class="nav" id="nav"></div><div class="tooltip" id="tooltip"></div><script>const data={{.Data}};let cur=0;const slides=document.getElementById('slides');const total=slides.children.length;const nav=document.getElementById('nav');const tip=document.getElementById('tooltip');for(let i=0;i<total;i++){const d=document.createElement('div');d.className='dot'+(i?'':' active');d.title='Go to slide '+(i+1);d.addEventListener('click',()=>goTo(i));nav.appendChild(d)}function paintNav(){[...nav.children].forEach((d,i)=>d.classList.toggle('active',i===cur))}function goTo(n){cur=Math.max(0,Math.min(total-1,n));slides.style.transform='translateX(-'+(cur*100)+'vw)';paintNav()}function go(n){goTo(cur+n)}document.addEventListener('keydown',e=>{if(e.key==='ArrowRight'||e.key===' ')go(1);if(e.key==='ArrowLeft')go(-1)});function showTip(e,html){tip.innerHTML=html;tip.style.display='block';tip.style.left=e.clientX+14+'px';tip.style.top=e.clientY+14+'px'}function hideTip(){tip.style.display='none'}function medal(i){return String(i+1)}function medalClass(i){return i===0?' gold':i===1?' silver':i===2?' bronze':''}function rankRow(x,i,type,spotlight,entering){return '<div class="rank'+medalClass(i)+(spotlight?' leaderboard-spotlight':'')+(entering?' reveal':'')+'"><div class="rank-main"><span class="medal">'+medal(i)+'</span><div><div class="rank-name">'+x.name+'</div><div class="rank-meta">'+(x.sent||0).toLocaleString()+' sent · '+(x.received||0).toLocaleString()+' received</div></div></div><div style="display:flex;flex-direction:column;align-items:flex-end;line-height:1"><div class="label" style="font-size:12px;letter-spacing:.08em;text-transform:uppercase">'+type+'</div><div class="rank-count">'+x[type].toLocaleString()+'</div></div></div>'}function rank(el,items,type,animateTop){const root=document.getElementById(el);if(!items.length){root.innerHTML='<div class="card">No data</div>';return}if(animateTop){root.innerHTML='<div class="leaderboard-shell"><div class="leaderboard-spotlight-wrap"></div><div class="rank-list"></div></div>';const spotlightWrap=root.querySelector('.leaderboard-spotlight-wrap');const list=root.querySelector('.rank-list');spotlightWrap.innerHTML=rankRow(items[0],0,type,true,true);items.slice(1).forEach(function(x,i){setTimeout(function(){list.insertAdjacentHTML('beforeend',rankRow(x,i+1,type,false,true))},420+i*140)});return}root.innerHTML=items.map(function(x,i){return rankRow(x,i,type,false,false)}).join('')}rank('contacts',data.topContacts.slice(0,10),'messages',true);rank('groups',data.topGroups.slice(0,5),'messages');document.getElementById('summaryTop').textContent=(data.topContacts[0]?('Top contact: '+data.topContacts[0].name):'No top contact')+(data.emojis&&data.emojis[0]?' · Top emoji: '+data.emojis[0].emoji:'')+(data.tapbacks&&data.tapbacks[0]?' · Top tapback: '+data.tapbacks[0].emoji:'')+' · Duration: '+(data.durationLabel||'');
function localDate(y,m,d){return new Date(y,m-1,d)}function keyDate(dt){const y=dt.getFullYear(),m=String(dt.getMonth()+1).padStart(2,'0'),d=String(dt.getDate()).padStart(2,'0');return y+'-'+m+'-'+d}function dateLabel(key){const p=key.split('-').map(Number);return localDate(p[0],p[1],p[2]).toLocaleDateString(undefined,{month:'short',day:'numeric',year:'numeric'})}function heat(){const counts=Object.fromEntries(data.dailyCounts.map(d=>[d.date,d.count]));const vals=data.dailyCounts.map(d=>d.count);const max=Math.max(1,...vals);const years=[...new Set(data.dailyCounts.map(d=>Number(d.date.slice(0,4))))].sort((a,b)=>a-b);let html='';years.forEach(year=>{const yearTotal=data.dailyCounts.filter(d=>d.date.startsWith(String(year))).reduce((s,d)=>s+d.count,0);let start=localDate(year,1,1),end=localDate(year,12,31);start.setDate(start.getDate()-start.getDay());let monthMarks='';let last=-1;let weeks=[];for(let d=new Date(start);d<=end;d.setDate(d.getDate()+7)){let week=[];for(let i=0;i<7;i++){const x=new Date(d);x.setDate(d.getDate()+i);if(x.getFullYear()===year&&x.getMonth()!==last){monthMarks+='<span style="position:absolute;left:'+(weeks.length*17)+'px">'+x.toLocaleDateString(undefined,{month:'short'})+'</span>';last=x.getMonth()}const key=keyDate(x);const inYear=x.getFullYear()===year;const c=inYear?(counts[key]||0):0;const l=c===0?0:Math.min(4,Math.ceil(c/max*4));week.push('<div class="cell l'+l+'" data-tip="'+dateLabel(key)+'<br>'+c.toLocaleString()+' messages"></div>')}weeks.push('<div class="week">'+week.join('')+'</div>')}html+='<div class="year-block"><div class="year-label">'+year+'<br><span class="label" style="font-size:13px">'+yearTotal.toLocaleString()+' msgs</span></div><div><div class="months">'+monthMarks+'</div><div class="heat"><div class="days"><span></span><span>Mon</span><span></span><span>Wed</span><span></span><span>Fri</span><span></span></div>'+weeks.join('')+'</div></div></div>'});document.getElementById('heatmap').innerHTML=html;document.querySelectorAll('[data-tip]').forEach(el=>{el.addEventListener('mousemove',e=>showTip(e,el.dataset.tip));el.addEventListener('mouseleave',hideTip)})}heat();

function evolution(){const contacts=data.relationshipLines,months=[...new Set(contacts.flatMap(c=>Object.keys(c.monthly)))].sort();const totals=months.map(m=>contacts.reduce((s,c)=>s+(c.monthly[m]||0),0));const max=Math.max(1,...totals);document.getElementById('evolution').innerHTML=months.map((m,i)=>'<div class="bar" data-tip="'+m+'<br>'+totals[i].toLocaleString()+' messages" style="height:'+Math.max(6,totals[i]/max*260)+'px"><span class="bar-label">'+m.slice(2)+'</span></div>').join('');document.querySelectorAll('.bar').forEach(el=>{el.addEventListener('mousemove',e=>showTip(e,el.dataset.tip));el.addEventListener('mouseleave',hideTip)})}evolution();
async function saveSummary(e){e.stopPropagation();const card=document.getElementById('summaryCard');const btn=e.currentTarget;btn.textContent='Saving...';try{const canvas=await html2canvas(card,{backgroundColor:'#ffffff',scale:2,logging:false});const a=document.createElement('a');a.download='imsgwrap-summary.png';a.href=canvas.toDataURL('image/png');a.click();btn.textContent='Saved'}catch(err){btn.textContent='Save failed'}setTimeout(()=>btn.textContent='Save summary image',1800)}
</script></body></html>`
