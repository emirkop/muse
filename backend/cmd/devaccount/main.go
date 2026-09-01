package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"muse-backend/internal/identity/application"
	"muse-backend/internal/identity/domain"
	"muse-backend/internal/identity/infrastructure"
	"muse-backend/internal/platform/database"
)

const (
	defaultEmail    = "dev@muse.local"
	defaultPassword = "MuseDev123!"
	defaultAvatar   = "avatar_1"
)

func main() {
	email := flag.String("email", defaultEmail, "email address for the development account")
	password := flag.String("password", defaultPassword, "password for the development account")
	avatar := flag.String("avatar", defaultAvatar, "one of the five predefined avatar ids (avatar_1 … avatar_5)")
	showExisting := flag.Bool("show-existing", false, "report the account's state and exit without writing anything")
	flag.Parse()

	if err := run(context.Background(), *email, *password, *avatar, *showExisting); err != nil {
		fmt.Fprintf(os.Stderr, "devaccount: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, rawEmail, rawPassword, rawAvatar string, showExisting bool) error {
	if env := os.Getenv("APP_ENV"); env != "development" {
		if env == "" {
			return errors.New("APP_ENV is not set — this command runs only with APP_ENV=development explicitly set")
		}
		return fmt.Errorf("APP_ENV is %q — this command runs only in development", env)
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required (the local development database)")
	}

	email, err := domain.NormaliseEmail(rawEmail)
	if err != nil {
		return fmt.Errorf("email %q: %w", rawEmail, err)
	}
	if err := domain.ValidatePassword(rawPassword); err != nil {
		return fmt.Errorf("password: %w (policy: %d–%d characters)",
			err, domain.PasswordMinimumLength, domain.PasswordMaximumLength)
	}
	avatarID := domain.AvatarID(rawAvatar)
	if !domain.IsValidAvatarID(avatarID) {
		return fmt.Errorf("avatar %q is not one of the five predefined avatars %v", rawAvatar, domain.AvailableAvatarIDs)
	}

	pool, err := database.Connect(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()
	if err := pool.ApplyMigrations(ctx); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}

	credentials := infrastructure.NewPostgresPasswordRepository(pool.Pool())
	accounts := application.NewAccountService(infrastructure.NewPostgresAccountRepository(pool.Pool()))

	existing, err := credentials.FindByEmail(ctx, email)
	switch {
	case err == nil:
		return reportExisting(ctx, accounts, existing, showExisting)
	case errors.Is(err, domain.ErrCredentialNotFound):
		if showExisting {
			fmt.Printf("No account exists for %s.\n", email)
			return nil
		}
	default:
		return fmt.Errorf("look up %s: %w", email, err)
	}

	hasher := infrastructure.NewDefaultArgon2idHasher()
	hash, err := hasher.Hash(rawPassword)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	now := time.Now()
	account, err := credentials.CreateAccountWithCredential(ctx,
		domain.Account{CreatedAt: now, UpdatedAt: now},
		domain.PasswordCredential{Email: email, Hash: hash, CreatedAt: now, UpdatedAt: now},
	)
	if err != nil {
		if errors.Is(err, domain.ErrEmailAlreadyRegistered) {
			return fmt.Errorf("an account for %s was created concurrently — re-run with -show-existing to inspect it", email)
		}
		return fmt.Errorf("create account: %w", err)
	}

	if err := accounts.UpdateAvatar(ctx, account.ID, avatarID); err != nil {
		return fmt.Errorf("assign avatar: %w (the account exists — re-run to finish setting it up)", err)
	}

	fmt.Printf(`Created a development account.

  email      %s
  password   %s
  account id %s
  avatar     %s

Nothing else was created: no Museum (that is a product flow to test), no
session, and no token. Sign in through the app's real Log In screen.
`, email, rawPassword, account.ID, avatarID)
	return nil
}

func reportExisting(
	ctx context.Context,
	accounts *application.AccountService,
	credential domain.PasswordCredential,
	requested bool,
) error {
	account, err := accounts.FindByID(ctx, credential.AccountID)
	if err != nil {
		return fmt.Errorf("an account already exists for %s (id %s), but it could not be read: %w",
			credential.Email, credential.AccountID, err)
	}

	avatar := string(account.AvatarID)
	if avatar == "" {
		avatar = "(none selected — the app will show Avatar Selection after sign-in)"
	}
	state := "active"
	if account.IsDeleted() {
		state = "DEACTIVATED — this account cannot sign in"
	}

	fmt.Printf(`An account already exists for %s. Nothing was changed.

  account id %s
  avatar     %s
  state      %s
  created    %s

The password was NOT reset — this command never overwrites an existing
credential. If you need a known password, either use the app's Forgot
Password flow, or delete this account from the development database and
run this command again.
`, credential.Email, account.ID, avatar, state, account.CreatedAt.Format(time.RFC3339))

	if requested {
		return nil
	}
	return errors.New("account already exists (re-run with -show-existing to inspect it without an error)")
}
