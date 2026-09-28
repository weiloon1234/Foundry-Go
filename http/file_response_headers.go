package http

import stdhttp "net/http"

func (p *preparedFile) headers(header stdhttp.Header) {
	clearResponseRepresentation(header)
	header.Set("Content-Type", string(p.media))
	header.Set("Content-Disposition", string(p.disposition))
	header.Set("X-Content-Type-Options", "nosniff")
	if p.varyAccept {
		appendVary(header, "Accept")
	}
	if p.cacheControl != "" {
		header.Set("Cache-Control", string(p.cacheControl))
	}
}
