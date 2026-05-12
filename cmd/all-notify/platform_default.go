//go:build !windows

package main

import "all_notify/internal/server"

func platformRun(cfg server.Config) (bool, error) {
	return false, nil
}
