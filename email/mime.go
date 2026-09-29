package email

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/textproto"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// messageID returns a Message-ID in domain. A delivery identity (the
// idempotency key, which queued email derives from the stable job ID) yields
// the same ID on every retry, so receivers can recognise repeated submissions.
// Without one the ID is random. An empty domain uses the sender's domain.
func messageID(m Message, key IdempotencyKey, domain string) (string, error) {
	if domain == "" {
		_, domain, _ = strings.Cut(m.from.mailbox, "@")
	}
	if domain == "" {
		domain = "foundry.invalid"
	}
	var id [16]byte
	if key != "" {
		digest := sha256.Sum256([]byte("foundry.email.message-id\x00" + string(key)))
		copy(id[:], digest[:])
	} else if _, err := rand.Read(id[:]); err != nil {
		return "", Construction
	}
	return "<" + hex.EncodeToString(id[:]) + "@" + domain + ">", nil
}
func buildMIME(m Message, attachments []ResolvedAttachment, limit int, id string) ([]byte, error) {
	b := &boundedBuffer{remaining: limit}
	write := func(k, v string) error { _, err := fmt.Fprintf(b, "%s: %s\r\n", k, v); return err }
	for _, h := range [][2]string{{"From", m.from.Header()}, {"To", addressHeader(m.to)}, {"Subject", encodeWords(m.subject)}, {"Date", time.Now().UTC().Format(time.RFC1123Z)}, {"Message-ID", id}, {"MIME-Version", "1.0"}, {"Content-Language", string(m.locale)}} {
		if h[0] == "Content-Language" && h[1] == "" {
			continue
		}
		if err := write(h[0], h[1]); err != nil {
			return nil, err
		}
	}
	for _, h := range []struct {
		name      string
		addresses []Address
	}{{"Cc", m.cc}, {"Reply-To", m.replyTo}} {
		if len(h.addresses) > 0 {
			if err := write(h.name, addressHeader(h.addresses)); err != nil {
				return nil, err
			}
		}
	}
	keys := make([]string, 0, len(m.headers))
	for k := range m.headers {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if err := write(k, mime.BEncoding.Encode("UTF-8", m.headers[k])); err != nil {
			return nil, err
		}
	}
	var inline, ordinary []ResolvedAttachment
	for _, a := range attachments {
		if a.reference.ContentID != "" {
			inline = append(inline, a)
		} else {
			ordinary = append(ordinary, a)
		}
	}
	body := func(w io.Writer) error { return writeRelated(w, m, inline) }
	if len(ordinary) > 0 {
		if err := writeMultipart(b, "mixed", body, ordinary); err != nil {
			return nil, Construction
		}
	} else if err := body(b); err != nil {
		return nil, Construction
	}
	return slices.Clone(b.Bytes()), nil
}
func addressHeader(addresses []Address) string {
	values := make([]string, len(addresses))
	for i, a := range addresses {
		values[i] = a.Header()
	}
	return strings.Join(values, ",\r\n ")
}

// Always use bounded encoded words, including long ASCII tokens. Folding never
// changes the decoded subject/header value and keeps every physical line short.
func encodeWords(text string) string {
	if text == "" {
		return ""
	}
	var words []string
	for len(text) > 0 {
		n := min(42, len(text))
		for n < len(text) && !utf8.RuneStart(text[n]) {
			n--
		}
		words = append(words, "=?UTF-8?B?"+base64.StdEncoding.EncodeToString([]byte(text[:n]))+"?=")
		text = text[n:]
	}
	return strings.Join(words, "\r\n ")
}
func writeRelated(w io.Writer, m Message, inline []ResolvedAttachment) error {
	body := func(w io.Writer) error { return writeBody(w, m) }
	if len(inline) > 0 {
		return writeMultipart(w, "related", body, inline)
	}
	return body(w)
}
func writeMultipart(w io.Writer, subtype string, body func(io.Writer) error, attachments []ResolvedAttachment) error {
	mw := multipart.NewWriter(w)
	if _, err := fmt.Fprintf(w, "Content-Type: multipart/%s; boundary=%q\r\n\r\n", subtype, mw.Boundary()); err != nil {
		return err
	}
	// MIME children write their own headers; an empty multipart header would add
	// an extra blank line. Buffer only the child headers, then stream its body.
	if err := writeChild(mw, body); err != nil {
		return err
	}
	for _, a := range attachments {
		if err := writeChild(mw, func(w io.Writer) error { return writeAttachment(w, a) }); err != nil {
			return err
		}
	}
	return mw.Close()
}

// childPartWriter delays CreatePart until the child's bounded header block is
// complete. Payload bytes stream directly to the shared message-size bound.
type childPartWriter struct {
	parent *multipart.Writer
	header []byte
	body   io.Writer
}

func (w *childPartWriter) Write(data []byte) (int, error) {
	if w.body != nil {
		return w.body.Write(data)
	}
	n := len(data)
	for i, c := range data {
		w.header = append(w.header, c)
		if len(w.header) > 64<<10 {
			return 0, Construction
		}
		if len(w.header) >= 4 && string(w.header[len(w.header)-4:]) == "\r\n\r\n" {
			h, err := textproto.NewReader(bufio.NewReader(bytes.NewReader(w.header))).ReadMIMEHeader()
			if err != nil {
				return 0, Construction
			}
			w.body, err = w.parent.CreatePart(h)
			if err != nil {
				return 0, err
			}
			w.header = nil
			written, err := w.body.Write(data[i+1:])
			return i + 1 + written, err
		}
	}
	return n, nil
}
func writeChild(mw *multipart.Writer, write func(io.Writer) error) error {
	w := &childPartWriter{parent: mw}
	if err := write(w); err != nil {
		return err
	}
	if w.body == nil {
		return Construction
	}
	return nil
}
func writeBody(w io.Writer, m Message) error {
	if m.text != "" && m.html != "" {
		mw := multipart.NewWriter(w)
		if _, err := fmt.Fprintf(w, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", mw.Boundary()); err != nil {
			return err
		}
		for _, part := range [][2]string{{"text/plain", m.text}, {"text/html", m.html}} {
			if err := writeChild(mw, func(w io.Writer) error { return writeText(w, part[0], part[1]) }); err != nil {
				return err
			}
		}
		return mw.Close()
	}
	if m.html != "" {
		return writeText(w, "text/html", m.html)
	}
	return writeText(w, "text/plain", m.text)
}
func writeText(w io.Writer, kind, text string) error {
	if _, err := fmt.Fprintf(w, "Content-Type: %s; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", kind); err != nil {
		return err
	}
	q := quotedprintable.NewWriter(w)
	if _, err := io.WriteString(q, text); err != nil {
		return err
	}
	return q.Close()
}
func writeAttachment(w io.Writer, a ResolvedAttachment) error {
	disposition := "attachment"
	if a.reference.ContentID != "" {
		disposition = "inline"
	}
	if _, err := fmt.Fprintf(w, "Content-Type: %s\r\nContent-Transfer-Encoding: base64\r\nContent-Disposition: %s\r\n", a.reference.ContentType, mime.FormatMediaType(disposition, map[string]string{"filename": a.reference.Filename})); err != nil {
		return err
	}
	if a.reference.ContentID != "" {
		if _, err := fmt.Fprintf(w, "Content-ID: <%s>\r\n", a.reference.ContentID); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(w, "\r\n"); err != nil {
		return err
	}
	for data := a.data; len(data) > 0; {
		n := min(57, len(data))
		if _, err := io.WriteString(w, base64.StdEncoding.EncodeToString(data[:n])+"\r\n"); err != nil {
			return err
		}
		data = data[n:]
	}
	return nil
}
