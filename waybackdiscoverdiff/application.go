package waybackdiscoverdiff

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/go-chi/cors"
	"github.com/go-redis/redis/v8"
	"github.com/hibiken/asynq"
	"github.com/smira/go-statsd"
	"github.com/spf13/viper"
)

func GetConfig(param string) any {
	viper.SetConfigName("conf")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig()
	if err != nil {
		panic(err)
	}

	return viper.Get(param)
}

var RedisClient *redis.Client

var Inspector *asynq.Inspector

var STATSDClient *statsd.Client

var AsynqServer *asynq.Server

var redisConnOpt = asynq.RedisClientOpt{
	Addr: "localhost:6379",
	DB:   1,
}

// Asynq client (used to enqueue tasks)
var AsynqClient = asynq.NewClient(redisConnOpt)

func Init() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Logging Conf???

	// Statsd
	STATSDClient = statsd.NewClient(fmt.Sprintf("%s:%d", StatsdHost, StatsdPort))
	defer STATSDClient.Close()

	// Redis
	RedisClient = redis.NewClient(&redis.Options{
		Addr:         RedisURL,
		DB:           1,
		PoolSize:     RedisMaxConnections,
		ReadTimeout:  time.Duration(RedisSocketTimeout) * time.Second,
		WriteTimeout: time.Duration(RedisSocketTimeout) * time.Second,
		DialTimeout:  10 * time.Second,
		IdleTimeout:  5 * time.Minute,
	})

	// Asynq Server

	// Asynq server config (runs workers)
	AsynqServer = asynq.NewServer(
		redisConnOpt,
		asynq.Config{
			Concurrency: 10,
			Queues: map[string]int{
				"wayback_discover_diff": 1,
			},
		},
	)

	cfg := CFG{
		Simhash: CFGSimhash{
			Size:        SimhashSize,
			ExpireAfter: SimhashExpireAfter,
		},
		Redis: &redis.Options{
			Addr:        RedisURL,
			DialTimeout: 10 * time.Second,
		},
		Threads: Threads,
		Snapshots: Snapshots{
			NumberPerYear: SnapshotsNumberPerYear,
			NumberPerPage: SnapshotsNumberPerPage,
		},
	}
	discover := NewDiscover(cfg)

	AsynqMux := asynq.NewServeMux()
	AsynqMux.HandleFunc(TypeDiscover, asynq.HandlerFunc(discover.DiscoverTaskHandler))
	go func() {
		if err := AsynqServer.Run(AsynqMux); err != nil {
			log.Fatalf("could not run server: %v", err)
		}
	}()

	// HTTP server
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Use(cors.Handler(cors.Options{
		// AllowedOrigins:   []string{"https://foo.com"}, // Use this to allow specific origin hosts
		AllowedOrigins: CORS,
		// AllowOriginFunc:  func(r *http.Request, origin string) bool { return true },
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300, // Maximum value not ignored by any of major browsers
	}))

	r.Get("/", http.HandlerFunc(ServeRoot))
	r.Get("/simhash", http.HandlerFunc(ServeSimhash(RedisClient)))
	r.Get("/calculate-simhash", http.HandlerFunc(ServeCalculateSimhash(RedisClient)))
	r.Get("/job", http.HandlerFunc(ServeJob(RedisClient)))

	srv := &http.Server{
		Addr:    ":8096",
		Handler: r,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("Shutting down servers...")

	srv.Shutdown(context.Background())
	AsynqServer.Shutdown()
}

var (
	// Simhash
	SimhashSize        = GetConfig("simhash.size").(int)
	SimhashExpireAfter = GetConfig("simhash.expire_after").(int)

	// Redis
	RedisURL                 = GetConfig("redis.url").(string)
	RedisDecodeResponses     = GetConfig("redis.decode_responses").(bool)
	RedisHealthCheckInterval = GetConfig("redis.health_check_interval").(int)
	RedisMaxConnections      = GetConfig("redis.max_connections").(int)
	RedisSocketKeepalive     = GetConfig("redis.socket_keepalive").(bool)
	RedisSocketTimeout       = GetConfig("redis.socket_timeout").(int)
	RedisRetryOnTimeout      = GetConfig("redis.retry_on_timeout").(bool)

	// Test Redis
	TestRedisPort = GetConfig("test_redis.port").(int)
	TestRedisHost = GetConfig("test_redis.host").(string)
	TestRedisDB   = GetConfig("test_redis.db").(int)

	// CDX Auth Token
	CDXAuthToken = GetConfig("cdx_auth_token").(string)

	// Celery
	CeleryResultBackend          = GetConfig("celery.result_backend").(string)
	CeleryBrokerURL              = GetConfig("celery.broker_url").(string)
	CeleryTaskDefaultQueue       = GetConfig("celery.task_default_queue").(string)
	CeleryTaskSoftTimeLimit      = GetConfig("celery.task_soft_time_limit").(int)
	CeleryWorkerMaxTasksPerChild = GetConfig("celery.worker_max_tasks_per_child").(int)

	// StatsD
	StatsdHost = GetConfig("statsd.host").(string)
	StatsdPort = GetConfig("statsd.port").(int)

	// Threads
	Threads = GetConfig("threads").(int)

	// Snapshots
	SnapshotsNumberPerYear = GetConfig("snapshots.number_per_year").(int)
	SnapshotsNumberPerPage = GetConfig("snapshots.number_per_page").(int)

	// CORS
	CORS = convertToStringSlice(GetConfig("cors").([]any))

	// Logging
	LoggingVersion                = GetConfig("logging.version").(int)
	LoggingDisableExistingLoggers = GetConfig("logging.disable_existing_loggers").(bool)
	LoggingRootLevel              = GetConfig("logging.root.level").(string)

	LoggingHandlers            = GetConfig("logging.handlers").(map[string]any)
	LoggingDefaultHandler      = LoggingHandlers["console"].(map[string]any)
	LoggingDefaultHandlerClass = LoggingDefaultHandler["class"].(string)

	LoggingFormatters             = GetConfig("logging.formatters").(map[string]any)
	LoggingDefaultFormatter       = LoggingFormatters["default"].(map[string]any)
	LoggingDefaultFormatterFormat = LoggingDefaultFormatter["format"].(string)

	LoggingLoggers           = GetConfig("logging.loggers").(map[string]any)
	LoggingWebLogger         = LoggingLoggers["wayback_discover_diff.web"].(map[string]any)
	LoggingWebLoggerLevel    = LoggingWebLogger["level"].(string)
	LoggingWebLoggerHandlers = convertToStringSlice(LoggingWebLogger["handlers"].([]any))
)

func convertToStringSlice(input []any) []string {
	result := make([]string, len(input))
	for i, v := range input {
		result[i] = fmt.Sprintf("%v", v)
	}
	return result
}
