package gelbooru

import (
	"FurLib/internal/fetcher"

	"go.uber.org/fx"
)

var Module = fx.Module("gelbooru",
	fx.Provide(
		fx.Annotate(NewClient,
			fx.As(new(fetcher.Client)),
			fx.ResultTags(`group:"clients"`),
		),
	),
)
