package app

import (
	"context"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/seron-cheng/infinite-ai/services/shared/config"
	"github.com/seron-cheng/infinite-ai/services/shared/db"
	"github.com/seron-cheng/infinite-ai/services/shared/httpx"
	"github.com/seron-cheng/infinite-ai/services/shared/store"
)

type Server struct {
	Config  config.Config
	DB      *pgxpool.Pool
	Redis   *redis.Client
	Store   *store.Store
	Router  chi.Router
	HTTP    *http.Server
	Proxy   *httputil.ReverseProxy
	CoreURL *url.URL
}

func New(ctx context.Context, cfg config.Config) (*Server, error) {
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if err := db.EnsureSchema(ctx, pool); err != nil {
		return nil, err
	}
	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	coreURL, err := url.Parse(cfg.CoreBaseURL)
	if err != nil {
		return nil, err
	}
	proxy := httputil.NewSingleHostReverseProxy(coreURL)

	server := &Server{
		Config:  cfg,
		DB:      pool,
		Redis:   rdb,
		Store:   store.New(pool, cfg),
		Proxy:   proxy,
		CoreURL: coreURL,
	}
	if err := server.Store.EnsureSeedData(ctx); err != nil {
		return nil, err
	}
	router := chi.NewRouter()
	router.Use(middleware.RealIP)
	router.Use(middleware.RequestID)
	router.Use(middleware.Recoverer)
	router.Use(middleware.Timeout(10 * time.Minute))
	router.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]any{
			"service": "bff",
			"status":  "ok",
		})
	})
	router.Mount("/", server.routes())
	server.Router = router
	server.HTTP = &http.Server{
		Addr:              ":" + cfg.BFFPort,
		Handler:           router,
		ReadHeaderTimeout: 15 * time.Second,
	}
	log.Printf("bff connected db=%s", db.CleanDSN(cfg.DatabaseURL))
	return server, nil
}

func (s *Server) Close() {
	if s.Redis != nil {
		_ = s.Redis.Close()
	}
	if s.DB != nil {
		s.DB.Close()
	}
}
