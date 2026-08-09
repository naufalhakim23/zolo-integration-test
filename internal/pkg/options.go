package pkg

import (
	"log/slog"

	"github.com/jmoiron/sqlx"

	"zolo-test-integration/config"
)

type OptionsApplication struct {
	Config *config.Config
	DB     *sqlx.DB
	Logger *slog.Logger
}
