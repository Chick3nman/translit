package main

import (
	"fmt"
	"strings"
)

// Japanese: kana <-> JIS kana keyboard keystrokes.
//
// Script layer (shared, no dictionary): a kana decomposes into a base kana plus
// an optional voicing mark. が = か + ゛(dakuten); ぱ = は + ゜(handakuten). On a
// JIS kana keyboard the mark is a separate keypress after the base, so the
// artifact for が is (key for か)(key for ゛). Katakana normalizes to hiragana
// first, since kana/katakana is a mode toggle, not a different physical key.
//
// Keymap layer (per layout): each kana occupies one physical key; the artifact
// is the character that key emits with the IME off. Two layouts are provided:
//
//   jis-kana     Model B, a JIS physical keyboard (Japan's dominant hardware).
//                Mark/punctuation keys emit their JIS-Roman legends (゛->@, ゜->[).
//                Complete and the well-tested primary.
//   jis-kana-us  Model A, a US-QWERTY physical keyboard with software kana.
//                Same arrangement, US legends on the shared keys. INHERENTLY
//                INCOMPLETE: a JIS keyboard has more keys than a US one, so a
//                few kana (む, the mark keys, ー, ろ) have no clean US home and
//                collide. Shipped with those collisions documented, not hidden.
//
// VERIFIED against an authoritative JIS keyboard-layout definition (the map
// of what each physical JIS key emits per shift level). Every Model B kana→key
// cell matches. The once-doubted ones are correct: the shifted-digit small kana
// ぉゃゅょ → & ' ( ); を → "~" (on the JIS 106 layout Shift+0 is asciitilde, not
// blank as on the old typewriter JIS); and ー / ろ both emit byte 0x5C ("\"),
// colliding as documented (reverse pins the common ー). jis-kana-us (Model A) is
// the US-physical approximation and remains inherently incomplete, since the JIS-only
// keys have no US home.

// kanaCore maps base hiragana to the US-QWERTY key for the alphanumeric keys,
// which are identical on US and JIS keyboards. High confidence.
var kanaCore = map[rune]string{
	'あ': "3", 'い': "e", 'う': "4", 'え': "5", 'お': "6",
	'か': "t", 'き': "g", 'く': "h", 'こ': "b",
	'さ': "x", 'し': "d", 'す': "r", 'せ': "p", 'そ': "c",
	'た': "q", 'ち': "a", 'つ': "z", 'て': "w", 'と': "s",
	'な': "u", 'に': "i", 'ぬ': "1", 'の': "k",
	'は': "f", 'ひ': "v", 'ふ': "2",
	'ま': "j", 'み': "n", 'も': "m",
	'や': "7", 'ゆ': "8", 'よ': "9",
	'ら': "o", 'り': "l",
	'わ': "0", 'ん': "y",
}

// voiced maps a dakuon kana to its base kana. Decomposition is a small static
// table rather than Unicode NFD to avoid a dependency and keep it obvious.
var voiced = map[rune]rune{
	'が': 'か', 'ぎ': 'き', 'ぐ': 'く', 'げ': 'け', 'ご': 'こ',
	'ざ': 'さ', 'じ': 'し', 'ず': 'す', 'ぜ': 'せ', 'ぞ': 'そ',
	'だ': 'た', 'ぢ': 'ち', 'づ': 'つ', 'で': 'て', 'ど': 'と',
	'ば': 'は', 'び': 'ひ', 'ぶ': 'ふ', 'べ': 'へ', 'ぼ': 'ほ',
	'ゔ': 'う',
}

// semivoiced maps a handakuon kana (p-row) to its base kana.
var semivoiced = map[rune]rune{
	'ぱ': 'は', 'ぴ': 'ひ', 'ぷ': 'ふ', 'ぺ': 'へ', 'ぽ': 'ほ',
}

// dakutenCombine / handakuCombine are the inverses, used by reverse to fold a
// mark key back onto the preceding base kana.
var dakutenCombine = invert(voiced)
var handakuCombine = invert(semivoiced)

func invert(m map[rune]rune) map[rune]rune {
	out := make(map[rune]rune, len(m))
	for k, v := range m {
		out[v] = k
	}
	return out
}

type japanese struct {
	name       string
	kana       map[rune]string // every kana (core + punct + small) -> keystrokes
	rev        map[rune]rune   // single ASCII key char -> base/small kana
	dakuten    string          // key for ゛
	handakuten string          // key for ゜
	dakutenCh  rune            // same, as a rune, for reverse recognition
	handakuCh  rune
}

// newJapanese dispatches a Japanese layout to its base converter (JIS kana or
// rōmaji), then wraps it (when opts.Kanji is set) so kanji words resolve to kana first.
func newJapanese(layout string, opts Options) (Converter, error) {
	if layout == "" {
		layout = "jis-kana"
	}
	var base Converter
	switch layout {
	case "romaji":
		base = newRomaji()
	case "jis-kana", "jis-kana-us":
		c, err := newJISKana(layout)
		if err != nil {
			return nil, err
		}
		base = c
	default:
		return nil, fmt.Errorf("japanese: unknown layout %q (supported: jis-kana, jis-kana-us, romaji)", layout)
	}
	if opts.Kanji {
		base = newKanji(base, opts.MaxVariants, opts.MaxReadings)
	}
	return base, nil
}

// newJISKana builds a JIS kana keyboard converter (Model B jis-kana or the
// incomplete Model A jis-kana-us).
func newJISKana(layout string) (Converter, error) {
	var punct, small map[rune]string
	var dak, han string

	switch layout {
	case "jis-kana": // Model B: JIS physical keyboard, JIS-Roman legends
		punct = map[rune]string{
			'け': ":", 'ね': ",", 'へ': "^", 'ほ': "-", 'む': "]",
			'め': "/", 'る': ".", 'れ': ";", 'ろ': "\\", 'ー': "\\",
		}
		small = map[rune]string{
			'ぁ': "#", 'ぅ': "$", 'ぇ': "%", 'ぉ': "&", 'ゃ': "'",
			'ゅ': "(", 'ょ': ")", 'を': "~", 'ぃ': "E", 'っ': "Z",
		}
		dak, han = "@", "["
	case "jis-kana-us": // Model A: US-QWERTY physical, US legends (incomplete)
		punct = map[rune]string{
			'け': "'", 'ね': ",", 'へ': "=", 'ほ': "-", 'む': "]",
			'め': "/", 'る': ".", 'れ': ";", 'ろ': "\\", 'ー': "\\",
		}
		small = map[rune]string{
			'ぁ': "#", 'ぅ': "$", 'ぇ': "%", 'ぉ': "^", 'ゃ': "&",
			'ゅ': "*", 'ょ': "(", 'を': ")", 'ぃ': "E", 'っ': "Z",
		}
		dak, han = "[", "]" // NOTE: han "]" collides with む on this layout
	default:
		return nil, fmt.Errorf("japanese: unknown layout %q (supported: jis-kana, jis-kana-us, romaji)", layout)
	}

	kana := make(map[rune]string, len(kanaCore)+len(punct)+len(small))
	for k, v := range kanaCore {
		kana[k] = v
	}
	for k, v := range punct {
		kana[k] = v
	}
	for k, v := range small {
		kana[k] = v
	}

	// Reverse map: single-char key -> kana. On a collision the first writer
	// wins arbitrarily via range order, so pin the known ambiguous key
	// deterministically afterward (ー and ろ both emit "\"; prefer the far more
	// common ー).
	rev := make(map[rune]rune, len(kana))
	for k, v := range kana {
		if len(v) == 1 {
			rev[rune(v[0])] = k
		}
	}
	rev['\\'] = 'ー'

	return &japanese{
		name:       "japanese (" + layout + ")",
		kana:       kana,
		rev:        rev,
		dakuten:    dak,
		handakuten: han,
		dakutenCh:  rune(dak[0]),
		handakuCh:  rune(han[0]),
	}, nil
}

func (j *japanese) Name() string { return j.name }

// ToKeys converts native kana text to its JIS kana keystroke artifact. Katakana
// is normalized to hiragana; voiced/semivoiced kana expand to base + mark key;
// anything not a kana passes through unchanged.
func (j *japanese) ToKeys(s string) []string {
	var b strings.Builder
	for _, r := range s {
		r = katakanaToHiragana(r)
		switch {
		case j.emitVoiced(&b, r):
			// handled
		default:
			if keys, ok := j.kana[r]; ok {
				b.WriteString(keys)
			} else {
				b.WriteRune(r)
			}
		}
	}
	return []string{b.String()}
}

// emitVoiced writes base+mark keystrokes if r is a (semi)voiced kana, returning
// whether it handled r.
func (j *japanese) emitVoiced(b *strings.Builder, r rune) bool {
	if base, ok := voiced[r]; ok {
		b.WriteString(j.kana[base])
		b.WriteString(j.dakuten)
		return true
	}
	if base, ok := semivoiced[r]; ok {
		b.WriteString(j.kana[base])
		b.WriteString(j.handakuten)
		return true
	}
	return false
}

// ToNative converts a JIS kana keystroke artifact back to kana. Each key maps to
// one base/small kana; a dakuten/handakuten key folds onto the pending base
// kana. Non-key characters flush the pending kana and pass through.
func (j *japanese) ToNative(s string) []string {
	var out strings.Builder
	var pending rune // 0 = none

	flush := func() {
		if pending != 0 {
			out.WriteRune(pending)
			pending = 0
		}
	}

	for _, c := range s {
		switch {
		case c == j.dakutenCh:
			if pending != 0 {
				if v, ok := dakutenCombine[pending]; ok {
					pending = v
					continue
				}
			}
			flush()
			out.WriteRune(c) // stray mark: pass the key through
		case c == j.handakuCh:
			if pending != 0 {
				if v, ok := handakuCombine[pending]; ok {
					pending = v
					continue
				}
			}
			flush()
			out.WriteRune(c)
		default:
			if kana, ok := j.rev[c]; ok {
				flush()
				pending = kana
			} else {
				flush()
				out.WriteRune(c)
			}
		}
	}
	flush()
	return []string{out.String()}
}

// katakanaToHiragana maps a katakana rune to its hiragana equivalent (same
// physical key). Non-katakana runes, and ー (U+30FC, shared by both), are
// returned unchanged.
func katakanaToHiragana(r rune) rune {
	if r >= 0x30A1 && r <= 0x30F6 {
		return r - 0x60
	}
	return r
}
