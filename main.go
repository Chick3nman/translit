package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
)

// version is printed by -version. Bump on release (or override at build time
// with -ldflags "-X main.version=...").
var version = "0.1.0"

// translit converts a wordlist between native-script text and the raw keystroke
// artifacts an IME would have produced, for use with hashcat --stdin.
//
//	cat phrases.txt | translit -lang ko -to keys    # 안녕  -> dkssud
//	cat artifacts.txt | translit -lang ko -to native # dkssud -> 안녕
//
// Reads UTF-8 lines from stdin, writes converted lines to stdout. One input
// line may yield several output lines when a conversion has multiple variants.
// With -keep-original, each input line is also echoed before its conversion(s)
// so the source word and its artifact both reach hashcat.
func main() {
	os.Exit(cli(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// cli parses args, builds the converter, and streams stdin to stdout. It returns
// the process exit code (0 ok, 2 usage error, 1 runtime error) and writes all
// diagnostics to stderr. It takes its I/O and args as parameters (and uses a
// fresh FlagSet) so the whole command-line contract is testable without globals
// or os.Exit.
func cli(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("translit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	lang := fs.String("lang", "korean", "source language: korean|ko | japanese|ja|jp | zhuyin|zh|bopomofo")
	layout := fs.String("layout", "", "keyboard layout (default per language): korean=2beolsik|3beolsik; japanese=jis-kana|jis-kana-us|romaji; zhuyin=dacheng|eten")
	to := fs.String("to", "keys", "direction: keys (native->keystrokes) | native (keystrokes->native) | both")
	keepOriginal := fs.Bool("keep-original", false, "also emit each original input line before its conversion(s)")
	maxHomophones := fs.Int("max-homophones", defaultMaxHomophones, "(zhuyin) reverse: max candidate chars per syllable")
	maxVariants := fs.Int("max-variants", defaultMaxVariants, "(zhuyin) max output candidates per input line (reverse fan-out / forward polyphone)")
	maxRank := fs.Int("max-rank", 0, "(zhuyin) reverse: only use chars with frequency rank <= N (0 = all common chars)")
	polyphone := fs.Bool("polyphone", false, "(zhuyin) forward: emit all readings of polyphonic chars, not just the primary")
	tone1Space := fs.Bool("tone1-space", true, "(zhuyin) emit a space for 1st-tone syllables (false = omit)")
	kanji := fs.Bool("kanji", false, "(japanese) resolve kanji words to kana via the embedded reading set before converting (forward)")
	maxReadings := fs.Int("max-readings", 0, "(zhuyin/kanji) cap readings kept per char/word (0 = all; 1 = primary only, trims rare readings)")
	maxPolyphone := fs.Int("max-polyphone", 0, "(zhuyin) forward: cap readings per polyphonic char, independent of -max-readings (0 = fall back to -max-readings); use with -polyphone")
	all := fs.Bool("all", false, "run EVERY language, layout, both directions, and kanji at once for maximum coverage (ignores -lang/-layout/-to)")
	showVersion := fs.Bool("version", false, "print version and exit")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0 // -h/-help printed usage; not an error
		}
		return 2
	}

	if *showVersion {
		fmt.Fprintf(stdout, "translit %s\n", version)
		return 0
	}

	opts := Options{
		MaxHomophones: *maxHomophones,
		MaxVariants:   *maxVariants,
		MaxRank:       *maxRank,
		Polyphone:     *polyphone,
		Tone1Space:    *tone1Space,
		Kanji:         *kanji,
		MaxReadings:   *maxReadings,
		MaxPolyphone:  *maxPolyphone,
	}

	var transform func(string) []string
	if *all {
		opts.Kanji = true // -all is maximum coverage: always resolve kanji too
		cs, err := allConverters(opts)
		if err != nil {
			fmt.Fprintf(stderr, "translit: %v\n", err)
			return 2
		}
		transform = allTransform(cs)
	} else {
		conv, err := newConverter(strings.ToLower(*lang), strings.ToLower(*layout), opts)
		if err != nil {
			fmt.Fprintf(stderr, "translit: %v\n", err)
			return 2
		}
		switch *to {
		case "keys":
			transform = conv.ToKeys
		case "native":
			transform = conv.ToNative
		case "both":
			transform = bothTransform(conv)
		default:
			fmt.Fprintf(stderr, "translit: unknown -to %q (use keys|native|both)\n", *to)
			return 2
		}
	}

	if err := run(stdin, stdout, transform, *keepOriginal); err != nil {
		fmt.Fprintf(stderr, "translit: %v\n", err)
		return 1
	}
	return 0
}

// bothTransform runs a line through both directions and concatenates the
// results (keystroke conversion first, then native). Because each direction
// passes through what it cannot convert, the opposite direction typically
// reproduces the input, so a native word and its artifact converge to the same
// pair of outputs. Per-line dedup in run() collapses any identical results.
func bothTransform(conv Converter) func(string) []string {
	return func(s string) []string {
		return append(conv.ToKeys(s), conv.ToNative(s)...)
	}
}

// allTransform runs a line through every converter in both directions (the -all
// mode). Per-line dedup in run() collapses the many identical passthroughs that
// different converters produce for the same input.
func allTransform(cs []Converter) func(string) []string {
	return func(s string) []string {
		var out []string
		for _, c := range cs {
			out = append(out, c.ToKeys(s)...)
			out = append(out, c.ToNative(s)...)
		}
		return out
	}
}

// run streams lines from r through transform to w. It uses bufio.Reader rather
// than Scanner so arbitrarily long wordlist lines are never truncated, and
// preserves each line's own terminator handling (strip CR/LF, re-add \n).
// When keepOriginal is set, each input line is echoed before its conversion(s).
//
// Output is deduplicated per input line only: a line never emits the same
// string twice (e.g. an unconvertible line under -keep-original, or two
// identical variants). It's a linear scan over a reused (zero-alloc) buffer
// holding just this line's outputs. That is trivial for the 1:1 languages, and O(k^2)
// but bounded by -max-variants for Zhuyin's fan-out. It is deliberately NOT
// global dedup across the stream, which would need an unbounded set and a
// per-line hash lookup, hurting fast-hash throughput.
func run(r io.Reader, w io.Writer, transform func(string) []string, keepOriginal bool) error {
	br := bufio.NewReader(r)
	bw := bufio.NewWriter(w)
	defer bw.Flush()

	writeLine := func(s string) error {
		if _, err := bw.WriteString(s); err != nil {
			return err
		}
		return bw.WriteByte('\n')
	}

	var emitted []string // reused across lines; holds this line's outputs only

	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			trimmed := strings.TrimRight(line, "\r\n")
			emitted = emitted[:0]
			if keepOriginal {
				if werr := writeLine(trimmed); werr != nil {
					return werr
				}
				emitted = append(emitted, trimmed)
			}
			for _, variant := range transform(trimmed) {
				if slices.Contains(emitted, variant) {
					continue
				}
				if werr := writeLine(variant); werr != nil {
					return werr
				}
				emitted = append(emitted, variant)
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
