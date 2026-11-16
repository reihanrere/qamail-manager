package handlers

import (
	"errors"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/reihanrere/qamail-manager/internal/models"
	"github.com/reihanrere/qamail-manager/internal/services"
	"github.com/reihanrere/qamail-manager/internal/utils"
)

// SessionCookieName is the cookie that carries the session token.
const SessionCookieName = "qamail_session"

// userLocalsKey is where RequireAuth stores the logged-in user for the handlers behind it.
const userLocalsKey = "authUser"

// maxLoggedEmailLength keeps attacker-chosen input short in the logs.
const maxLoggedEmailLength = 100

// AuthHandler exposes login, logout and the current user, and guards the rest of the API.
type AuthHandler struct {
	service        *services.AuthService
	cookieSecure   bool
	allowedOrigins []string
}

// NewAuthHandler builds the handler. cookieSecure marks the session cookie Secure (HTTPS only).
// allowedOrigins (CORS_ALLOWED_ORIGINS) are the browser origins besides the app's own that may
// call login.
func NewAuthHandler(service *services.AuthService, cookieSecure bool, allowedOrigins []string) *AuthHandler {
	return &AuthHandler{service: service, cookieSecure: cookieSecure, allowedOrigins: allowedOrigins}
}

// isJSONRequest reports whether the body is declared as JSON. Forms can only send
// urlencoded, multipart or text/plain, so requiring JSON keeps cross-site forms out.
func isJSONRequest(c fiber.Ctx) bool {
	mediaType, _, _ := strings.Cut(c.Get(fiber.HeaderContentType), ";")
	return strings.EqualFold(strings.TrimSpace(mediaType), fiber.MIMEApplicationJSON)
}

// originAllowed reports whether a browser request comes from the app's own origin or a
// listed one. A request without an Origin header (curl, scripts, older browsers) passes:
// only browsers can be steered by another site, and they send Origin on every POST.
func (h *AuthHandler) originAllowed(c fiber.Ctx) bool {
	origin := c.Get(fiber.HeaderOrigin)
	if origin == "" {
		return true
	}
	if slices.Contains(h.allowedOrigins, strings.TrimRight(origin, "/")) {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false // "null" (sandboxed pages, some redirects) and malformed values
	}
	return strings.EqualFold(parsed.Host, c.Host())
}

// loginRequest is the body for POST /api/auth/login.
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// userResponse is the part of a user the client may see.
type userResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

func toUserResponse(u models.User) userResponse {
	return userResponse{ID: u.ID.String(), Email: u.Email, Role: u.Role}
}

// CurrentUser returns the user RequireAuth attached to the request, or nil outside it.
func CurrentUser(c fiber.Ctx) *models.User {
	user, _ := c.Locals(userLocalsKey).(*models.User)
	return user
}

// userID is the logged-in user's id, or uuid.Nil outside RequireAuth. The services refuse
// uuid.Nil, so a handler mounted without the guard fails closed instead of seeing all data.
func userID(c fiber.Ctx) uuid.UUID {
	if user := CurrentUser(c); user != nil {
		return user.ID
	}
	return uuid.Nil
}

// RequireAuth rejects requests without a valid session cookie with 401 and otherwise puts the
// user on the request. Register it once on the /api group, before the routes it protects.
func (h *AuthHandler) RequireAuth(c fiber.Ctx) error {
	user, err := h.service.Authenticate(c.Context(), c.Cookies(SessionCookieName))
	if err != nil {
		return respondError(c, err, http.StatusInternalServerError)
	}
	c.Locals(userLocalsKey, user)
	return c.Next()
}

// RequireAdmin answers 403 unless the logged-in user is an admin. Put it after RequireAuth,
// on the routes only admins may use.
func (h *AuthHandler) RequireAdmin(c fiber.Ctx) error {
	if user := CurrentUser(c); user == nil || !user.IsAdmin() {
		return respondError(c, services.ErrForbidden, http.StatusForbidden)
	}
	return c.Next()
}

// RequireSameOrigin refuses a browser request that another site sent (login CSRF's cousin):
// for the admin routes that change users, on top of the SameSite cookie. Requests without an
// Origin header (curl, scripts) pass, like on login.
func (h *AuthHandler) RequireSameOrigin(c fiber.Ctx) error {
	if !h.originAllowed(c) {
		return utils.Error(c, http.StatusForbidden, codeCrossOrigin, "cross-origin request refused", nil)
	}
	return c.Next()
}

// Login handles POST /api/auth/login.
func (h *AuthHandler) Login(c fiber.Ctx) error {
	// Login needs no cookie, so SameSite cannot protect it: without these checks another
	// site could log the victim's browser into the attacker's account (login CSRF)
	if !isJSONRequest(c) {
		return utils.Error(c, http.StatusBadRequest, codeInvalidBody, "invalid request body", nil)
	}
	if !h.originAllowed(c) {
		return utils.Error(c, http.StatusForbidden, codeCrossOrigin, "cross-origin request refused", nil)
	}
	var body loginRequest
	if err := c.Bind().JSON(&body); err != nil {
		return utils.Error(c, http.StatusBadRequest, codeInvalidBody, "invalid request body", nil)
	}
	ip := c.IP()

	result, err := h.service.Login(c.Context(), body.Email, body.Password, ip)
	if err != nil {
		var tooMany *services.TooManyAttemptsError
		switch {
		case errors.As(err, &tooMany):
			c.Set(fiber.HeaderRetryAfter, strconv.Itoa(retryAfterSeconds(tooMany.RetryAfter)))
			log.Printf("auth: login locked out email=%q ip=%s", loggable(body.Email), ip)
		case errors.Is(err, services.ErrLoginBusy):
			c.Set(fiber.HeaderRetryAfter, "1")
			log.Printf("auth: login busy email=%q ip=%s", loggable(body.Email), ip)
		case errors.Is(err, services.ErrInvalidCredentials):
			log.Printf("auth: login failed email=%q ip=%s", loggable(body.Email), ip)
		}
		return respondError(c, err, http.StatusInternalServerError)
	}

	log.Printf("auth: login ok user=%s ip=%s", result.User.Email, ip)
	h.setSessionCookie(c, result.Token, result.ExpiresAt)
	return utils.Success(c, http.StatusOK, "Success", toUserResponse(result.User))
}

// Logout handles POST /api/auth/logout: revokes the session and clears the cookie.
func (h *AuthHandler) Logout(c fiber.Ctx) error {
	if err := h.service.Logout(c.Context(), c.Cookies(SessionCookieName)); err != nil {
		return respondError(c, err, http.StatusInternalServerError)
	}
	h.setSessionCookie(c, "", time.Unix(0, 0))
	return utils.Success(c, http.StatusOK, "Success", nil)
}

// Me handles GET /api/auth/me.
func (h *AuthHandler) Me(c fiber.Ctx) error {
	return utils.Success(c, http.StatusOK, "Success", toUserResponse(*CurrentUser(c)))
}

func (h *AuthHandler) setSessionCookie(c fiber.Ctx, token string, expires time.Time) {
	cookie := &fiber.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HTTPOnly: true,
		Secure:   h.cookieSecure,
		SameSite: fiber.CookieSameSiteLaxMode,
	}
	if token == "" {
		cookie.MaxAge = -1
	}
	c.Cookie(cookie)
}

// retryAfterSeconds rounds up so the client never retries too early.
func retryAfterSeconds(d time.Duration) int {
	seconds := int((d + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return seconds
}

// loggable trims and flattens client input before it goes into a log line.
func loggable(s string) string {
	s = strings.NewReplacer("\n", " ", "\r", " ").Replace(strings.TrimSpace(s))
	if len(s) > maxLoggedEmailLength {
		s = s[:maxLoggedEmailLength]
	}
	return s
}
