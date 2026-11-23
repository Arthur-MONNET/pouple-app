package i18n

// frRemote is the French of what Pouple says about the access from the phones.
var frRemote = map[string]string{
	"Turn on access from the phone, on a network that is connected, before pairing one.":                                                            "Active l’accès depuis le téléphone, sur un réseau connecté, avant d’en associer un.",
	"“%s” is not the name of a network interface.":                                                                                                  "« %s » n’est pas le nom d’une interface réseau.",
	"The port must be between 1024 and 65535.":                                                                                                      "Le port doit être compris entre 1024 et 65535.",
	"Turn on access from outside and enter an address, or wait for Pouple to detect it, before sharing it with the phone.":                          "Active l’accès depuis l’extérieur et saisis une adresse, ou attends que Pouple la détecte, avant de la partager avec le téléphone.",
	"“%s” is not a valid public name or address: use a host name such as home.example.net, or an IP address, with no http://, port, path or space.": "« %s » n’est pas un nom ou une adresse publique valide : utilise un nom d’hôte comme maison.exemple.net, ou une adresse IP, sans http://, port, chemin ni espace.",
	"The external port must be 0 (the same as the port above) or between 1 and 65535.":                                                              "Le port externe doit valoir 0 (le même que le port ci-dessus) ou être compris entre 1 et 65535.",
	"Enter the public name or address of your home network to reach the Mac from outside.":                                                          "Saisis le nom ou l’adresse publique de ton réseau à la maison pour joindre le Mac depuis l’extérieur.",
	"“%s” is not a valid ngrok domain: use the name of your static domain, such as my-mac.ngrok-free.app, with no https://, port, path or space.":   "« %s » n’est pas un domaine ngrok valide : utilise le nom de ton domaine statique, comme mon-mac.ngrok-free.app, sans https://, port, chemin ni espace.",
	"That does not look like an ngrok authtoken: copy it again from your ngrok dashboard, without spaces.":                                          "Ça ne ressemble pas à un authtoken ngrok : copie-le à nouveau depuis ton tableau de bord ngrok, sans espace.",
	"Enter your ngrok domain and authtoken to turn the tunnel on.":                                                                                  "Saisis ton domaine ngrok et ton authtoken pour activer le tunnel.",
	"Pouple cannot reach ngrok: check the Mac’s network connection. It keeps trying.":                                                               "Pouple ne joint pas ngrok : vérifie la connexion réseau du Mac. Il continue d’essayer.",
	"ngrok refused the authtoken: copy it again from your ngrok dashboard.":                                                                         "ngrok a refusé l’authtoken : copie-le à nouveau depuis ton tableau de bord ngrok.",
	"ngrok refused the domain: it must be a domain reserved in your ngrok account.":                                                                 "ngrok a refusé le domaine : il doit être réservé dans ton compte ngrok.",
	"Your ngrok account’s limit is reached, or this domain is already online elsewhere: stop the other ngrok session using it.":                     "La limite de ton compte ngrok est atteinte, ou ce domaine est déjà en ligne ailleurs : arrête l’autre session ngrok qui l’utilise.",
	"Your ngrok account cannot start a tunnel: check its status in the ngrok dashboard.":                                                            "Ton compte ngrok ne peut pas démarrer de tunnel : vérifie son état dans le tableau de bord ngrok.",
	"The tunnel stopped unexpectedly. Pouple will try again.":                                                                                       "Le tunnel s’est arrêté sans que tu l’aies demandé. Pouple va réessayer.",
	"The ngrok authtoken could not be read from the Keychain: enter it again.":                                                                      "L’authtoken ngrok n’a pas pu être lu dans le trousseau : saisis-le à nouveau.",
	"The tunnel could not start. Pouple will try again.":                                                                                            "Le tunnel n’a pas pu démarrer. Pouple va réessayer.",
	"This device is not paired.": "Cet appareil n’est pas associé.",
}

func init() {
	for english, french := range frRemote {
		fr[english] = french
	}
}
