package main

import "fmt"

// Converter performs bidirectional transliteration between native script text
// and the raw US-QWERTY keystroke sequence an IME would have consumed to type
// it. Each implementation is one language + keyboard layout.
//
// Both methods return a slice of variants. Most Korean conversions are 1:1 and
// return a single element, but the slice is the contract from day one because
// later layouts (Zhuyin reverse, in particular) fan out to many candidates per
// input and the caller already knows how to emit N output lines per input line.
type Converter interface {
	// ToKeys maps native text -> keystroke artifact(s).
	// e.g. "안녕" -> ["dkssud"]
	ToKeys(s string) []string

	// ToNative maps keystroke artifact(s) -> native text candidate(s).
	// e.g. "dkssud" -> ["안녕"]
	ToNative(s string) []string

	// Name is the canonical language/layout name for diagnostics.
	Name() string
}

// Options holds the tunable conversion knobs. Only Zhuyin uses them today
// (its mapping is many-to-many and dictionary-backed); Korean and Japanese are
// bijective and ignore them.
type Options struct {
	MaxHomophones int  // zhuyin reverse: max candidate chars per syllable
	MaxVariants   int  // max output candidates per input line (fan-out guard)
	MaxRank       int  // zhuyin reverse: only chars with freq rank <= N (0 = all)
	Polyphone     bool // zhuyin forward: emit all readings, not just the primary
	Tone1Space    bool // zhuyin: emit a space for 1st-tone syllables
	Kanji         bool // japanese forward: resolve kanji words to kana via the embedded reading set
	MaxReadings   int  // cap readings kept per char/word (0 = all); trims rare readings
	MaxPolyphone  int  // zhuyin forward: cap readings per polyphonic char (0 = fall back to MaxReadings)
}

// allLayouts enumerates every supported (language, layout) pair. It drives
// -all (run everything at once) and doubles as the authoritative list of what
// the tool supports.
var allLayouts = []struct{ lang, layout string }{
	{"korean", "2beolsik"},
	{"korean", "3beolsik"},
	{"japanese", "jis-kana"},
	{"japanese", "jis-kana-us"},
	{"japanese", "romaji"},
	{"zhuyin", "dacheng"},
	{"zhuyin", "eten"},
}

// allConverters builds one converter per (language, layout) pair for -all mode.
// opts is passed through, so -kanji (if set) applies to the Japanese converters.
func allConverters(opts Options) ([]Converter, error) {
	cs := make([]Converter, 0, len(allLayouts))
	for _, s := range allLayouts {
		c, err := newConverter(s.lang, s.layout, opts)
		if err != nil {
			return nil, err
		}
		cs = append(cs, c)
	}
	return cs, nil
}

// newConverter resolves a language + optional layout to a Converter. An empty
// layout selects each language's default. Returns an error for an unknown
// language or an unsupported layout so the caller can report it precisely.
func newConverter(lang, layout string, opts Options) (Converter, error) {
	switch lang {
	case "ko", "korean":
		switch layout {
		case "", "2beolsik":
			return newKorean(), nil
		case "3beolsik", "3f", "3beolsik-final", "sebeolsik-final":
			return newKorean3f(), nil
		default:
			return nil, fmt.Errorf("korean: unknown layout %q (supported: 2beolsik, 3beolsik)", layout)
		}
	case "ja", "jp", "japanese":
		return newJapanese(layout, opts)
	case "zh", "zhuyin", "bopomofo":
		return newZhuyin(layout, opts)
	default:
		return nil, fmt.Errorf("unknown language %q (supported: korean, japanese, zhuyin)", lang)
	}
}
