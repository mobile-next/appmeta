package appmeta

import "errors"

const androidManifestPath = "AndroidManifest.xml"

func parseAPK(a *archive) (*Info, error) {
	return nil, errors.New("appmeta: apk not implemented")
}
