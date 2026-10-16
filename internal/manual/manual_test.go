package manual

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var linkRe = regexp.MustCompile(`\]\(#([a-z0-9-]+)\)`)

func build(t *testing.T) *Doc {
	t.Helper()
	// The reference chapters the server generates, as stand-ins: the written chapters link to them.
	var gen []Chapter
	for _, title := range []string{"Scenario Reference", "Suite Reference", "Target Reference", "Server Options", "API Reference"} {
		gen = append(gen, Chapter{Title: title, Intro: Table([]string{"ID", "Name"}, [][]string{{"a", "b"}})})
	}
	d, err := Build("test", gen...)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// Cross-references are written as slugs; one that does not resolve would ship as a link that goes nowhere.
func TestEveryCrossReferenceResolves(t *testing.T) {
	anchors := build(t).Anchors()
	prose, err := LoadProse()
	if err != nil {
		t.Fatal(err)
	}
	var broken []string
	for name, src := range prose {
		for _, m := range linkRe.FindAllStringSubmatch(src, -1) {
			if _, ok := anchors[m[1]]; !ok {
				broken = append(broken, fmt.Sprintf("%s.md links to #%s", name, m[1]))
			}
		}
	}
	sort.Strings(broken)
	if len(broken) > 0 {
		t.Errorf("cross-references that do not resolve:\n  %s", strings.Join(broken, "\n  "))
	}
}

// The manual is one file that opens anywhere: nothing it shows may be fetched from elsewhere.
func TestTheManualIsSelfContained(t *testing.T) {
	html := build(t).HTML()
	for _, bad := range []string{"<script", `src="http`, "@import", "url(http", "<link "} {
		if strings.Contains(html, bad) {
			t.Errorf("the manual contains %q", bad)
		}
	}
	if !strings.Contains(html, "<style>") {
		t.Error("no embedded stylesheet")
	}
}

func TestNotesTablesAndCodeRender(t *testing.T) {
	html := build(t).HTML()
	for _, want := range []string{`class="note"`, "<table>", `<pre class="code">`, `class="toc"`} {
		if !strings.Contains(html, want) {
			t.Errorf("the manual has no %s", want)
		}
	}
	if strings.Contains(html, "<p>&gt; ") || strings.Contains(html, "<td>---</td>") {
		t.Error("markup rendered as literal text")
	}
}

func TestAHeadingInsideACodeFenceDoesNotSplitTheChapter(t *testing.T) {
	ch, err := parseProseChapter("# Title\n\n## Real\n\n```\n## not a heading\n```\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(ch.Sections) != 1 || !strings.Contains(ch.Sections[0].Body, "## not a heading") {
		t.Fatalf("%+v", ch.Sections)
	}
}
