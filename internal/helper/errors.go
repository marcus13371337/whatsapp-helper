package helper

import "fmt"

type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

func Errorf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

const (
	CodeBadRequest          = "bad_request"
	CodeUnknownMethod       = "unknown_method"
	CodeHelloFirst          = "hello_first"
	CodeUnsupportedProtocol = "unsupported_protocol"
	CodeBadFolder           = "bad_folder"
	CodeNotLoggedIn         = "not_logged_in"
	CodeAlreadyLoggedIn     = "already_logged_in"
	CodeBadSession          = "bad_session"
	CodeNotConnected        = "not_connected"
	CodeNotFound            = "not_found"
	CodeNotAvailable        = "not_available"
	CodeOutsideFolder       = "outside_folder"
	CodeFileProblem         = "file_problem"
	CodeTooLarge            = "too_large"
	CodeMetaRefused         = "meta_refused"
	CodePairingTimeout      = "pairing_timeout"
	CodeBadPhone            = "bad_phone"
	CodePairingFailed       = "pairing_failed"
	CodeInternal            = "internal"
)

var errInternal = &Error{Code: CodeInternal, Message: "Something went wrong inside the helper. Its log has the details."}
