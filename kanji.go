package main

import (
	_ "embed"
	"strings"
	"sync"
)

// data/ja_readings.txt (our clean-room generation) maps each kanji word to its
// kana reading(s): "word\treading1,reading2,...". Generated from model knowledge
// and fact-checked against a public reference dictionary at dev time (the reference is never shipped).
//
//go:embed data/ja_readings.txt
var kanjiData string

// data/ja_kanji.txt maps each common single kanji to its on/kun reading(s), used
// ONLY as a forward fallback: when word segmentation finds no dictionary word at
// a position, an un-dictionaried kanji yields a best-guess reading instead of a
// raw CJK char in the ASCII keystream. Clean-room generated, fact-checked against
// a public kanji-reading reference at dev time (never shipped).
//
//go:embed data/ja_kanji.txt
var kanjiCharData string

// maxSegLookahead bounds greedy segmentation's per-position word search (runes).
const maxSegLookahead = 16

// The parsed dictionary is a process-wide singleton: -all builds three Japanese
// converters, and parsing/holding the maps once (rather than per converter)
// saves the triple cost. The -max-readings cap is applied at lookup time (not
// baked in here), so the shared tables stay cap-independent.
var (
	kanjiOnce   sync.Once
	kanjiFwd    map[string][]string // kanji word -> kana reading(s)
	kanjiRev    map[string][]string // kana reading -> kanji word(s)
	kanjiChar   map[string][]string // single kanji -> reading(s), forward fallback
	kanjiMaxLen int                 // longest word, in runes (segmentation bound)
)

func loadKanjiDict() {
	kanjiOnce.Do(func() {
		kanjiFwd = make(map[string][]string, 20000)
		kanjiRev = map[string][]string{}
		for _, line := range strings.Split(kanjiData, "\n") {
			tab := strings.IndexByte(line, '\t')
			if tab < 0 {
				continue
			}
			word := line[:tab]
			readings := strings.Split(line[tab+1:], ",")
			kanjiFwd[word] = readings
			if n := len([]rune(word)); n > kanjiMaxLen {
				kanjiMaxLen = n
			}
			for _, r := range readings {
				kanjiRev[r] = append(kanjiRev[r], word)
			}
		}
		if kanjiMaxLen > maxSegLookahead {
			kanjiMaxLen = maxSegLookahead
		}
		kanjiChar = map[string][]string{}
		for _, line := range strings.Split(kanjiCharData, "\n") {
			tab := strings.IndexByte(line, '\t')
			if tab < 0 {
				continue
			}
			kanjiChar[line[:tab]] = strings.Split(line[tab+1:], ",")
		}
	})
}

// kanji decorates a Japanese kana converter, resolving kanji via the shared
// embedded word→reading map. Forward uses WORD-level greedy longest-match
// segmentation (so 今日→きょう, the correct compound reading, and mixed
// kanji+kana phrases resolve word by word), converting each segment's reading(s)
// through the base converter and fanning out the capped cartesian product.
//
// Reverse is WHOLE-READING only: it decodes to kana via the base converter, then
// offers candidate kanji words for the *whole* kana reading (私, 渡し, … for
// わたし), plus the kana itself. It does NOT segment the kana or rank the words.
// Full reverse segmentation + a reading→word fan-out is a noise firehose without
// word-frequency data, so it's left for a future pass.
type kanji struct {
	base        Converter
	maxVariants int
	maxReadings int // cap readings kept per word (0 = all); trims rare readings
}

func newKanji(base Converter, maxVariants, maxReadings int) Converter {
	if maxVariants < 1 {
		maxVariants = 1
	}
	loadKanjiDict()
	return &kanji{base: base, maxVariants: maxVariants, maxReadings: maxReadings}
}

func (k *kanji) Name() string { return k.base.Name() + " +kanji" }

// ToKeys greedily segments the input into dictionary words (longest match wins)
// and literal runs, converts each segment's reading(s) via the base converter,
// and returns the capped cartesian product of the pieces.
func (k *kanji) ToKeys(s string) []string {
	rs := []rune(s)
	var segments [][]string
	for i := 0; i < len(rs); {
		matched := false
		hi := i + kanjiMaxLen
		if hi > len(rs) {
			hi = len(rs)
		}
		for j := hi; j > i; j-- {
			if readings, ok := kanjiFwd[string(rs[i:j])]; ok {
				if k.maxReadings > 0 && len(readings) > k.maxReadings {
					readings = readings[:k.maxReadings]
				}
				var opts []string
				for _, r := range readings {
					opts = append(opts, k.base.ToKeys(r)...)
				}
				segments = append(segments, opts)
				i = j
				matched = true
				break
			}
		}
		if !matched {
			// Not a dictionary word. For a kanji, fall back to its single-kanji
			// reading(s) so it becomes keystrokes instead of a raw CJK char in an
			// ASCII keystream; otherwise (kana/other) convert via the base.
			r := string(rs[i])
			if cr, ok := kanjiChar[r]; ok {
				readings := cr
				if k.maxReadings > 0 && len(readings) > k.maxReadings {
					readings = readings[:k.maxReadings]
				}
				var opts []string
				for _, rd := range readings {
					opts = append(opts, k.base.ToKeys(rd)...)
				}
				segments = append(segments, opts)
			} else {
				segments = append(segments, k.base.ToKeys(r))
			}
			i++
		}
	}
	return cartesian(segments, k.maxVariants)
}

// ToNative decodes to kana via the base converter, then offers candidate kanji
// words for any whole kana reading found in the dictionary (words first, then
// the kana itself), capped.
func (k *kanji) ToNative(s string) []string {
	kana := k.base.ToNative(s)
	var out []string
	for _, kc := range kana {
		out = append(out, kanjiRev[kc]...)
	}
	out = append(out, kana...)
	if len(out) > k.maxVariants {
		out = out[:k.maxVariants]
	}
	return out
}
