package main

import "strings"

// Korean: Hangul <-> 2-beolsik (두벌식, KS X 5002 standard) keystrokes.
//
// Forward (native->keys) is pure arithmetic: a precomposed Hangul syllable
// decomposes into (initial, medial, final) indices by the standard formula, and
// each jamo maps to a fixed key sequence (compound jamo = two keys; doubled
// consonants and ㅒ/ㅖ require shift, which surfaces as an uppercase ASCII key).
//
// Reverse (keys->native) runs a composition automaton: map each key back to a
// jamo, accumulate into a syllable, and when a complete L+V+T syllable is
// followed by a vowel, migrate the trailing consonant to become the next
// syllable's initial (the "하나" rule). This mirrors how a real 2-beolsik IME
// composes, so forward then reverse round-trips.

const (
	hangulBase  = 0xAC00
	hangulLast  = 0xD7A3
	jungCount   = 21
	jongCount   = 28
	compatFirst = 0x3131 // ㄱ, first Hangul Compatibility Jamo
	compatLast  = 0x3163 // ㅣ, last single-jamo we handle
)

// choKeys[L] -> keystrokes for the initial consonant at choseong index L (0..18).
var choKeys = [19]string{
	"r", "R", "s", "e", "E", "f", "a", "q", "Q", "t",
	"T", "d", "w", "W", "c", "z", "x", "v", "g",
}

// jungKeys[V] -> keystrokes for the medial vowel at jungseong index V (0..20).
// Compound vowels are the two keys the IME combines (ㅘ = ㅗ+ㅏ = "hk").
var jungKeys = [21]string{
	"k", "o", "i", "O", "j", "p", "u", "P", "h", "hk",
	"ho", "hl", "y", "n", "nj", "np", "nl", "b", "m", "ml",
	"l",
}

// jongKeys[T] -> keystrokes for the final consonant at jongseong index T (0..27).
// Index 0 is "no final" and emits nothing. Compound finals are two keys.
var jongKeys = [28]string{
	"", "r", "R", "rt", "s", "sw", "sg", "e", "f", "fr",
	"fa", "fq", "ft", "fx", "fv", "fg", "a", "q", "qt", "t",
	"T", "d", "w", "c", "z", "x", "v", "g",
}

// A jamo is one phonetic unit the automaton tracks. kind distinguishes
// consonants (which can be an initial or a final) from vowels (medials).
type jamoKind int

const (
	kindConsonant jamoKind = iota
	kindVowel
)

type jamo struct {
	kind jamoKind
	// For a consonant: index into choKeys when used as an initial, and index
	// into jongKeys when used as a final (0 if it cannot be a final).
	cho, jong int
	// For a vowel: index into jungKeys.
	jung int
}

// keyToJamo maps a single physical key (US-QWERTY, case-sensitive) to its jamo.
// Built once at init from the key tables so the forward and reverse directions
// cannot drift apart.
var keyToJamo = buildKeyToJamo()

func buildKeyToJamo() map[rune]jamo {
	m := map[rune]jamo{}

	// Consonants: a letter is a consonant jamo. Record its initial index and,
	// if it is a legal final, its final index. Only single-key jamo seed the
	// map here; compound finals are assembled by the automaton from two keys.
	for cho, k := range choKeys {
		r := rune(k[0])
		j := m[r]
		j.kind = kindConsonant
		j.cho = cho
		m[r] = j
	}
	for jong, k := range jongKeys {
		if len(k) != 1 {
			continue // compound final: built by combining two single finals
		}
		r := rune(k[0])
		j := m[r]
		j.kind = kindConsonant
		j.jong = jong
		m[r] = j
	}
	// Vowels: only single-key vowels seed the map; compound vowels (ㅘ etc.)
	// are assembled by the automaton from two vowel keys.
	for jung, k := range jungKeys {
		if len(k) != 1 {
			continue
		}
		r := rune(k[0])
		m[r] = jamo{kind: kindVowel, jung: jung}
	}
	return m
}

// jungCompound[a][b] -> combined medial index when vowel a is followed by
// vowel b (ㅗ+ㅏ=ㅘ). Absent pair means the two vowels do not combine.
var jungCompound = map[[2]int]int{
	{8, 0}:   9,  // ㅗ+ㅏ=ㅘ
	{8, 1}:   10, // ㅗ+ㅐ=ㅙ
	{8, 20}:  11, // ㅗ+ㅣ=ㅚ
	{13, 4}:  14, // ㅜ+ㅓ=ㅝ
	{13, 5}:  15, // ㅜ+ㅔ=ㅞ
	{13, 20}: 16, // ㅜ+ㅣ=ㅟ
	{18, 20}: 19, // ㅡ+ㅣ=ㅢ
}

// jongCompound[a][b] -> combined final index when final a is followed by
// consonant b (ㄱ+ㅅ=ㄳ). Keyed by jongseong index of a and the consonant's
// own final index for b.
var jongCompound = map[[2]int]int{
	{1, 19}:  3,  // ㄱ+ㅅ=ㄳ
	{4, 22}:  5,  // ㄴ+ㅈ=ㄵ
	{4, 27}:  6,  // ㄴ+ㅎ=ㄶ
	{8, 1}:   9,  // ㄹ+ㄱ=ㄺ
	{8, 16}:  10, // ㄹ+ㅁ=ㄻ
	{8, 17}:  11, // ㄹ+ㅂ=ㄼ
	{8, 19}:  12, // ㄹ+ㅅ=ㄽ
	{8, 25}:  13, // ㄹ+ㅌ=ㄾ
	{8, 26}:  14, // ㄹ+ㅍ=ㄿ
	{8, 27}:  15, // ㄹ+ㅎ=ㅀ
	{17, 19}: 18, // ㅂ+ㅅ=ㅄ
}

// jongSplit reverses jongCompound: a compound final index -> its two component
// jongseong indices, so the automaton can migrate the trailing component.
var jongSplit = buildJongSplit()

func buildJongSplit() map[int][2]int {
	m := map[int][2]int{}
	// consonant jong index for key -> used to recover the second component's
	// jongseong index from jongCompound keys.
	for pair, combined := range jongCompound {
		// pair[0] is the first component's jongseong index; pair[1] is the
		// second consonant's own final index.
		m[combined] = [2]int{pair[0], pair[1]}
	}
	return m
}

type korean struct{}

func newKorean() *korean { return &korean{} }

// composeHangul folds NFD conjoining jamo (L block U+1100, V block U+1161, T
// block U+11A8) into precomposed syllables, so NFD-normalized input converts
// like NFC. NFC input is unaffected, since it has no L-followed-by-V runs. Shared by
// both Korean converters' forward path.
func composeHangul(s string) string {
	rs := []rune(s)
	out := make([]rune, 0, len(rs))
	for i := 0; i < len(rs); {
		r := rs[i]
		if r >= 0x1100 && r <= 0x1112 && i+1 < len(rs) && rs[i+1] >= 0x1161 && rs[i+1] <= 0x1175 {
			l := int(r - 0x1100)
			v := int(rs[i+1] - 0x1161)
			t, adv := 0, 2
			if i+2 < len(rs) && rs[i+2] >= 0x11A8 && rs[i+2] <= 0x11C2 {
				t = int(rs[i+2] - 0x11A7)
				adv = 3
			}
			out = append(out, rune(hangulBase+(l*jungCount+v)*jongCount+t))
			i += adv
		} else {
			out = append(out, r)
			i++
		}
	}
	return string(out)
}

func (k *korean) Name() string { return "korean (2-beolsik)" }

// ToKeys converts native Hangul text to its 2-beolsik keystroke artifact.
// Non-Hangul runes (ASCII, digits, symbols) pass through unchanged so mixed
// passwords like "안녕1234!" become "dkssud1234!".
func (k *korean) ToKeys(s string) []string {
	s = composeHangul(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= hangulBase && r <= hangulLast:
			si := int(r) - hangulBase
			cho := si / (jungCount * jongCount)
			jung := (si % (jungCount * jongCount)) / jongCount
			jong := si % jongCount
			b.WriteString(choKeys[cho])
			b.WriteString(jungKeys[jung])
			b.WriteString(jongKeys[jong]) // "" when jong == 0
		case r >= compatFirst && r <= compatLast:
			// A standalone compatibility jamo (lone consonant/vowel).
			b.WriteString(compatJamoKeys(r))
		default:
			b.WriteRune(r)
		}
	}
	return []string{b.String()}
}

// ToNative converts a 2-beolsik keystroke artifact back to Hangul. Runs of
// jamo-producing keys feed the composition automaton; any other character
// flushes the current syllable and passes through unchanged.
func (k *korean) ToNative(s string) []string {
	var out strings.Builder
	var a automaton
	for _, r := range s {
		j, ok := keyToJamo[r]
		if !ok {
			out.WriteString(a.flush())
			out.WriteRune(r)
			continue
		}
		out.WriteString(a.feed(j))
	}
	out.WriteString(a.flush())
	return []string{out.String()}
}

// automaton accumulates jamo into a single in-progress syllable. The three
// slots mirror a Hangul syllable: cho (initial), jung (medial), jong (final).
// -1 means "unset"; jong uses 0 for "no final" to match the index tables.
type automaton struct {
	cho, jung, jong int
	active          bool // true once any slot is set
}

func newSyllable() automaton { return automaton{cho: -1, jung: -1, jong: 0} }

// feed advances the automaton by one jamo and returns any syllable(s) that got
// finalized as a result (as native text). The in-progress syllable stays in the
// automaton until a later jamo forces it out or flush() is called.
func (a *automaton) feed(j jamo) string {
	if !a.active {
		*a = newSyllable()
		a.active = true
	}
	if j.kind == kindConsonant {
		return a.feedConsonant(j)
	}
	return a.feedVowel(j)
}

func (a *automaton) feedConsonant(j jamo) string {
	switch {
	case a.cho == -1:
		if a.jung != -1 {
			// A lone vowel is already in progress; a consonant cannot attach as
			// its initial (an initial precedes its medial). Emit the lone vowel
			// and begin a fresh syllable with this consonant.
			out := a.render()
			*a = newSyllable()
			a.active = true
			a.cho = j.cho
			return out
		}
		// Nothing started yet (or just flushed): this consonant is the initial.
		a.cho = j.cho
		return ""
	case a.jung == -1:
		// Initial with no vowel, another consonant arrives: the two cannot
		// share a syllable. Emit the lone initial and restart.
		out := compatConsonant(a.cho)
		*a = newSyllable()
		a.active = true
		a.cho = j.cho
		return out
	case a.jong == 0:
		// L+V so far: try this consonant as the final.
		if j.jong != 0 {
			a.jong = j.jong
			return ""
		}
		// Not a legal final (shouldn't happen for real keys): flush and restart.
		out := a.render()
		*a = newSyllable()
		a.active = true
		a.cho = j.cho
		return out
	default:
		// L+V+T already: try to combine into a compound final.
		if comb, ok := jongCompound[[2]int{a.jong, j.jong}]; ok {
			a.jong = comb
			return ""
		}
		// Otherwise finalize this syllable and start the next with this initial.
		out := a.render()
		*a = newSyllable()
		a.active = true
		a.cho = j.cho
		return out
	}
}

func (a *automaton) feedVowel(j jamo) string {
	switch {
	case a.jung == -1:
		// First vowel of the syllable (cho may be -1 for a lone vowel).
		a.jung = j.jung
		return ""
	case a.jong == 0:
		// Two vowels, no final yet: try a compound vowel (ㅗ+ㅏ=ㅘ).
		if comb, ok := jungCompound[[2]int{a.jung, j.jung}]; ok {
			a.jung = comb
			return ""
		}
		// Not combinable: finalize current, start a vowel-only syllable.
		out := a.render()
		*a = newSyllable()
		a.active = true
		a.jung = j.jung
		return out
	default:
		// L+V+T followed by a vowel: the final migrates to become the next
		// syllable's initial (하나 rule). For a compound final, only the
		// trailing component migrates.
		var migrate int // jongseong index of the consonant that moves
		if parts, ok := jongSplit[a.jong]; ok {
			a.jong = parts[0]
			migrate = parts[1]
		} else {
			migrate = a.jong
			a.jong = 0
		}
		out := a.render()
		*a = newSyllable()
		a.active = true
		a.cho = jongToCho(migrate)
		a.jung = j.jung
		return out
	}
}

// flush finalizes and returns whatever syllable is in progress, then resets.
func (a *automaton) flush() string {
	if !a.active {
		return ""
	}
	out := a.render()
	*a = automaton{}
	return out
}

// render turns the current slots into native text. A full L+V(+T) syllable
// composes to a precomposed Hangul rune; partial fragments fall back to their
// standalone compatibility jamo (rare in real artifacts, but never dropped).
func (a *automaton) render() string {
	switch {
	case a.cho >= 0 && a.jung >= 0:
		code := hangulBase + (a.cho*jungCount+a.jung)*jongCount + a.jong
		return string(rune(code))
	case a.cho >= 0 && a.jung == -1:
		return compatConsonant(a.cho)
	case a.cho == -1 && a.jung >= 0:
		return compatVowel(a.jung)
	default:
		return ""
	}
}
