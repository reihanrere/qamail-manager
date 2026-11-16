package main

import (
	"context"
	"log"
	"os"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"

	"github.com/reihanrere/qamail-manager/internal/cli"
	"github.com/reihanrere/qamail-manager/internal/config"
	"github.com/reihanrere/qamail-manager/internal/database"
	"github.com/reihanrere/qamail-manager/internal/handlers"
	"github.com/reihanrere/qamail-manager/internal/mailer"
	"github.com/reihanrere/qamail-manager/internal/mailtm"
	"github.com/reihanrere/qamail-manager/internal/providers"
	"github.com/reihanrere/qamail-manager/internal/routes"
	"github.com/reihanrere/qamail-manager/internal/services"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	db, err := database.Connect(cfg)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	// `api user ...` manages users and exits instead of starting the server
	if len(os.Args) > 1 && os.Args[1] == "user" {
		users := services.NewUserService(db, cfg.PasswordMinLength)
		reader := cli.TerminalPasswordReader(os.Stdin, os.Stderr)
		if err := cli.RunUser(context.Background(), os.Args[2:], users, reader, os.Stdout); err != nil {
			log.SetFlags(0)
			log.Fatalf("%v", err)
		}
		return
	}

	mailtmClient := mailtm.NewClient(cfg.MailTMBaseURL)
	mailtmService := mailtm.NewService(mailtmClient)

	accountService, err := services.NewAccountService(db, services.Settings{
		DefaultProvider: cfg.MailProvider,
		Limits:          services.Limits{TagMaxLength: cfg.TagMaxLength, NoteMaxLength: cfg.NoteMaxLength},

		UsernameMaxAttempts:   cfg.UsernameMaxAttempts,
		BulkGenerateMax:       cfg.BulkGenerateMax,
		FirstNames:            cfg.UsernameFirstNames,
		LastNames:             cfg.UsernameLastNames,
		LegacyUsernamePattern: cfg.LegacyUsernamePattern,
		UserAccountQuota:      cfg.UserAccountQuota,

		InboxSearchMaxPages: cfg.InboxSearchMaxPages,
		InboxSyncInterval:   cfg.InboxSyncInterval,
		MailTMRequestDelay:  cfg.MailTMRequestDelay,

		MessageRetention:     cfg.MessageRetention,
		MessagePruneInterval: cfg.MessagePruneInterval,

		AutoMarkUsed:         cfg.AutoMarkUsed,
		AccountCleanupAfter:  cfg.AccountCleanupAfter,
		AccountCleanupAction: cfg.AccountCleanupAction,

		IngestEnabled:      cfg.IngestSecret != "",
		IngestMaxMIMEParts: cfg.IngestMaxMIMEParts,
	},
		providers.NewMailTM(mailtmService),
		providers.NewLocal(db, cfg.CatchallDomains...),
	)
	if err != nil {
		log.Fatalf("invalid service settings: %v", err)
	}
	accountService.SetMailer(mailer.New(mailer.Config{
		Host:     cfg.SMTPHost,
		Port:     cfg.SMTPPort,
		Username: cfg.SMTPUsername,
		Password: cfg.SMTPPassword,
		Security: cfg.SMTPSecurity,
	}))
	if err := accountService.LoadSettingOverrides(context.Background()); err != nil {
		log.Printf("settings: %v", err)
	}
	accountService.StartInboxSync(context.Background())
	accountService.StartMessagePruner(context.Background())

	authService := services.NewAuthService(db, services.AuthSettings{
		SessionTTL:       cfg.SessionTTL,
		LoginMaxAttempts: cfg.LoginMaxAttempts,
		LoginWindow:      cfg.LoginWindow,

		MaxConcurrentVerifies: cfg.LoginMaxConcurrentVerifies,
		VerifyWait:            cfg.LoginVerifyWait,
	})
	authHandler := handlers.NewAuthHandler(authService, cfg.CookieSecure, cfg.CORSAllowedOrigins)
	accountHandler := handlers.NewAccountHandler(accountService)
	inboxHandler := handlers.NewInboxHandler(accountService)
	userHandler := handlers.NewUserHandler(services.NewUserService(db, cfg.PasswordMinLength), accountService)
	eventsHandler := handlers.NewEventsHandler(accountService.Events(), authService, cfg.EventsHeartbeatInterval)

	if cfg.IngestSecret != "" {
		startIngestServer(cfg, accountService)
	} else {
		log.Println("ingest: disabled (INGEST_SECRET is empty)")
	}

	app := fiber.New(fiber.Config{
		AppName: cfg.AppName,
		// c.IP() (the login rate limit key) reads the real client address from
		// CLIENT_IP_HEADER, but only when the request comes from a trusted proxy
		ProxyHeader:      cfg.ClientIPHeader,
		TrustProxy:       true,
		TrustProxyConfig: fiber.TrustProxyConfig{Proxies: cfg.TrustedProxies},
	})

	app.Use(recover.New())
	app.Use(logger.New())
	// Same-origin deployments need no CORS at all; only listed origins get it, with cookies
	if len(cfg.CORSAllowedOrigins) > 0 {
		app.Use(cors.New(cors.Config{
			AllowOrigins:     cfg.CORSAllowedOrigins,
			AllowCredentials: true,
		}))
	}

	app.Get("/health", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	routes.RegisterRoutes(app, authHandler, accountHandler, inboxHandler, eventsHandler, userHandler)
	routes.RegisterDocsRoutes(app)

	log.Printf("%s listening on port %s", cfg.AppName, cfg.AppPort)
	if err := app.Listen(":" + cfg.AppPort); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// startIngestServer serves only the ingest endpoint on its own port, so the Cloudflare
// tunnel can be pointed at it without exposing the rest of the API.
func startIngestServer(cfg *config.Config, accountService *services.AccountService) {
	app := fiber.New(fiber.Config{
		AppName:   cfg.AppName + " ingest",
		BodyLimit: cfg.IngestMaxBytes,
	})
	app.Use(recover.New())
	app.Use(logger.New())
	routes.RegisterIngestRoutes(app, handlers.NewIngestHandler(accountService, cfg.IngestSecret))

	go func() {
		log.Printf("ingest listening on port %s", cfg.IngestPort)
		if err := app.Listen(":" + cfg.IngestPort); err != nil {
			log.Fatalf("ingest server error: %v", err)
		}
	}()
}
