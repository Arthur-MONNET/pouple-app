package servicectx

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"unicode"

	"github.com/Arthur-MONNET/pouple/core/internal/connectors/slite"
	"github.com/Arthur-MONNET/pouple/core/internal/connectors/svchttp"
	"github.com/Arthur-MONNET/pouple/core/internal/engine"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
)

const (
	// maxQueryWords is how many words one Slite search carries: a long sentence finds nothing.
	maxQueryWords = 4
	// maxSearches bounds the attempts made when the first search finds nothing.
	maxSearches = 5
	// maxOpened is how many of the best notes are read in full, and how much of each.
	maxOpened     = 2
	maxOpenedText = 1500
	// maxChildren is how many notes of a folder are listed.
	maxChildren = 15
	// maxAskedText bounds the text of one answer from Slite's own assistant.
	maxAskedText = 2500
	// maxReadText bounds a note the assistant asked to read.
	maxReadText = 6000
)

// plainWords are words that name the tool or the act of searching, not what is searched: "slite" in "is there a place
// in Slite with…" matches nothing useful.
var plainWords = func() map[string]bool {
	set := map[string]bool{}
	for _, word := range strings.Fields(`slite asana gitlab note notes endroit liste leur leurs detail details trucs truc chose choses exemple cherche chercher trouve trouver regarde regarder dossier folder place thing things list`) {
		set[word] = true
	}
	return set
}()

// sliteQueries are the searches to try, most specific first. The longest words are the most telling, so a message
// is reduced to its few longest ones; if that finds nothing, to the two longest, then to each of the three longest.
func sliteQueries(words []string) []string {
	var kept []string
	for _, word := range words {
		if !plainWords[word] {
			kept = append(kept, word)
		}
	}
	if len(kept) == 0 {
		kept = words // every word was a plain one: better a poor search than none
	}
	ranked := slices.Clone(kept)
	slices.SortStableFunc(ranked, func(a, b string) int { return len([]rune(b)) - len([]rune(a)) })
	pick := func(n int) []string {
		best := ranked[:min(n, len(ranked))]
		var ordered []string
		for _, word := range kept { // the message's own order
			if slices.Contains(best, word) && !slices.Contains(ordered, word) {
				ordered = append(ordered, word)
			}
		}
		return ordered
	}
	var queries []string
	add := func(query string) {
		if query != "" && !slices.Contains(queries, query) {
			queries = append(queries, query)
		}
	}
	add(strings.Join(pick(maxQueryWords), " "))
	add(strings.Join(pick(2), " "))
	for _, word := range pick(3) {
		add(word)
	}
	return queries[:min(len(queries), maxSearches)]
}

// searchNotes runs the queries until one finds notes, then returns them; the last list says what was tried.
func searchNotes(ctx context.Context, client Slite, queries []string) (hits []slite.Hit, tried []string, err error) {
	seen := map[string]bool{}
	for _, query := range queries {
		found, searchErr := client.Search(ctx, query, maxNotes)
		tried = append(tried, query)
		if searchErr != nil {
			if len(hits) > 0 {
				return hits, tried, nil
			}
			return nil, tried, searchErr
		}
		for _, hit := range found {
			if !seen[hit.ID] {
				seen[hit.ID] = true
				hits = append(hits, hit)
			}
		}
		if len(hits) > 0 {
			break
		}
	}
	return hits[:min(len(hits), maxNotes)], tried, nil
}

func (g *Gatherer) slite(ctx context.Context, client Slite, words []string) section {
	out := section{source: slite.ID}
	hits, tried, err := searchNotes(ctx, client, sliteQueries(words))
	if err != nil {
		out.lines = append(out.lines, fmt.Sprintf("Slite, notes matching the message: unavailable (%s).", svchttp.Reason(err).Text(i18n.EN)))
		out.steps = append(out.steps, engine.ServiceStep{Source: slite.ID, Message: i18n.NewMessage("Slite: could not search (%s)", svchttp.Reason(err)), Failed: true})
		return out
	}
	last := tried[len(tried)-1]
	out.lines = append(out.lines, fmt.Sprintf("Slite, notes matching the message (search: %s; written by colleagues):", last))
	if len(tried) > 1 {
		out.lines = append(out.lines, fmt.Sprintf("(a longer search found nothing; searches tried: %s)", strings.Join(quoteAll(tried), ", ")))
	}
	opened := openNotes(ctx, client, hits)
	for i, hit := range hits {
		out.lines = append(out.lines, noteLine(hit))
		out.lines = append(out.lines, opened[i]...)
	}
	if len(hits) == 0 {
		out.lines = append(out.lines, "- (no note found; you can ask for another search, a folder or a note: see the lookups of the final answer)")
	}
	out.steps = append(out.steps, engine.ServiceStep{Source: slite.ID, Message: i18n.NewMessage("Slite: searched “%s” (%d notes)", last, len(hits))})
	return out
}

func quoteAll(texts []string) []string {
	quoted := make([]string, len(texts))
	for i, text := range texts {
		quoted[i] = quote(text, 80)
	}
	return quoted
}

// noteLine is one note, with the id the assistant can ask about.
func noteLine(hit slite.Hit) string {
	line := "- " + quote(hit.Title, maxTitle)
	var where []string
	if slite.ValidNoteID(hit.ID) {
		where = append(where, "id "+hit.ID)
	}
	if hit.Kind == "collection" {
		where = append(where, "folder")
	}
	if hit.Channel != "" {
		where = append(where, "in "+quote(hit.Channel, maxNameOrLabel))
	}
	if updated := date(hit.UpdatedAt); updated != "" {
		where = append(where, "updated "+updated)
	}
	if len(where) > 0 {
		line += " (" + strings.Join(where, ", ") + ")"
	}
	if hit.Excerpt != "" {
		line += ": excerpt " + quote(hit.Excerpt, maxExcerpt)
	}
	return line
}

// openNotes reads, in parallel and within the budget, the start of the best notes and the content of the folders
// among them. What cannot be read is left out: the note line stays and the assistant can ask again.
func openNotes(ctx context.Context, client Slite, hits []slite.Hit) [][]string {
	details := make([][]string, len(hits))
	var wait sync.WaitGroup
	for i, hit := range hits[:min(len(hits), maxOpened)] {
		if !slite.ValidNoteID(hit.ID) {
			continue
		}
		wait.Add(1)
		go func() {
			defer wait.Done()
			defer func() { _ = recover() }()
			if hit.Kind == "collection" {
				details[i] = folderLines(ctx, client, hit.ID)
				return
			}
			if note, err := client.Read(ctx, hit.ID, maxOpenedText); err == nil {
				details[i] = textLines("start of the note", note.Content, note.Truncated)
			}
		}()
	}
	wait.Wait()
	return details
}

// folderLines lists what a folder holds.
func folderLines(ctx context.Context, client Slite, id string) []string {
	children, total, err := client.Children(ctx, id, maxChildren)
	if err != nil {
		return nil
	}
	lines := []string{fmt.Sprintf("  inside this folder (%d notes in all, %d shown):", total, len(children))}
	for _, child := range children {
		lines = append(lines, "  "+noteLine(slite.Hit{ID: child.ID, Title: child.Title, UpdatedAt: child.UpdatedAt}))
	}
	return lines
}

// textLines shows what a colleague wrote as quoted lines: every line starts with a marker, so no line of a note can
// pass for a line of this prompt, and angle brackets are neutralised like everywhere else.
func textLines(label, text string, truncated bool) []string {
	var lines []string
	for _, raw := range strings.Split(text, "\n") {
		if line := strings.TrimRightFunc(clean(raw, 400), unicode.IsSpace); line != "" {
			lines = append(lines, "  | "+line)
		}
	}
	if len(lines) == 0 {
		return nil
	}
	head := "  " + label + " (written by colleagues, data):"
	if truncated {
		head = "  " + label + " (written by colleagues, data; cut):"
	}
	return append([]string{head}, lines...)
}

func (g *Gatherer) sliteLookup(ctx context.Context, client Slite, lookup engine.Lookup) section {
	out := section{source: slite.ID}
	target := strings.TrimSpace(lookup.Target)
	fail := func(err error, what string) section {
		out.lines = append(out.lines, fmt.Sprintf("Slite, %s: unavailable (%s).", what, svchttp.Reason(err).Text(i18n.EN)))
		out.steps = append(out.steps, engine.ServiceStep{Source: slite.ID, Message: i18n.NewMessage("Slite: %s failed (%s)", what, svchttp.Reason(err)), Failed: true})
		return out
	}
	switch lookup.Action {
	case "search":
		query := clean(target, 120)
		hits, _, err := searchNotes(ctx, client, []string{query})
		if err != nil {
			return fail(err, "search "+quote(query, 120))
		}
		out.lines = append(out.lines, fmt.Sprintf("Slite, search %s (written by colleagues):", quote(query, 120)))
		for _, hit := range hits {
			out.lines = append(out.lines, noteLine(hit))
		}
		if len(hits) == 0 {
			out.lines = append(out.lines, "- (no note found)")
		}
		out.steps = append(out.steps, engine.ServiceStep{Source: slite.ID, Message: i18n.NewMessage("Slite: searched “%s” (%d notes)", query, len(hits))})
	case "children":
		children, total, err := client.Children(ctx, target, maxChildren)
		if err != nil {
			return fail(err, "folder "+quote(target, 64))
		}
		out.lines = append(out.lines, fmt.Sprintf("Slite, inside note %s (%d notes in all, %d shown; written by colleagues):", quote(target, 64), total, len(children)))
		for _, child := range children {
			out.lines = append(out.lines, noteLine(slite.Hit{ID: child.ID, Title: child.Title, UpdatedAt: child.UpdatedAt}))
		}
		if len(children) == 0 {
			out.lines = append(out.lines, "- (nothing inside)")
		}
		out.steps = append(out.steps, engine.ServiceStep{Source: slite.ID, Message: i18n.NewMessage("Slite: opened the folder %s (%d notes)", quote(target, 64), len(children))})
	case "read":
		note, err := client.Read(ctx, target, maxReadText)
		if err != nil {
			return fail(err, "note "+quote(target, 64))
		}
		heading := "Slite, note " + quote(note.Title, maxTitle)
		if slite.ValidNoteID(note.ID) {
			heading += " (id " + note.ID + ")"
		}
		out.lines = append(out.lines, heading+":")
		out.lines = append(out.lines, textLines("text", note.Content, note.Truncated)...)
		out.steps = append(out.steps, engine.ServiceStep{Source: slite.ID, Message: i18n.NewMessage("Slite: read “%s”", clean(note.Title, maxTitle))})
	case "ask":
		question := clean(target, 500)
		answer, err := client.Ask(ctx, question)
		if err != nil {
			return fail(err, "question "+quote(question, 120))
		}
		out.lines = append(out.lines, fmt.Sprintf("Slite's own assistant answered the question %s (generated from the workspace: a lead to check, not a fact):", quote(question, 120)))
		text, cut := answer.Text, false
		if runes := []rune(text); len(runes) > maxAskedText {
			text, cut = string(runes[:maxAskedText]), true
		}
		out.lines = append(out.lines, textLines("answer", text, cut)...)
		for _, source := range answer.Sources {
			out.lines = append(out.lines, noteLine(slite.Hit{ID: source.ID, Title: source.Title}))
		}
		out.steps = append(out.steps, engine.ServiceStep{Source: slite.ID, Message: i18n.NewMessage("Slite: asked “%s”", question)})
	default:
		out.lines = append(out.lines, "Slite: this kind of lookup is not supported.")
	}
	return out
}
