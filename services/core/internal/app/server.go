package app

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/redis/go-redis/v9"
	"github.com/seron-cheng/infinite-ai/services/shared/auth"
	"github.com/seron-cheng/infinite-ai/services/shared/config"
	"github.com/seron-cheng/infinite-ai/services/shared/db"
	"github.com/seron-cheng/infinite-ai/services/shared/httpx"
	"github.com/seron-cheng/infinite-ai/services/shared/store"
)

type Server struct {
	Config config.Config
	DB     *pgxpool.Pool
	Redis  *redis.Client
	MinIO  *minio.Client
	Store  *store.Store
	Router chi.Router
	HTTP   *http.Server

	runCancelMu sync.Mutex
	runCancels  map[string]context.CancelFunc

	openAIAdapterMu    sync.RWMutex
	openAIAdapterCache map[string]openAIEndpointAdaptation
}

func New(ctx context.Context, cfg config.Config) (*Server, error) {
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if err := db.EnsureSchema(ctx, pool); err != nil {
		return nil, err
	}
	rdb := redis.NewClient(&redis.Options{
		Addr: cfg.RedisAddr,
	})
	minioClient, err := minio.New(cfg.MinIOEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinIOAccessKey, cfg.MinIOSecretKey, ""),
		Secure: cfg.MinIOUseSSL,
	})
	if err != nil {
		return nil, err
	}

	server := &Server{
		Config:             cfg,
		DB:                 pool,
		Redis:              rdb,
		MinIO:              minioClient,
		Store:              store.New(pool, cfg),
		runCancels:         map[string]context.CancelFunc{},
		openAIAdapterCache: map[string]openAIEndpointAdaptation{},
	}
	if err := server.Store.EnsureSeedData(ctx); err != nil {
		return nil, err
	}
	server.loadEndpointAdaptations(ctx)
	if err := server.ensureObjectBucket(ctx); err != nil {
		return nil, err
	}
	router := chi.NewRouter()
	router.Use(middleware.RealIP)
	router.Use(middleware.RequestID)
	router.Use(middleware.Recoverer)
	router.Use(middleware.Timeout(90 * time.Second))
	router.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]any{
			"service": "core",
			"status":  "ok",
			"routes": map[string]string{
				"defaultChatRoute": cfg.DefaultChatRoute,
				"deepSearchRoute":  cfg.DeepSearchRoute,
				"tokenKind":        auth.BearerToken("internal"),
			},
		})
	})
	router.Mount("/", server.routes())
	server.Router = router
	server.HTTP = &http.Server{
		Addr:              ":" + cfg.CorePort,
		Handler:           router,
		ReadHeaderTimeout: 15 * time.Second,
	}
	log.Printf("core connected db=%s", db.CleanDSN(cfg.DatabaseURL))
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

func (s *Server) ensureObjectBucket(ctx context.Context) error {
	exists, err := s.MinIO.BucketExists(ctx, s.Config.MinIOBucket)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return s.MinIO.MakeBucket(ctx, s.Config.MinIOBucket, minio.MakeBucketOptions{})
}
