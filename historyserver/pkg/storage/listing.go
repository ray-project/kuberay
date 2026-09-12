package storage

import "strings"

// RelativeFilePaths converts object paths returned by a flat storage listing
// into file paths relative to the listed prefix. Directory marker objects and
// objects outside the prefix are omitted.
func RelativeFilePaths(prefix string, objectPaths []string) []string {
	prefix = strings.TrimSuffix(prefix, "/") + "/"
	relativePaths := make([]string, 0, len(objectPaths))

	for _, objectPath := range objectPaths {
		relativePath, found := strings.CutPrefix(objectPath, prefix)
		if !found || relativePath == "" || strings.HasSuffix(relativePath, "/") {
			continue
		}
		relativePaths = append(relativePaths, relativePath)
	}

	return relativePaths
}
