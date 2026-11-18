package i18n

// frMemory is the French of what Pouple says about the memory.
var frMemory = map[string]string{
	"invalid memory fact":                                                                     "fait de mémoire non valide",
	"this note is not in the memory":                                                          "cette note n’est pas dans la mémoire",
	"The memory folder cannot be read":                                                        "Le dossier de la mémoire ne peut pas être lu",
	"The memory folder cannot be created":                                                     "Le dossier de la mémoire ne peut pas être créé",
	"The memory has no folder: choose one in Settings":                                        "La mémoire n’a pas de dossier : choisis-en un dans les Réglages",
	"the memory was written but git could not commit it":                                      "la mémoire est écrite, mais git n’a pas pu la commiter",
	"git could not commit the memory: %s":                                                     "git n’a pas pu commiter la mémoire : %s",
	"The memory folder could not be made a git repository: %s":                                "Le dossier de la mémoire n’a pas pu devenir un dépôt git : %s",
	"The changes made in the memory outside Pouple could not be committed":                    "Les changements faits dans la mémoire hors de Pouple n’ont pas pu être commités",
	"a fact to remember cannot be empty":                                                      "un fait à retenir ne peut pas être vide",
	"a fact to remember is too long":                                                          "un fait à retenir est trop long",
	"the title of a note is too long":                                                         "le titre d’une note est trop long",
	"a fact to remember is one line, without control characters":                              "un fait à retenir tient sur une ligne, sans caractère de contrôle",
	"a fact to remember needs a note or the title of a new one":                               "un fait à retenir a besoin d’une note, ou du titre d’une nouvelle",
	"“%s” is not a note of the memory":                                                        "« %s » n’est pas une note de la mémoire",
	"The note “%s” cannot be read":                                                            "La note « %s » ne peut pas être lue",
	"The note “%s” cannot be written":                                                         "La note « %s » ne peut pas être écrite",
	"Memory: searched “%s”":                                                                   "Mémoire : recherche de « %s »",
	"Memory: read “%s”":                                                                       "Mémoire : lecture de « %s »",
	"the memory is switched off in Settings":                                                  "la mémoire est désactivée dans les Réglages",
	"nothing waits to be remembered":                                                          "rien n’attend d’être retenu",
	"the suggested facts changed since you read them":                                         "les faits proposés ont changé depuis que tu les as lus",
	"choose at least one fact, each only once":                                                "choisis au moins un fait, chacun une seule fois",
	"Declined the facts to remember":                                                          "Faits à retenir refusés",
	"Remembered:\n%s":                                                                         "Retenu :\n%s",
	"Git could not commit it: the notes are written, but their history does not show it yet.": "Git n’a pas pu le commiter : les notes sont écrites, mais leur historique ne le montre pas encore.",
	"Choose a full path for the memory, not / or your home folder":                            "Choisis un chemin complet pour la mémoire, pas / ni ton dossier personnel",
	"the title of a note is a few plain words":                                                "le titre d’une note tient en quelques mots simples",
	"The memory folder is a git repository with a remote: private facts could be pushed. Choose another folder in Settings.": "Le dossier de la mémoire est un dépôt git avec un remote : des faits privés pourraient être poussés. Choisis un autre dossier dans les Réglages.",
	"this fact changed since you read it":                   "ce fait a changé depuis que tu l’as lu",
	"The history of the memory cannot be read":              "L’historique de la mémoire ne peut pas être lu",
	"tidying the memory up is not available":                "le rangement de la mémoire n’est pas disponible",
	"turn the memory on before connecting your tools to it": "active la mémoire avant d’y brancher tes outils",
	"unknown command-line tool":                             "outil en ligne de commande inconnu",
	"“%s” cannot be written":                                "« %s » ne peut pas être écrit",
	"“%s” cannot be read":                                   "« %s » ne peut pas être lu",
	"“%s” is not a plain file: add the lines yourself":      "« %s » n’est pas un fichier ordinaire : ajoute les lignes toi-même",
	`"decision" must be "keep" or "drop"`:                   `"decision" doit valoir "keep" ou "drop"`,
	`"connected" must be true or false`:                     `"connected" doit valoir true ou false`,
	"The memory must stay out of the projects folder":       "La mémoire doit rester en dehors du dossier des projets",
}

func init() {
	for english, french := range frMemory {
		fr[english] = french
	}
}
