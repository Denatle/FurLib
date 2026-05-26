package dispatcher

import "go.uber.org/fx"

var Module = fx.Module("dispatcher",
	fx.Provide(NewDispatcher),
)
