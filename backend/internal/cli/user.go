// Package cli implements the "user" command of the backend binary, the only way to manage
// users: there is no public signup and no admin screen.
package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"golang.org/x/term"

	"github.com/reihanrere/qamail-manager/internal/auth"
	"github.com/reihanrere/qamail-manager/internal/models"
	"github.com/reihanrere/qamail-manager/internal/services"
)

// Usage is printed for a missing or unknown subcommand.
const Usage = `Usage: api user <command>

Commands:
  add <email> [--admin]      create a user (role "user", or "admin" with --admin)
  list                       list users with their account counts
  passwd <email>             set a new password and sign the user out everywhere
  disable <email>            block logins and end active sessions; data is kept
  enable <email>             allow logins again
  delete <email> [--force]   delete a user; refused while they own accounts,
                             --force also deletes those accounts and their stored mail

The password is read from the terminal (hidden) or, when input is piped, from the first
line of stdin. It is never accepted as an argument, so it stays out of the process list
and the shell history.

Inside Docker:
  docker compose exec backend ./api user add someone@example.com
  printf '%s\n' "$PW" | docker compose exec -T backend ./api user add someone@example.com
`

// UserManager is what the command needs from the user service.
type UserManager interface {
	Create(ctx context.Context, email, password, role string) (*models.User, error)
	List(ctx context.Context) ([]services.UserSummary, error)
	SetPassword(ctx context.Context, email, password string) error
	SetDisabled(ctx context.Context, email string, disabled bool) error
	Delete(ctx context.Context, email string, force bool) (int64, error)
}

// PasswordReader returns a new password. confirm asks for it twice when interactive.
type PasswordReader func(prompt string, confirm bool) (string, error)

// RunUser executes `user <args...>` and writes results to out.
func RunUser(ctx context.Context, args []string, users UserManager, readPassword PasswordReader, out io.Writer) error {
	if len(args) == 0 {
		return errors.New(Usage)
	}
	command, flags, positional, err := parseArgs(args)
	if err != nil {
		return err
	}
	needEmail := func() (string, error) {
		if len(positional) != 1 {
			return "", fmt.Errorf("%s needs exactly one <email> (passwords are never passed as arguments)\n\n%s", command, Usage)
		}
		return positional[0], nil
	}
	only := func(allowed ...string) error {
		for flag := range flags {
			ok := false
			for _, a := range allowed {
				ok = ok || a == flag
			}
			if !ok {
				return fmt.Errorf("%s does not take %s", command, flag)
			}
		}
		return nil
	}

	switch command {
	case "add":
		if err := only("--admin"); err != nil {
			return err
		}
		email, err := needEmail()
		if err != nil {
			return err
		}
		password, err := readPassword("Password for "+email+": ", true)
		if err != nil {
			return err
		}
		role := models.RoleUser
		if flags["--admin"] {
			role = models.RoleAdmin
		}
		user, err := users.Create(ctx, email, password, role)
		if err != nil {
			return friendly(err)
		}
		fmt.Fprintf(out, "created %s %s\n", user.Role, user.Email)
		return nil

	case "list":
		if err := only(); err != nil || len(positional) > 0 {
			return fmt.Errorf("list takes no arguments")
		}
		list, err := users.List(ctx)
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "EMAIL\tROLE\tSTATUS\tACCOUNTS\tCREATED")
		for _, u := range list {
			status := "enabled"
			if u.Disabled {
				status = "disabled"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", u.Email, u.Role, status, u.Accounts, u.CreatedAt.Format("2006-01-02"))
		}
		return w.Flush()

	case "passwd":
		if err := only(); err != nil {
			return err
		}
		email, err := needEmail()
		if err != nil {
			return err
		}
		password, err := readPassword("New password for "+email+": ", true)
		if err != nil {
			return err
		}
		if err := users.SetPassword(ctx, email, password); err != nil {
			return friendly(err)
		}
		fmt.Fprintf(out, "password changed for %s; active sessions ended\n", auth.NormalizeEmail(email))
		return nil

	case "disable", "enable":
		if err := only(); err != nil {
			return err
		}
		email, err := needEmail()
		if err != nil {
			return err
		}
		if err := users.SetDisabled(ctx, email, command == "disable"); err != nil {
			return friendly(err)
		}
		fmt.Fprintf(out, "%sd %s\n", command, auth.NormalizeEmail(email))
		return nil

	case "delete":
		if err := only("--force"); err != nil {
			return err
		}
		email, err := needEmail()
		if err != nil {
			return err
		}
		accounts, err := users.Delete(ctx, email, flags["--force"])
		if err != nil {
			return friendly(err)
		}
		fmt.Fprintf(out, "deleted %s", auth.NormalizeEmail(email))
		if accounts > 0 {
			fmt.Fprintf(out, " and %d mail account(s) with their stored messages (mailboxes at the provider are untouched)", accounts)
		}
		fmt.Fprintln(out)
		return nil
	}
	return fmt.Errorf("unknown command %q\n\n%s", command, Usage)
}

// parseArgs splits a command line into the command, its flags and its plain arguments, in
// any order ("add --admin x@y" and "add x@y --admin" are the same).
func parseArgs(args []string) (command string, flags map[string]bool, positional []string, err error) {
	command = args[0]
	flags = map[string]bool{}
	for _, arg := range args[1:] {
		if strings.HasPrefix(arg, "-") {
			if arg != "--admin" && arg != "--force" {
				return "", nil, nil, fmt.Errorf("unknown flag %q\n\n%s", arg, Usage)
			}
			flags[arg] = true
			continue
		}
		positional = append(positional, arg)
	}
	return command, flags, positional, nil
}

// friendly turns service errors into messages meant for a person at a terminal.
func friendly(err error) error {
	switch {
	case errors.Is(err, auth.ErrInvalidEmail):
		return errors.New("that is not a valid email address")
	case errors.Is(err, auth.ErrPasswordTooShort):
		return errors.New("the password is too short (see PASSWORD_MIN_LENGTH)")
	case errors.Is(err, auth.ErrPasswordTooLong):
		return fmt.Errorf("the password is too long (at most %d bytes)", auth.MaxPasswordBytes)
	case errors.Is(err, services.ErrUserExists):
		return errors.New("a user with this email already exists")
	case errors.Is(err, services.ErrUserNotFound):
		return errors.New("no user with this email")
	case errors.Is(err, services.ErrUserHasAccount):
		return errors.New("this user still owns mail accounts; use --force to delete them too")
	case errors.Is(err, services.ErrLastAdmin):
		return errors.New("refused: this is the only enabled admin; create or enable another admin first")
	}
	return err
}

// TerminalPasswordReader reads a password from stdin: hidden from the terminal (asked twice
// when confirm is set), or the first line when stdin is a pipe. Prompts go to stderr.
func TerminalPasswordReader(stdin *os.File, stderr io.Writer) PasswordReader {
	piped := bufio.NewReader(stdin)
	return func(prompt string, confirm bool) (string, error) {
		fd := int(stdin.Fd())
		if !term.IsTerminal(fd) {
			line, err := piped.ReadString('\n')
			if err != nil && (!errors.Is(err, io.EOF) || line == "") {
				return "", errors.New("no password on stdin")
			}
			return strings.TrimRight(line, "\r\n"), nil
		}
		fmt.Fprint(stderr, prompt)
		first, err := term.ReadPassword(fd)
		fmt.Fprintln(stderr)
		if err != nil {
			return "", err
		}
		if confirm {
			fmt.Fprint(stderr, "Repeat it: ")
			second, err := term.ReadPassword(fd)
			fmt.Fprintln(stderr)
			if err != nil {
				return "", err
			}
			if string(first) != string(second) {
				return "", errors.New("the two passwords do not match")
			}
		}
		return string(first), nil
	}
}
