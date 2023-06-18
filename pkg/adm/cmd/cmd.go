/*
Copyright 2023 The Firefly Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cmd

import (
	"io"

	"github.com/lithammer/dedent"
	"github.com/spf13/cobra"
)

// NewFireflyadmCommand creates the `firelflyadm` command.
func NewFireflyadmCommand(in io.Reader, out, err io.Writer) *cobra.Command {
	cmds := &cobra.Command{
		Use:   "fireflyadm",
		Short: "fireflyadm: easily bootstrap a secure Firefly platform.",
		Long: dedent.Dedent(`

			    ┌──────────────────────────────────────────────────────────┐
			    │ FIREFLYADM                                               │
			    │ Easily bootstrap a secure Firefly platform               │
			    │                                                          │
			    │ Please give us feedback at:                              │
			    │ https://github.com/carlory/firefly/issues                │
			    └──────────────────────────────────────────────────────────┘

			Example usage:

			    Create a Firefly platform on a Kubernetes cluster with one
			    control-plane (which controls all the clusters), and one 
			    worker cluster (where your actual workloads, like Pods and
			    Deployments run).

			    ┌──────────────────────────────────────────────────────────┐
			    │ On the first cluster:                                    │
			    ├──────────────────────────────────────────────────────────┤
			    │ control-plane# fireflyadm init                           │
			    └──────────────────────────────────────────────────────────┘

			    ┌──────────────────────────────────────────────────────────┐
			    │ On the second cluster:                                   │
			    ├──────────────────────────────────────────────────────────┤
			    │ worker# fireflyadm join <arguments-returned-from-init>   │
			    └──────────────────────────────────────────────────────────┘

			    You can then repeat the second step on as many other clusters as you like.

		`),
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	cmds.ResetFlags()

	cmds.AddCommand(newCmdInit(out, nil))
	return cmds
}
