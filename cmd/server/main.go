package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"go.uber.org/zap"

	"paperly/cmd/server/logging"
	"paperly/internal/api"
	"paperly/internal/auth"
	"paperly/internal/backup"
	"paperly/internal/config"
	"paperly/internal/health"
	httpmw "paperly/internal/http/middleware"
	"paperly/internal/maintenance"
	"paperly/internal/repository"
	"paperly/internal/seed"
	"paperly/internal/service"
	"paperly/internal/system"
	"paperly/internal/web"
	"paperly/internal/worker"
)

// Injected at build time via -ldflags "-X main.version=... -X main.buildTime=...", see Makefile.
var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	// main init failure may panic (AGENTS.md F11: panic allowed only for main init).
	logger, err := zap.NewProduction()
	if err != nil {
		panic(fmt.Errorf("init logger: %w", err))
	}
	defer func() { _ = logger.Sync() }()

	printBanner()

	ctx := context.Background()

	// Runtime config: environment variables -> strongly typed struct. Only "misconfigured" fails at
	// startup (config.Load contract).
	cfgC, err := config.Load()
	if err != nil {
		panic(fmt.Errorf("load config: %w", err))
	}
	dataDir := cfgC.DataDir
	addr := cfgC.Addr
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		panic(fmt.Errorf("create data dir: %w", err))
	}
	dbPath := filepath.Join(dataDir, "paperly.db")

	db, err := repository.Open(ctx, dbPath)
	if err != nil {
		panic(fmt.Errorf("open database: %w", err))
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Error("close database", zap.Error(err))
		}
	}()
	logger.Info("database opened", zap.String("path", dbPath))

	e := echo.New()
	// Echo v5's Logger is a *slog.Logger; use a zap bridge handler to unify log format.
	// The same bridge instance is shared by echo and the auth middleware so all logs go through
	// zap structured output.
	slogLogger := slog.New(logging.NewSlogHandler(logger))
	e.Logger = slogLogger

	// Middleware order follows AGENTS.md: Recover -> Logger -> CORS -> (JWT ->) Validator -> Handler.
	// The single binary serves frontend and backend from the same origin, so no CORS is needed;
	// during development frontend/dev-proxy.ts performs same-origin reverse proxying.
	e.Use(middleware.Recover())
	// Access log: Echo v5 uses RequestLogger (slog backend), emitted as structured JSON via zap.
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogLatency:  true,
		LogMethod:   true,
		LogURI:      true,
		LogStatus:   true,
		LogRemoteIP: true,
		LogValuesFunc: func(_ *echo.Context, v middleware.RequestLoggerValues) error {
			if v.Error != nil || v.Status >= http.StatusInternalServerError {
				logger.Error("request error",
					zap.String("method", v.Method),
					zap.String("uri", v.URI),
					zap.Int("status", v.Status),
					zap.Duration("latency", v.Latency),
					zap.String("remote_ip", v.RemoteIP),
					zap.Error(v.Error),
				)
				return nil
			}
			logger.Info("request",
				zap.String("method", v.Method),
				zap.String("uri", v.URI),
				zap.Int("status", v.Status),
				zap.Duration("latency", v.Latency),
			)
			return nil
		},
	}))

	// Dependency injection: repository (implements domain interfaces) -> service (depends on domain
	// interfaces) -> api.
	kidRepo := repository.NewKidRepoSQLite(db)
	examRepo := repository.NewExamRepoSQLite(db)
	// The wake signal turns an enqueue into an immediate Worker wakeup; the Worker
	// keeps a 30s fallback poll, so a dropped signal is safe. Constructed once and
	// injected into every repository that enqueues tasks (plain Insert + the
	// same-transaction page-reorder enqueue).
	wake := worker.NewWakeSignal()
	pageRepo := repository.NewPageRepoSQLite(db).WithNotifier(wake)
	// Image settings: loaded from disk into memory at startup, zero disk IO on the upload hot path;
	// UI changes are written back to config.json.
	imageSettings := service.NewImageSettings(cfgC.Image.CompressPreset, config.DefaultPath())
	examSvc := service.NewExamService(examRepo, pageRepo, dataDir, imageSettings).WithTxManager(db)
	// Upload gate: concurrency + queue sized by the host memory tier (protects
	// low-memory hosts from OOM while leaving high-memory hosts unlimited).
	uploadGate := system.NewUploadGate(system.Tier())
	imageSettingsHandler := api.NewImageSettingsHandler(imageSettings)

	// subjects dictionary (seed import + read/write endpoints: adding/removing user subjects
	// on the settings page).
	subjectRepo := repository.NewSubjectRepoSQLite(db)
	// kid_subjects: per-kid subject whitelist (kid editor / upload page filtering).
	kidSubjectRepo := repository.NewKidSubjectRepoSQLite(db)
	// Atomic "profile + subjects" write path: create/update a kid and replace its
	// kid_subjects whitelist in one transaction (db implements domain.TxManager).
	kidSvc := service.NewKidService(kidRepo, db, subjectRepo, kidSubjectRepo)
	kidHandler := api.NewKidHandler(kidSvc)
	subjectSvc := service.NewSubjectServiceWithKids(subjectRepo, kidRepo, kidSubjectRepo)
	subjectHandler := api.NewSubjectHandler(subjectSvc)
	kidSubjectSvc := service.NewKidSubjectService(kidRepo, subjectRepo, kidSubjectRepo)
	kidSubjectHandler := api.NewKidSubjectHandler(kidSubjectSvc)

	// knowledge_points (settings page "knowledge point management", a one-level tree grouped
	// by subject).
	kpRepo := repository.NewKnowledgePointRepoSQLite(db)
	kpSvc := service.NewKnowledgePointService(kpRepo, subjectRepo)
	kpHandler := api.NewKnowledgePointHandler(kpSvc)

	// tasks (settings page "async & background queues": view / retry / clear finished).
	taskRepo := repository.NewTaskRepoSQLite(db).WithNotifier(wake)
	taskSvc := service.NewTaskService(taskRepo)
	taskHandler := api.NewTaskHandler(taskSvc)

	// users (parent accounts: display name + entry PIN). On first startup onboarding guides
	// registering the first parent instead of auto-creating an account.
	userRepo := repository.NewUserRepoSQLite(db)
	userSvc := service.NewUserService(userRepo)
	userHandler := api.NewUserHandler(userSvc)

	// Auth skeleton: the JWT key is generated on first startup (jwt.key, 0600) and read into memory.
	// Login/middleware/new endpoints all reuse the same Store; the key never reaches logs or DTOs.
	jwtStore, err := auth.LoadOrCreate(dataDir)
	if err != nil {
		panic(fmt.Errorf("init jwt key: %w", err))
	}
	logger.Info("jwt key ready", zap.String("path", filepath.Join(dataDir, auth.KeyName)))
	authSvc := service.NewAuthService(userRepo, kidRepo, jwtStore)
	requireAny := httpmw.RequireAnyAudience(slogLogger, jwtStore, authSvc)
	requireParent := httpmw.RequireAudience(slogLogger, jwtStore, authSvc, httpmw.AudienceParent)
	requireKid := httpmw.RequireAudience(slogLogger, jwtStore, authSvc, httpmw.AudienceKid)
	// /api/me* is a single group with three entries split by token audience; constructor params are
	// (requireAny, requireParent).
	authHandler := api.NewAuthHandler(authSvc, requireAny, requireParent)
	// Built after authSvc: the summary endpoints resolve the author's display name
	// via AuthService (parent nickname / kid name) when signing an appended segment.
	examHandler := api.NewExamHandler(examSvc, uploadGate, authSvc)

	// First-start onboarding: if the seed directory is empty, copy the built-in six-subject seed from
	// embed.FS (idempotent, never overwrites modified files).
	if seedRes, err := seed.EnsureSeed(dataDir); err != nil {
		logger.Warn("ensure builtin seed failed", zap.Error(err))
	} else if len(seedRes.Copied) > 0 {
		logger.Info("builtin seed bootstrapped", zap.Strings("copied", seedRes.Copied))
	}

	// subjects dictionary import (idempotent, deduplicated by code).
	if rows, err := seed.LoadSubjects(ctx, dataDir); err != nil {
		logger.Warn("load subjects seed failed", zap.Error(err))
	} else if n, err := subjectRepo.ImportSeed(ctx, rows); err != nil {
		logger.Warn("import subjects seed failed", zap.Error(err))
	} else if n > 0 {
		logger.Info("subjects seed inserted", zap.Int64("inserted", n))
	}

	// Ops (backup / health check / import / daily maintenance).
	// The four services share one set of library-level repositories: system_metrics is the sole
	// persistence site for global observable counters.
	opsRepo := repository.NewOpsRepoSQLite(db)
	metricRepo := repository.NewSystemMetricRepoSQLite(db)
	cursorRepo := repository.NewMaintenanceCursorRepoSQLite(db)

	// First-start onboarding (/welcome): state determination + one-time completion marker
	// (cursors.onboarding_done). Depends on cursorRepo (completion marker) and
	// userRepo/kidRepo (combined check that both entities are non-empty).
	onboardingSvc := service.NewOnboardingService(userRepo, kidRepo, cursorRepo)
	onboardingHandler := api.NewOnboardingHandler(onboardingSvc, authSvc)

	// When onboarding is incomplete, the four users/kids setup paths are allowed anonymously
	// (Welcome first-start guide); all other requests and post-onboarding behavior are unchanged and
	// still gated by auth. See the middleware comment for fail-closed.
	onbBypass := httpmw.WithOnboardingBypass(slogLogger, requireParent, onboardingSvc)
	// Same bypass but gated by requireAny (parent or kid): the subject dictionary is
	// read by both sides, so its post-onboarding auth must stay requireAny while still
	// allowing the anonymous GET /api/subjects the onboarding kid form needs.
	onbBypassAny := httpmw.WithOnboardingBypass(slogLogger, requireAny, onboardingSvc)

	backupSvc := backup.NewService(opsRepo, metricRepo, dataDir)
	healthSvc := health.NewService(opsRepo, metricRepo, cursorRepo, dataDir)
	systemOpsSvc := service.NewSystemOpsService(opsRepo, cursorRepo)
	adminHandler := api.NewAdminHandler(backupSvc, healthSvc, systemOpsSvc, dataDir)

	// Home aggregate metrics: four truth cards for exams/database/storage/health.
	// Backup time is injected via LastBackupAtFunc — the metrics layer does not depend on the
	// backup package.
	metricsSvc := service.NewMetricsService(opsRepo, metricRepo, healthSvc, backupSvc.LastBackupAt, dataDir)
	metricsHandler := api.NewMetricsHandler(metricsSvc, requireAny, requireParent)

	// Home business aggregates (4 KPI cards / upload persistence / latest archive per subject /
	// subject distribution).
	// **Scoped to a single kid**: the parent side passes kid_id (defaults to the first active kid);
	// the student side uses /kid/dashboard (kid_id taken from JWT claims) — both sides share the same
	// Build, so parent/student seeing identical numbers is guaranteed by "sharing one construction
	// path" rather than manually aligning two separate computations.
	dashboardRepo := repository.NewDashboardRepoSQLite(db)
	dashboardSvc := service.NewDashboardService(dashboardRepo, kidRepo, nil)
	dashboardHandler := api.NewDashboardHandler(dashboardSvc)

	// Notification center (messages table): family-level for parents, narrowed by kid_id for students.
	// The same MessageService serves both sides; only the "recipient boundary" differs — splitting it
	// into two services would eventually let the two sides' validation rules drift.
	messageRepo := repository.NewMessageRepoSQLite(db)
	messageSvc := service.NewMessageService(messageRepo)
	messageHandler := api.NewMessageHandler(messageSvc)
	// Kid invite flow: anonymous invite code verification / setting a PIN issues a kid JWT directly.
	// Built-in per-IP rate limiting; access_code is only consumed, never logged.
	inviteHandler := api.NewKidInviteHandler(authSvc)

	// Async infrastructure wiring.
	// Daily maintenance scheduler + background task Worker share one set of narrow interface
	// implementations; main only assembles, never implements. Both run as resident goroutines and
	// exit gracefully with runCtx (process signals).
	maintenanceSvc := maintenance.NewService(metricRepo, opsRepo, taskRepo, kidRepo, opsRepo, dataDir, slogLogger)
	maintenanceScheduler := maintenance.NewScheduler(maintenanceSvc, cursorRepo, slogLogger)

	taskWorker := worker.Deps{
		Log:     slogLogger,
		DataDir: dataDir,
		Tasks:   taskRepo,
		Wake:    wake.Wake(),
		Kids:    kidRepo,
		Backup: worker.BackupRunnerFunc(func(ctx context.Context) error {
			_, _, err := backupSvc.Create(ctx)
			return err
		}),
		Notify: messageSvc,
		Pages:  pageRepo,
	}

	// Health check: real liveness probe — reads schema_version.display_tag to confirm both the
	// connection and the schema are ready.
	e.GET("/health", func(c *echo.Context) error {
		displayTag, err := db.Health(c.Request().Context())
		if err != nil {
			logger.Error("health probe failed", zap.Error(err))
			return c.JSON(http.StatusInternalServerError, map[string]any{
				"status":    "error",
				"db_status": "error",
				"detail":    err.Error(),
			})
		}
		return c.JSON(http.StatusOK, map[string]any{
			"status":         "ok",
			"db_status":      "ok",
			"schema_version": repository.SchemaVersionBaseline(),
			"spec_version":   displayTag,
		})
	})

	// Business API (login sessions use JWT, see the authHandler wiring above; full UI auth is P2).
	apiGroup := e.Group("/api")
	{
		g := apiGroup.Group("", onbBypass)
		kidHandler.RegisterRoutes(g)
		kidSubjectHandler.RegisterRoutes(g)
	}
	{
		g := apiGroup.Group("", requireAny)
		examHandler.RegisterRoutes(g)
	}
	{
		g := apiGroup.Group("", onbBypassAny)
		subjectHandler.RegisterRoutes(g)
	}
	{
		g := apiGroup.Group("", requireParent)
		adminHandler.RegisterRoutes(g)
	}
	metricsHandler.RegisterRoutes(apiGroup)
	{
		g := apiGroup.Group("", requireParent)
		dashboardHandler.RegisterRoutes(g)
	}
	{
		g := apiGroup.Group("", requireAny)
		messageHandler.RegisterRoutes(g)
	}
	// Kid-side home aggregate uses JWT auth (kid_id from token claims, reusing the parent-side Build).
	apiGroup.GET("/kid/dashboard", dashboardHandler.KidOverview, requireKid)
	inviteHandler.RegisterRoutes(apiGroup)
	{
		g := apiGroup.Group("", requireAny)
		kpHandler.RegisterRoutes(g)
	}
	{
		g := apiGroup.Group("", requireParent)
		taskHandler.RegisterRoutes(g)
	}
	{
		g := apiGroup.Group("", onbBypass)
		userHandler.RegisterRoutes(g)
	}
	authHandler.RegisterRoutes(apiGroup)
	{
		g := apiGroup.Group("", requireParent)
		imageSettingsHandler.RegisterRoutes(g)
	}
	onboardingHandler.RegisterRoutes(apiGroup)

	// Unmatched /api/* returns JSON 404 (instead of falling through to the SPA fallback
	// returning 200 HTML).
	// Must be registered after the apiGroup routes and before the SPA /* route.
	apiGroup.Any("/*", func(c *echo.Context) error {
		return c.JSON(http.StatusNotFound, map[string]any{
			"code":    40004,
			"message": "endpoint not found",
			"detail":  c.Request().Method + " " + c.Request().URL.Path,
		})
	})

	// Frontend static assets + SPA fallback (must be registered last: /* must not swallow the
	// /api/* and /health routes above). The frontend build output is embedded into the single
	// binary via go:embed; when not built, a warning is printed and it is skipped.
	if spa, err := web.Handler(); err != nil {
		logger.Warn("frontend assets unavailable; SPA routes disabled", zap.Error(err))
	} else {
		e.GET("/*", echo.WrapHandler(spa))
	}

	// Startup + graceful shutdown: Echo v5's StartConfig has built-in signal-driven graceful shutdown
	// (GracefulTimeout defaults to 10s, consistent with the previous implementation).
	runCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Resident background execution: exits when runCtx is cancelled (Ctrl-C / SIGTERM); a task
	// currently executing is not interrupted.
	go maintenanceScheduler.Run(runCtx)
	go worker.Run(runCtx, taskWorker)

	logger.Info("server started",
		zap.String("addr", addr),
		zap.String("version", version),
		zap.String("build_time", buildTime),
	)

	if err := (echo.StartConfig{
		Address:         addr,
		HideBanner:      true,
		HidePort:        true,
		GracefulTimeout: 10 * time.Second,
	}).Start(runCtx, e); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatal("server error", zap.Error(err))
	}
	logger.Info("server stopped")
}
