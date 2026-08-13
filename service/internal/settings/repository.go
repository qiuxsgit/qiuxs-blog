package settings

import (
	"context"
	"time"
)

type SiteRepository interface {
	GetSite(context.Context) (Site, error)
	CreateSite(context.Context, Site, time.Time) (Site, error)
	UpdateSite(context.Context, Site, int64, time.Time) (Site, error)
}
