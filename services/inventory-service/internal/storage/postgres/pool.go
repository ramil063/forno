package postgres

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Config — настройки пула. Занулённое поле означает «оставить дефолт pgx».
type Config struct {
	DSN               string
	MaxConns          int32
	MinConns          int32
	MaxConnLifetime   time.Duration
	MaxConnIdleTime   time.Duration
	HealthCheckPeriod time.Duration
	ApplicationName   string
	StatementTimeout  time.Duration
}

// New создаёт пул соединений и проверяет, что база отвечает.
func New(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	if strings.TrimSpace(cfg.DSN) == "" {
		return nil, errEmptyDSN
	}
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse config: %w", err)
	}
	if cfg.MaxConns > 0 {
		poolCfg.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		poolCfg.MinConns = cfg.MinConns
	}
	if cfg.MaxConnLifetime > 0 {
		poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	}
	if cfg.MaxConnIdleTime > 0 {
		poolCfg.MaxConnIdleTime = cfg.MaxConnIdleTime
	}
	if cfg.HealthCheckPeriod > 0 {
		poolCfg.HealthCheckPeriod = cfg.HealthCheckPeriod
	}
	if cfg.ApplicationName != "" {
		poolCfg.ConnConfig.RuntimeParams["application_name"] = cfg.ApplicationName
	}
	if cfg.StatementTimeout > 0 {
		poolCfg.ConnConfig.RuntimeParams["statement_timeout"] = strconv.FormatInt(cfg.StatementTimeout.Milliseconds(), 10)
	}

	pgxPool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err = pgxPool.Ping(pingCtx); err != nil {
		pgxPool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}

	return pgxPool, nil
}

func (c Config) validate() error {
	if c.MaxConns < 0 {
		return fmt.Errorf("postgres: MaxConns не может быть отрицательным: %d", c.MaxConns)
	}
	if c.MinConns < 0 {
		return fmt.Errorf("postgres: MinConns не может быть отрицательным: %d", c.MinConns)
	}
	return nil
}
