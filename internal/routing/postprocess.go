package routing

import "regexp"

var nextModelRe = regexp.MustCompile(`<<next_model:\s*([^>]+?)\s*>>`)

// ExtractNextModel finds a <<next_model: name>> directive in text.
// Returns the cleaned text (directive removed) and the model name (or "").
func ExtractNextModel(text string) (string, string) {
	m := nextModelRe.FindStringSubmatch(text)
	if m == nil {
		return text, ""
	}
	clean := nextModelRe.ReplaceAllString(text, "")
	return clean, m[1]
}
