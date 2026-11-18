package main

import (
	_ "embed"
	"fmt"
	"strconv"
	"strings"
)

// Zhuyin (Bopomofo): Traditional Chinese <-> keyboard keystrokes.
//
// Unlike Korean/Japanese this needs a dictionary: the embedded, committed
// data/zhuyin_data.txt, one tab-separated line per char ("char\trank\treading1,…"),
// where a reading is a bopomofo string plus a tone digit 1-5 (5 = neutral) and
// rank is the char's frequency order (0 = uncommon, excluded from reverse). The
// dictionary is layout-agnostic; a layout (dacheng or eten) is just a pair of
// key tables applied at runtime.
//
// Forward (Hanzi->keys) uses each char's PRIMARY (most common) reading by
// default, giving one deterministic artifact per line, the most likely thing a
// user actually typed. The -polyphone option instead emits every reading.
//
// Reverse (keys->Hanzi) is the big fan-out we built the []string contract for.
// Tone keys delimit syllables (you must press a tone/space to commit), so
// segmentation splits on them; each syllable's reading maps to many homophone
// characters, ranked by frequency and capped.

//go:embed data/zhuyin_data.txt
var zhuyinData string

const (
	defaultMaxHomophones = 20  // top-N candidate chars per syllable on reverse
	defaultMaxVariants   = 200 // cap on total candidate strings per input line
)

// zhuyinLayout is a per-layout keymap. Because the dictionary stores readings
// (bopomofo + tone), a layout is fully described by these four tables.
type zhuyinLayout struct {
	bopomofoKey map[rune]string // bopomofo symbol -> key
	toneKey     map[int]string  // tones 2-5 -> key (tone 1 is per-converter, tone1Key)
	keyBopomofo map[rune]rune   // reverse of bopomofoKey
	keyTone     map[rune]int    // reverse of toneKey, plus space -> tone 1
}

func newLayout(bopo map[rune]string, tone map[int]string) zhuyinLayout {
	kb := make(map[rune]rune, len(bopo))
	for sym, k := range bopo {
		kb[rune(k[0])] = sym
	}
	kt := map[rune]int{' ': 1} // space commits a 1st-tone syllable in both layouts
	for t, k := range tone {
		kt[rune(k[0])] = t
	}
	return zhuyinLayout{bopomofoKey: bopo, toneKey: tone, keyBopomofo: kb, keyTone: kt}
}

// Dàqiān (大千) is the standard Taiwan layout (the one that yields ji32k7au4a83).
var dachengBopomofoKey = map[rune]string{
	'ㄅ': "1", 'ㄆ': "q", 'ㄇ': "a", 'ㄈ': "z", 'ㄉ': "2", 'ㄊ': "w", 'ㄋ': "s",
	'ㄌ': "x", 'ㄍ': "e", 'ㄎ': "d", 'ㄏ': "c", 'ㄐ': "r", 'ㄑ': "f", 'ㄒ': "v",
	'ㄓ': "5", 'ㄔ': "t", 'ㄕ': "g", 'ㄖ': "b", 'ㄗ': "y", 'ㄘ': "h", 'ㄙ': "n",
	'ㄧ': "u", 'ㄨ': "j", 'ㄩ': "m",
	'ㄚ': "8", 'ㄛ': "i", 'ㄜ': "k", 'ㄝ': ",", 'ㄞ': "9", 'ㄟ': "o", 'ㄠ': "l",
	'ㄡ': ".", 'ㄢ': "0", 'ㄣ': "p", 'ㄤ': ";", 'ㄥ': "/", 'ㄦ': "-",
}
var dachengToneKey = map[int]string{2: "6", 3: "3", 4: "4", 5: "7"}

// ETen (倚天) is a mnemonic layout with consonants near their pinyin letters.
// Table transcribed from a reference ETen layout definition (verified against
// its source). Tones sit on 1-4 (neutral on 1), 1st tone on space.
var etenBopomofoKey = map[rune]string{
	'ㄅ': "b", 'ㄆ': "p", 'ㄇ': "m", 'ㄈ': "f", 'ㄉ': "d", 'ㄊ': "t", 'ㄋ': "n",
	'ㄌ': "l", 'ㄍ': "v", 'ㄎ': "k", 'ㄏ': "h", 'ㄐ': "g", 'ㄑ': "7", 'ㄒ': "c",
	'ㄓ': ",", 'ㄔ': ".", 'ㄕ': "/", 'ㄖ': "j", 'ㄗ': ";", 'ㄘ': "'", 'ㄙ': "s",
	'ㄧ': "e", 'ㄨ': "x", 'ㄩ': "u",
	'ㄚ': "a", 'ㄛ': "o", 'ㄜ': "r", 'ㄝ': "w", 'ㄞ': "i", 'ㄟ': "q", 'ㄠ': "z",
	'ㄡ': "y", 'ㄢ': "8", 'ㄣ': "9", 'ㄤ': "0", 'ㄥ': "-", 'ㄦ': "=",
}
var etenToneKey = map[int]string{2: "2", 3: "3", 4: "4", 5: "1"}

type zhuyin struct {
	name string
	zhuyinLayout
	charReadings map[rune][]string // char -> readings (bopomofo+toneDigit), primary first
	readingChars map[string][]rune // reading -> chars, frequency-ranked (common chars only)

	// Tunable knobs (see Options).
	maxHomophones int
	maxVariants   int
	maxReadings   int // cap readings kept per char (0 = all); trims rare/literary readings
	maxPolyphone  int // forward polyphone: cap readings per char (0 = fall back to maxReadings)
	polyphone     bool
	tone1Key      string // key emitted for 1st tone: " " or "" when space is suppressed
}

func newZhuyin(layout string, opts Options) (Converter, error) {
	var lt zhuyinLayout
	switch layout {
	case "", "dacheng", "dachien", "大千":
		layout = "dacheng"
		lt = newLayout(dachengBopomofoKey, dachengToneKey)
	case "eten", "et", "倚天":
		layout = "eten"
		lt = newLayout(etenBopomofoKey, etenToneKey)
	default:
		return nil, fmt.Errorf("zhuyin: unknown layout %q (supported: dacheng, eten)", layout)
	}

	// Clamp the fan-out guards to >=1 so a degenerate flag can't disable them.
	maxHomophones := opts.MaxHomophones
	if maxHomophones < 1 {
		maxHomophones = 1
	}
	maxVariants := opts.MaxVariants
	if maxVariants < 1 {
		maxVariants = 1
	}
	maxRank := opts.MaxRank
	if maxRank < 0 {
		maxRank = 0 // a negative rank would exclude every char; treat as "all"
	}
	maxReadings := opts.MaxReadings
	if maxReadings < 0 {
		maxReadings = 0
	}
	maxPolyphone := opts.MaxPolyphone
	if maxPolyphone < 0 {
		maxPolyphone = 0
	}
	tone1Key := ""
	if opts.Tone1Space {
		tone1Key = " "
	}

	z := &zhuyin{
		name:          "zhuyin (" + layout + ")",
		zhuyinLayout:  lt,
		charReadings:  map[rune][]string{},
		readingChars:  map[string][]rune{},
		maxHomophones: maxHomophones,
		maxVariants:   maxVariants,
		maxReadings:   maxReadings,
		maxPolyphone:  maxPolyphone,
		polyphone:     opts.Polyphone,
		tone1Key:      tone1Key,
	}

	for _, line := range strings.Split(zhuyinData, "\n") {
		if line == "" {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 3 {
			continue
		}
		char := []rune(cols[0])[0]
		rank, _ := strconv.Atoi(cols[1])
		readings := strings.Split(cols[2], ",")
		z.charReadings[char] = readings
		// Only common (ranked) chars feed the reverse index, so homophone
		// fan-out stays realistic. MaxRank tightens that universe further to
		// the top-N most frequent chars. The data file is globally rank-sorted,
		// so appending in file order keeps each list frequency-ranked.
		if rank > 0 && (maxRank == 0 || rank <= maxRank) {
			rs := readings
			if maxReadings > 0 && len(rs) > maxReadings {
				rs = rs[:maxReadings] // trim rare/literary readings (primary-first ordering)
			}
			for _, r := range rs {
				z.readingChars[r] = append(z.readingChars[r], char)
			}
		}
	}
	return z, nil
}

func (z *zhuyin) Name() string { return z.name }

// ToKeys converts Traditional Chinese to keystrokes. By default each char uses
// its primary (most common) reading, yielding one artifact. With polyphone
// enabled, polyphonic chars contribute all their readings and the output is the
// capped cartesian product. Non-Han characters pass through.
func (z *zhuyin) ToKeys(s string) []string {
	var positions [][]string
	for _, r := range s {
		if readings, ok := z.charReadings[r]; ok {
			if !z.polyphone {
				readings = readings[:1]
			} else {
				// Dedicated forward cap if set, else the shared maxReadings.
				lim := z.maxPolyphone
				if lim == 0 {
					lim = z.maxReadings
				}
				if lim > 0 && len(readings) > lim {
					readings = readings[:lim]
				}
			}
			opts := make([]string, len(readings))
			for i, rd := range readings {
				opts[i] = z.readingToKeys(rd)
			}
			positions = append(positions, opts)
		} else {
			positions = append(positions, []string{string(r)})
		}
	}
	return cartesian(positions, z.maxVariants)
}

// readingToKeys turns a reading ("ㄨㄛ3") into keystrokes: map each bopomofo
// symbol to its key, then the trailing tone digit to its tone key. 1st tone uses
// the configurable tone1Key (space by default, or empty).
func (z *zhuyin) readingToKeys(reading string) string {
	rs := []rune(reading)
	if len(rs) == 0 {
		return ""
	}
	tone := int(rs[len(rs)-1] - '0')
	var b strings.Builder
	for _, sym := range rs[:len(rs)-1] {
		b.WriteString(z.bopomofoKey[sym])
	}
	if tone == 1 {
		b.WriteString(z.tone1Key)
	} else {
		b.WriteString(z.toneKey[tone])
	}
	return b.String()
}

// ToNative segments a keystroke artifact on tone keys and expands each syllable
// to its frequency-ranked homophone characters, returning the capped cartesian
// product of candidates. Unsegmentable runs and non-key characters pass through.
func (z *zhuyin) ToNative(s string) []string {
	var positions [][]string // each position: candidate strings (chars or literal)
	var syl []rune           // accumulated bopomofo key chars

	flushSyl := func() {
		if len(syl) > 0 {
			// Bopomofo with no closing tone, so it can't be resolved; pass raw keys.
			positions = append(positions, []string{string(syl)})
			syl = syl[:0]
		}
	}

	for _, c := range s {
		switch {
		case z.isTone(c):
			if len(syl) > 0 {
				reading := z.keysToReading(syl, z.keyTone[c])
				if chars, ok := z.readingChars[reading]; ok {
					positions = append(positions, charsToStrings(chars, z.maxHomophones))
				} else {
					// valid bopomofo but no ranked char with this reading
					positions = append(positions, []string{string(syl) + string(c)})
				}
				syl = syl[:0]
			} else {
				positions = append(positions, []string{string(c)}) // stray tone key
			}
		case z.isBopomofo(c):
			syl = append(syl, c)
		default:
			flushSyl()
			positions = append(positions, []string{string(c)})
		}
	}
	flushSyl()

	return cartesian(positions, z.maxVariants)
}

func (z *zhuyin) isTone(c rune) bool     { _, ok := z.keyTone[c]; return ok }
func (z *zhuyin) isBopomofo(c rune) bool { _, ok := z.keyBopomofo[c]; return ok }

// keysToReading rebuilds a reading string ("ㄨㄛ3") from accumulated key chars
// plus a tone number.
func (z *zhuyin) keysToReading(keys []rune, tone int) string {
	var b strings.Builder
	for _, k := range keys {
		b.WriteRune(z.keyBopomofo[k])
	}
	b.WriteString(strconv.Itoa(tone))
	return b.String()
}

// charsToStrings returns the first n characters as single-rune strings.
func charsToStrings(chars []rune, n int) []string {
	if len(chars) > n {
		chars = chars[:n]
	}
	out := make([]string, len(chars))
	for i, c := range chars {
		out[i] = string(c)
	}
	return out
}

// cartesian expands positions into candidate strings, truncating to limit after
// each position so output stays bounded while candidates keep full length.
func cartesian(positions [][]string, limit int) []string {
	result := []string{""}
	for _, opts := range positions {
		next := make([]string, 0, len(result)*len(opts))
		for _, prefix := range result {
			for _, o := range opts {
				next = append(next, prefix+o)
			}
		}
		if len(next) > limit {
			next = next[:limit]
		}
		result = next
	}
	return result
}
