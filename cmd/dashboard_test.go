package cmd

import "testing"

func TestDashboardCommandRunsConfiguredPort(t *testing.T) {
	prevRunDashboard := runDashboard
	prevPort := dashboardPort
	defer func() {
		runDashboard = prevRunDashboard
		dashboardPort = prevPort
	}()

	called := false
	receivedPort := 0
	runDashboard = func(port int) error {
		called = true
		receivedPort = port
		return nil
	}
	dashboardPort = 9191

	if err := dashboardCmd.RunE(dashboardCmd, nil); err != nil {
		t.Fatalf("dashboard command returned error: %v", err)
	}
	if !called {
		t.Fatal("expected runDashboard to be called")
	}
	if receivedPort != 9191 {
		t.Fatalf("expected port 9191, got %d", receivedPort)
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
