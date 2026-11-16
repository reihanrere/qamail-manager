package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/reihanrere/qamail-manager/internal/models"
	"github.com/reihanrere/qamail-manager/internal/services"
	"github.com/reihanrere/qamail-manager/internal/utils"
)

// UserHandler is the admin API for managing users. Every route sits behind RequireAdmin,
// and the routes that change users also behind RequireSameOrigin (see routes.go).
type UserHandler struct {
	users    *services.UserService
	accounts *services.AccountService
}

// NewUserHandler builds the handler; accounts supplies each user's quota.
func NewUserHandler(users *services.UserService, accounts *services.AccountService) *UserHandler {
	return &UserHandler{users: users, accounts: accounts}
}

// managedUser is what the admin sees of a user. It never carries the password or its hash.
type managedUser struct {
	ID        string         `json:"id"`
	Email     string         `json:"email"`
	Role      string         `json:"role"`
	Disabled  bool           `json:"disabled"`
	Accounts  int64          `json:"accounts"`
	Quota     services.Quota `json:"quota"`
	CreatedAt time.Time      `json:"createdAt"`
}

func (h *UserHandler) toManagedUser(c fiber.Ctx, u models.User, accounts int64) (managedUser, error) {
	quota, err := h.accounts.Quota(c.Context(), u.ID)
	if err != nil {
		return managedUser{}, err
	}
	return managedUser{
		ID: u.ID.String(), Email: u.Email, Role: u.Role, Disabled: u.Disabled,
		Accounts: accounts, Quota: quota, CreatedAt: u.CreatedAt,
	}, nil
}

type createUserRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type updateUserRequest struct {
	Disabled *bool   `json:"disabled"`
	Password *string `json:"password"`
}

// List handles GET /api/users.
func (h *UserHandler) List(c fiber.Ctx) error {
	list, err := h.users.List(c.Context())
	if err != nil {
		return respondError(c, err, http.StatusInternalServerError)
	}
	out := make([]managedUser, 0, len(list))
	for _, item := range list {
		user, err := h.toManagedUser(c, item.User, item.Accounts)
		if err != nil {
			return respondError(c, err, http.StatusInternalServerError)
		}
		out = append(out, user)
	}
	return utils.Success(c, http.StatusOK, "Success", out)
}

// Create handles POST /api/users. The role defaults to "user".
func (h *UserHandler) Create(c fiber.Ctx) error {
	var body createUserRequest
	if !isJSONRequest(c) || c.Bind().JSON(&body) != nil {
		return utils.Error(c, http.StatusBadRequest, codeInvalidBody, "invalid request body", nil)
	}
	if strings.TrimSpace(body.Role) == "" {
		body.Role = models.RoleUser
	}
	created, err := h.users.Create(c.Context(), body.Email, body.Password, body.Role)
	if err != nil {
		return respondError(c, err, http.StatusInternalServerError)
	}
	user, err := h.toManagedUser(c, *created, 0)
	if err != nil {
		return respondError(c, err, http.StatusInternalServerError)
	}
	return utils.Success(c, http.StatusCreated, "Success", user)
}

// Update handles PATCH /api/users/:id: disable or enable, and reset the password. Either
// signs the user out of every session.
func (h *UserHandler) Update(c fiber.Ctx) error {
	id, err := parseUserID(c)
	if err != nil {
		return respondError(c, err, http.StatusBadRequest)
	}
	var body updateUserRequest
	if !isJSONRequest(c) || c.Bind().JSON(&body) != nil {
		return utils.Error(c, http.StatusBadRequest, codeInvalidBody, "invalid request body", nil)
	}
	if body.Disabled == nil && body.Password == nil {
		return respondError(c, fmt.Errorf("%w: nothing to change", services.ErrInvalidInput), http.StatusBadRequest)
	}
	changes := services.UserChanges{Disabled: body.Disabled, Password: body.Password}
	if err := h.users.UpdateByID(c.Context(), userID(c), id, changes); err != nil {
		return respondError(c, err, http.StatusInternalServerError)
	}
	return utils.Success(c, http.StatusOK, "Success", nil)
}

// Delete handles DELETE /api/users/:id. A user who still owns accounts is refused with 409
// (and the count) unless ?force=true, which deletes the accounts and stored mail too.
func (h *UserHandler) Delete(c fiber.Ctx) error {
	id, err := parseUserID(c)
	if err != nil {
		return respondError(c, err, http.StatusBadRequest)
	}
	force := strings.EqualFold(c.Query("force"), "true")
	accounts, err := h.users.DeleteByID(c.Context(), userID(c), id, force)
	if errors.Is(err, services.ErrUserHasAccount) {
		return utils.Error(c, http.StatusConflict, codeUserHasAccounts, err.Error(), map[string]any{"accounts": accounts})
	}
	if err != nil {
		return respondError(c, err, http.StatusInternalServerError)
	}
	return utils.Success(c, http.StatusOK, "Success", fiber.Map{"accounts": accounts})
}

// errInvalidUserID is returned by parseUserID for a malformed :id parameter.
var errInvalidUserID = errors.New("invalid user id")

func parseUserID(c fiber.Ctx) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return uuid.Nil, errInvalidUserID
	}
	return id, nil
}
