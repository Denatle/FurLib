package archivator

import "go.uber.org/fx"

var Module = fx.Module("archivator",
	fx.Provide(NewRepository),
	fx.Provide(NewArchivator),
)
