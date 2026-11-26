package text

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"math"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// Meeting transcripts (forge 18e): a transcript file from a meeting tool,
// or pasted text, read into numbered lines with their speakers. Transcript
// files come from strangers, so a server never calls ParseTranscript in
// its own process: ChildTranscripts runs it in a limited child process
// (TranscriptChild.go). The parser only reads: it runs no script, follows
// no link and opens no embedded file; of a .docx it reads only
// word/document.xml.

// Limits on reading a transcript.
const (
	// MaxTranscriptBytes is the largest transcript file read.
	MaxTranscriptBytes = 10 << 20
	// MaxTranscriptLines is the most lines given; more are dropped and the
	// transcript is Truncated.
	MaxTranscriptLines = 5000
	// MaxTranscriptLineChars is the longest line given; a longer one is
	// clipped (and the transcript Truncated).
	MaxTranscriptLineChars = 4000
	// MaxTranscriptChars is the most text all lines hold together; the
	// rest is dropped (and the transcript Truncated).
	MaxTranscriptChars = 2_000_000
	// maxSpeakerChars is the longest speaker name kept.
	maxSpeakerChars = 100
	// maxMergedChars is how long a line merged from consecutive entries
	// of one speaker (VTT, SRT, JSON) may grow.
	maxMergedChars = 1000
	// maxDocxXML is the largest word/document.xml read, uncompressed.
	maxDocxXML = 20 << 20
	// maxDocxEntries is the most entries a .docx zip may hold.
	maxDocxEntries = 200
	// maxTurnBytes bounds the raw text gathered for one turn before it is
	// cleaned and clipped.
	maxTurnBytes = 64 << 10
)

// Transcript formats ParseTranscript reads.
const (
	TranscriptText = "text"
	TranscriptVTT  = "vtt"
	TranscriptSRT  = "srt"
	TranscriptDocx = "docx"
	TranscriptJSON = "json"
)

// TranscriptLine is one turn: who spoke, when (hh:mm:ss, when known) and
// what they said. N counts from 1.
type TranscriptLine struct {
	N       int    `json:"n"`
	Speaker string `json:"speaker"`
	At      string `json:"at,omitempty"`
	Text    string `json:"text"`
}

// Transcript is a parsed transcript: its format, its lines, the distinct
// speakers in order of first appearance, and whether something was cut
// to the limits.
type Transcript struct {
	Format    string           `json:"format"`
	Lines     []TranscriptLine `json:"lines"`
	Speakers  []string         `json:"speakers"`
	Truncated bool             `json:"truncated"`
}

// Errors ParseTranscript gives, in plain language.
var (
	ErrTranscriptFormat   = errors.New("the file is not a transcript format this app reads: use .txt, .vtt, .srt, .docx or .json")
	ErrTranscriptEmpty    = errors.New("the transcript holds no lines")
	ErrTranscriptTooLarge = fmt.Errorf("the transcript is larger than %d MB", MaxTranscriptBytes>>20)
	ErrDocxMalformed      = errors.New("the .docx file is malformed")
	ErrDocxTooLarge       = fmt.Errorf("the .docx file's text is larger than %d MB", maxDocxXML>>20)
	ErrDocxTooManyParts   = fmt.Errorf("the .docx file holds more than %d parts", maxDocxEntries)
	ErrTranscriptJSON     = errors.New("the JSON file is malformed")
	ErrTranscriptNoList   = errors.New("the JSON file holds no list of transcript entries this app reads")
)

// ParseTranscript reads a transcript by its name and content type, else by
// its content (pasted text with no name). It parses in the CALLING
// process: a server only calls it through ChildTranscripts. It never
// panics: a panic is reported as an error.
func ParseTranscript(name, contentType string, raw []byte) (t Transcript, err error) {
	defer func() {
		if r := recover(); r != nil {
			t, err = Transcript{}, errors.New("the transcript could not be read")
		}
	}()
	if len(raw) > MaxTranscriptBytes {
		return Transcript{}, ErrTranscriptTooLarge
	}
	format, err := transcriptFormat(name, contentType, raw)
	if err != nil {
		return Transcript{}, err
	}
	b := &transcriptBuilder{t: Transcript{Format: format, Lines: []TranscriptLine{}, Speakers: []string{}}}
	switch format {
	case TranscriptVTT, TranscriptSRT:
		b.merge = true
		parseCues(decodeTranscript(raw), b)
	case TranscriptJSON:
		b.merge = true
		if err := parseTranscriptJSON(decodeTranscript(raw), b); err != nil {
			return Transcript{}, err
		}
	case TranscriptDocx:
		paras, err := docxParagraphs(raw)
		if err != nil {
			return Transcript{}, err
		}
		parsePlainTranscript(paras, b)
	default:
		parsePlainTranscript(decodeTranscript(raw), b)
	}
	if len(b.t.Lines) == 0 {
		return Transcript{}, ErrTranscriptEmpty
	}
	return b.t, nil
}

// transcriptFormat picks the format: by extension or content type, else
// by sniffing the content.
func transcriptFormat(name, contentType string, raw []byte) (string, error) {
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	ext := strings.ToLower(path.Ext(strings.TrimSpace(name)))
	switch {
	case ext == ".vtt" || ct == "text/vtt":
		return TranscriptVTT, nil
	case ext == ".srt" || ct == "application/x-subrip" || ct == "text/srt":
		return TranscriptSRT, nil
	case ext == ".docx" || ct == "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return TranscriptDocx, nil
	case ext == ".json" || ct == "application/json":
		return TranscriptJSON, nil
	case ext == ".doc" || ext == ".pdf" || ct == "application/pdf" || ct == "application/msword":
		return "", ErrTranscriptFormat
	}
	// A .txt, pasted text or an unknown name: by the content
	if bytes.HasPrefix(raw, []byte("PK\x03\x04")) {
		if ext == "" || ext == ".txt" {
			return TranscriptDocx, nil
		}
		return "", ErrTranscriptFormat
	}
	s := decodeTranscript(raw[:min(len(raw), 8192)])
	named := ext == ".txt" || ext == ".text" || ext == ".md" || strings.HasPrefix(ct, "text/")
	if !named && binaryText(s) {
		return "", ErrTranscriptFormat
	}
	trimmed := strings.TrimLeft(s, " \t\r\n\uFEFF")
	switch {
	case strings.HasPrefix(trimmed, "WEBVTT"):
		return TranscriptVTT, nil
	case srtStart.MatchString(trimmed):
		return TranscriptSRT, nil
	case (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) && json.Valid(bytes.TrimSpace([]byte(decodeTranscript(raw)))):
		return TranscriptJSON, nil
	}
	return TranscriptText, nil
}

var srtStart = regexp.MustCompile(`^\d+[ \t]*\r?\n[ \t]*\d{1,2}:\d{2}:\d{2}[,.]\d{1,3}[ \t]*-->`)

// binaryText tells text from a binary file: control characters other than
// white space are rare in text.
func binaryText(s string) bool {
	n, bad := 0, 0
	for _, r := range s {
		n++
		if r == 0 || unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' && r != '\f' {
			bad++
		}
	}
	return n > 0 && bad*20 > n
}

// decodeTranscript makes text valid UTF-8: UTF-8 (a BOM dropped), UTF-16
// by its BOM, else Windows-1252.
func decodeTranscript(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}):
		b = b[3:]
	case len(b) >= 2 && (b[0] == 0xFF && b[1] == 0xFE || b[0] == 0xFE && b[1] == 0xFF):
		le := b[0] == 0xFF
		u := make([]uint16, 0, len(b)/2)
		for i := 2; i+1 < len(b); i += 2 {
			if le {
				u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
			} else {
				u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
			}
		}
		return string(utf16.Decode(u))
	}
	if utf8.Valid(b) {
		return string(b)
	}
	var out strings.Builder
	out.Grow(len(b))
	for _, c := range b {
		if c >= 0x80 && c < 0xA0 {
			out.WriteRune(cp1252[c-0x80])
		} else {
			out.WriteRune(rune(c))
		}
	}
	return out.String()
}

// cp1252 is Windows-1252's 0x80-0x9F; the rest is Latin-1.
var cp1252 = [32]rune{
	'€', '�', '‚', 'ƒ', '„', '…', '†', '‡', 'ˆ', '‰', 'Š', '‹', 'Œ', '�', 'Ž', '�',
	'�', '‘', '’', '“', '”', '•', '–', '—', '˜', '™', 'š', '›', 'œ', '�', 'ž', 'Ÿ',
}

// cleanTranscript is one line's text: valid UTF-8, no control or
// zero-width characters, white space collapsed, trimmed.
func cleanTranscript(s string) string {
	s = strings.ToValidUTF8(s, "�")
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		switch {
		case r == '\uFEFF' || r == '\u200B' || r == '\u200E' || r == '\u200F':
			continue
		case unicode.IsSpace(r):
			space = b.Len() > 0
			continue
		case unicode.IsControl(r):
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
	}
	return b.String()
}

// clipRunes cuts s to at most n characters.
func clipRunes(s string, n int) (string, bool) {
	if len(s) <= n || utf8.RuneCountInString(s) <= n {
		return s, false
	}
	i := 0
	for j := range s {
		if i == n {
			return s[:j], true
		}
		i++
	}
	return s, false
}

// transcriptBuilder gathers lines up to the limits, merging consecutive
// entries of one speaker when merge is set.
type transcriptBuilder struct {
	t     Transcript
	merge bool
	chars int
	full  bool
	seen  map[string]bool
}

// add adds a turn; empty text is dropped.
func (b *transcriptBuilder) add(speaker, at, text string) {
	if b.full {
		if cleanTranscript(text) != "" {
			b.t.Truncated = true
		}
		return
	}
	speaker, _ = clipRunes(cleanTranscript(speaker), maxSpeakerChars)
	text = cleanTranscript(text)
	if text == "" {
		return
	}
	text, cut := clipRunes(text, MaxTranscriptLineChars)
	if cut {
		b.t.Truncated = true
	}
	n := utf8.RuneCountInString(text)
	if b.merge && len(b.t.Lines) > 0 {
		last := &b.t.Lines[len(b.t.Lines)-1]
		if last.Speaker == speaker && utf8.RuneCountInString(last.Text)+1+n <= maxMergedChars && b.chars+1+n <= MaxTranscriptChars {
			last.Text += " " + text
			b.chars += 1 + n
			return
		}
	}
	if len(b.t.Lines) >= MaxTranscriptLines {
		b.full, b.t.Truncated = true, true
		return
	}
	if room := MaxTranscriptChars - b.chars; n > room {
		text, _ = clipRunes(text, room)
		n = room
		b.full, b.t.Truncated = true, true
		if text == "" {
			return
		}
	}
	b.chars += n
	b.t.Lines = append(b.t.Lines, TranscriptLine{N: len(b.t.Lines) + 1, Speaker: speaker, At: at, Text: text})
	if speaker != "" {
		if b.seen == nil {
			b.seen = map[string]bool{}
		}
		if !b.seen[speaker] {
			b.seen[speaker] = true
			b.t.Speakers = append(b.t.Speakers, speaker)
		}
	}
}

// Timestamps.

// tsPattern is a timestamp: [h:]mm:ss or m:ss, with an optional fraction.
const tsPattern = `(\d{1,3}:\d{2}(?::\d{2})?(?:[.,]\d{1,3})?)`

var (
	tsOnly   = regexp.MustCompile(`^[\[(]?` + tsPattern + `[\])]?$`)
	tsPrefix = regexp.MustCompile(`^[\[(]?` + tsPattern + `[\])]?\s*(?:[-–—|]\s*)?(\S.*)$`)
	// header is "Speaker Name  0:03" or "Speaker Name (00:01:02)"
	header = regexp.MustCompile(`^(.+?)\s+[\[(]?` + tsPattern + `[\])]?$`)
	// labelTS is a label ending with its time: "Anna (00:01:02)"
	labelTS = regexp.MustCompile(`^(.+?)\s*[\[(]` + tsPattern + `[\])]$`)
)

// clock turns a timestamp into hh:mm:ss; "" when it is none.
func clock(ts string) string {
	ts = strings.TrimSpace(ts)
	if i := strings.IndexAny(ts, ".,"); i >= 0 {
		ts = ts[:i]
	}
	parts := strings.Split(ts, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return ""
	}
	total := 0
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 100000 {
			return ""
		}
		total = total*60 + n
	}
	return seconds(float64(total))
}

// seconds is a time in seconds as hh:mm:ss.
func seconds(s float64) string {
	if math.IsNaN(s) || math.IsInf(s, 0) || s < 0 || s > 1e7 {
		return ""
	}
	n := int(s)
	return fmt.Sprintf("%02d:%02d:%02d", n/3600, n/60%60, n%60)
}

// Speaker labels.

// stopLabels are words that end with a colon in prose and are never a
// speaker: "Note: the deposit is 20%".
var stopLabels = map[string]bool{
	"note": true, "notes": true, "nb": true, "n.b": true, "ps": true, "p.s": true, "re": true, "fyi": true, "todo": true, "to do": true,
	"action": true, "actions": true, "action item": true, "action items": true, "agenda": true, "summary": true, "decision": true,
	"decisions": true, "update": true, "important": true, "warning": true, "example": true, "e.g": true, "i.e": true, "tip": true,
	"next steps": true, "attendees": true, "participants": true, "date": true, "time": true, "subject": true, "location": true,
	"title": true, "meeting": true, "duration": true, "transcript": true, "recording": true, "url": true, "link": true, "http": true,
	"https": true, "edit": true, "edited": true, "source": true, "see": true, "caution": true, "remember": true, "reminder": true,
	"answer": true, "question": true, "result": true, "results": true, "total": true, "price": true, "cost": true, "deadline": true,
}

// labelStrength is 0 when s cannot name a speaker, 1 when it may (weak:
// lowercase, or four or five words) and 2 when it looks like a name.
func labelStrength(s string) int {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	if n == 0 || n > 40 || strings.Contains(s, "://") || strings.HasPrefix(strings.ToLower(s), "www.") {
		return 0
	}
	words := strings.Fields(s)
	if len(words) > 5 || stopLabels[strings.ToLower(strings.TrimRight(strings.Join(words, " "), "."))] {
		return 0
	}
	first, _ := utf8.DecodeRuneInString(s)
	if !unicode.IsLetter(first) {
		return 0
	}
	for _, r := range s {
		if !(unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsDigit(r) || r == ' ' || strings.ContainsRune(".-'’()&_", r)) {
			return 0
		}
	}
	if last := words[len(words)-1]; strings.HasSuffix(last, ".") && utf8.RuneCountInString(last) > 4 {
		return 0 // a word ending a sentence, not an abbreviation such as Dr.
	}
	if unicode.IsLower(first) || len(words) > 3 {
		return 1
	}
	return 2
}

// colonLabel splits "Speaker: text" (the colon followed by white space or
// the end); ok is false when the line has no such label.
func colonLabel(line string) (label, at, rest string, ok bool) {
	i := -1
	for j, r := range line {
		if r == ':' || r == '：' {
			next := line[j+utf8.RuneLen(r):]
			if next == "" || next[0] == ' ' || next[0] == '\t' {
				i = j
				rest = strings.TrimSpace(next)
				break
			}
		}
	}
	if i <= 0 {
		return "", "", "", false
	}
	label = strings.TrimSpace(line[:i])
	if m := labelTS.FindStringSubmatch(label); m != nil {
		label, at = strings.TrimSpace(m[1]), clock(m[2])
	}
	return label, at, rest, label != ""
}

// Plain text.

// textTurn is a turn of plain text being gathered.
type textTurn struct {
	speaker, at string
	parts       []string
	size        int
	open        bool
}

// parsePlainTranscript reads plain text turns: "Speaker: text",
// "[00:01:02] Speaker: text", "00:01:02 Speaker: text", and a header
// "Speaker Name  0:03" (or a speaker line followed by a time line) before
// the turn's lines. A line with no speaker continues the turn before it;
// before any speaker, paragraphs are lines with no speaker.
func parsePlainTranscript(s string, b *transcriptBuilder) {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(strings.TrimPrefix(lines[i], "\uFEFF"))
	}
	// How often each name labels a line, so a label seen once in the
	// middle of a turn is read as text unless it looks like a name
	count := map[string]int{}
	for i, l := range lines {
		if name, _, ok := headerAt(lines, i); ok {
			count[name]++
		} else if label, _, _, ok := colonLabel(l); ok && labelStrength(label) > 0 {
			count[label]++
		} else if m := tsPrefix.FindStringSubmatch(l); m != nil {
			if label, _, _, ok := colonLabel(m[2]); ok && labelStrength(label) > 0 {
				count[label]++
			}
		}
	}
	var cur textTurn
	turns := 0
	pendingAt := ""
	prevBlank, prevStart := true, false
	flush := func() {
		if cur.open {
			b.add(cur.speaker, cur.at, strings.Join(cur.parts, " "))
		}
		cur = textTurn{}
	}
	start := func(speaker, at, text string) {
		flush()
		if at == "" {
			at = pendingAt
		}
		pendingAt = ""
		cur = textTurn{speaker: speaker, at: at, open: true}
		if speaker != "" {
			turns++
		}
		if text != "" {
			cur.parts, cur.size = []string{text}, len(text)
		}
	}
	accept := func(label string, strong bool) bool {
		switch labelStrength(label) {
		case 2:
			return count[label] >= 2 || turns == 0 || prevBlank || prevStart || strong
		case 1:
			return count[label] >= 2
		}
		return false
	}
	for i := 0; i < len(lines) && !b.full; i++ {
		l := lines[i]
		if l == "" {
			if cur.open && cur.speaker == "" {
				flush()
			}
			prevBlank, prevStart = true, false
			continue
		}
		wasBlank := prevBlank
		prevBlank = false
		// A header: "Anna Schmidt  0:03", or "Anna Schmidt" over "0:03"
		if name, wide, ok := headerAt(lines, i); ok && labelStrength(name) == 2 && (wide || count[name] >= 2 || turns == 0 || wasBlank) {
			at := ""
			if m := header.FindStringSubmatch(l); m != nil && strings.TrimSpace(m[1]) == name {
				at = clock(m[2])
			} else {
				i++
				at = clock(tsOnly.FindStringSubmatch(lines[i])[1])
			}
			start(name, at, "")
			prevStart = true
			continue
		}
		if m := tsOnly.FindStringSubmatch(l); m != nil {
			pendingAt = clock(m[1])
			continue
		}
		at, body := "", l
		if m := tsPrefix.FindStringSubmatch(l); m != nil {
			at, body = clock(m[1]), m[2]
		}
		if label, lat, rest, ok := colonLabel(body); ok && accept(label, at != "") {
			if lat != "" {
				at = lat
			}
			start(label, at, rest)
			prevStart = true
			continue
		}
		prevStart = false
		if cur.open && (cur.speaker != "" || !wasBlank) {
			if cur.size < maxTurnBytes {
				cur.parts = append(cur.parts, body)
				cur.size += len(body) + 1
			} else {
				b.t.Truncated = true
			}
			continue
		}
		start("", at, body)
	}
	flush()
}

// headerAt reports whether line i is a speaker header: "Name  0:03",
// "Name (00:01:02)", or a name alone with a time alone on the next line.
// wide is set when the time is set apart from the name by more than one
// space, a tab or brackets, as meeting tools write it and prose does not.
func headerAt(lines []string, i int) (name string, wide, ok bool) {
	l := lines[i]
	if l == "" || strings.ContainsAny(l, ":：") && !header.MatchString(l) {
		return "", false, false
	}
	if m := header.FindStringSubmatch(l); m != nil {
		name = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(m[1]), "-–—|"))
		if labelStrength(name) == 0 {
			return "", false, false
		}
		gap := strings.TrimPrefix(l, m[1])
		gap = gap[:len(gap)-len(strings.TrimLeft(gap, " \t"))]
		wide = gap != " " || strings.HasSuffix(l, ")") || strings.HasSuffix(l, "]")
		return name, wide, true
	}
	if i+1 < len(lines) && tsOnly.MatchString(lines[i+1]) && !strings.ContainsAny(l, ":：") && labelStrength(l) > 0 {
		return l, false, true
	}
	return "", false, false
}

// Cues: WebVTT and SRT.

var (
	cueTime  = regexp.MustCompile(`^\s*((?:\d+:)?\d{1,2}:\d{2}(?:[.,]\d{1,3})?)\s*-->`)
	voiceTag = regexp.MustCompile(`^<v(?:\.[^\s>]*)?(?:\s+([^>]*))?>`)
	anyTag   = regexp.MustCompile(`<[^>]*>|\{\\[^}]*\}`)
)

// parseCues reads WebVTT or SRT cues: blocks apart by blank lines, the
// timing line's start as the time, the speaker from a <v Speaker> voice
// tag (Teams) or a "Speaker: " prefix (Zoom, SRT). Header, NOTE, STYLE
// and REGION blocks have no timing line and are skipped.
func parseCues(s string, b *transcriptBuilder) {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(s, "\n")
	for i := 0; i < len(lines) && !b.full; i++ {
		m := cueTime.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		at := clock(m[1])
		var payload []string
		for i++; i < len(lines) && strings.TrimSpace(lines[i]) != ""; i++ {
			if cueTime.MatchString(lines[i]) {
				i--
				break
			}
			payload = append(payload, strings.TrimSpace(lines[i]))
		}
		for _, seg := range voiceSegments(strings.Join(payload, "\n")) {
			text := html.UnescapeString(anyTag.ReplaceAllString(seg.text, ""))
			speaker := seg.speaker
			if speaker == "" {
				if label, _, rest, ok := colonLabel(strings.TrimSpace(text)); ok && labelStrength(label) > 0 {
					speaker, text = label, rest
				}
			}
			b.add(html.UnescapeString(speaker), at, text)
		}
	}
}

// voiceSegment is a cue's text in one voice.
type voiceSegment struct{ speaker, text string }

// voiceSegments splits a cue's payload at its <v Speaker> tags.
func voiceSegments(p string) []voiceSegment {
	var out []voiceSegment
	cur := voiceSegment{}
	var text strings.Builder
	for len(p) > 0 {
		if p[0] == '<' {
			if m := voiceTag.FindStringSubmatch(p); m != nil {
				cur.text = text.String()
				out = append(out, cur)
				text.Reset()
				cur = voiceSegment{speaker: strings.TrimSpace(m[1])}
				p = p[len(m[0]):]
				continue
			}
			if strings.HasPrefix(p, "</v>") {
				cur.text = text.String()
				out = append(out, cur)
				text.Reset()
				cur = voiceSegment{}
				p = p[4:]
				continue
			}
		}
		if p[0] == '\n' {
			text.WriteByte(' ')
		} else {
			text.WriteByte(p[0])
		}
		p = p[1:]
	}
	cur.text = text.String()
	return append(out, cur)
}

// JSON from meeting tools.

var (
	jsonListKeys    = []string{"utterances", "sentences", "segments", "transcript", "entries", "results", "lines", "messages", "monologues"}
	jsonSpeakerKeys = []string{"speaker", "speaker_name", "speakerName", "name", "participant", "user", "author"}
	jsonTextKeys    = []string{"text", "content", "transcript", "sentence", "raw_text"}
	jsonStartKeys   = []string{"start", "start_time", "startTime", "timestamp", "offset"}
	jsonStartMsKeys = []string{"start_ms", "startMs", "start_time_ms", "startTimeMs", "offset_ms", "offsetMs", "timestamp_ms"}
)

// parseTranscriptJSON reads a list of entries: the top-level array, or a
// list under one of jsonListKeys (also one level down).
func parseTranscriptJSON(s string, b *transcriptBuilder) error {
	d := json.NewDecoder(strings.NewReader(s))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return ErrTranscriptJSON
	}
	list, ok := v.([]any)
	if !ok {
		obj, isObj := v.(map[string]any)
		if !isObj {
			return ErrTranscriptNoList
		}
		list, ok = findJSONList(obj)
		if !ok {
			for _, k := range []string{"transcript", "text"} {
				if t, isText := obj[k].(string); isText && strings.TrimSpace(t) != "" {
					b.merge = false
					parsePlainTranscript(t, b)
					return nil
				}
			}
			return ErrTranscriptNoList
		}
	}
	// Times are milliseconds when a key says so, or when every time is a
	// whole number and some are larger than ten hours in seconds
	ms, whole, large := false, true, false
	for _, item := range list {
		m, _ := item.(map[string]any)
		if _, k := firstKey(m, jsonStartMsKeys); k != "" {
			ms = true
		}
		if n, ok := firstValue(m, jsonStartKeys).(json.Number); ok {
			if strings.ContainsAny(n.String(), ".eE") {
				whole = false
			} else if f, err := n.Float64(); err == nil && f > 36000 {
				large = true
			}
		}
	}
	ms = ms || whole && large
	for _, item := range list {
		if b.full {
			if item != nil {
				b.t.Truncated = true
			}
			break
		}
		switch it := item.(type) {
		case string:
			b.add("", "", it)
		case map[string]any:
			b.add(jsonSpeaker(firstValue(it, jsonSpeakerKeys)), jsonStart(it, ms), jsonText(it))
		}
	}
	return nil
}

// findJSONList finds the entries' list in an object, or one level down.
func findJSONList(obj map[string]any) ([]any, bool) {
	for _, k := range jsonListKeys {
		if l, ok := obj[k].([]any); ok {
			return l, true
		}
	}
	for _, k := range jsonListKeys {
		if inner, ok := obj[k].(map[string]any); ok {
			for _, k2 := range jsonListKeys {
				if l, ok := inner[k2].([]any); ok {
					return l, true
				}
			}
		}
	}
	return nil, false
}

// firstKey is the first of keys m holds, and its value.
func firstKey(m map[string]any, keys []string) (any, string) {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v, k
		}
	}
	return nil, ""
}

func firstValue(m map[string]any, keys []string) any {
	v, _ := firstKey(m, keys)
	return v
}

// jsonSpeaker is a speaker: a string, a number (Speaker 0) or an object
// with a name.
func jsonSpeaker(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case json.Number:
		return "Speaker " + s.String()
	case map[string]any:
		for _, k := range []string{"name", "display_name", "displayName", "full_name", "username"} {
			if n, ok := s[k].(string); ok && n != "" {
				return n
			}
		}
	}
	return ""
}

// jsonText is an entry's text, or its words joined.
func jsonText(m map[string]any) string {
	for _, k := range jsonTextKeys {
		if s, ok := m[k].(string); ok {
			return s
		}
	}
	words, _ := m["words"].([]any)
	parts := make([]string, 0, len(words))
	size := 0
	for _, w := range words {
		var s string
		switch x := w.(type) {
		case string:
			s = x
		case map[string]any:
			for _, k := range []string{"punctuated_word", "text", "word"} {
				if t, ok := x[k].(string); ok {
					s = t
					break
				}
			}
		}
		if s != "" && size < maxTurnBytes {
			parts = append(parts, s)
			size += len(s) + 1
		}
	}
	return strings.Join(parts, " ")
}

// jsonStart is an entry's start as hh:mm:ss.
func jsonStart(m map[string]any, ms bool) string {
	if v, _ := firstKey(m, jsonStartMsKeys); v != nil {
		if n, ok := v.(json.Number); ok {
			if f, err := n.Float64(); err == nil {
				return seconds(f / 1000)
			}
		}
	}
	switch v := firstValue(m, jsonStartKeys).(type) {
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return ""
		}
		if ms {
			f /= 1000
		}
		return seconds(f)
	case string:
		if c := clock(v); c != "" {
			return c
		}
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return seconds(f)
		}
	}
	return ""
}

// Word documents.

// docxParagraphs reads a .docx's paragraphs, one per line, from
// word/document.xml alone: nothing else in the zip is opened.
func docxParagraphs(raw []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", ErrDocxMalformed
	}
	if len(zr.File) > maxDocxEntries {
		return "", ErrDocxTooManyParts
	}
	var doc *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			doc = f
			break
		}
	}
	if doc == nil {
		return "", ErrDocxMalformed
	}
	if doc.UncompressedSize64 > maxDocxXML {
		return "", ErrDocxTooLarge
	}
	rc, err := doc.Open()
	if err != nil {
		return "", ErrDocxMalformed
	}
	defer rc.Close()
	lr := &boundedReader{r: rc, left: maxDocxXML}
	d := xml.NewDecoder(lr)
	d.Strict = false
	var out, para strings.Builder
	depth, inText := 0, false
	for {
		tok, err := d.Token()
		if errors.Is(err, errBounded) || lr.over {
			return "", ErrDocxTooLarge
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", ErrDocxMalformed
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth > 512 {
				return "", ErrDocxMalformed
			}
			switch t.Name.Local {
			case "t":
				inText = true
			case "tab":
				para.WriteByte('\t')
			case "br", "cr":
				para.WriteByte('\n')
			case "p":
				para.Reset()
			}
		case xml.EndElement:
			depth--
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				if out.Len() < MaxTranscriptBytes*2 {
					out.WriteString(para.String())
					out.WriteByte('\n')
				}
				para.Reset()
			}
		case xml.CharData:
			if inText && para.Len() < maxTurnBytes {
				para.Write(t)
			}
		}
	}
	if para.Len() > 0 {
		out.WriteString(para.String())
	}
	return out.String(), nil
}

var errBounded = errors.New("bounded")

// boundedReader reads at most left bytes, then fails.
type boundedReader struct {
	r    io.Reader
	left int64
	over bool
}

func (b *boundedReader) Read(p []byte) (int, error) {
	if b.left <= 0 {
		// Anything more is over the bound
		var one [1]byte
		if n, _ := b.r.Read(one[:]); n > 0 {
			b.over = true
			return 0, errBounded
		}
		return 0, io.EOF
	}
	if int64(len(p)) > b.left {
		p = p[:b.left]
	}
	n, err := b.r.Read(p)
	b.left -= int64(n)
	return n, err
}
