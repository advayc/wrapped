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
	GeneratedAt        string         `json:"generatedAt"`
	Timeframe          timeframe      `json:"timeframe"`
	TotalMessages      int            `json:"totalMessages"`
	SentMessages       int            `json:"sentMessages"`
	ReceivedMessages   int            `json:"receivedMessages"`
	AttachmentMessages int            `json:"attachmentMessages"`
	ReactionCount      int            `json:"reactionCount"`
	UniqueContacts     int            `json:"uniqueContacts"`
	TopContacts        []contactStat  `json:"topContacts"`
	TopGroups          []groupStat    `json:"topGroups"`
	DailyCounts        []dayCount     `json:"dailyCounts"`
	ConversationHeat   []hourDayCount `json:"conversationHeat"`
	GoldenHour         string         `json:"goldenHour"`
	GoldenDay          string         `json:"goldenDay"`
	BusiestDay         busiestDay     `json:"busiestDay"`
	LongestStreak      streakStat     `json:"longestStreak"`
	Words              []wordCount    `json:"words"`
	Emojis             []emojiCount   `json:"emojis"`
	EmojiSignature     string         `json:"emojiSignature"`
	StarterYouPct      int            `json:"starterYouPct"`
	AvgYourReplyMin    float64        `json:"avgYourReplyMin"`
	AvgTheirReplyMin   float64        `json:"avgTheirReplyMin"`
	DoubleTexts        int            `json:"doubleTexts"`
	BurstShort         int            `json:"burstShort"`
	BurstLong          int            `json:"burstLong"`
	LengthScatter      []contactStat  `json:"lengthScatter"`
	RelationshipLines  []contactStat  `json:"relationshipLines"`
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
		m.ContactKey, m.ContactName = contactFor(m.Handle, contacts)
		if m.ContactName == "" && m.IsGroup {
			m.ContactName = groupName(m, contacts, db)
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
	for _, p := range []string{"Loved \"", "Liked \"", "Disliked \"", "Laughed at \"", "Emphasized \"", "Questioned \""} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
func groupName(m message, contacts map[string]string, db *sql.DB) string {
	if m.ChatName != "" {
		return m.ChatName
	}
	return fmt.Sprintf("Group %d", m.ChatID)
}

func analyze(msgs []message, participants map[int64]int, tf timeframe) analysis {
	contactMap := map[string]*contactStat{}
	groupMap := map[int64]*groupStat{}
	daily := map[string]int{}
	heat := map[string]int{}
	words := map[string]int{}
	emojis := map[string]int{}
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
		heat[fmt.Sprintf("%d:%d", int(m.At.Weekday()), m.At.Hour())]++
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
	heatRows := make([]hourDayCount, 0, len(heat))
	var gh hourDayCount
	for k, c := range heat {
		parts := strings.Split(k, ":")
		d, _ := strconv.Atoi(parts[0])
		h, _ := strconv.Atoi(parts[1])
		row := hourDayCount{Day: d, Hour: h, Count: c}
		heatRows = append(heatRows, row)
		if c > gh.Count {
			gh = row
		}
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
	return analysis{GeneratedAt: time.Now().Format(time.RFC3339), Timeframe: tf, TotalMessages: total, SentMessages: sent, ReceivedMessages: recv, AttachmentMessages: attach, ReactionCount: reactions, UniqueContacts: len(contactMap), TopContacts: limitContacts(contacts, 20), TopGroups: limitGroups(groups, 5), DailyCounts: days, ConversationHeat: heatRows, GoldenHour: formatHour(gh.Hour), GoldenDay: weekdayName(gh.Day), BusiestDay: busy, LongestStreak: streak, Words: topWords(words, 60), Emojis: topEmojis(emojis, 20), EmojiSignature: firstEmoji(emojis), StarterYouPct: starterPct, AvgYourReplyMin: avg(yourReplySamples), AvgTheirReplyMin: avg(theirReplySamples), DoubleTexts: doubleTexts, BurstShort: burstShort, BurstLong: burstLong, LengthScatter: limitContacts(contacts, 12), RelationshipLines: limitContacts(contacts, 5)}
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
		if len([]rune(f)) >= 3 && !stop[f] {
			counts[f]++
		}
	}
}
func collectEmoji(text string, counts map[string]int) {
	for _, r := range text {
		if isEmoji(r) {
			counts[string(r)]++
		}
	}
}
func isEmoji(r rune) bool { return (r >= 0x1F300 && r <= 0x1FAFF) || (r >= 0x2600 && r <= 0x27BF) }
func stopwords() map[string]bool {
	words := strings.Fields("the and for you that this with have but not are was from just like what when your all can out get got she him her they them then than there here about would could should because its it's im i'm dont don't yeah yes lol lmao too very also into did does how why who been our their his has had were will gonna wanna one two okay ok")
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
	for i := range a.LengthScatter {
		a.LengthScatter[i].Name = fmt.Sprintf("Contact %d", i+1)
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

const htmlPage = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>iMessage Wrapped</title><link rel="preconnect" href="https://fonts.googleapis.com"><link rel="preconnect" href="https://fonts.gstatic.com" crossorigin><link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700;800&display=swap" rel="stylesheet"><style>
:root{--ink:#050505;--muted:#6b7280;--line:#e5e7eb;--soft:#f8fafc;--green0:#ebedf0;--green1:#9be9a8;--green2:#40c463;--green3:#30a14e;--green4:#216e39}*{box-sizing:border-box}body{margin:0;font-family:Inter,system-ui,sans-serif;color:var(--ink);background:white;overflow:hidden}.slides{height:100vh;display:flex;transition:transform .45s cubic-bezier(.22,1,.36,1)}.slide{min-width:100vw;height:100vh;padding:56px;display:flex;flex-direction:column;justify-content:center;gap:26px}.kicker{font-size:13px;font-weight:800;letter-spacing:.12em;text-transform:uppercase;color:#111}.hero{font-size:clamp(54px,9vw,128px);line-height:.88;letter-spacing:-.08em;font-weight:800}.sub{font-size:clamp(18px,2vw,28px);color:var(--muted);max-width:760px}.grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:14px}.card{border:1px solid var(--line);border-radius:24px;padding:22px;background:white;box-shadow:0 8px 35px rgba(15,23,42,.05)}.num{font-size:clamp(42px,6vw,88px);font-weight:800;letter-spacing:-.06em}.label{color:var(--muted);font-weight:600}.rank{display:flex;align-items:center;justify-content:space-between;padding:14px 0;border-bottom:1px solid var(--line);gap:20px}.rank:last-child{border-bottom:0}.rank b{font-size:20px}.pill{display:inline-flex;border:1px solid var(--line);border-radius:999px;padding:8px 12px;color:var(--muted);font-weight:700}.heatwrap{overflow:auto;padding-bottom:12px}.months{height:20px;position:relative;margin-left:36px;color:var(--muted);font-size:12px}.heat{display:flex;gap:4px}.days{width:32px;display:grid;grid-template-rows:repeat(7,13px);gap:4px;color:var(--muted);font-size:11px}.week{display:grid;grid-template-rows:repeat(7,13px);gap:4px}.cell{width:13px;height:13px;border-radius:3px;background:var(--green0)}.l1{background:var(--green1)}.l2{background:var(--green2)}.l3{background:var(--green3)}.l4{background:var(--green4)}.matrix{display:grid;grid-template-columns:60px repeat(24,1fr);gap:4px;align-items:center}.mcell{height:18px;border-radius:4px;background:var(--green0)}.bars{display:flex;align-items:end;height:260px;gap:8px}.bar{flex:1;background:#111;border-radius:10px 10px 0 0;min-height:4px}.cloud{display:flex;flex-wrap:wrap;gap:12px;align-items:center}.scatter{width:100%;height:360px;border:1px solid var(--line);border-radius:24px}.nav{position:fixed;bottom:24px;left:50%;transform:translateX(-50%);display:flex;gap:8px;align-items:center}.dot{width:8px;height:8px;border-radius:99px;background:#d1d5db}.dot.active{width:28px;background:#111}.arrow{position:fixed;top:50%;transform:translateY(-50%);border:0;background:white;font-size:42px;cursor:pointer}.prev{left:18px}.next{right:18px}@media(max-width:800px){.slide{padding:34px 22px}.grid{grid-template-columns:1fr}.hero{font-size:58px}.matrix{grid-template-columns:42px repeat(24,12px);overflow:auto}.mcell{width:12px}.arrow{display:none}}
</style></head><body><main class="slides" id="slides">
<section class="slide"><div class="kicker">iMessage Wrapped</div><div class="hero">{{.Report.Timeframe.Label}}</div><div class="sub">A local-only Wrapped for your Messages data. Nothing uploaded. {{.Report.TotalMessages}} countable messages analyzed.</div><span class="pill">click or arrow keys to move</span></section>
<section class="slide"><div class="kicker">Total history</div><div class="grid"><div class="card"><div class="num">{{.Report.TotalMessages}}</div><div class="label">messages</div></div><div class="card"><div class="num">{{.Report.SentMessages}}</div><div class="label">sent by you</div></div><div class="card"><div class="num">{{.Report.ReceivedMessages}}</div><div class="label">received</div></div></div><div class="sub">Attachments-only messages count for activity. Tapbacks are tracked separately as {{.Report.ReactionCount}} reactions.</div></section>
<section class="slide"><div class="kicker">Top Contact Leaderboard</div><div id="contacts"></div></section>
<section class="slide"><div class="kicker">Group chats</div><div id="groups"></div></section>
<section class="slide"><div class="kicker">GitHub-style activity</div><div class="heatwrap"><div id="heatmap"></div></div><div class="sub">Your busiest day was <b>{{.Report.BusiestDay.Date}}</b> with <b>{{.Report.BusiestDay.Messages}}</b> messages.</div></section>
<section class="slide"><div class="kicker">Conversation Heatmap</div><div class="sub">Golden hour: <b>{{.Report.GoldenHour}}</b> on <b>{{.Report.GoldenDay}}</b>.</div><div id="matrix"></div></section>
<section class="slide"><div class="kicker">Words + Emoji Signature</div><div class="cloud" id="cloud"></div><div class="sub">Your most-used emoji: <b style="font-size:42px">{{.Report.EmojiSignature}}</b></div></section>
<section class="slide"><div class="kicker">Streak + Starters</div><div class="grid"><div class="card"><div class="num">{{.Report.LongestStreak.Days}}</div><div class="label">day streak with {{.Report.LongestStreak.Name}}</div></div><div class="card"><div class="num">{{.Report.StarterYouPct}}%</div><div class="label">started by you after 8h silence</div></div><div class="card"><div class="num">{{printf "%.1f" .Report.AvgYourReplyMin}}</div><div class="label">avg minutes for you to reply</div></div></div></section>
<section class="slide"><div class="kicker">Double Text Trigger</div><div class="grid"><div class="card"><div class="num">{{.Report.DoubleTexts}}</div><div class="label">consecutive sends without a reply</div></div><div class="card"><div class="num">{{.Report.BurstShort}}</div><div class="label">short-message bursts</div></div><div class="card"><div class="num">{{.Report.BurstLong}}</div><div class="label">long paragraph sends</div></div></div></section>
<section class="slide"><div class="kicker">Length Comparison</div><svg class="scatter" id="scatter"></svg></section>
<section class="slide"><div class="kicker">Relationship Evolution</div><div class="bars" id="evolution"></div></section>
<section class="slide"><div class="kicker">Receipt Breakdown</div><div class="hero">{{.Report.ReactionCount}}</div><div class="sub">Tapbacks/reactions found. They are not counted as normal messages.</div></section>
</main><button class="arrow prev" onclick="go(-1)">‹</button><button class="arrow next" onclick="go(1)">›</button><div class="nav" id="nav"></div><script>const data={{.Data}};let cur=0;const slides=document.getElementById('slides');const total=slides.children.length;const nav=document.getElementById('nav');for(let i=0;i<total;i++){const d=document.createElement('div');d.className='dot'+(i?'':' active');nav.appendChild(d)}function paintNav(){[...nav.children].forEach((d,i)=>d.classList.toggle('active',i===cur))}function go(n){cur=Math.max(0,Math.min(total-1,cur+n));slides.style.transform='translateX(-'+(cur*100)+'vw)';paintNav()}document.addEventListener('keydown',e=>{if(e.key==='ArrowRight'||e.key===' ')go(1);if(e.key==='ArrowLeft')go(-1)});document.addEventListener('click',e=>{if(e.target.closest('button'))return;go(1)});
function rank(el,items,type='messages'){document.getElementById(el).innerHTML=items.length?items.map((x,i)=>'<div class=rank><b>'+(i+1)+'. '+x.name+'</b><span>'+x[type].toLocaleString()+'</span></div>').join(''):'<div class=card>No data</div>'}rank('contacts',data.topContacts.slice(0,10));rank('groups',data.topGroups.slice(0,5));
function heat(){const counts=Object.fromEntries(data.dailyCounts.map(d=>[d.date,d.count]));const vals=data.dailyCounts.map(d=>d.count);const max=Math.max(1,...vals);let start=new Date(data.timeframe.start),end=new Date(data.timeframe.end);start=new Date(start.getFullYear(),start.getMonth(),start.getDate()-start.getDay());let html='<div class=heat><div class=days><span></span><span>Mon</span><span></span><span>Wed</span><span></span><span>Fri</span><span></span></div>';for(let d=new Date(start);d<=end;d.setDate(d.getDate()+7)){html+='<div class=week>';for(let i=0;i<7;i++){const x=new Date(d);x.setDate(d.getDate()+i);const key=x.toISOString().slice(0,10);const c=counts[key]||0;const l=c===0?0:Math.min(4,Math.ceil(c/max*4));html+='<div title="'+key+': '+c+'" class="cell l'+l+'"></div>'}html+='</div>'}document.getElementById('heatmap').innerHTML=html+'</div>'}heat();
function matrix(){const m={};data.conversationHeat.forEach(x=>m[x.day+':'+x.hour]=x.count);const max=Math.max(1,...data.conversationHeat.map(x=>x.count));let html='<div class=matrix><b></b>';for(let h=0;h<24;h++)html+='<small>'+h+'</small>';['Sun','Mon','Tue','Wed','Thu','Fri','Sat'].forEach((day,d)=>{html+='<b>'+day+'</b>';for(let h=0;h<24;h++){const c=m[d+':'+h]||0,l=c===0?0:Math.min(4,Math.ceil(c/max*4));html+='<div title="'+day+' '+h+':00 '+c+'" class="mcell l'+l+'"></div>'}});document.getElementById('matrix').innerHTML=html+'</div>'}matrix();
const maxWord=Math.max(1,...data.words.map(w=>w.count));document.getElementById('cloud').innerHTML=data.words.slice(0,40).map(w=>'<span style="font-size:'+(14+w.count/maxWord*42)+'px;font-weight:'+(500+w.count/maxWord*300)+'">'+w.text+'</span>').join('');
function scatter(){const svg=document.getElementById('scatter'),w=svg.clientWidth||800,h=360;svg.setAttribute('viewBox','0 0 '+w+' '+h);const xs=data.lengthScatter.map(x=>x.avgSentChars),ys=data.lengthScatter.map(x=>x.avgRecvChars);const mx=Math.max(1,...xs),my=Math.max(1,...ys);let html='<line x1="50" y1="310" x2="760" y2="310" stroke="#ddd"/><line x1="50" y1="20" x2="50" y2="310" stroke="#ddd"/>';data.lengthScatter.forEach(x=>{const cx=50+x.avgSentChars/mx*(w-90),cy=310-x.avgRecvChars/my*270;html+='<circle cx="'+cx+'" cy="'+cy+'" r="8" fill="#111"><title>'+x.name+': you '+x.avgSentChars.toFixed(1)+', replies '+x.avgRecvChars.toFixed(1)+'</title></circle>'});svg.innerHTML=html}scatter();
function evolution(){const contacts=data.relationshipLines,months=[...new Set(contacts.flatMap(c=>Object.keys(c.monthly)))].sort();const max=Math.max(1,...contacts.flatMap(c=>Object.values(c.monthly)));document.getElementById('evolution').innerHTML=months.map(m=>'<div class=bar title="'+m+'" style="height:'+Math.max(4,contacts.reduce((s,c)=>s+(c.monthly[m]||0),0)/max*240)+'px"></div>').join('')}evolution();
</script></body></html>`
