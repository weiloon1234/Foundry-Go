package s3

import (
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/storage"
)

// Provider text, signed URLs and credential details remain only in Unwrap.
// HTTP rejection can establish Unchanged; an interrupted response after a
// publishing request remains Unknown and must never trigger an automatic retry.
func failure(op storage.Operation, state storage.Outcome, err error) *storage.Error {
	var result *storage.Error
	inspection := callback.Isolated("S3 error classification", func() error {
		result = classifyFailure(op, state, err)
		return nil
	})
	if inspection != nil {
		return storage.Failure(storage.Unavailable, op, state, inspection)
	}
	return result
}

func classifyFailure(op storage.Operation, state storage.Outcome, err error) *storage.Error {
	code := storage.Unavailable
	candidates := [...]storage.Code{storage.NotFound, storage.Forbidden, storage.Unsupported, storage.PreconditionFailed, storage.RangeNotSatisfiable, storage.LimitExceeded, storage.Invalid, storage.IntegrityFailed, storage.Closed}
	selected := len(candidates)
	var own *storage.Error
	var api smithy.APIError
	var response *smithyhttp.ResponseError
	var apiMatched, responseMatched bool
	complete := errorgraph.Walk(err, func(current error) bool {
		if detail, matched := errorgraph.AsShallow[*storage.Error](current); matched {
			own = detail
			return false
		}
		for i := 0; i < selected; i++ {
			if errorgraph.Matches(current, candidates[i]) {
				selected = i
				break
			}
		}
		if !apiMatched {
			api, apiMatched = errorgraph.AsShallow[smithy.APIError](current)
		}
		if !responseMatched {
			response, responseMatched = errorgraph.AsShallow[*smithyhttp.ResponseError](current)
		}
		return true
	})
	if !complete {
		return storage.Failure(code, op, state, err)
	}
	if selected < len(candidates) {
		code = candidates[selected]
	}
	if own != nil {
		result := storage.Failure(own.Code(), op, state, err)
		if id, ok := own.Cleanup().Get(); ok {
			result = result.WithCleanup(id)
		}
		return result
	}
	if api != nil {
		if api.ErrorCode() == "NoSuchBucket" {
			if state == storage.Unknown {
				state = storage.Unchanged
			}
			return storage.Failure(storage.Unavailable, op, state, err)
		}
		switch api.ErrorCode() {
		case "NoSuchKey", "NoSuchVersion", "NotFound":
			code = storage.NotFound
		case "AccessDenied", "InvalidAccessKeyId", "SignatureDoesNotMatch", "ExpiredToken", "InvalidToken":
			code = storage.Forbidden
		case "PreconditionFailed", "ConditionalRequestConflict":
			code = storage.PreconditionFailed
		case "InvalidRange":
			code = storage.RangeNotSatisfiable
		case "BadDigest", "InvalidDigest":
			code = storage.IntegrityFailed
		case "EntityTooLarge":
			code = storage.LimitExceeded
		}
	}
	if response != nil {
		status := response.HTTPStatusCode()
		if state == storage.Unknown && status >= 400 && status < 500 && status != 408 && status != 429 {
			state = storage.Unchanged
		}
		if code == storage.Unavailable {
			switch status {
			case 403:
				code = storage.Forbidden
			case 404:
				code = storage.NotFound
			case 409, 412:
				code = storage.PreconditionFailed
			case 416:
				code = storage.RangeNotSatisfiable
			}
		}
	}
	return storage.Failure(code, op, state, err)
}

func noSuchUpload(err error) bool {
	var api smithy.APIError
	errorgraph.Walk(err, func(current error) bool {
		var matched bool
		api, matched = errorgraph.AsShallow[smithy.APIError](current)
		return !matched
	})
	return api != nil && api.ErrorCode() == "NoSuchUpload"
}
