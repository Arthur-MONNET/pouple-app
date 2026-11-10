package i18n

// frProviders holds the French of everything the AI providers (Codex, Claude) add: the Claude connector, the choice of
// provider and model, the test of a provider, and the work log of Claude. They live apart from fr so that the two
// tables can change independently; init merges them. Placeholders must keep their order: a test checks it.
var frProviders = map[string]string{
	// Claude connector
	"Install Claude Code": "Installer Claude Code",
	"Pouple uses Anthropic's Claude Code on your Mac. If it is not installed, one click installs it with Anthropic's official installer.": "Pouple utilise Claude Code d’Anthropic sur ton Mac. S’il n’est pas installé, un clic l’installe avec l’installeur officiel d’Anthropic.",
	"Sign in with Claude": "Se connecter avec Claude",
	"A browser page opens: sign in with the Claude account you want Pouple to use. It has its own login and never touches your usual Claude Code.": "Une page s’ouvre dans ton navigateur : connecte-toi avec le compte Claude que Pouple doit utiliser. Il a sa propre connexion et ne touche jamais à ton Claude Code habituel.",
	"Claude Code is already installed (%s). You can now sign in.":                                                                                  "Claude Code est déjà installé (%s). Tu peux maintenant te connecter.",
	"Installing Claude Code with Anthropic’s official installer…":                                                                                  "Installation de Claude Code avec l’installeur officiel d’Anthropic…",
	"Claude Code could not be installed. Check your internet connection and try again.":                                                            "Claude Code n’a pas pu être installé. Vérifie ta connexion internet et réessaie.",
	"Claude Code was installed but cannot be started. Open a new Terminal window and run “claude --version” to see why.":                           "Claude Code est installé mais ne démarre pas. Ouvre une nouvelle fenêtre Terminal et lance « claude --version » pour comprendre pourquoi.",
	"Claude Code is installed. You can now sign in.":                                                                                               "Claude Code est installé. Tu peux maintenant te connecter.",
	"The Claude Code CLI is not installed yet. Click “Install Claude Code”.":                                                                       "Le CLI Claude Code n’est pas encore installé. Clique sur « Installer Claude Code ».",
	"Claude Code could not tell who is signed in. Update Claude Code and try again.":                                                               "Claude Code n’a pas pu dire qui est connecté. Mets Claude Code à jour et réessaie.",
	"Not signed in yet. Click “Sign in with Claude”.":                                                                                              "Pas encore connecté. Clique sur « Se connecter avec Claude ».",
	"Claude Code":               "Claude Code",
	"Not a Claude subscription": "Pas un abonnement Claude",
	"This login is billed per use, not by a Claude plan. Sign in with a Claude subscription to avoid unexpected costs.": "Cette connexion est facturée à l’utilisation, pas par un abonnement Claude. Connecte-toi avec un abonnement Claude pour éviter les coûts imprévus.",

	// Setup: one AI is needed
	"Connect one of these first: %s": "Connecte d’abord l’un de ceux-ci : %s",

	// Engine: running a turn on Claude, and testing a provider
	"%s is not available":                     "%s n’est pas disponible",
	"The model name “%s” is not valid":        "Le nom de modèle « %s » n’est pas valide",
	"The saved conversation id is not valid":  "L’identifiant de conversation enregistré n’est pas valide",
	"Unknown sandbox “%s”":                    "Mode d’accès inconnu « %s »",
	"The answer schema could not be read: %s": "Le schéma de réponse est illisible (%s)",
	"An attachment is not an image":           "Une pièce jointe n’est pas une image",
	"Not allowed: %s":                         "Refusé : %s",
	"Read %s":                                 "Lecture de %s",
	"Searched files: %s":                      "Recherche de fichiers : %s",
	"Searched for “%s”":                       "Recherche de « %s »",
	"Wrote %s":                                "Écriture de %s",
	"Used %s":                                 "Outil utilisé : %s",
	"The test of %s took more than %d seconds and was stopped": "Le test de %s a dépassé %d secondes et a été arrêté",
	"%s's usage limit is reached. Try again later.":            "La limite d’utilisation de %s est atteinte. Réessaie plus tard.",

	// Settings and providers API
	"The AI must be “codex” or “claude”":                                                                "L’IA doit être « codex » ou « claude »",
	"“%s” is not connected yet: connect it in Connectors first":                                         "« %s » n’est pas encore connecté : connecte-le d’abord dans Connecteurs",
	"A model name has at most 80 characters: letters, digits and . _ : / -, and it cannot start with -": "Un nom de modèle fait 80 caractères au plus : lettres, chiffres et . _ : / -, sans commencer par -",
	"%s is not connected yet. Connect it in Connectors.":                                                "%s n’est pas encore connecté. Connecte-le dans Connecteurs.",
	"The provider is unknown":                                                                           "Cette IA est inconnue",
	"Opus: the most capable, always the latest":                                                         "Opus : le plus capable, toujours le dernier",
	"Sonnet: fast and capable, always the latest":                                                       "Sonnet : rapide et capable, toujours le dernier",
	"Haiku: the fastest, always the latest":                                                             "Haiku : le plus rapide, toujours le dernier",
	"Fable: for the hardest tasks, always the latest":                                                   "Fable : pour les tâches les plus difficiles, toujours le dernier",
	"Claude Opus 5.5":   "Claude Opus 5.5",
	"Claude Sonnet 5.5": "Claude Sonnet 5.5",
	"Claude Haiku 5.5":  "Claude Haiku 5.5",
	"Claude Fable 5.1":  "Claude Fable 5.1",
	"Leave empty to use Claude’s own default. Any model name your Claude CLI accepts can be typed.": "Laisse vide pour utiliser le modèle par défaut de Claude. Tu peux saisir n’importe quel nom de modèle accepté par ton CLI Claude.",
	"The list of models could not be read": "La liste des modèles n’a pas pu être lue",
	"Leave empty to use Codex’s own default. Any model name your Codex CLI accepts can be typed.": "Laisse vide pour utiliser le modèle par défaut de Codex. Tu peux saisir n’importe quel nom de modèle accepté par ton CLI Codex.",
}

func init() {
	for english, french := range frProviders {
		fr[english] = french
	}
}
