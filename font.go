package main

import "strings"

// @region ui:font -- BIG-DIGIT GLYPH TABLES + RENDERER

// glyph tables are borrowed from pom. only the runes the
// app actually renders are present: digits, colon, space, and the letters of
// "DONE".
type bigFont struct {
	rows   int
	glyphs map[rune][]string
}

// solid slab, 5 rows. verticals are 2 cols wide so they read the same weight as
// the 1-row horizontals, since terminal cells are roughly 2:1 tall.
var fontSlab = bigFont{rows: 5, glyphs: map[rune][]string{
	'0': {"██████", "██  ██", "██  ██", "██  ██", "██████"},
	'1': {"  ████", "    ██", "    ██", "    ██", "    ██"},
	'2': {"██████", "    ██", "██████", "██    ", "██████"},
	'3': {"██████", "    ██", "██████", "    ██", "██████"},
	'4': {"██  ██", "██  ██", "██████", "    ██", "    ██"},
	'5': {"██████", "██    ", "██████", "    ██", "██████"},
	'6': {"██████", "██    ", "██████", "██  ██", "██████"},
	'7': {"██████", "    ██", "    ██", "    ██", "    ██"},
	'8': {"██████", "██  ██", "██████", "██  ██", "██████"},
	'9': {"██████", "██  ██", "██████", "    ██", "    ██"},
	':': {"  ", "██", "  ", "██", "  "},
	' ': {"   ", "   ", "   ", "   ", "   "},
	'O': {"██████", "██  ██", "██  ██", "██  ██", "██████"},
	'D': {"█████ ", "██  ██", "██  ██", "██  ██", "█████ "},
	'N': {"██  ██", "███ ██", "██████", "██ ███", "██  ██"},
	'E': {"██████", "██    ", "██████", "██    ", "██████"},
}}

// half-block compact, 3 rows. the fallback for short terminals.
var fontCompact = bigFont{rows: 3, glyphs: map[rune][]string{
	'0': {"▛▀▜", "▌ ▐", "▙▄▟"},
	'1': {" ▛▌", "  ▌", "  ▌"},
	'2': {"▀▀▜", "▛▀▘", "▙▄▄"},
	'3': {"▀▀▜", " ▀▜", "▄▄▟"},
	'4': {"▌ ▌", "▙▄▌", "  ▌"},
	'5': {"▛▀▀", "▀▀▜", "▄▄▟"},
	'6': {"▛▀▀", "▛▀▜", "▙▄▟"},
	'7': {"▀▀▜", "  ▌", "  ▌"},
	'8': {"▛▀▜", "▛▀▜", "▙▄▟"},
	'9': {"▛▀▜", "▙▄▌", "▄▄▟"},
	':': {"▪", " ", "▪"},
	' ': {"  ", "  ", "  "},
	'O': {"▛▀▜", "▌ ▐", "▙▄▟"},
	'D': {"▛▀▖", "▌ ▐", "▙▄▘"},
	'N': {"▛▖▐", "▌▝▖", "▌ ▐"},
	'E': {"▛▀▀", "▙▄▖", "▙▄▄"},
}}

const glyphGap = 1

// renders text as rows of glyph art. unknown runes are skipped.
func (f bigFont) render(s string) []string {
	out := make([]string, f.rows)
	first := true
	for _, ch := range strings.ToUpper(s) {
		g, ok := f.glyphs[ch]
		if !ok {
			continue
		}
		if !first {
			for r := range out {
				out[r] += spaces(glyphGap)
			}
		}
		for r := range out {
			out[r] += g[r]
		}
		first = false
	}
	return out
}

func (f bigFont) width(s string) int {
	rows := f.render(s)
	if len(rows) == 0 {
		return 0
	}
	return len([]rune(rows[0]))
}

// picks the font for the space available. compact is used when the panel is too
// short for 5 rows of art, or when the slab render would not fit the width.
func pickFont(s string, availW, termH int) bigFont {
	if termH < 16 {
		return fontCompact
	}
	if fontSlab.width(s) > availW {
		return fontCompact
	}
	return fontSlab
}
