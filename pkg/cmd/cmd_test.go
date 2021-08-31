/*
Copyright 2014 The Kubernetes Authors.

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
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	"k8s.io/kubectl/pkg/cmd/flags"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

func TestNormalizationFuncGlobalExistence(t *testing.T) {
	// This test can be safely deleted when we will not support multiple flag formats
	root := NewKubectlCommand(os.Stdin, os.Stdout, os.Stderr, nil, flags.ZitiFlags{})

	if root.Parent() != nil {
		t.Fatal("We expect the root command to be returned")
	}
	if root.GlobalNormalizationFunc() == nil {
		t.Fatal("We expect that root command has a global normalization function")
	}

	if reflect.ValueOf(root.GlobalNormalizationFunc()).Pointer() != reflect.ValueOf(root.Flags().GetNormalizeFunc()).Pointer() {
		t.Fatal("root command seems to have a wrong normalization function")
	}

	sub := root
	for sub.HasSubCommands() {
		sub = sub.Commands()[0]
	}

	// In case of failure of this test check this PR: spf13/cobra#110
	if reflect.ValueOf(sub.Flags().GetNormalizeFunc()).Pointer() != reflect.ValueOf(root.Flags().GetNormalizeFunc()).Pointer() {
		t.Fatal("child and root commands should have the same normalization functions")
	}
}

func TestKubectlCommandHandlesPlugins(t *testing.T) {
	tests := []struct {
		name             string
		args             []string
		expectPlugin     string
		expectPluginArgs []string
		expectError      string
	}{
		{
			name:             "test that normal commands are able to be executed, when no plugin overshadows them",
			args:             []string{"kubectl", "get", "foo"},
			expectPlugin:     "",
			expectPluginArgs: []string{},
		},
		{
			name:             "test that a plugin executable is found based on command args",
			args:             []string{"kubectl", "foo", "--bar"},
			expectPlugin:     "plugin/testdata/kubectl-foo",
			expectPluginArgs: []string{"--bar"},
		},
		{
			name: "test that a plugin does not execute over an existing command by the same name",
			args: []string{"kubectl", "version"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pluginsHandler := &testPluginHandler{
				pluginsDirectory: "plugin/testdata",
			}
			_, in, out, errOut := genericclioptions.NewTestIOStreams()

			cmdutil.BehaviorOnFatal(func(str string, code int) {
				errOut.Write([]byte(str))
			})

			zFlags := flags.ZitiFlags{}

			root := NewDefaultKubectlCommandWithArgs(pluginsHandler, test.args, in, out, errOut, nil, zFlags)
			if err := root.Execute(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if pluginsHandler.err != nil && pluginsHandler.err.Error() != test.expectError {
				t.Fatalf("unexpected error: expected %q to occur, but got %q", test.expectError, pluginsHandler.err)
			}

			if pluginsHandler.executedPlugin != test.expectPlugin {
				t.Fatalf("unexpected plugin execution: expected %q, got %q", test.expectPlugin, pluginsHandler.executedPlugin)
			}

			if len(pluginsHandler.withArgs) != len(test.expectPluginArgs) {
				t.Fatalf("unexpected plugin execution args: expected %q, got %q", test.expectPluginArgs, pluginsHandler.withArgs)
			}
		})
	}
}

type testPluginHandler struct {
	pluginsDirectory string

	// execution results
	executedPlugin string
	withArgs       []string
	withEnv        []string

	err error
}

func (h *testPluginHandler) Lookup(filename string) (string, bool) {
	// append supported plugin prefix to the filename
	filename = fmt.Sprintf("%s-%s", "kubectl", filename)

	dir, err := os.Stat(h.pluginsDirectory)
	if err != nil {
		h.err = err
		return "", false
	}

	if !dir.IsDir() {
		h.err = fmt.Errorf("expected %q to be a directory", h.pluginsDirectory)
		return "", false
	}

	plugins, err := ioutil.ReadDir(h.pluginsDirectory)
	if err != nil {
		h.err = err
		return "", false
	}

	for _, p := range plugins {
		if p.Name() == filename {
			return fmt.Sprintf("%s/%s", h.pluginsDirectory, p.Name()), true
		}
	}

	h.err = fmt.Errorf("unable to find a plugin executable %q", filename)
	return "", false
}

func (h *testPluginHandler) Execute(executablePath string, cmdArgs, env []string) error {
	h.executedPlugin = executablePath
	h.withArgs = cmdArgs
	h.withEnv = env
	return nil
}

func TestKubectlCommandHeadersHooks(t *testing.T) {
	tests := map[string]struct {
		envVar    string
		addsHooks bool
	}{
		"empty environment variable; hooks added": {
			envVar:    "",
			addsHooks: true,
		},
		"random env var value; hooks added": {
			envVar:    "foo",
			addsHooks: true,
		},
		"true env var value; hooks added": {
			envVar:    "true",
			addsHooks: true,
		},
		"false env var value; hooks NOT added": {
			envVar:    "false",
			addsHooks: false,
		},
		"zero env var value; hooks NOT added": {
			envVar:    "0",
			addsHooks: false,
		},
	}

	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			cmds := &cobra.Command{}
			kubeConfigFlags := genericclioptions.NewConfigFlags(true).WithDeprecatedPasswordFlag()
			if kubeConfigFlags.WrapConfigFn != nil {
				t.Fatal("expected initial nil WrapConfigFn")
			}
			os.Setenv(kubectlCmdHeaders, testCase.envVar)
			addCmdHeaderHooks(cmds, kubeConfigFlags)
			// Valdidate whether the hooks were added.
			if testCase.addsHooks && kubeConfigFlags.WrapConfigFn == nil {
				t.Error("after adding kubectl command header, expecting non-nil WrapConfigFn")
			}
			if !testCase.addsHooks && kubeConfigFlags.WrapConfigFn != nil {
				t.Error("env var feature gate should have blocked setting WrapConfigFn")
			}
		})
	}
}
