package gateway

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strings"
	"time"

	"switchboard/internal/auth"
	"switchboard/internal/config"
	"switchboard/internal/mailer"
	authstore "switchboard/internal/store/auth"
	wstransport "switchboard/internal/transport/ws"
	"switchboard/services/gateway/handlers"

	"github.com/IBM/sarama"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

type Server struct {
	cfg            *config.Config
	log            zerolog.Logger
	http           *http.Server
	pool           *pgxpool.Pool
	rdb            *redis.Client
	nc             *nats.Conn
	hub            *wstransport.Hub
	producer       sarama.SyncProducer
	sm             *auth.SessionManager
	enc            *auth.Encryptor
	turnstile      *auth.TurnstileVerifier
	mailer         *mailer.Mailer
	authMiddleware *auth.Middleware
	meH            *handlers.MeHandler
}

func NewServer(cfg *config.Config, log zerolog.Logger, pool *pgxpool.Pool, rdb *redis.Client, nc *nats.Conn, producer sarama.SyncProducer) *Server {
	s := &Server{cfg: cfg, log: log, pool: pool, rdb: rdb, nc: nc, producer: producer}

	s.hub = wstransport.NewHub()
	go s.hub.Run()

	sm, err := auth.NewSessionManager(cfg.Auth.SessionSecret)
	if err != nil {
		log.Fatal().Err(err).Msg("session manager init")
	}
	enc, err := auth.NewEncryptor(cfg.Auth.EncryptionKey)
	if err != nil {
		log.Fatal().Err(err).Msg("encryptor init")
	}
	s.sm = sm
	s.enc = enc
	s.turnstile = auth.NewTurnstileVerifier(cfg.Turnstile.SecretKey)
	s.mailer = mailer.New(cfg.SMTP, log)
	s.authMiddleware = auth.NewMiddleware(sm, pool, log)
	s.meH = handlers.NewMeHandler(pool, log, cfg, sm, enc)

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.http = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 0,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

func mustPage(partial string) *template.Template {
	return template.Must(template.ParseFiles(
		"services/gateway/templates/base.html",
		"services/gateway/templates/partials/navbar_user.html",
		partial,
	))
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	evh := handlers.NewEventsHandler(s.pool, s.log, s.nc)
	rdh := handlers.NewRedisHandler(s.rdb, s.log, s.nc)
	nath := handlers.NewNATSHandler(s.nc, s.log)
	wsh := handlers.NewWSHandler(s.hub, s.log, s.nc)
	kafkah := handlers.NewKafkaHandler(s.producer, s.nc, s.log)
	firehose := handlers.NewFirehoseHandler(s.nc, s.log)
	authH := handlers.NewAuthHandler(s.pool, s.log, s.cfg, s.sm, s.enc, s.turnstile, s.mailer)
	oauthH := handlers.NewOAuthHandler(s.pool, s.log, s.cfg, s.sm)

	indexTmpl := mustPage("services/gateway/templates/index.html")
	postgresTmpl := mustPage("services/gateway/templates/partials/events.html")
	redisTmpl := mustPage("services/gateway/templates/partials/redis.html")
	natsTmpl := mustPage("services/gateway/templates/partials/nats.html")
	firehoseTmpl := mustPage("services/gateway/templates/partials/firehose.html")
	wsTmpl := mustPage("services/gateway/templates/partials/ws.html")
	kafkaTmpl := mustPage("services/gateway/templates/partials/kafka.html")

	// Pages
	mux.HandleFunc("GET /{$}", s.renderPage(indexTmpl))
	mux.HandleFunc("GET /postgres", s.renderPage(postgresTmpl))
	mux.HandleFunc("GET /redis", s.renderPage(redisTmpl))
	mux.HandleFunc("GET /nats", s.renderPage(natsTmpl))
	mux.HandleFunc("GET /ws", s.renderPage(wsTmpl))
	mux.HandleFunc("GET /kafka", s.renderPage(kafkaTmpl))

	// Health
	mux.HandleFunc("GET /health", s.handleHealth)
	// Health checks proxy — evita CORS en el browser
	mux.HandleFunc("GET /health/gateway", s.handleHealth)
	mux.HandleFunc("GET /health/notifier", s.proxyHealth("http://localhost:8081/health"))
	mux.HandleFunc("GET /health/processor", s.proxyHealth("http://localhost:8082/health"))

	// Postgres
	mux.HandleFunc("GET /events", evh.List)
	mux.HandleFunc("POST /events", evh.Create)
	mux.HandleFunc("DELETE /events/{id}", evh.Delete)

	// Redis
	mux.HandleFunc("POST /redis/set", rdh.Set)
	mux.HandleFunc("GET /redis/get", rdh.Get)
	mux.HandleFunc("DELETE /redis/del", rdh.Del)
	mux.HandleFunc("POST /redis/publish", rdh.Publish)
	mux.HandleFunc("GET /redis/subscribe", rdh.Subscribe)

	// NATS
	mux.HandleFunc("POST /nats/request", nath.Request)
	mux.HandleFunc("GET /nats/subscribe", nath.Subscribe)

	// WebSocket
	mux.HandleFunc("GET /ws/connect", wsh.Connect)
	mux.HandleFunc("GET /ws/count", wsh.Count)

	// Kafka
	mux.HandleFunc("POST /kafka/produce", kafkah.Produce)
	mux.HandleFunc("GET /kafka/results", kafkah.Results) // SSE — resultados del processor

	// Firehose
	mux.HandleFunc("GET /firehose", s.renderPage(firehoseTmpl))
	mux.HandleFunc("GET /firehose/stream", firehose.Stream)

	// Auth
	mux.Handle("GET /auth/signin", s.authMiddleware.RedirectIfAuth(http.HandlerFunc(authH.ShowLogin)))
	mux.Handle("POST /auth/signin", s.authMiddleware.RedirectIfAuth(http.HandlerFunc(authH.Login)))
	mux.Handle("GET /auth/signup", s.authMiddleware.RedirectIfAuth(http.HandlerFunc(authH.ShowRegister)))
	mux.Handle("POST /auth/signup", s.authMiddleware.RedirectIfAuth(http.HandlerFunc(authH.Register)))
	mux.Handle("POST /auth/signout", http.HandlerFunc(authH.Logout))
	mux.Handle("GET /auth/verify-email", http.HandlerFunc(authH.VerifyEmail))
	mux.Handle("GET /auth/verify-email/pending", http.HandlerFunc(authH.VerifyEmailPending))
	mux.Handle("GET /auth/resend-verification", http.HandlerFunc(authH.ResendVerification))

	// OAuth
	mux.Handle("GET /auth/google", http.HandlerFunc(oauthH.RedirectToGoogle))
	mux.Handle("GET /auth/google/callback", http.HandlerFunc(oauthH.GoogleCallback))

	// Recovery Account
	mux.Handle("GET /auth/forgot-password", s.authMiddleware.RedirectIfAuth(http.HandlerFunc(authH.ShowForgotPassword)))
	mux.Handle("POST /auth/forgot-password", s.authMiddleware.RedirectIfAuth(http.HandlerFunc(authH.ForgotPassword)))
	mux.Handle("GET /auth/reset-password", s.authMiddleware.RedirectIfAuth(http.HandlerFunc(authH.ShowResetPassword)))
	mux.Handle("POST /auth/reset-password", s.authMiddleware.RedirectIfAuth(http.HandlerFunc(authH.ResetPassword)))

	// ── /me routes ────────────────────────────────────────────────
	mux.Handle("GET /me/profile", s.authMiddleware.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.meH.Profile(s.loadNavUser(r)).ServeHTTP(w, r)
	})))
	mux.Handle("POST /me/profile", s.authMiddleware.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.meH.UpdateProfile(s.loadNavUser(r)).ServeHTTP(w, r)
	})))
	mux.Handle("GET /me/security", s.authMiddleware.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.meH.Security(s.loadNavUser(r)).ServeHTTP(w, r)
	})))
	mux.Handle("POST /me/security/password", s.authMiddleware.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.meH.ChangePassword(s.loadNavUser(r)).ServeHTTP(w, r)
	})))
	mux.Handle("POST /me/security/delete", s.authMiddleware.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.meH.DeleteAccount(s.loadNavUser(r)).ServeHTTP(w, r)
	})))
	mux.Handle("GET /me/sessions", s.authMiddleware.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.meH.Sessions(s.loadNavUser(r)).ServeHTTP(w, r)
	})))
	mux.Handle("POST /me/sessions/revoke", s.authMiddleware.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.meH.RevokeSession(s.loadNavUser(r)).ServeHTTP(w, r)
	})))
	mux.Handle("POST /me/sessions/revoke-all", s.authMiddleware.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.meH.RevokeAllSessions(s.loadNavUser(r)).ServeHTTP(w, r)
	})))
	mux.Handle("GET /me/mfa", s.authMiddleware.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.meH.MFA(s.loadNavUser(r)).ServeHTTP(w, r)
	})))

	// Multi Factor Authentication
	mux.Handle("GET /me/mfa/setup", s.authMiddleware.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.meH.MFASetup(s.loadNavUser(r)).ServeHTTP(w, r)
	})))
	mux.Handle("POST /me/mfa/setup", s.authMiddleware.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.meH.MFASetupConfirm(s.loadNavUser(r)).ServeHTTP(w, r)
	})))
	mux.Handle("GET /me/mfa/backup-codes", s.authMiddleware.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.meH.MFABackupCodes(s.loadNavUser(r)).ServeHTTP(w, r)
	})))
	mux.Handle("GET /me/mfa/backup-codes/download", s.authMiddleware.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.meH.MFABackupCodesDownload(s.loadNavUser(r)).ServeHTTP(w, r)
	})))
	mux.Handle("POST /me/mfa/disable", s.authMiddleware.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.meH.MFADisable(s.loadNavUser(r)).ServeHTTP(w, r)
	})))
}

func (s *Server) renderPage(tmpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		data := map[string]any{
			"User": s.loadNavUser(r),
		}
		if err := tmpl.ExecuteTemplate(w, "base.html", data); err != nil {
			s.log.Error().Err(err).Msg("render page")
		}
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok","service":"gateway"}`))
}

func (s *Server) Start() error {
	s.log.Info().Str("addr", s.http.Addr).Msg("gateway listening")
	return s.http.ListenAndServe()
}

func (s *Server) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.http.Shutdown(ctx); err != nil {
		s.log.Error().Err(err).Msg("shutdown error")
	}
}

func (s *Server) proxyHealth(target string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp, err := http.Get(target)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status":"error","service":"unreachable"}`))
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}
}

func (s *Server) loadNavUser(r *http.Request) *handlers.NavUser {
	sessionID, err := s.sm.ParseCookie(r)
	if err != nil {
		s.log.Debug().Err(err).Msg("loadNavUser: parse cookie failed")
		return nil
	}
	s.log.Debug().Str("session_id", sessionID).Msg("loadNavUser: parsed session ID")

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	session, err := authstore.GetSession(ctx, s.pool, sessionID)
	if err != nil {
		s.log.Debug().Err(err).Str("session_id", sessionID).Msg("loadNavUser: get session failed")
		return nil
	}
	s.log.Debug().Str("user_id", session.UserID.String()).Msg("loadNavUser: session found")

	if !session.IsActive || time.Now().After(session.ExpiresAt) {
		s.log.Debug().Msg("loadNavUser: session inactive or expired")
		return nil
	}

	user, err := authstore.GetUserByID(ctx, s.pool, session.UserID)
	if err != nil {
		s.log.Debug().Err(err).Msg("loadNavUser: get user failed")
		return nil
	}
	s.log.Debug().Str("email", user.Email).Msg("loadNavUser: user found")

	if !user.IsActive {
		s.log.Debug().Msg("loadNavUser: user inactive")
		return nil
	}

	displayName := ""
	if user.DisplayName != nil {
		displayName = *user.DisplayName
	}
	if displayName == "" {
		displayName = user.Email
	}

	avatarURL := ""
	if user.AvatarURL != nil {
		avatarURL = *user.AvatarURL
	}

	return &handlers.NavUser{
		ID:          user.ID.String(),
		Email:       user.Email,
		DisplayName: displayName,
		AvatarURL:   avatarURL,
		Role:        user.Role,
		MFAEnabled:  user.MFAEnabled,
		Initials:    initials(displayName),
	}
}

func initials(name string) string {
	parts := strings.Fields(name)
	switch len(parts) {
	case 0:
		return "?"
	case 1:
		if len(parts[0]) > 0 {
			return strings.ToUpper(string(parts[0][0]))
		}
		return "?"
	default:
		return strings.ToUpper(string(parts[0][0]) + string(parts[len(parts)-1][0]))
	}
}
