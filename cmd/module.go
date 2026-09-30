package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/hazim-j/agent-wow/internal/config"
	"github.com/hazim-j/agent-wow/pkg/modules/discovery"
	"github.com/spf13/cobra"
)

func newModuleCommand() *cobra.Command {
	var asJSON bool
	command := &cobra.Command{Use: "module", Short: "Inspect installed gameplay modules", Args: cobra.NoArgs}
	list := &cobra.Command{Use: "list", Short: "List modules and their configured interfaces without starting Docker", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cmd.SilenceUsage = true
		items, err := moddisc.Discover(filepath.Join(config.Get().ConfigDir, "modules"))
		if err != nil {
			return err
		}
		if asJSON {
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(struct {
				Modules []moddisc.Manifest `json:"modules"`
			}{items})
		}
		if len(items) == 0 {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "No modules installed.")
			return err
		}
		out := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
		fmt.Fprintln(out, "NAME\tENABLED\tDESCRIPTION\tREQUIRES\tMETHODS\tPACKETS\tBEFORE LOGOUT\tFILES")
		for _, m := range items {
			methods := make([]string, 0, len(m.RPC))
			for alias, target := range m.RPC {
				methods = append(methods, m.Name+"."+alias+"="+target)
			}
			sort.Strings(methods)
			packets := make([]string, 0, len(m.Packets))
			for op, target := range m.Packets {
				packets = append(packets, op+"="+target)
			}
			sort.Strings(packets)
			fmt.Fprintf(out, "%s\t%t\t%s\t%s\t%s\t%s\t%s\t%s; %s; %s\n", m.Name, m.Enabled, strings.ReplaceAll(m.Description, "\n", " "), strings.Join(m.Requires, ","), strings.Join(methods, ","), strings.Join(packets, ","), m.Lifecycle.BeforeLogout, m.Path, filepath.Join(m.Directory, m.Compose.File), filepath.Join(m.Directory, m.GRPC.DescriptorSet))
		}
		return out.Flush()
	}}
	list.Flags().BoolVar(&asJSON, "json", false, "Output module manifests as JSON")
	command.AddCommand(list)
	return command
}
func init() { rootCmd.AddCommand(newModuleCommand()) }
