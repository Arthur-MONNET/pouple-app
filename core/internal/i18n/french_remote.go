package i18n

// frRemote is the French of what Pouple says about the access from the phones.
var frRemote = map[string]string{
	"Turn on access from the phone, on a network that is connected, before pairing one.": "Active l’accès depuis le téléphone, sur un réseau connecté, avant d’en associer un.",
	"“%s” is not the name of a network interface.":                                       "« %s » n’est pas le nom d’une interface réseau.",
	"The port must be between 1024 and 65535.":                                           "Le port doit être compris entre 1024 et 65535.",
	"Choose the network the phone uses: Pouple never listens on every network at once.":  "Choisis le réseau qu’utilise le téléphone : Pouple n’écoute jamais sur tous les réseaux à la fois.",
	"This device is not paired.":                                                         "Cet appareil n’est pas associé.",
}

func init() {
	for english, french := range frRemote {
		fr[english] = french
	}
}
