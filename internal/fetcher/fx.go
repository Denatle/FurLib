package fetcher

import "go.uber.org/fx"

var Module = fx.Module("fetcher",
	fx.Provide(NewFetcher),
)
