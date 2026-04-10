package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// searchCmd is a deprecated alias for findCmd.
// It prints a deprecation notice and delegates to runFind.
// Kept for one release cycle so existing scripts keep working.
var searchCmd = &cobra.Command{
	Use:        "search [query]",
	Short:      "Deprecated: use 'axon find <query>' instead",
	Long:       `Deprecated: use 'axon find <query>' instead. See 'axon find --help'.`,
	Deprecated: "use 'axon find' instead",
	Hidden:     true,
	Args:       cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(os.Stderr, "Note: 'axon search' is deprecated — use 'axon find' instead.")
		return runFind(cmd, args)
	},
}
