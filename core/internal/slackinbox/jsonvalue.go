package slackinbox

// Slack payloads are read leniently: a malformed entry is skipped instead of failing the whole page, which is why the
// decoded JSON is walked with these helpers rather than unmarshalled into strict structs.

type object = map[string]any

func asObject(value any) (object, bool) {
	o, ok := value.(map[string]any)
	return o, ok
}

func str(o object, key string) (string, bool) {
	s, ok := o[key].(string)
	return s, ok
}

func list(o object, key string) ([]any, bool) {
	a, ok := o[key].([]any)
	return a, ok
}

func flag(o object, key string) bool {
	b, _ := o[key].(bool)
	return b
}
