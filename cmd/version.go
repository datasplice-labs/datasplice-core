package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Display the version of the application",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Println(fmt.Sprintf("Datasplice Core version: %s", rootCmd.Version))
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
