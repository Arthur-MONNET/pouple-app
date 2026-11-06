package servicectx

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// clean makes a third-party string safe to put in a prompt line: one line, no control or invisible formatting
// characters, no angle brackets (they could open or close a delimiter), and a bounded length.
func clean(text string, max int) string {
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return ' '
		}
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, text)
	text = strings.Join(strings.Fields(text), " ")
	if runes := []rune(text); len(runes) > max {
		text = string(runes[:max-1]) + "…"
	}
	return strings.NewReplacer("<", "‹", ">", "›").Replace(text)
}

// quote is clean plus quotation marks and escapes: a title is shown as a title, never as a sentence of the prompt.
func quote(text string, max int) string { return strconv.Quote(clean(text, max)) }

var (
	enumeration = regexp.MustCompile(`^[a-z_]{1,40}$`)
	reference   = regexp.MustCompile(`^[A-Za-z0-9_./-]{1,150}$`)
	day         = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`)
)

// word keeps a status that must be a plain lowercase identifier (a pipeline status), else "unknown".
func word(text string) string {
	if enumeration.MatchString(text) {
		return text
	}
	return "unknown"
}

// date keeps the day of an ISO date or time, "" when it is not one.
func date(text string) string { return day.FindString(text) }

// link keeps a web address that is plain enough to show, without its query or fragment.
func link(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil {
		return ""
	}
	parsed.RawQuery, parsed.Fragment = "", ""
	text := parsed.String()
	if len(text) > 300 || strings.ContainsAny(text, " \"<>") {
		return ""
	}
	return text
}

// path shows a project path as it is when it only has path characters, else quoted.
func path(text string) string {
	if reference.MatchString(text) {
		return text
	}
	return quote(text, 150)
}

const (
	minSearchWords = 2
	maxSearchWords = 12
)

var stopwords = func() map[string]bool {
	set := map[string]bool{}
	for _, word := range strings.Fields(`le la les un une des de du d l et ou mais donc que qui quoi est sont pour par sur dans avec sans ce cet cette ces mon ma mes ton ta tes son sa ses nous vous ils elles je tu il elle on ne pas plus au aux en y a the a an and or but of to in on for with is are was be this that it my your can could you please merci salut bonjour hello hi peux veux voudrais fait faire comment est-ce how what why when where who which does did should would quel quelle quels pourquoi quand`) {
		set[word] = true
	}
	return set
}()

var (
	codeBlock = regexp.MustCompile("(?s)```.*?```")
	slackTag  = regexp.MustCompile(`<[^>]*>`)
	address   = regexp.MustCompile(`https?://\S+`)
	mention   = regexp.MustCompile(`[@#]\w+`)
)

// SearchTerms are the words of a message worth searching for in a task list or a knowledge base: code, links and
// mentions are dropped, then small words. Fewer than two words left means small talk: nothing is searched.
func SearchTerms(message string) []string {
	text := codeBlock.ReplaceAllString(message, " ")
	text = slackTag.ReplaceAllString(text, " ")
	text = address.ReplaceAllString(text, " ")
	text = mention.ReplaceAllString(text, " ")
	var words []string
	seen := map[string]bool{}
	for _, candidate := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
		if len([]rune(candidate)) < 3 || stopwords[candidate] || seen[candidate] {
			continue
		}
		seen[candidate] = true
		words = append(words, candidate)
		if len(words) == maxSearchWords {
			break
		}
	}
	if len(words) < minSearchWords {
		return nil
	}
	return words
}
