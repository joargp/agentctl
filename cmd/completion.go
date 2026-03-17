package cmd

import (
	"github.com/joargp/agentctl/internal/session"
	"github.com/spf13/cobra"
)

// completeSessionIDs provides shell completion for session ID arguments.
func completeSessionIDs(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	sessions, err := session.List()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var ids []string
	for _, s := range sessions {
		desc := s.Model
		if s.Name != "" {
			desc = s.Name + " (" + s.Model + ")"
		}
		ids = append(ids, s.ID+"\t"+desc)
	}
	return ids, cobra.ShellCompDirectiveNoFileComp
}
