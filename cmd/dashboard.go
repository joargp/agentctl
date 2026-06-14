package cmd

import (
	"github.com/joargp/agentctl/internal/dashboard"
	"github.com/spf13/cobra"
)

var runDashboard = dashboard.Run

var dashboardPort int

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Run the browser dashboard",
	Long:  "Run the local browser dashboard for inspecting, streaming, and killing agentctl sessions.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDashboard(dashboardPort)
	},
}

func init() {
	dashboardCmd.Flags().IntVar(&dashboardPort, "port", 8080, "port to run the dashboard server on")
	rootCmd.AddCommand(dashboardCmd)
}
