package main

// Hangul Compatibility Jamo (U+3131..U+3163) handling.
//
// These tables bridge two representations:
//   - syllable indices (choseong/jungseong/jongseong) used by the arithmetic
//     composition formula, and
//   - standalone compatibility jamo runes, which is how a lone, un-composed
//     consonant or vowel appears in text.
//
// They cover two edge paths that must never silently drop input:
//   - forward: a wordlist rune that is already a bare jamo (compatJamoKeys)
//   - reverse: the automaton holding a fragment that cannot form a full
//     syllable, e.g. a trailing lone consonant (compatConsonant/compatVowel).

// compatKeys maps each compatibility jamo to its 2-beolsik keystrokes, indexed
// by (rune - 0x3131). The block is contiguous and interleaves compound finals
// (ㄳ, ㄵ, …) and compound vowels (ㅘ, …) with the simple jamo, so the order
// here is literal rather than derived.
var compatKeys = [...]string{
	"r", "R", "rt", "s", "sw", "sg", "e", "E", "f", "fr", // ㄱ..ㄺ
	"fa", "fq", "ft", "fx", "fv", "fg", "a", "q", "Q", "qt", // ㄻ..ㅄ
	"t", "T", "d", "w", "W", "c", "z", "x", "v", "g", // ㅅ..ㅎ
	"k", "o", "i", "O", "j", "p", "u", "P", "h", "hk", // ㅏ..ㅘ
	"ho", "hl", "y", "n", "nj", "np", "nl", "b", "m", "ml", // ㅙ..ㅢ
	"l", // ㅣ
}

// choCompat maps a choseong (initial) index to its compatibility jamo rune.
// Not contiguous with choseong order because the compat block puts compound
// finals between the simple consonants.
var choCompat = [19]rune{
	0x3131, 0x3132, 0x3134, 0x3137, 0x3138, 0x3139, 0x3141, 0x3142, 0x3143, 0x3145,
	0x3146, 0x3147, 0x3148, 0x3149, 0x314A, 0x314B, 0x314C, 0x314D, 0x314E,
}

// jongToChoIdx maps a jongseong (final) index to the choseong index of the same
// consonant, or -1 if it has no initial form (none, or a compound final). Used
// when a final migrates to become the next syllable's initial.
var jongToChoIdx = [28]int{
	-1, 0, 1, -1, 2, -1, -1, 3, 5, -1,
	-1, -1, -1, -1, -1, -1, 6, 7, -1, 9,
	10, 11, 12, 14, 15, 16, 17, 18,
}

// compatJamoKeys returns the keystrokes for a standalone compatibility jamo
// rune. The caller guarantees r is within [compatFirst, compatLast].
func compatJamoKeys(r rune) string {
	return compatKeys[r-compatFirst]
}

// compatConsonant returns the standalone compatibility jamo for a choseong
// index (used when a syllable fragment has an initial but no vowel).
func compatConsonant(cho int) string {
	return string(choCompat[cho])
}

// compatVowel returns the standalone compatibility jamo for a jungseong index
// (used when a fragment has a vowel but no initial). The vowel portion of the
// compat block is contiguous starting at ㅏ (U+314F).
func compatVowel(jung int) string {
	return string(rune(0x314F + jung))
}

// jongToCho converts a jongseong index to the choseong index of the same
// consonant. Only ever called with simple finals, which always have an initial
// form, so the -1 slots are never hit in practice.
func jongToCho(jong int) int {
	return jongToChoIdx[jong]
}
