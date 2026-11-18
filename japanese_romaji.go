package main

import "strings"

// Japanese rōmaji (wāpuro) input. Unlike jis-kana, rōmaji input types Latin
// letters directly, so the IME-off artifact is the *readable* rōmaji itself
// (わたし -> "watashi"), not gibberish.
//
// Forward maps kana to a canonical rōmaji; reverse parses rōmaji back to kana by
// greedy longest match. Like Zhuyin, this is NOT perfectly bijective, since rōmaji is
// inherently lossy (especially around ん and single/double consonants). It
// round-trips for the canonical forms produced here.
//
// Conventions (wāpuro / Hepburn-ish):
//   - consonants: shi, chi, tsu, fu, ji
//   - ぢ/づ -> di/du, to stay distinct from じ/ず (ji/zu)
//   - ん -> "nn" (unambiguous; reverse also accepts a lone "n")
//   - sokuon っ -> doubles the next syllable's leading consonant (きって -> "kitte")
//   - long vowel ー -> "-"
//   - standalone small kana use an "l" prefix (ぁ -> "la", っ -> "ltu")

var kanaRomaji = map[rune]string{
	'あ': "a", 'い': "i", 'う': "u", 'え': "e", 'お': "o",
	'か': "ka", 'き': "ki", 'く': "ku", 'け': "ke", 'こ': "ko",
	'が': "ga", 'ぎ': "gi", 'ぐ': "gu", 'げ': "ge", 'ご': "go",
	'さ': "sa", 'し': "shi", 'す': "su", 'せ': "se", 'そ': "so",
	'ざ': "za", 'じ': "ji", 'ず': "zu", 'ぜ': "ze", 'ぞ': "zo",
	'た': "ta", 'ち': "chi", 'つ': "tsu", 'て': "te", 'と': "to",
	'だ': "da", 'ぢ': "di", 'づ': "du", 'で': "de", 'ど': "do",
	'な': "na", 'に': "ni", 'ぬ': "nu", 'ね': "ne", 'の': "no",
	'は': "ha", 'ひ': "hi", 'ふ': "fu", 'へ': "he", 'ほ': "ho",
	'ば': "ba", 'び': "bi", 'ぶ': "bu", 'べ': "be", 'ぼ': "bo",
	'ぱ': "pa", 'ぴ': "pi", 'ぷ': "pu", 'ぺ': "pe", 'ぽ': "po",
	'ま': "ma", 'み': "mi", 'む': "mu", 'め': "me", 'も': "mo",
	'や': "ya", 'ゆ': "yu", 'よ': "yo",
	'ら': "ra", 'り': "ri", 'る': "ru", 'れ': "re", 'ろ': "ro",
	'わ': "wa", 'を': "wo", 'ん': "nn",
	'ゔ': "vu", 'ー': "-",
	// standalone small kana
	'ぁ': "la", 'ぃ': "li", 'ぅ': "lu", 'ぇ': "le", 'ぉ': "lo",
	'ゃ': "lya", 'ゅ': "lyu", 'ょ': "lyo", 'っ': "ltu", 'ゎ': "lwa",
}

// digraphPrefix + smallVowel compose palatalized syllables (きゃ = ky+a = "kya",
// しゃ = sh+a = "sha").
var digraphPrefix = map[rune]string{
	'き': "ky", 'ぎ': "gy", 'し': "sh", 'じ': "j", 'ち': "ch", 'ぢ': "dy",
	'に': "ny", 'ひ': "hy", 'び': "by", 'ぴ': "py", 'み': "my", 'り': "ry",
}
var smallVowel = map[rune]string{'ゃ': "a", 'ゅ': "u", 'ょ': "o"}

// romajiKana is the reverse index: rōmaji token -> kana (1-2 kana). Built from
// the forward tables plus a lone-"n" alias and the composed digraphs.
var romajiKana = buildRomajiKana()

func buildRomajiKana() map[string]string {
	m := make(map[string]string, len(kanaRomaji)+40)
	for k, v := range kanaRomaji {
		m[v] = string(k)
	}
	m["n"] = "ん" // accept a lone "n" as well as "nn"
	for base, pre := range digraphPrefix {
		for small, vow := range smallVowel {
			m[pre+vow] = string(base) + string(small)
		}
	}
	return m
}

type romaji struct{}

func newRomaji() Converter { return &romaji{} }

func (r *romaji) Name() string { return "japanese (romaji)" }

// ToKeys converts kana to canonical rōmaji. Katakana normalizes to hiragana;
// non-kana passes through unchanged.
func (r *romaji) ToKeys(s string) []string {
	runes := []rune(s)
	var b strings.Builder
	for i := 0; i < len(runes); {
		c := katakanaToHiragana(runes[i])

		// Digraph: palatal base + small ゃ/ゅ/ょ.
		if pre, ok := digraphPrefix[c]; ok && i+1 < len(runes) {
			if v, ok2 := smallVowel[katakanaToHiragana(runes[i+1])]; ok2 {
				b.WriteString(pre + v)
				i += 2
				continue
			}
		}

		// Sokuon: double the next syllable's leading consonant.
		if c == 'っ' {
			if nr := peekRomaji(runes, i+1); nr != "" && isConsonantByte(nr[0]) {
				b.WriteByte(nr[0])
			} else {
				b.WriteString("ltu")
			}
			i++
			continue
		}

		if rom, ok := kanaRomaji[c]; ok {
			b.WriteString(rom)
			i++
			continue
		}
		b.WriteRune(runes[i]) // passthrough (original, not normalized)
		i++
	}
	return []string{b.String()}
}

// peekRomaji returns the rōmaji the unit at index j would emit (digraph or
// monograph), used for sokuon doubling. Empty if j is past the end or non-kana.
func peekRomaji(runes []rune, j int) string {
	if j >= len(runes) {
		return ""
	}
	c := katakanaToHiragana(runes[j])
	if pre, ok := digraphPrefix[c]; ok && j+1 < len(runes) {
		if v, ok2 := smallVowel[katakanaToHiragana(runes[j+1])]; ok2 {
			return pre + v
		}
	}
	return kanaRomaji[c] // "" if not a kana
}

// ToNative parses rōmaji back to kana by greedy longest match, with explicit
// handling for sokuon (doubled consonant) and ん. Unmatched bytes pass through.
func (r *romaji) ToNative(s string) []string {
	var b strings.Builder
	for i := 0; i < len(s); {
		// Sokuon: a doubled consonant (not "nn", not a vowel) is っ + the rest.
		if i+1 < len(s) && s[i] == s[i+1] && isConsonantByte(s[i]) && s[i] != 'n' {
			b.WriteString("っ")
			i++
			continue
		}

		// ん: "nn" -> ん; a lone "n" at end or before a consonant -> ん; a lone
		// "n" before a vowel or "y" is the onset of a な/や-row syllable, so fall
		// through to longest match.
		if s[i] == 'n' {
			if i+1 < len(s) && s[i+1] == 'n' {
				b.WriteString("ん")
				i += 2
				continue
			}
			if i+1 >= len(s) || (!isVowelByte(s[i+1]) && s[i+1] != 'y') {
				b.WriteString("ん")
				i++
				continue
			}
		}

		matched := false
		for l := 3; l >= 1; l-- {
			if i+l <= len(s) {
				if kana, ok := romajiKana[s[i:i+l]]; ok {
					b.WriteString(kana)
					i += l
					matched = true
					break
				}
			}
		}
		if !matched {
			b.WriteByte(s[i])
			i++
		}
	}
	return []string{b.String()}
}

func isVowelByte(c byte) bool {
	switch c {
	case 'a', 'e', 'i', 'o', 'u':
		return true
	}
	return false
}

func isConsonantByte(c byte) bool {
	return c >= 'a' && c <= 'z' && !isVowelByte(c)
}
