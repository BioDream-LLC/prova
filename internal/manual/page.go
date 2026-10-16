package manual

import "strings"

// isHorizontalRule reports whether a line is a markdown thematic break.
//
// Requires three or more of the same character and nothing else. Checked after the table case in
// renderProse, which claims any line starting with a pipe, so a table's |---|---| separator never
// reaches this.
func isHorizontalRule(line string) bool {
	t := strings.TrimSpace(line)
	if len(t) < 3 {
		return false
	}

	c := t[0]
	if c != '-' && c != '*' && c != '_' {
		return false
	}
	for i := 0; i < len(t); i++ {
		if t[i] != c {
			return false
		}
	}

	return true
}

type heading struct {
	level int
	title string
}

// headingOf returns the heading a line represents, or nil if it is not one.
//
// Requires a space after the hashes, so that a line beginning with a hash for any other reason -
// a shell comment inside an unfenced block, a C preprocessor directive, an issue reference like
// #412 - is left as prose rather than silently becoming a heading.
func headingOf(line string) *heading {
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level == 0 || level > 6 {
		return nil
	}
	if level >= len(line) || line[level] != ' ' {
		return nil
	}

	title := strings.TrimSpace(line[level+1:])
	if title == "" {
		return nil
	}

	return &heading{level: level, title: title}
}

// isRawHTMLLine reports whether a line is a bare HTML tag that should be emitted as itself.
//
// Deliberately narrow: the line must begin at column zero with < or </, then a tag name, then a
// space, > or />. That matches the structural markup the README uses - <table>, <tr>, <details>,
// <summary>, <img>, <div> - and does not match prose that happens to contain a comparison, an
// arrow in a sentence, or an inline <code> reference, all of which must keep going through the
// markdown path and be escaped.
func isRawHTMLLine(line string) bool {
	if !strings.HasPrefix(line, "<") {
		return false
	}
	rest := strings.TrimPrefix(line[1:], "/")
	if rest == "" {
		return false
	}

	// A tag name, which must start with a letter.
	c := rest[0]
	if !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') {
		return false
	}

	for i := 0; i < len(rest); i++ {
		c := rest[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			continue
		case c == ' ' || c == '>' || c == '/' || c == '\n':
			// A tag name followed by something that can legally follow one.
			return true
		default:
			// Anything else means this was not a tag: an inequality, an arrow, prose.
			return false
		}
	}

	return false
}
