package main

import (
	"context"
	"errors"
	"testing"
)

func TestWaitForLauncherStopsWhenTrayCloses(t *testing.T) {
	trayErr := make(chan error, 1)
	trayErr <- nil
	serverStopped, err := waitForLauncher(context.Background(), make(chan error), trayErr)
	if err != nil || serverStopped {
		t.Fatalf("waitForLauncher() = (%v, %v), want (false, nil)", serverStopped, err)
	}
}

func TestWaitForLauncherReportsServerFailure(t *testing.T) {
	want := errors.New("server failed")
	serverErr := make(chan error, 1)
	serverErr <- want
	serverStopped, err := waitForLauncher(context.Background(), serverErr, make(chan error))
	if !serverStopped || !errors.Is(err, want) {
		t.Fatalf("waitForLauncher() = (%v, %v), want (true, %v)", serverStopped, err, want)
	}
}
