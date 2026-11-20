package i18n

// frGitHub is the French of the GitHub connector, its pull request watcher and its context. It lives apart from fr, like
// frInApp, so that the tables can change independently; init merges them. Placeholders must keep their order: a test
// checks it.
var frGitHub = map[string]string{
	// Connector
	"Your pull requests and reviews": "Tes pull requests et tes relectures",
	"Starts with github_pat_ or ghp_. It stays in your Mac's keychain and is never shown again.": "Commence par github_pat_ ou ghp_. Il reste dans le trousseau de ton Mac et n’est plus jamais affiché.",
	"does not look like a GitHub token (it starts with github_pat_ or ghp_)":                     "ne ressemble pas à un jeton GitHub (il commence par github_pat_ ou ghp_)",
	"API address": "Adresse de l’API",
	"Only for GitHub Enterprise. Keep the default for github.com.": "Seulement pour GitHub Enterprise. Garde la valeur par défaut pour github.com.",
	"Create a fine-grained token":                                  "Crée un jeton à accès précis",
	"In GitHub, open Settings, Developer settings, then Personal access tokens. The button below takes you there.": "Dans GitHub, ouvre Settings, Developer settings, puis Personal access tokens. Le bouton ci-dessous t’y emmène.",
	"Open GitHub tokens":       "Ouvrir les jetons GitHub",
	"Give it read-only access": "Donne-lui la lecture seule",
	"Choose the repositories, then read-only access to Pull requests, Checks, Commit statuses, Contents and Metadata. Pouple only reads: it never changes anything in GitHub.": "Choisis les dépôts, puis la lecture seule pour Pull requests, Checks, Commit statuses, Contents et Metadata. Pouple ne fait que lire : il ne modifie jamais rien dans GitHub.",
	"Copy it right away (GitHub shows it only once) and paste it in this connection form, never in chat. For GitHub Enterprise, enter its API address too.":                    "Copie-le tout de suite (GitHub ne l’affiche qu’une fois) et colle-le dans ce formulaire de connexion, jamais dans le chat. Pour GitHub Enterprise, saisis aussi l’adresse de son API.",

	// Address and validation
	"This is not a valid web address. Example: https://api.github.com":                                                                               "Ce n’est pas une adresse web valide. Exemple : https://api.github.com",
	"Keep https://api.github.com, or for GitHub Enterprise give its address without any other path (for example https://github.example.com/api/v3).": "Garde https://api.github.com, ou pour GitHub Enterprise donne son adresse sans autre chemin (par exemple https://github.example.com/api/v3).",
	"GitHub does not accept this token. Create a new personal access token and paste it again.":                                                      "GitHub n’accepte pas ce jeton. Crée un nouveau jeton d’accès personnel et colle-le à nouveau.",
	"GitHub refused this token. Check that it is still valid and that your organization allows it.":                                                  "GitHub a refusé ce jeton. Vérifie qu’il est encore valide et que ton organisation l’autorise.",
	"This address does not look like a GitHub API. Keep https://api.github.com, or give the address of your GitHub Enterprise.":                      "Cette adresse ne ressemble pas à une API GitHub. Garde https://api.github.com, ou donne l’adresse de ton GitHub Enterprise.",
	"GitHub refused to search your pull requests with this token. Check its permissions.":                                                            "GitHub a refusé de chercher tes pull requests avec ce jeton. Vérifie ses permissions.",
	"This is not a pull request reference.": "Ce n’est pas une référence de pull request.",
	"This is not a commit.":                 "Ce n’est pas un commit.",

	// Profile
	"Create a new token before it ends, then connect GitHub again.": "Crée un nouveau jeton avant la fin, puis reconnecte GitHub.",
	"Single sign-on authorization needed":                           "Autorisation d’authentification unique nécessaire",
	"An organization uses SAML single sign-on: on GitHub, open the token and choose Configure SSO to authorize it, or that organization's pull requests stay invisible.": "Une organisation utilise l’authentification unique SAML : sur GitHub, ouvre le jeton et choisis Configure SSO pour l’autoriser, sinon les pull requests de cette organisation restent invisibles.",
	"Open pull requests":            "Pull requests ouvertes",
	"Reviews requested":             "Relectures demandées",
	"Check the token's permissions": "Vérifie les permissions du jeton",
	"Pouple cannot read the permissions of a fine-grained token. For the repositories you want it to see, give read-only access to Pull requests, Checks, Commit statuses, Contents and Metadata.": "Pouple ne peut pas lire les permissions d’un jeton à accès précis. Pour les dépôts qu’il doit voir, donne la lecture seule pour Pull requests, Checks, Commit statuses, Contents et Metadata.",
	"This classic token has the repo scope, which can also write in your repositories. Pouple only reads: a fine-grained token with read-only access is safer.":                                    "Ce jeton classique a le scope repo, qui permet aussi d’écrire dans tes dépôts. Pouple ne fait que lire : un jeton à accès précis en lecture seule est plus sûr.",
	"Public repositories only": "Dépôts publics uniquement",
	"This classic token has public_repo: Pouple cannot see the pull requests of private repositories. Use a fine-grained read-only token to include them.": "Ce jeton classique a public_repo : Pouple ne voit pas les pull requests des dépôts privés. Utilise un jeton à accès précis en lecture seule pour les inclure.",
	"Missing repository scope": "Scope de dépôt manquant",
	"This classic token has neither repo nor public_repo: Pouple may miss pull requests. A fine-grained read-only token is the better choice.": "Ce jeton classique n’a ni repo ni public_repo : Pouple risque de manquer des pull requests. Un jeton à accès précis en lecture seule est le meilleur choix.",

	// Watcher
	"the GitHub connection could not be read":       "la connexion GitHub est illisible",
	"the saved GitHub connection could not be used": "la connexion GitHub enregistrée est inutilisable",

	// Context of a request
	"GitHub: could not read your pull requests (%s)":          "GitHub : impossible de lire tes pull requests (%s)",
	"GitHub: read your pull requests (%d open, %d to review)": "GitHub : tes pull requests lues (%d ouvertes, %d à relire)",
	"GitHub: %s failed (%s)":                                  "GitHub : %s a échoué (%s)",
	"GitHub: read the pull request %s (%d comments)":          "GitHub : pull request %s lue (%d commentaires)",
	"GitHub: read the diff of %s (%d files)":                  "GitHub : diff de %s lu (%d fichiers)",
}

func init() {
	for english, french := range frGitHub {
		fr[english] = french
	}
}
