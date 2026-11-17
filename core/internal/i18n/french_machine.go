package i18n

// frMachine is the French of what Pouple says about the Mac's disk and memory.
var frMachine = map[string]string{
	"The disk is full: that is why it failed. Free some space, then investigate again.":                                                                                                     "Le disque est plein : c’est pour ça que ça a échoué. Libère de l’espace, puis relance l’enquête.",
	"The disk is full (%s free): that is why it failed. Free some space, then investigate again.":                                                                                           "Le disque est plein (%s libres) : c’est pour ça que ça a échoué. Libère de l’espace, puis relance l’enquête.",
	"When it failed, the disk was almost full (%s free) and the Mac was short of memory (%s in use): that is most likely why. Free some space and close some apps, then investigate again.": "Au moment de l’échec, le disque était presque plein (%s libres) et le Mac manquait de mémoire (%s utilisée) : c’est très probablement la cause. Libère de l’espace et ferme des apps, puis relance l’enquête.",
	"When it failed, the disk was almost full (%s free): that is most likely why. Free some space, then investigate again.":                                                                 "Au moment de l’échec, le disque était presque plein (%s libres) : c’est très probablement la cause. Libère de l’espace, puis relance l’enquête.",
	"When it failed, the Mac was short of memory (%s in use): that is most likely why. Close some apps, then investigate again.":                                                            "Au moment de l’échec, le Mac manquait de mémoire (%s utilisée) : c’est très probablement la cause. Ferme des apps, puis relance l’enquête.",
	"When it failed, the disk had %s free and the memory was %s in use: tight, so it may have played a part.":                                                                               "Au moment de l’échec, le disque avait %s libres et la mémoire était utilisée à %s : c’est juste, ça a pu jouer.",
	"When it failed, the disk had %s free and the memory was %s in use: neither is the cause.":                                                                                              "Au moment de l’échec, le disque avait %s libres et la mémoire était utilisée à %s : ni l’un ni l’autre n’est en cause.",
}

func init() {
	for english, french := range frMachine {
		fr[english] = french
	}
}
