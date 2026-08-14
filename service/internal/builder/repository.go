package builder

import "context"

type ConfigRepository interface {
	Save(context.Context, ConfigInput) (ConfigView, error)
	Load(context.Context) (StoredConfig, error)
}
