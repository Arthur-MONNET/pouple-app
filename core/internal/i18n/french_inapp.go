package i18n

// frInApp is the French of what Pouple says now that Slack is optional and a conversation can start in the app. It lives
// apart from fr, like frProviders, so that the two tables can change independently; init merges them. Placeholders must
// keep their order: a test checks it.
var frInApp = map[string]string{
	// Slack is optional
	"Messages that mention you become requests": "Les messages qui te mentionnent deviennent des demandes",
	"this service is not connected":             "ce service n’est pas connecté",
	"%s's usage limit is reached: new requests from others wait until %s. What you ask yourself still goes through.": "La limite d’utilisation de %s est atteinte : les nouvelles demandes des autres attendent jusqu’à %s. Ce que tu demandes toi-même passe quand même.",
	"New requests from others wait 30 minutes; what you ask yourself still goes through.":                            "Les nouvelles demandes des autres attendent 30 minutes ; ce que tu demandes toi-même passe quand même.",

	// Conversations started in the app
	"Pouple conversation":                                         "Conversation Pouple",
	"%s is not a project of your projects folder":                 "%s n’est pas un projet de ton dossier de projets",
	"too many requests are waiting: try again when some are done": "trop de demandes attendent : réessaie quand certaines seront terminées",
}

func init() {
	for english, french := range frInApp {
		fr[english] = french
	}
}
