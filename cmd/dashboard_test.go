package cmd

import "testing"

func TestDashboardCommandRunsConfiguredPort(t *testing.T) {
	prevRunDashboard := runDashboard
	prevPort := dashboardPort
	prevNoOpen := dashboardNoOpen
	defer func() {
		runDashboard = prevRunDashboard
		dashboardPort = prevPort
		dashboardNoOpen = prevNoOpen
	}()

	called := false
	receivedPort := 0
	receivedAutoOpen := false
	runDashboard = func(port int, autoOpen bool) error {
		called = true
		receivedPort = port
		receivedAutoOpen = autoOpen
		return nil
	}
	dashboardPort = 9191
	dashboardNoOpen = false

	if err := dashboardCmd.RunE(dashboardCmd, nil); err != nil {
		t.Fatalf("dashboard command returned error: %v", err)
	}
	if !called {
		t.Fatal("expected runDashboard to be called")
	}
	if receivedPort != 9191 {
		t.Fatalf("expected port 9191, got %d", receivedPort)
	}
	if !receivedAutoOpen {
		t.Fatal("expected dashboard to open the browser by default")
	}
}

func TestDashboardCommandCanDisableBrowserOpening(t *testing.T) {
	prevRunDashboard := runDashboard
	prevNoOpen := dashboardNoOpen
	defer func() {
		runDashboard = prevRunDashboard
		dashboardNoOpen = prevNoOpen
	}()

	receivedAutoOpen := true
	runDashboard = func(_ int, autoOpen bool) error {
		receivedAutoOpen = autoOpen
		return nil
	}
	dashboardNoOpen = true

	if err := dashboardCmd.RunE(dashboardCmd, nil); err != nil {
		t.Fatalf("dashboard command returned error: %v", err)
	}
	if receivedAutoOpen {
		t.Fatal("expected --no-open to disable browser opening")
	}
}

func TestDashboardCommandHasDefaultPort(t *testing.T) {
	flag := dashboardCmd.Flags().Lookup("port")
	if flag == nil {
		t.Fatal("expected port flag to exist")
	}
	if flag.DefValue != "8080" {
		t.Fatalf("expected default port 8080, got %q", flag.DefValue)
	}
}

func TestDashboardCommandOpensBrowserByDefault(t *testing.T) {
	flag := dashboardCmd.Flags().Lookup("no-open")
	if flag == nil {
		t.Fatal("expected no-open flag to exist")
	}
	if flag.DefValue != "false" {
		t.Fatalf("expected browser opening by default, got no-open default %q", flag.DefValue)
	}
}
