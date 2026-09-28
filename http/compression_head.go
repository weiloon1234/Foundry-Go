package http

import stdhttp "net/http"

// HEAD emits representation metadata without allocating an encoder or writing
// content. Unknown GET size/type omits fields that would require generating it.
func (w *compressionResponse) finishHead() {
	if w.mode != compressionPending {
		return
	}
	if w.status == 0 {
		w.WriteHeader(stdhttp.StatusOK)
	}
	if !bodylessCompressionStatus(w.status) {
		w.adjustUnwrittenRepresentation(w.status)
	}
	w.commit(false)
}

// HEAD and 304 cannot retain an identity length/digest for a potentially encoded
// GET representation. Reuse the normal response policy, without an encoder.
func (w *compressionResponse) adjustUnwrittenRepresentation(status int) {
	if compressionTransformExcluded(status, w.final) {
		return
	}
	encoder, available := w.preferences.choose(w.config.Encoders)
	if !available {
		return
	}
	if compressionAllowed(status, w.final) {
		known := w.declaredLength >= 0 || w.written > 0
		size := w.declaredLength
		if size < 0 {
			size = w.written
		}
		switch {
		case size >= int64(w.config.MinBytes) || w.preferences.quality("identity") == 0:
			w.final.Set("Content-Encoding", string(encoder.encoding))
			w.final.Del("Content-Length")
			invalidateCompressedIntegrity(w.final)
		case !known:
			w.omitUnknownEncodingMetadata()
		}
	} else if w.final.Get("Content-Type") == "" {
		w.omitUnknownEncodingMetadata()
	}
}

// An unwritten body cannot establish encoded size or byte identity. Conditional
// 304 responses still retain an existing semantic validator for cache updates.
func (w *compressionResponse) omitUnknownEncodingMetadata() {
	w.final.Del("Content-Length")
	invalidateCompressedIntegrity(w.final)
	if w.status != stdhttp.StatusNotModified {
		w.final.Del("Etag")
	}
}
