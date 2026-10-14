package config

import (
	"flag"
	"fmt"
	"strconv"
)

type Config struct {
	Address           string
	DBPath            string
	StaticDir         string
	TaskWorkers       int
	TaskRetentionDays int
}

func Parse(args []string, lookup func(string) string) (Config, error) {
	flags := flag.NewFlagSet("openlist2emby", flag.ContinueOnError)
	address := flags.String("addr", envOrDefault(lookup, "OPENLIST2EMBY_ADDR", "127.0.0.1:8091"), "listen address")
	dbPath := flags.String("db", envOrDefault(lookup, "OPENLIST2EMBY_DB", "./data/openlist2emby.db"), "SQLite database path")
	staticDir := flags.String("static", "", "frontend dist directory")
	defaultWorkers, err := envInt(lookup, "OPENLIST2EMBY_TASK_WORKERS", 2)
	if err != nil {
		return Config{}, err
	}
	defaultRetention, err := envInt(lookup, "OPENLIST2EMBY_TASK_RETENTION_DAYS", 30)
	if err != nil {
		return Config{}, err
	}
	taskWorkers := flags.Int("task-workers", defaultWorkers, "task worker count")
	taskRetentionDays := flags.Int("task-retention-days", defaultRetention, "completed task retention in days")
	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}
	if *taskWorkers <= 0 || *taskWorkers > 32 {
		return Config{}, fmt.Errorf("task workers must be between 1 and 32")
	}
	if *taskRetentionDays <= 0 {
		return Config{}, fmt.Errorf("task retention days must be positive")
	}
	return Config{
		Address:           *address,
		DBPath:            *dbPath,
		StaticDir:         *staticDir,
		TaskWorkers:       *taskWorkers,
		TaskRetentionDays: *taskRetentionDays,
	}, nil
}

func envOrDefault(lookup func(string) string, key, fallback string) string {
	if lookup != nil {
		if value := lookup(key); value != "" {
			return value
		}
	}
	return fallback
}

func envInt(lookup func(string) string, key string, fallback int) (int, error) {
	value := envOrDefault(lookup, key, strconv.Itoa(fallback))
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return parsed, nil
}
