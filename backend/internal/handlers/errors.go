package handlers

import (
	"errors"
	"net/http"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/reihanrere/qamail-manager/internal/auth"
	"github.com/reihanrere/qamail-manager/internal/mailtm"
	"github.com/reihanrere/qamail-manager/internal/providers"
	"github.com/reihanrere/qamail-manager/internal/services"
	"github.com/reihanrere/qamail-manager/internal/utils"
)

// errorStatus maps a service or Mail.tm error to an HTTP status code.
// Unrecognised errors get fallback (e.g. 502 when Mail.tm itself failed).
func errorStatus(err error, fallback int) int {
	var apiErr *mailtm.APIError
	var tooMany *services.TooManyAttemptsError
	switch {
	case errors.Is(err, services.ErrInvalidCredentials), errors.Is(err, services.ErrUnauthenticated):
		return http.StatusUnauthorized
	case errors.As(err, &tooMany), errors.Is(err, services.ErrLoginBusy):
		return http.StatusTooManyRequests
	case errors.Is(err, services.ErrForbidden):
		return http.StatusForbidden
	case isQuotaError(err):
		return http.StatusForbidden
	case errors.Is(err, errInvalidAccountID), errors.Is(err, errInvalidUserID), errors.Is(err, services.ErrInvalidInput),
		errors.Is(err, services.ErrInvalidRole), errors.Is(err, auth.ErrInvalidEmail),
		errors.Is(err, auth.ErrPasswordTooShort), errors.Is(err, auth.ErrPasswordTooLong):
		return http.StatusBadRequest
	case errors.Is(err, services.ErrUserExists), errors.Is(err, services.ErrUserHasAccount),
		errors.Is(err, services.ErrLastAdmin), errors.Is(err, services.ErrSelfAction):
		return http.StatusConflict
	case errors.Is(err, services.ErrUserNotFound):
		return http.StatusNotFound
	case errors.Is(err, services.ErrAccountNotFound), errors.Is(err, providers.ErrMessageNotFound):
		return http.StatusNotFound
	case errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound:
		// e.g. a message id that no longer exists on Mail.tm
		return http.StatusNotFound
	default:
		return fallback
	}
}

// isQuotaError matches the validation error for a full account quota, which is a 403
// (the request is fine, the user's allowance is not).
func isQuotaError(err error) bool {
	v, ok := services.AsValidationError(err)
	return ok && v.Code == "quota_exceeded"
}

// Error codes for failures that are not a services.ValidationError.
const (
	codeInvalidBody      = "invalid_request_body"
	codeInvalidAccountID = "invalid_account_id"
	codeInvalidInput     = "invalid_input"
	codeAccountNotFound  = "account_not_found"
	codeMessageNotFound  = "message_not_found"
	codeInvalidLogin     = "invalid_credentials"
	codeUnauthenticated  = "unauthenticated"
	codeAdminRequired    = "admin_required"
	codeTooManyLogins    = "too_many_login_attempts"
	codeCrossOrigin      = "cross_origin_request"
	codeLoginBusy        = "login_busy"
	codeInvalidUserID    = "invalid_user_id"
	codeUserNotFound     = "user_not_found"
	codeUserExists       = "user_exists"
	codeUserHasAccounts  = "user_has_accounts"
	codeLastAdmin        = "last_admin"
	codeSelfAction       = "cannot_modify_self"
	codeInvalidEmail     = "invalid_email"
	codePasswordShort    = "password_too_short"
	codePasswordLong     = "password_too_long"
	codeInvalidRole      = "invalid_role"
	codeUpstream         = "upstream_error"
	codeInternal         = "internal_error"
)

// errorCode picks the code clients translate; params are only set for validation errors.
func errorCode(err error, status int) (string, map[string]any) {
	if v, ok := services.AsValidationError(err); ok {
		return v.Code, v.Params
	}
	var tooMany *services.TooManyAttemptsError
	switch {
	case errors.Is(err, services.ErrInvalidCredentials):
		return codeInvalidLogin, nil
	case errors.Is(err, services.ErrUnauthenticated):
		return codeUnauthenticated, nil
	case errors.Is(err, services.ErrForbidden):
		return codeAdminRequired, nil
	case errors.As(err, &tooMany):
		return codeTooManyLogins, map[string]any{"retryAfterSeconds": retryAfterSeconds(tooMany.RetryAfter)}
	case errors.Is(err, services.ErrLoginBusy):
		return codeLoginBusy, nil
	case errors.Is(err, errInvalidAccountID):
		return codeInvalidAccountID, nil
	case errors.Is(err, errInvalidUserID):
		return codeInvalidUserID, nil
	case errors.Is(err, services.ErrUserNotFound):
		return codeUserNotFound, nil
	case errors.Is(err, services.ErrUserExists):
		return codeUserExists, nil
	case errors.Is(err, services.ErrUserHasAccount):
		return codeUserHasAccounts, nil
	case errors.Is(err, services.ErrLastAdmin):
		return codeLastAdmin, nil
	case errors.Is(err, services.ErrSelfAction):
		return codeSelfAction, nil
	case errors.Is(err, auth.ErrInvalidEmail):
		return codeInvalidEmail, nil
	case errors.Is(err, auth.ErrPasswordTooShort):
		return codePasswordShort, nil
	case errors.Is(err, auth.ErrPasswordTooLong):
		return codePasswordLong, map[string]any{"max": auth.MaxPasswordBytes}
	case errors.Is(err, services.ErrInvalidRole):
		return codeInvalidRole, nil
	case errors.Is(err, services.ErrInvalidInput):
		return codeInvalidInput, nil
	case errors.Is(err, services.ErrAccountNotFound):
		return codeAccountNotFound, nil
	case status == http.StatusNotFound:
		return codeMessageNotFound, nil
	case status == http.StatusBadGateway:
		return codeUpstream, nil
	default:
		return codeInternal, nil
	}
}

// respondError writes the error envelope with the status chosen by errorStatus.
func respondError(c fiber.Ctx, err error, fallback int) error {
	status := errorStatus(err, fallback)
	code, params := errorCode(err, status)
	return utils.Error(c, status, code, err.Error(), params)
}

// errInvalidAccountID is returned by parseAccountID for a malformed :id parameter.
var errInvalidAccountID = errors.New("invalid account id")

// parseAccountID reads the :id route parameter as a UUID.
func parseAccountID(c fiber.Ctx) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return uuid.Nil, errInvalidAccountID
	}
	return id, nil
}
