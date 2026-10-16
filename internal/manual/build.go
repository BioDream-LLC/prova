package manual

import (
	"fmt"
	"strings"
)

// order is the reading order of the written chapters. Written out rather than derived from file names: a manual's
// order is an argument — what a reader needs before the next thing makes sense — and whoever reorders it should have
// to edit this list and see what they are changing.
var order = []string{
	"introduction",
	"getting-started",
	"web-interface",
	"accounts",
	"scenarios",
	"protocols",
	"verdicts",
	"suites",
	"reports",
	"command-line",
	"deployment",
	"security",
}

// Build assembles the manual: the written chapters in reading order, followed by the generated reference chapters the
// caller supplies (the shipped scenarios and suites, the casebook, the server options), which are produced from the
// running code so they cannot describe something that does not exist.
func Build(version string, generated ...Chapter) (*Doc, error) {
	prose, err := LoadProse()
	if err != nil {
		return nil, err
	}
	d := &Doc{
		Title:    "prova Reference Manual",
		Subtitle: "A financial protocol test & proof harness: scenarios, verdicts, proofs and operation",
		Version:  version,
		Description: "Reference manual for prova, the financial protocol test and proof harness for ISO 20022, " +
			"ISO 8583 and FIX: scenarios, suites, formal verdicts, proof certificates, reporting and operation.",
	}
	used := map[string]bool{}
	for _, name := range order {
		src, ok := prose[name]
		if !ok {
			return nil, fmt.Errorf("manual: chapter %q is in the reading order but chapters/%s.md does not exist", name, name)
		}
		ch, err := parseProseChapter(src)
		if err != nil {
			return nil, fmt.Errorf("manual: chapter %q: %w", name, err)
		}
		d.Chapters = append(d.Chapters, *ch)
		used[name] = true
	}
	var unused []string
	for name := range prose {
		if !used[name] {
			unused = append(unused, name)
		}
	}
	if len(unused) > 0 {
		return nil, fmt.Errorf("manual: chapters/%s.md is not in the reading order", strings.Join(unused, ".md, chapters/"))
	}
	d.Chapters = append(d.Chapters, generated...)
	return d, nil
}

// Table renders rows as a manual table. Cells are escaped by the renderer; a "|" inside a cell is replaced so it cannot
// split the row.
func Table(header []string, rows [][]string) string {
	var b strings.Builder
	cell := func(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "|", "/"), "\n", " ") }
	b.WriteString("| " + strings.Join(header, " | ") + " |\n|")
	for range header {
		b.WriteString("---|")
	}
	b.WriteString("\n")
	for _, r := range rows {
		cs := make([]string, len(r))
		for i, c := range r {
			cs[i] = cell(c)
		}
		b.WriteString("| " + strings.Join(cs, " | ") + " |\n")
	}
	return b.String()
}

// parseProseChapter reads a written chapter file.
//
// The first heading is the chapter title, ## opens a section and ### a subsection. Anything before the first ## is the
// chapter introduction.
func parseProseChapter(src string) (*Chapter, error) {
	lines := strings.Split(src, "\n")
	ch := &Chapter{}

	var introLines []string
	var curSection *Section
	var curSub *Subsection
	var buf []string

	flush := func() {
		text := strings.TrimSpace(strings.Join(buf, "\n"))
		buf = nil
		switch {
		case curSub != nil:
			curSub.Body = text
		case curSection != nil:
			curSection.Body = text
		default:
			introLines = append(introLines, text)
		}
	}

	closeSub := func() {
		if curSub != nil && curSection != nil {
			curSection.Subsections = append(curSection.Subsections, *curSub)
			curSub = nil
		}
	}
	closeSection := func() {
		closeSub()
		if curSection != nil {
			ch.Sections = append(ch.Sections, *curSection)
			curSection = nil
		}
	}

	inFence := false
	for _, l := range lines {
		// Headings inside a code fence are content, not structure. Without this a yaml comment starting with ## would
		// silently split a chapter in two.
		if strings.HasPrefix(l, "```") {
			inFence = !inFence
			buf = append(buf, l)
			continue
		}
		if inFence {
			buf = append(buf, l)
			continue
		}

		switch {
		case strings.HasPrefix(l, "### "):
			flush()
			closeSub()
			if curSection == nil {
				return nil, fmt.Errorf("a subsection appears before any section: %q", l)
			}
			curSub = &Subsection{Title: strings.TrimSpace(strings.TrimPrefix(l, "### "))}

		case strings.HasPrefix(l, "## "):
			flush()
			closeSection()
			curSection = &Section{Title: strings.TrimSpace(strings.TrimPrefix(l, "## "))}

		case strings.HasPrefix(l, "# "):
			flush()
			if ch.Title != "" {
				return nil, fmt.Errorf("a chapter file has two titles; the second is %q", l)
			}
			ch.Title = strings.TrimSpace(strings.TrimPrefix(l, "# "))

		default:
			buf = append(buf, l)
		}
	}
	flush()
	closeSection()

	if ch.Title == "" {
		return nil, fmt.Errorf("a chapter file has no title line starting with a single #")
	}
	ch.Intro = strings.TrimSpace(strings.Join(introLines, "\n\n"))

	return ch, nil
}
