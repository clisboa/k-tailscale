package helper

import (
	"context"
)

type Paths struct{ Runtime, Payload, Helpers, Base, Rules, Lock, Version, ServiceLock, Links, Socket, CoreLink, CandidateHelper, SystemRoot string }
type service interface {
	Version(context.Context, string) (string, error)
	Running(context.Context) (bool, error)
	Start(context.Context) error
	Stop(context.Context) error
	Healthy(context.Context, string) error
	DisableNativeUpdates(context.Context) error
	Logout(context.Context) error
}
type Engine struct {
	paths       Paths
	upstream    *Upstream
	service     service
	free        func(string) (int64, error)
	eligibility func() error
	addon       *addonSource
	persist     func(string) error
}
