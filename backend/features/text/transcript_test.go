package text

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain lets the test binary be a transcript child, so
// ChildTranscripts(os.Executable()) works in tests.
func TestMain(m *testing.M) {
	if TranscriptChildRequested() {
		ServeTranscriptChild()
		return
	}
	os.Exit(m.Run())
}

const teamsVTT = `WEBVTT

NOTE produced by Teams

STYLE
::cue { color: white }

0f6a1c2e-1/12-0
00:00:03.120 --> 00:00:05.400 align:start position:10%
<v Anna Schmidt>Good morning, everyone.</v>

0f6a1c2e-1/13-0
00:00:05.400 --> 00:00:08.000
<v Anna Schmidt>Let's go over the venue &amp; the catering.</v>

0f6a1c2e-2/1-0
00:00:08.500 --> 00:00:11.000
<v.loud Ben Okafor>The <b>deposit</b> is due Friday.</v>
`

const zoomVTT = `WEBVTT

1
00:00:01.000 --> 00:00:03.000
Anna Schmidt: Hi Ben.

2
00:00:03.500 --> 00:00:06.000
Ben Okafor: Hi Anna, thanks for joining.

3
00:01:02.250 --> 00:01:04.000
Ben Okafor: Shall we start?
`

const srtFile = "1\r\n00:00:01,000 --> 00:00:02,500\r\nAnna: Welcome to the review.\r\n\r\n2\r\n00:00:03,000 --> 00:00:04,000\r\n<i>Ben: Thanks.</i>\r\n\r\n3\r\n01:02:03,000 --> 01:02:05,000\r\nAnna: Last item.\r\n"

func mustParse(t *testing.T, name, ct, raw string) Transcript {
	t.Helper()
	tr, err := ParseTranscript(name, ct, []byte(raw))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return tr
}

func lineStrings(tr Transcript) []string {
	out := make([]string, len(tr.Lines))
	for i, l := range tr.Lines {
		out[i] = fmt.Sprintf("%d|%s|%s|%s", l.N, l.Speaker, l.At, l.Text)
	}
	return out
}

func expectLines(t *testing.T, tr Transcript, want ...string) {
	t.Helper()
	got := lineStrings(tr)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("lines:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestTranscriptTeamsVTT(t *testing.T) {
	tr := mustParse(t, "meeting.vtt", "", teamsVTT)
	if tr.Format != TranscriptVTT {
		t.Errorf("format %q", tr.Format)
	}
	expectLines(t, tr,
		"1|Anna Schmidt|00:00:03|Good morning, everyone. Let's go over the venue & the catering.",
		"2|Ben Okafor|00:00:08|The deposit is due Friday.")
	if strings.Join(tr.Speakers, ",") != "Anna Schmidt,Ben Okafor" || tr.Truncated {
		t.Errorf("speakers %v truncated %v", tr.Speakers, tr.Truncated)
	}
}

func TestTranscriptZoomVTT(t *testing.T) {
	tr := mustParse(t, "", "text/vtt", zoomVTT)
	expectLines(t, tr,
		"1|Anna Schmidt|00:00:01|Hi Ben.",
		"2|Ben Okafor|00:00:03|Hi Anna, thanks for joining. Shall we start?")
	// Pasted with no name: sniffed
	if tr := mustParse(t, "", "", zoomVTT); tr.Format != TranscriptVTT || len(tr.Lines) != 2 {
		t.Errorf("sniffed: %v", tr)
	}
}

func TestTranscriptSRT(t *testing.T) {
	tr := mustParse(t, "review.srt", "", srtFile)
	expectLines(t, tr,
		"1|Anna|00:00:01|Welcome to the review.",
		"2|Ben|00:00:03|Thanks.",
		"3|Anna|01:02:03|Last item.")
	if tr := mustParse(t, "", "", srtFile); tr.Format != TranscriptSRT {
		t.Errorf("sniffed %q", tr.Format)
	}
}

func TestTranscriptPlainColon(t *testing.T) {
	tr := mustParse(t, "notes.txt", "", `Weekly sync

Anna: Welcome. The venue is booked.
Note: the deposit is 20%.
Ben: Good. I will check the menu
and send it round.
Anna: Thanks.
Ben: Q3 numbers: up 4%.
`)
	expectLines(t, tr,
		"1|||Weekly sync",
		"2|Anna||Welcome. The venue is booked. Note: the deposit is 20%.",
		"3|Ben||Good. I will check the menu and send it round.",
		"4|Anna||Thanks.",
		"5|Ben||Q3 numbers: up 4%.")
	if strings.Join(tr.Speakers, ",") != "Anna,Ben" {
		t.Errorf("speakers %v", tr.Speakers)
	}
}

func TestTranscriptPlainTimestamps(t *testing.T) {
	tr := mustParse(t, "", "", "[00:00:05] Anna: Shall we start?\n[00:01:10] Ben: Yes.\n00:02:03 Anna (Host): The plan: two rooms.\nmore detail here\n")
	expectLines(t, tr,
		"1|Anna|00:00:05|Shall we start?",
		"2|Ben|00:01:10|Yes.",
		"3|Anna (Host)|00:02:03|The plan: two rooms. more detail here")
}

func TestTranscriptPlainHeaders(t *testing.T) {
	// Otter: "Name  0:03" headers
	tr := mustParse(t, "otter.txt", "", `Anna Schmidt  0:03
Hello everyone, thanks for joining.
We meet at 10:30 tomorrow.

Ben Okafor  0:10
Thanks Anna.

Anna Schmidt  1:02:05
One more thing.
`)
	expectLines(t, tr,
		"1|Anna Schmidt|00:00:03|Hello everyone, thanks for joining. We meet at 10:30 tomorrow.",
		"2|Ben Okafor|00:00:10|Thanks Anna.",
		"3|Anna Schmidt|01:02:05|One more thing.")
	// A speaker line over a time line
	tr = mustParse(t, "", "", "Anna Schmidt\n00:00:03\nHello.\n\nBen Okafor\n00:00:09\nHi.\nSee you at\n12:00\n")
	expectLines(t, tr,
		"1|Anna Schmidt|00:00:03|Hello.",
		"2|Ben Okafor|00:00:09|Hi. See you at")
}

func TestTranscriptLabelsAreConservative(t *testing.T) {
	for _, s := range []string{"Note", "http://x", "1 thing", "The deposit is 20%", "a b c d e f", "Results", "Mr. Smith said."} {
		if labelStrength(s) == 2 {
			t.Errorf("%q is a strong label", s)
		}
	}
	for _, s := range []string{"Anna", "Speaker 1", "Dr. Jane Doe", "SPEAKER_00", "Ana-María O'Neil"} {
		if labelStrength(s) != 2 {
			t.Errorf("%q is not a strong label", s)
		}
	}
}

func docx(t *testing.T, documentXML string, extra int) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, _ := z.Create("[Content_Types].xml")
	w.Write([]byte(`<?xml version="1.0"?><Types/>`))
	for i := range extra {
		w, _ := z.Create(fmt.Sprintf("word/media/x%d.txt", i))
		w.Write([]byte("x"))
	}
	w, _ = z.Create("word/document.xml")
	w.Write([]byte(documentXML))
	z.Close()
	return b.Bytes()
}

const docXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:p><w:r><w:t>Anna Schmidt</w:t></w:r><w:r><w:tab/><w:t>0:03</w:t></w:r></w:p>
<w:p><w:r><w:t xml:space="preserve">Hello </w:t></w:r><w:r><w:t>everyone.</w:t></w:r><w:del><w:r><w:delText>gone</w:delText></w:r></w:del></w:p>
<w:p><w:r><w:t>Ben Okafor</w:t><w:tab/><w:t>0:10</w:t></w:r></w:p>
<w:p><w:r><w:t>Thanks.</w:t><w:br/><w:t>Second line.</w:t></w:r></w:p>
</w:body></w:document>`

func TestTranscriptDocx(t *testing.T) {
	tr, err := ParseTranscript("Meeting.docx", "", docx(t, docXML, 0))
	if err != nil {
		t.Fatal(err)
	}
	expectLines(t, tr,
		"1|Anna Schmidt|00:00:03|Hello everyone.",
		"2|Ben Okafor|00:00:10|Thanks. Second line.")
	// Sniffed when pasted as a file with no name
	if tr, err := ParseTranscript("", "", docx(t, docXML, 0)); err != nil || tr.Format != TranscriptDocx {
		t.Errorf("sniffed: %v %v", tr.Format, err)
	}
}

func TestTranscriptDocxRefused(t *testing.T) {
	if _, err := ParseTranscript("a.docx", "", docx(t, docXML, 201)); err != ErrDocxTooManyParts {
		t.Errorf("too many parts: %v", err)
	}
	// A zip bomb: a small file whose document.xml inflates past the bound
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, _ := z.Create("word/document.xml")
	w.Write([]byte("<w:document><w:body><w:p><w:r><w:t>"))
	chunk := bytes.Repeat([]byte("a"), 1<<20)
	for range 21 {
		w.Write(chunk)
	}
	z.Close()
	if b.Len() > 1<<20 {
		t.Fatalf("bomb is %d bytes", b.Len())
	}
	if _, err := ParseTranscript("a.docx", "", b.Bytes()); err != ErrDocxTooLarge {
		t.Errorf("bomb: %v", err)
	}
	// The same with its declared size lied about
	raw := b.Bytes()
	lied := bytes.Clone(raw)
	if i := bytes.LastIndex(lied, []byte("PK\x01\x02")); i >= 0 {
		copy(lied[i+24:i+28], []byte{0xFF, 0x00, 0x00, 0x00})
	}
	if _, err := ParseTranscript("a.docx", "", lied); err == nil {
		t.Errorf("lying bomb read")
	}
	if _, err := ParseTranscript("a.docx", "", []byte("not a zip")); err != ErrDocxMalformed {
		t.Errorf("not a zip: %v", err)
	}
	if _, err := ParseTranscript("a.docx", "", docx(t, "<w:document><w:p>", 0)); err == nil {
		t.Errorf("broken XML read")
	}
	deep := strings.Repeat("<a>", 2000)
	if _, err := ParseTranscript("a.docx", "", docx(t, deep, 0)); err != ErrDocxMalformed {
		t.Errorf("deep XML: %v", err)
	}
}

func TestTranscriptJSON(t *testing.T) {
	// AssemblyAI utterances: speaker letters, times in ms
	tr := mustParse(t, "a.json", "", `{"id":"x","utterances":[
		{"speaker":"A","text":"Hello there.","start":250,"end":1200},
		{"speaker":"A","text":"Second sentence.","start":1300,"end":2000},
		{"speaker":"B","text":"Hi.","start":62000,"end":63000}]}`)
	expectLines(t, tr, "1|A|00:00:00|Hello there. Second sentence.", "2|B|00:01:02|Hi.")
	// Fireflies sentences: speaker_name, seconds
	tr = mustParse(t, "", "application/json", `{"data":1,"sentences":[
		{"index":0,"speaker_name":"Anna","text":"Budget first.","raw_text":"budget first","start_time":1.5},
		{"index":1,"speaker_name":"Ben","raw_text":"Agreed.","start_time":65.2}]}`)
	expectLines(t, tr, "1|Anna|00:00:01|Budget first.", "2|Ben|00:01:05|Agreed.")
	// Whisper segments, nested under transcript; whisperx speaker
	tr = mustParse(t, "w.json", "", `{"transcript":{"segments":[
		{"id":0,"start":0.0,"end":2.5,"text":" Hello.","speaker":"SPEAKER_00"},
		{"id":1,"start":3661.0,"end":3663,"text":" Late.","speaker":"SPEAKER_01"}]}}`)
	expectLines(t, tr, "1|SPEAKER_00|00:00:00|Hello.", "2|SPEAKER_01|01:01:01|Late.")
	// Deepgram-like: numeric speaker, words
	tr = mustParse(t, "d.json", "", `[{"speaker":0,"words":[{"word":"so","punctuated_word":"So,"},{"word":"yes"}],"start":"00:00:07"},
		{"user":{"name":"Cara"},"content":"Bye.","start_ms":125000}]`)
	expectLines(t, tr, "1|Speaker 0|00:00:07|So, yes", "2|Cara|00:02:05|Bye.")
	// A plain transcript string
	tr = mustParse(t, "t.json", "", `{"transcript":"Anna: Hi.\nBen: Hello."}`)
	expectLines(t, tr, "1|Anna||Hi.", "2|Ben||Hello.")
	if _, err := ParseTranscript("x.json", "", []byte(`{"foo":1}`)); err != ErrTranscriptNoList {
		t.Errorf("no list: %v", err)
	}
	if _, err := ParseTranscript("x.json", "", []byte(`{"foo":`)); err != ErrTranscriptJSON {
		t.Errorf("malformed: %v", err)
	}
}

func TestTranscriptMergeLimit(t *testing.T) {
	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	for i := range 30 {
		fmt.Fprintf(&b, "00:00:%02d.000 --> 00:00:%02d.500\nAnna: %s\n\n", i, i, strings.Repeat("word ", 20)) // ~100 chars a cue
	}
	tr := mustParse(t, "x.vtt", "", b.String())
	if len(tr.Lines) < 3 || len(tr.Lines) > 4 {
		t.Fatalf("%d lines", len(tr.Lines))
	}
	for _, l := range tr.Lines {
		if len([]rune(l.Text)) > maxMergedChars {
			t.Errorf("line %d has %d chars", l.N, len(l.Text))
		}
	}
	if tr.Lines[1].At != "00:00:10" || tr.Lines[1].N != 2 {
		t.Errorf("second line %+v", tr.Lines[1])
	}
}

func TestTranscriptLimits(t *testing.T) {
	if _, err := ParseTranscript("a.txt", "", make([]byte, MaxTranscriptBytes+1)); err != ErrTranscriptTooLarge {
		t.Errorf("too large: %v", err)
	}
	var b strings.Builder
	for i := range MaxTranscriptLines + 10 {
		fmt.Fprintf(&b, "Speaker %d: line %d\n", i%3, i)
	}
	tr := mustParse(t, "a.txt", "", b.String())
	if len(tr.Lines) != MaxTranscriptLines || !tr.Truncated || tr.Lines[MaxTranscriptLines-1].N != MaxTranscriptLines {
		t.Errorf("%d lines, truncated %v", len(tr.Lines), tr.Truncated)
	}
	tr = mustParse(t, "a.txt", "", "Anna: "+strings.Repeat("x", MaxTranscriptLineChars+50))
	if len(tr.Lines[0].Text) != MaxTranscriptLineChars || !tr.Truncated {
		t.Errorf("clipped to %d, truncated %v", len(tr.Lines[0].Text), tr.Truncated)
	}
	// The total text is bounded too
	b.Reset()
	long := strings.Repeat("y", MaxTranscriptLineChars-1)
	for i := range MaxTranscriptChars/MaxTranscriptLineChars + 5 {
		fmt.Fprintf(&b, "Speaker %d: %s\n", i%2, long)
	}
	tr = mustParse(t, "a.txt", "", b.String())
	total := 0
	for _, l := range tr.Lines {
		total += len(l.Text)
	}
	if total > MaxTranscriptChars || !tr.Truncated {
		t.Errorf("total %d truncated %v", total, tr.Truncated)
	}
	if _, err := ParseTranscript("a.txt", "", []byte("\n \n\t\n")); err != ErrTranscriptEmpty {
		t.Errorf("empty: %v", err)
	}
	if _, err := ParseTranscript("a.pdf", "", []byte("%PDF-1.4")); err != ErrTranscriptFormat {
		t.Errorf("pdf: %v", err)
	}
}

func TestTranscriptEncodings(t *testing.T) {
	// Windows-1252 and UTF-16 with a BOM; control characters dropped
	tr := mustParse(t, "a.txt", "", "Ren\xe9e: Caf\xe9 \x93ok\x94\x01.\n")
	expectLines(t, tr, "1|Renée||Café “ok”.")
	u16 := []byte{0xFF, 0xFE}
	for _, r := range "Anna: Hi\n" {
		u16 = append(u16, byte(r), 0)
	}
	tr = mustParse(t, "a.txt", "", string(u16))
	expectLines(t, tr, "1|Anna||Hi")
}

// No input makes the parser panic: random bytes and every fixture cut
// short, in every format.
func TestTranscriptNeverPanics(t *testing.T) {
	fixtures := map[string][]byte{
		"a.vtt": []byte(teamsVTT), "b.vtt": []byte(zoomVTT), "a.srt": []byte(srtFile),
		"a.docx": docx(t, docXML, 0), "a.json": []byte(`{"utterances":[{"speaker":"A","text":"x","start":1}]}`),
		"a.txt": []byte("Anna Schmidt  0:03\nHi\n[00:01] Ben: yo\n"),
	}
	names := []string{"a.vtt", "a.srt", "a.docx", "a.json", "a.txt", ""}
	check := func(name string, raw []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("%s panicked on %q: %v", name, raw, r)
			}
		}()
		_, _ = ParseTranscript(name, "", raw)
		// ParseTranscript recovers itself; call the parsers directly too
		b := &transcriptBuilder{}
		parsePlainTranscript(string(raw), b)
		parseCues(string(raw), b)
		_ = parseTranscriptJSON(string(raw), b)
		_, _ = docxParagraphs(raw)
	}
	for name, raw := range fixtures {
		for i := 0; i <= len(raw); i++ {
			check(name, raw[:i])
			check("", raw[:i])
		}
	}
	rng := rand.New(rand.NewSource(1))
	for range 2000 {
		raw := make([]byte, rng.Intn(400))
		rng.Read(raw)
		for _, n := range names {
			check(n, raw)
		}
	}
}

func TestChildTranscripts(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	read := ChildTranscripts(exe)
	tr, err := read(context.Background(), "m.vtt", "", []byte(teamsVTT))
	if err != nil || len(tr.Lines) != 2 || tr.Lines[1].Speaker != "Ben Okafor" || tr.Format != TranscriptVTT {
		t.Fatalf("child: %+v %v", tr, err)
	}
	if _, err := read(context.Background(), "x.json", "", []byte(`{"foo":`)); err == nil || err.Error() != ErrTranscriptJSON.Error() {
		t.Errorf("parse error: %v", err)
	}
	if _, err := read(context.Background(), "x.txt", "", make([]byte, MaxTranscriptBytes+1)); err != ErrTranscriptTooLarge {
		t.Errorf("too large: %v", err)
	}
}

// The parent survives a child that hangs or crashes.
func TestChildTranscriptsHangAndCrash(t *testing.T) {
	dir := t.TempDir()
	hang := filepath.Join(dir, "hang.sh")
	crash := filepath.Join(dir, "crash.sh")
	if err := os.WriteFile(hang, []byte("#!/bin/sh\nwhile :; do :; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(crash, []byte("#!/bin/sh\necho '{\"transcript\": {'\nexit 2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := transcriptWait
	transcriptWait = 300 * time.Millisecond
	defer func() { transcriptWait = old }()
	start := time.Now()
	if _, err := ChildTranscripts(hang)(context.Background(), "a.txt", "", []byte("Anna: hi")); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Errorf("hang: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("hang took %v", time.Since(start))
	}
	if _, err := ChildTranscripts(crash)(context.Background(), "a.txt", "", []byte("Anna: hi")); err == nil || !strings.Contains(err.Error(), "safely") {
		t.Errorf("crash: %v", err)
	}
}
