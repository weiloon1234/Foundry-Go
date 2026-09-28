package http

import "slices"

// AssetRouteInfo describes a framework file boundary without inventing a JSON
// DTO. It never exposes a local directory or a filesystem implementation.
type AssetRouteInfo struct {
	Prefix   string           `json:"prefix"`
	Index    AssetPath        `json:"index,omitempty"`
	File     FileResponseInfo `json:"file"`
	Fallback bool             `json:"fallback"`
	Excluded []string         `json:"excluded,omitempty"`
}

func (a *Assets) description(prefix string) AssetRouteInfo {
	seen := map[MediaType]bool{"application/octet-stream": true}
	for _, media := range a.config.Media {
		seen[MediaType(normalizedFileMedia(media))] = true
	}
	media := make([]MediaType, 0, len(seen))
	for value := range seen {
		media = append(media, value)
	}
	slices.Sort(media)
	if prefix == "" {
		prefix = "/"
	}
	return AssetRouteInfo{Prefix: prefix, Index: a.config.Index, File: FileResponseInfo{MediaTypes: media, Seekable: true}}
}
func (i AssetRouteInfo) clone() AssetRouteInfo {
	i.File.MediaTypes = slices.Clone(i.File.MediaTypes)
	i.Excluded = slices.Clone(i.Excluded)
	return i
}
