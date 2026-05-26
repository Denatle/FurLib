package api

import (
	"FurLib/internal/config"
	"context"
	"errors"
	"net/http"

	"go.uber.org/fx"
	"go.uber.org/zap"
)

var Module = fx.Module("api",
	fx.Provide(NewAPI),
	fx.Invoke(RegisterLifecycle),
)

func RegisterLifecycle(lc fx.Lifecycle, api *API, cfg config.APIConfig) {
	addr := cfg.Addr
	server := &http.Server{Addr: addr, Handler: api.router}

	lc.Append(fx.Hook{
		OnStart: func(_ context.Context) error {
			api.log.Info("api listening", zap.String("addr", addr))

			go func() {
				if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					api.log.Error("server error", zap.Error(err))
				}
			}()

			return nil
		},
		OnStop: func(ctx context.Context) error {
			api.log.Info("api shutting down")
			return server.Shutdown(ctx)
		},
	})
}
