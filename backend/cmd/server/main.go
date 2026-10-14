package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/sevxn007/openlist2emby/backend/internal/app"
	"github.com/sevxn007/openlist2emby/backend/internal/audio/tagger"
	musicclient "github.com/sevxn007/openlist2emby/backend/internal/client/music"
	"github.com/sevxn007/openlist2emby/backend/internal/client/openlist"
	tmdbclient "github.com/sevxn007/openlist2emby/backend/internal/client/tmdb"
	"github.com/sevxn007/openlist2emby/backend/internal/config"
	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/emby"
	"github.com/sevxn007/openlist2emby/backend/internal/medialibrary"
	"github.com/sevxn007/openlist2emby/backend/internal/repository/sqlite"
	"github.com/sevxn007/openlist2emby/backend/internal/task"
	"github.com/sevxn007/openlist2emby/backend/internal/transport/httpapi"
)

const (
	httpShutdownTimeout = 10 * time.Second
	taskShutdownTimeout = 15 * time.Second
)

func main() {
	restrictProcessFilePermissions()
	if err := run(); err != nil {
		log.Printf("openlist2emby stopped: %v", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Parse(os.Args[1:], os.Getenv)
	if err != nil {
		return fmt.Errorf("parse configuration: %w", err)
	}

	store, err := sqlite.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer store.Close()

	openListClient := openlist.NewClient(nil)
	mediaMetadata := tmdbclient.NewService()
	musicMetadata := musicclient.NewClient(nil)
	application := app.NewService(
		store,
		openListClient,
		mediaMetadata,
		musicMetadata,
		tagger.New(),
	)
	mediaLibraryClients := func(baseURL, adminKey string) medialibrary.OpenList {
		return openlist.NewMediaLibraryClient(openListClient, baseURL, adminKey)
	}
	databasePath, err := filepath.Abs(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("resolve database path: %w", err)
	}
	defaultMediaPath := filepath.Join(filepath.Dir(databasePath), "media")
	application.SetDefaultMediaMetadataPath(defaultMediaPath)
	mediaPath := defaultMediaPath
	if settings, settingsErr := store.GetSettings(context.Background()); settingsErr == nil {
		if settings.MediaMetadataPath != "" {
			mediaPath = settings.MediaMetadataPath
		} else {
			_ = store.SetMediaMetadataPath(context.Background(), defaultMediaPath)
		}
	}
	mediaAssets, err := medialibrary.NewAssetFiles(mediaPath)
	if err != nil {
		return fmt.Errorf("prepare local media metadata: %w", err)
	}
	mediaLibraries := medialibrary.NewService(store, mediaLibraryClients, mediaMetadata)
	mediaLibraries.SetAssetFiles(mediaAssets)
	videoFrames := medialibrary.NewFFmpegFrameExtractor()
	mediaLibraries.SetVideoFrameExtractor(videoFrames)
	if paths, tags, referenceErr := store.ListMediaAssetReferences(context.Background()); referenceErr == nil {
		if gcErr := mediaAssets.GarbageCollect(paths, tags); gcErr != nil {
			log.Printf("local media metadata cleanup failed: %v", gcErr)
		}
	}
	embyStore, err := emby.NewSQLiteStore(context.Background(), store.DB())
	if err != nil {
		return fmt.Errorf("create Emby store: %w", err)
	}
	application.SetUserTokenRevoker(embyStore)
	embyServer, err := emby.NewServer(mediaLibraries, embyStore, store, application)
	if err != nil {
		return fmt.Errorf("create Emby compatibility server: %w", err)
	}
	embyListener := emby.NewListener(embyServer)
	application.SetMediaLibraryRuntime(embyListener)
	if settings, settingsErr := store.GetSettings(context.Background()); settingsErr == nil {
		if err := embyListener.Reconfigure(context.Background(), settings.MediaLibraryEnabled, settings.EmbyPort); err != nil {
			log.Printf("Emby compatibility server is not listening: %v", err)
		}
	} else if !errors.Is(settingsErr, domain.ErrNotSetup) {
		return fmt.Errorf("read media library settings: %w", settingsErr)
	}
	mediaScanHandler := medialibrary.NewTaskHandler(store, mediaLibraryClients, mediaMetadata, musicMetadata)
	mediaScanHandler.SetAssetFiles(mediaAssets)
	mediaScanHandler.SetVideoFrameExtractor(videoFrames)
	registry, err := task.NewRegistry(
		app.NewMusicEmbedTaskHandler(application, mediaLibraries),
		mediaScanHandler,
		medialibrary.NewStorageMigrationHandler(store, mediaAssets),
	)
	if err != nil {
		return fmt.Errorf("create task registry: %w", err)
	}
	taskOptions := task.DefaultOptions()
	taskOptions.Workers = cfg.TaskWorkers
	taskOptions.Retention = time.Duration(cfg.TaskRetentionDays) * 24 * time.Hour
	taskManager, err := task.NewManager(store, registry, taskOptions)
	if err != nil {
		return fmt.Errorf("create task manager: %w", err)
	}
	if err := taskManager.Start(context.Background()); err != nil {
		return fmt.Errorf("start task manager: %w", err)
	}

	managerHandler := httpapi.NewServerWithMedia(
		application,
		cfg.StaticDir,
		taskManager,
		mediaLibraries,
	)
	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           managerHandler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.ListenAndServe()
	}()
	log.Printf("openlist2emby listening on %s", cfg.Address)
	if cfg.StaticDir != "" {
		log.Printf("serving frontend from %s", cfg.StaticDir)
	}

	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-signalContext.Done():
	case serverErr := <-serverErrors:
		if !errors.Is(serverErr, http.ErrServerClosed) {
			_ = shutdownEmbyListener(embyListener)
			_ = shutdownTasks(taskManager)
			return serverErr
		}
	}

	serverErr := shutdownHTTPServer(server)
	embyErr := shutdownEmbyListener(embyListener)
	taskErr := shutdownTasks(taskManager)
	return errors.Join(serverErr, embyErr, taskErr)
}

func shutdownHTTPServer(server *http.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), httpShutdownTimeout)
	defer cancel()
	err := server.Shutdown(ctx)
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.Join(err, server.Close())
	}
	return err
}

func shutdownEmbyListener(listener *emby.Listener) error {
	ctx, cancel := context.WithTimeout(context.Background(), httpShutdownTimeout)
	defer cancel()
	return listener.Shutdown(ctx)
}

func shutdownTasks(manager *task.Manager) error {
	ctx, cancel := context.WithTimeout(context.Background(), taskShutdownTimeout)
	defer cancel()
	return manager.Shutdown(ctx)
}
