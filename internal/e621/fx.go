package e621

import (
	"FurLib/internal/fetcher"

	"go.uber.org/fx"
)

var Module = fx.Module("e621",
	fx.Provide(
		fx.Annotate(NewClient,
			fx.As(new(fetcher.Client)),
			fx.ResultTags(`group:"clients"`),
		),
	),
)
