package run

import (
	"context"
	"errors"
)

var ErrBusy = errors.New("background capacity exhausted")
var ErrStopping = errors.New("background runner stopping")

type Task func(context.Context)
type Reservation interface {
	Submit(Task) error
	Release()
}
type Admission interface {
	Reserve(context.Context) (Reservation, error)
}
