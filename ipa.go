package appmeta

import (
	"errors"
	"strings"
)

// findAppBundle returns the "Payload/<name>.app/" prefix of the main app.
func findAppBundle(a *archive) string {
	best := ""
	for name := range a.files {
		rest, ok := strings.CutPrefix(name, "Payload/")
		if !ok {
			continue
		}
		app, file, ok := strings.Cut(rest, "/")
		bundle := "Payload/" + app + "/"
		// Pick the smallest name so the result does not depend on map order.
		if ok && strings.HasSuffix(app, ".app") && file == "Info.plist" && (best == "" || bundle < best) {
			best = bundle
		}
	}
	return best
}

func parseIPA(a *archive) (*Info, error) {
	return nil, errors.New("appmeta: ipa not implemented")
}
