package i18n

// frRemote is the French of what Pouple says about the access from the phones.
var frRemote = map[string]string{
	"Turn on access from the phone, on a network that is connected, before pairing one.":                                                            "Active l’accès depuis le téléphone, sur un réseau connecté, avant d’en associer un.",
	"“%s” is not the name of a network interface.":                                                                                                  "« %s » n’est pas le nom d’une interface réseau.",
	"The port must be between 1024 and 65535.":                                                                                                      "Le port doit être compris entre 1024 et 65535.",
	"Choose the network the phone uses: Pouple never listens on every network at once.":                                                             "Choisis le réseau qu’utilise le téléphone : Pouple n’écoute jamais sur tous les réseaux à la fois.",
	"Turn on access from outside and enter an address, or wait for Pouple to detect it, before sharing it with the phone.":                          "Active l’accès depuis l’extérieur et saisis une adresse, ou attends que Pouple la détecte, avant de la partager avec le téléphone.",
	"“%s” is not a valid public name or address: use a host name such as home.example.net, or an IP address, with no http://, port, path or space.": "« %s » n’est pas un nom ou une adresse publique valide : utilise un nom d’hôte comme maison.exemple.net, ou une adresse IP, sans http://, port, chemin ni espace.",
	"The external port must be 0 (the same as the port above) or between 1 and 65535.":                                                              "Le port externe doit valoir 0 (le même que le port ci-dessus) ou être compris entre 1 et 65535.",
	"Enter the public name or address of your home network to reach the Mac from outside.":                                                          "Saisis le nom ou l’adresse publique de ton réseau à la maison pour joindre le Mac depuis l’extérieur.",
	"This device is not paired.": "Cet appareil n’est pas associé.",
}

func init() {
	for english, french := range frRemote {
		fr[english] = french
	}
}
