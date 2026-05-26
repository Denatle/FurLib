package librarian

import "go.uber.org/fx"

var Module = fx.Module("librarian",
	fx.Provide(NewLibrarian),
)
