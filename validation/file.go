package validation

import (
	"mime"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/filename"
)

// FileValue supplies captured file metadata. HTTP UploadedFile implements it;
// domain adapters must supply the same truthful metadata contract. These rules
// perform no I/O and never consult a client-declared content type. Metadata
// methods execute within the ordinary owned validation callback boundary.
type FileValue interface {
	IsZero() bool
	Size() int64
	ContentType() string
	Extension() string
}

// FilePresent rejects an absent handle. A captured zero-byte file is present;
// use FileMinSize(1) when nonempty content is required. Optional and Required
// adapters retain the separate question of whether the form part was supplied.
func FilePresent[F FileValue]() Rule[F] {
	return valueRule(Spec{ID: "foundry.file_present"}, true, func(_ *execution, file F) (bool, error) { return !file.IsZero() && file.Size() >= 0, nil })
}

// FileMinSize compares exact byte counts, including an inclusive zero bound.
func FileMinSize[F FileValue](minimum int64) Rule[F] {
	if minimum < 0 {
		return failed[F](invalid("minimum file size must not be negative"))
	}
	return valueRule(Spec{ID: "foundry.file_min_size", Parameters: []Parameter{scalarParameter("bytes", minimum)}}, true, func(_ *execution, file F) (bool, error) { return !file.IsZero() && file.Size() >= minimum, nil })
}

// FileMaxSize compares bytes without converting through floating point.
func FileMaxSize[F FileValue](maximum int64) Rule[F] {
	if maximum < 0 {
		return failed[F](invalid("maximum file size must not be negative"))
	}
	return valueRule(Spec{ID: "foundry.file_max_size", Parameters: []Parameter{scalarParameter("bytes", maximum)}}, true, func(_ *execution, file F) (bool, error) {
		if file.IsZero() {
			return false, nil
		}
		size := file.Size()
		return size >= 0 && size <= maximum, nil
	})
}

// FileContentTypes compares detected MIME media types. Declarations may use an
// exact type, a major-type wildcard such as image/*, or */*. Parameters such as
// charset do not participate in matching. The HTTP detector is a bounded sniff,
// not full image/document validation; imaging validation belongs to that adapter.
// Rules remain server-only because browser file.type is client metadata.
func FileContentTypes[F FileValue](allowed ...string) Rule[F] {
	members, err := fileMembership(allowed, func(text string) (string, bool) { return fileMediaType(text, false) })
	if err != nil {
		return failed[F](err)
	}
	return valueRule(Spec{ID: "foundry.file_content_types", Parameters: members.info.Spec.Parameters}, true, func(s *execution, file F) (bool, error) {
		if file.IsZero() {
			return false, nil
		}
		input := file.ContentType()
		if !textValue(s, input) {
			return false, nil
		}
		media, ok := fileMediaType(input, true)
		if !ok || strings.Contains(media, "*") {
			return false, nil
		}
		major, _, _ := strings.Cut(media, "/")
		for _, candidate := range []string{media, major + "/*", "*/*"} {
			if accepted, err := members.leaf(s, candidate); accepted || err != nil {
				return accepted, err
			}
		}
		return false, nil
	})
}

// FileExtensions compares the normalized final filename extension without a
// leading dot. An allowed extension never proves the file's actual content;
// combine it with a detected content-type rule when that behavior is required.
func FileExtensions[F FileValue](allowed ...string) Rule[F] {
	members, err := fileMembership(allowed, fileExtension)
	if err != nil {
		return failed[F](err)
	}
	return valueRule(Spec{ID: "foundry.file_extensions", Parameters: members.info.Spec.Parameters}, true, func(s *execution, file F) (bool, error) {
		if file.IsZero() {
			return false, nil
		}
		input := file.Extension()
		if !textValue(s, input) {
			return false, nil
		}
		extension, ok := fileExtension(input)
		if !ok {
			return false, nil
		}
		return members.leaf(s, extension)
	})
}

// Reuse OneOf's bound, duplicate detection, ownership and metadata rather than
// maintaining a second membership registry for MIME types and extensions.
func fileMembership(allowed []string, normalize func(string) (string, bool)) (Rule[string], error) {
	if len(allowed) == 0 || len(allowed) >= maxRuleNodes {
		return Rule[string]{}, invalid("file validation requires a bounded nonempty allow list")
	}
	values := make([]string, len(allowed))
	for i, item := range allowed {
		if !validText(item, false) {
			return Rule[string]{}, invalid("invalid file validation declaration")
		}
		normalized, ok := normalize(item)
		if !ok {
			return Rule[string]{}, invalid("invalid file validation declaration")
		}
		values[i] = normalized
	}
	rule := OneOf(values...)
	return rule, rule.Validate()
}

func fileMediaType(text string, parameters bool) (string, bool) {
	media, params, err := mime.ParseMediaType(text)
	if err != nil || !parameters && len(params) != 0 {
		return "", false
	}
	major, minor, slash := strings.Cut(media, "/")
	if !slash || major == "" || minor == "" || strings.Contains(minor, "/") || strings.Contains(major, "*") && major != "*" || strings.Contains(minor, "*") && minor != "*" || major == "*" && minor != "*" {
		return "", false
	}
	return media, true
}
func fileExtension(text string) (string, bool) {
	extension := filename.Extension("file." + text)
	return extension, extension != "" && extension == strings.ToLower(text)
}
