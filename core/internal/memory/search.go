package memory

import (
	"cmp"
	"slices"
	"strings"
	"unicode"
)

// maxHits is how many lines one search returns.
const maxHits = 15

// Hit is one line of a note that matches a search.
type Hit struct {
	Note Note   `json:"note"`
	Line string `json:"line"`
}

// Search finds the lines of the notes that hold the words of query, the lines with the most of them first. With shared
// set, private facts are never searched.
func (s *Store) Search(query string, shared bool) ([]Hit, error) {
	terms := searchTerms(query)
	if len(terms) == 0 {
		return nil, nil
	}
	notes, err := s.Notes(shared)
	if err != nil {
		return nil, err
	}
	type scored struct {
		hit   Hit
		score int
	}
	var found []scored
	for _, note := range notes {
		_, content, err := s.Read(note.Path, shared)
		if err != nil {
			continue
		}
		titleScore := matches(strings.ToLower(note.Title), terms)
		for line := range strings.SplitSeq(content, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "# ") {
				continue
			}
			if score := matches(strings.ToLower(trimmed), terms); score > 0 {
				found = append(found, scored{Hit{Note: note, Line: clipRunes(trimmed, MaxFact+120)}, score*2 + titleScore})
			}
		}
	}
	slices.SortStableFunc(found, func(a, b scored) int { return cmp.Compare(b.score, a.score) })
	hits := make([]Hit, 0, min(len(found), maxHits))
	for _, item := range found[:min(len(found), maxHits)] {
		hits = append(hits, item.hit)
	}
	return hits, nil
}

// searchTerms are the distinct words of a query, in lower case; a word of one letter finds everything and is left out.
func searchTerms(query string) []string {
	var terms []string
	for _, word := range strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len([]rune(word)) > 1 && !slices.Contains(terms, word) {
			terms = append(terms, word)
		}
	}
	return terms
}

func matches(text string, terms []string) int {
	count := 0
	for _, term := range terms {
		if strings.Contains(text, term) {
			count++
		}
	}
	return count
}
