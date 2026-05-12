//go:build windows

package main

import (
	"context"
	"errors"

	"all_notify/internal/server"
	"golang.org/x/sys/windows/svc"
)

func platformRun(cfg server.Config) (bool, error) {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return false, err
	}
	if !isService {
		return false, nil
	}
	return true, svc.Run(windowsServiceName, &windowsService{cfg: cfg})
}

type windowsService struct {
	cfg server.Config
}

func (s *windowsService) Execute(args []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errc := make(chan error, 1)
	go func() {
		errc <- runApp(ctx, s.cfg)
	}()

	status <- svc.Status{State: svc.StartPending}
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	for {
		select {
		case req := <-requests:
			switch req.Cmd {
			case svc.Interrogate:
				status <- req.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				err := <-errc
				if err != nil && !errors.Is(err, context.Canceled) {
					return true, 1
				}
				status <- svc.Status{State: svc.Stopped}
				return false, 0
			default:
				status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
			}
		case err := <-errc:
			if err != nil {
				return true, 1
			}
			status <- svc.Status{State: svc.Stopped}
			return false, 0
		}
	}
}
