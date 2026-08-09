package pkg

import (
	"log/slog"

	"github.com/jmoiron/sqlx"

	"zolo-test-integration/config"
)

// OptionsApplication is the shared dependency bundle every layer embeds.
type OptionsApplication struct {
	Config    *config.Config
	DB        *sqlx.DB
	Logger    *slog.Logger
	Localizer *Localizer
}
