package engine

import (
	"regexp"
	"strings"
	"unicode"
)

// fold lowercases, drops accents and unifies apostrophes, so vocabulary lists stay short.
func fold(text string) string {
	replacer := strings.NewReplacer(
		"à", "a", "â", "a", "ä", "a", "á", "a", "ã", "a",
		"é", "e", "è", "e", "ê", "e", "ë", "e",
		"î", "i", "ï", "i", "í", "i",
		"ô", "o", "ö", "o", "ó", "o", "õ", "o",
		"ù", "u", "û", "u", "ü", "u", "ú", "u",
		"ç", "c", "ñ", "n", "’", "'",
	)
	return replacer.Replace(strings.ToLower(text))
}

// neutralize makes third-party text unable to open or close a prompt delimiter.
func neutralize(text string) string {
	return strings.NewReplacer("<", "‹", ">", "›").Replace(text)
}

func oneLine(text string, max int) string {
	line := neutralize(strings.Join(strings.Fields(text), " "))
	runes := []rune(line)
	if len(runes) > max {
		return string(runes[:max])
	}
	return line
}

func block(tag, content string) string {
	return "<" + tag + ">\n" + neutralize(content) + "\n</" + tag + ">"
}

var acknowledgements = func() map[string]bool {
	words := `merci mercii merciii lot much so thanks thank thx thanx ty you beaucoup bien a toi vous tous
ok okay oki okk oks k ouais ouai oui yes yep yup non nop nope top super parfait cool nickel genial excellent bravo gg nice great perfect awesome sympa
note vu recu compris noted got it seen received ca marche roule dac dacc accord d parfaitement entendu bonne journee soiree bonjour salut hello hi hey bye ciao
lol mdr ptdr haha hahaha hehe xd`
	set := map[string]bool{}
	for _, word := range strings.Fields(words) {
		set[word] = true
	}
	return set
}()

var (
	angleTags = regexp.MustCompile(`<[^>]*>`)
	emojiCode = regexp.MustCompile(`:[a-z0-9_+-]+:`)
)

// IsSmallTalk reports a message that only says thanks, ok or a greeting: nothing to do, so no Codex turn is spent.
// Anything with a question mark, a number or a word outside the small vocabulary is not small talk.
func IsSmallTalk(message string) bool {
	if strings.Contains(message, "?") {
		return false
	}
	text := emojiCode.ReplaceAllString(angleTags.ReplaceAllString(fold(message), " "), " ")
	text = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsSpace(r) {
			return r
		}
		return ' '
	}, text)
	words := strings.Fields(text)
	if len(words) == 0 {
		// Only emoji or reactions.
		return strings.TrimSpace(message) != ""
	}
	if len(words) > 5 {
		return false
	}
	for _, word := range words {
		if !acknowledgements[word] {
			return false
		}
	}
	return true
}

// Confirmation is what a short typed message means for a proposal waiting for an answer.
type Confirmation int

const (
	Unclear Confirmation = iota
	Approve
	Decline
)

var (
	declinePatterns = compile(
		`^(non|no|nope|nan)\b`,
		`\b(annule|annuler|cancel|laisse tomber|laisse[- ]le|pas maintenant|plus tard|stop|abandonne)\b`,
		`\bne (le |l')?(fais|fait|lance|code|implemente|touche) pas\b`,
		`\b(don't|do not|dont) (do|run|apply|change|touch)\b`,
	)
	approvePatterns = compile(
		`\b(oui|ouais|ouai|yes|yep|yup|ok|okay|d'accord|dacord|go|allez|vas[- ]?y|valide|valides|approuve|approuves|confirme|confirm|proceed)\b`,
		`\b(lance|lances|lancer)\b`,
		`\b(fais|fait)[- ]le\b`,
		`\bdo it\b`,
		`\b(implemente|implementer|coder|code[- ]le)\b`,
		`\btu codes?\b`,
	)
	// Something added to the approval ("ok but change X") is an instruction, not a go-ahead.
	condition = regexp.MustCompile(`\b(mais|sauf|but|except|plutot|instead|sans|avec|change|modifie|ajoute|d'abord|first|apres)\b`)
)

func compile(patterns ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(patterns))
	for i, pattern := range patterns {
		out[i] = regexp.MustCompile(pattern)
	}
	return out
}

func matchesAny(patterns []*regexp.Regexp, text string) bool {
	for _, pattern := range patterns {
		if pattern.MatchString(text) {
			return true
		}
	}
	return false
}

// ConfirmationOf reads a short message typed by the user in the app as a go-ahead or a refusal. Text that comes
// from anywhere else (Slack, a log, Codex) never goes through here. Anything unclear is not a decision.
func ConfirmationOf(message string) Confirmation {
	text := strings.TrimSpace(fold(message))
	words := strings.Fields(text)
	if len(words) == 0 || len(words) > 8 || strings.Contains(text, "?") {
		return Unclear
	}
	if matchesAny(declinePatterns, text) {
		return Decline
	}
	if condition.MatchString(text) {
		return Unclear
	}
	if matchesAny(approvePatterns, text) {
		return Approve
	}
	return Unclear
}

var tokenPatterns = compile(
	`xox[a-z]-[A-Za-z0-9-]{10,}`, `xapp-[A-Za-z0-9-]{10,}`, `glpat-[A-Za-z0-9_-]{10,}`, `\bsk-[A-Za-z0-9_-]{20,}`,
	`gh[pousr]_[A-Za-z0-9]{20,}`, `github_pat_[A-Za-z0-9_]{20,}`, `\bAKIA[0-9A-Z]{16}\b`,
	// a JWT (the Codex login tokens are some) and a private key block
	`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`,
	`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?(?:-----END [A-Z ]*PRIVATE KEY-----|$)`,
)

const redacted = "[redacted]"

// Redactor removes the credentials Pouple holds (exact values) and common token shapes from text the model wrote.
// Codex can read files its sandbox allows, so a manipulated investigation could quote a secret in its answer.
type Redactor func(string) string

func NewRedactor(secrets ...string) Redactor {
	var exact []string
	for _, secret := range secrets {
		if len(secret) >= 8 {
			exact = append(exact, secret)
		}
	}
	return func(text string) string {
		for _, secret := range exact {
			text = strings.ReplaceAll(text, secret, redacted)
		}
		for _, pattern := range tokenPatterns {
			text = pattern.ReplaceAllString(text, redacted)
		}
		return text
	}
}
