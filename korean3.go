package main

import "strings"

// Korean 3-beolsik Final (세벌식 최종) is a three-set layout where initial
// consonants, vowels, and final consonants occupy *separate* keys (unlike
// 2-beolsik, where a consonant key is position-independent). Key tables are
// transcribed from a reference 3-beolsik Final keyboard definition (verified against its
// conjoining-jamo values).
//
// Because keys are position-explicit, this is a separate converter from
// 2-beolsik: its reverse automaton never has to guess whether a consonant is an
// initial or a final, and there is no final-migration rule. Doubled initials
// (ㄲㄸㅃㅆㅉ) and ㅗ/ㅜ-compound vowels (ㅘㅙㅚㅝㅞㅟ) have no dedicated key. They
// are formed by combination (double-press / two vowel keys), exactly as
// a standard combination table specifies.
//
// Precomposed syllables and standalone compatibility jamo (lone ㅋ, etc.) are
// both handled; NFD input is composed first (composeHangul), shared with
// 2-beolsik.

// choKey3f[L] -> key(s) for the initial at choseong index L. Doubles are the
// base initial pressed twice (combination).
var choKey3f = [19]string{
	"k", "kk", "h", "u", "uu", "y", "i", ";", ";;", "n",
	"nn", "j", "l", "ll", "o", "0", "'", "p", "m",
}

// jungKey3f[V] -> key(s) for the medial at jungseong index V. ㅗ/ㅜ-compounds are
// two vowel keys (combination). (ㅗ is also on "/", ㅜ on "9"; "v"/"b" are the
// canonical forward keys, both accepted on reverse.)
var jungKey3f = [21]string{
	"f", "r", "6", "G", "t", "c", "e", "7", "v", "vf",
	"vr", "vd", "4", "b", "bt", "bc", "bd", "5", "g", "8",
	"d",
}

// jongKey3f[T] -> key for the final at jongseong index T (0 = none). Every final,
// including every cluster, has a dedicated key, the hallmark of 3f Final.
var jongKey3f = [28]string{
	"", "x", "!", "V", "s", "E", "S", "A", "w", "@",
	"F", "D", "T", "%", "$", "R", "z", "3", "X", "q",
	"2", "a", "#", "Z", "C", "W", "Q", "1",
}

// doubleCho maps (initial index, same initial index) -> the doubled initial, the
// combination used to type ㄲㄸㅃㅆㅉ (none of which has its own key).
var doubleCho = map[[2]int]int{
	{0, 0}: 1, {3, 3}: 4, {7, 7}: 8, {9, 9}: 10, {12, 12}: 13,
}

type jamo3Kind int

const (
	kind3Cho jamo3Kind = iota
	kind3Jung
	kind3Jong
)

type jamo3 struct {
	kind jamo3Kind
	idx  int
}

// keyToJamo3f maps each physical key to its (position-explicit) jamo. Built from
// the single-key entries of the three tables; 3f keys never collide across
// positions, so the mapping is unambiguous.
var keyToJamo3f = buildKeyToJamo3f()

func buildKeyToJamo3f() map[rune]jamo3 {
	m := map[rune]jamo3{}
	for i, k := range choKey3f {
		if len(k) == 1 {
			m[rune(k[0])] = jamo3{kind3Cho, i}
		}
	}
	for i, k := range jungKey3f {
		if len(k) == 1 {
			m[rune(k[0])] = jamo3{kind3Jung, i}
		}
	}
	for i, k := range jongKey3f {
		if i == 0 || len(k) != 1 {
			continue
		}
		m[rune(k[0])] = jamo3{kind3Jong, i}
	}
	// Alternate vowel keys (same jamo on a second position).
	m['/'] = jamo3{kind3Jung, 8}  // ㅗ
	m['9'] = jamo3{kind3Jung, 13} // ㅜ
	return m
}

type korean3f struct{}

func newKorean3f() Converter { return &korean3f{} }

func (k *korean3f) Name() string { return "korean (3-beolsik final)" }

// ToKeys decomposes each precomposed Hangul syllable and maps its initial,
// medial, and final to 3f keys. Non-syllable runes (incl. standalone
// compatibility jamo, v1) pass through unchanged.
func (k *korean3f) ToKeys(s string) []string {
	s = composeHangul(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= hangulBase && r <= hangulLast:
			si := int(r) - hangulBase
			cho := si / (jungCount * jongCount)
			jung := (si % (jungCount * jongCount)) / jongCount
			jong := si % jongCount
			b.WriteString(choKey3f[cho])
			b.WriteString(jungKey3f[jung])
			b.WriteString(jongKey3f[jong]) // "" when jong == 0
		case r >= compatFirst && r <= compatLast:
			if keys := compat3fKeys(r); keys != "" {
				b.WriteString(keys)
			} else {
				b.WriteRune(r)
			}
		default:
			b.WriteRune(r)
		}
	}
	return []string{b.String()}
}

// compat3fKeys returns the 3f keystrokes for a standalone compatibility jamo:
// a lone consonant is typed as its initial; a compound-final-only consonant
// (ㄳ, …) as its final; a vowel (incl. compounds) as its medial. Returns "" for
// anything outside that set (caller passes it through).
func compat3fKeys(r rune) string {
	if i, ok := compatConsonantCho[r]; ok {
		return choKey3f[i]
	}
	if i, ok := compatCompoundJong[r]; ok {
		return jongKey3f[i]
	}
	if r >= 0x314F && r <= 0x3163 { // ㅏ..ㅣ vowel block, contiguous
		return jungKey3f[r-0x314F]
	}
	return ""
}

// compatConsonantCho maps a simple compatibility consonant to its choseong index
// (how a lone consonant is typed, as an initial).
var compatConsonantCho = map[rune]int{
	'ㄱ': 0, 'ㄲ': 1, 'ㄴ': 2, 'ㄷ': 3, 'ㄸ': 4, 'ㄹ': 5, 'ㅁ': 6, 'ㅂ': 7, 'ㅃ': 8, 'ㅅ': 9,
	'ㅆ': 10, 'ㅇ': 11, 'ㅈ': 12, 'ㅉ': 13, 'ㅊ': 14, 'ㅋ': 15, 'ㅌ': 16, 'ㅍ': 17, 'ㅎ': 18,
}

// compatCompoundJong maps a compound-final-only compatibility consonant (no
// choseong form) to its jongseong index.
var compatCompoundJong = map[rune]int{
	'ㄳ': 3, 'ㄵ': 5, 'ㄶ': 6, 'ㄺ': 9, 'ㄻ': 10, 'ㄼ': 11, 'ㄽ': 12, 'ㄾ': 13, 'ㄿ': 14, 'ㅀ': 15, 'ㅄ': 18,
}

// ToNative runs the 3f composition automaton. A new initial starts a new
// syllable; a repeated initial combines into a doubled initial; two vowels
// combine into a compound vowel; a final attaches to the current CV. There is no
// initial/final ambiguity and no migration.
func (k *korean3f) ToNative(s string) []string {
	var out strings.Builder
	a := automaton{cho: -1, jung: -1, jong: 0}
	active := false

	flush := func() {
		if active {
			out.WriteString(a.render())
			a = automaton{cho: -1, jung: -1, jong: 0}
			active = false
		}
	}

	for _, c := range s {
		j, ok := keyToJamo3f[c]
		if !ok {
			flush()
			out.WriteRune(c)
			continue
		}
		switch j.kind {
		case kind3Cho:
			switch {
			case !active:
				active = true
				a.cho = j.idx
			case a.cho != -1 && a.jung == -1:
				// Initial with no vowel + another initial: double it, else restart.
				if d, ok := doubleCho[[2]int{a.cho, j.idx}]; ok {
					a.cho = d
				} else {
					flush()
					active = true
					a.cho = j.idx
				}
			default:
				flush()
				active = true
				a.cho = j.idx
			}
		case kind3Jung:
			if !active {
				active = true
			}
			switch {
			case a.jung == -1:
				a.jung = j.idx
			case a.jong == 0:
				if comp, ok := jungCompound[[2]int{a.jung, j.idx}]; ok {
					a.jung = comp
				} else {
					flush()
					active = true
					a.jung = j.idx
				}
			default:
				flush()
				active = true
				a.jung = j.idx
			}
		case kind3Jong:
			if active && a.cho != -1 && a.jung != -1 && a.jong == 0 {
				a.jong = j.idx
			} else {
				// A final with no CV to attach to can't compose; pass the key.
				flush()
				out.WriteRune(c)
			}
		}
	}
	flush()
	return []string{out.String()}
}
