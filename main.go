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

package main

import (
	"context"
	"io/ioutil"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/openziti-incubator/kubectl/pkg/cmd"
	"github.com/openziti-incubator/kubectl/pkg/cmd/plugin"
	"github.com/spf13/cobra"

	"github.com/openziti/sdk-golang/ziti"
	"github.com/openziti/sdk-golang/ziti/config"

	"github.com/openziti-incubator/kubectl/pkg/util/logs"
	"github.com/sirupsen/logrus"

	// Import to initialize client auth plugins.
	"github.com/go-yaml/yaml"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var configFilePath string
var serviceName string

type ZitiFlags struct {
	zConfig string
	service string
}

type Context struct {
	ZConfig string `yaml:"zConfig"`
	Service string `yaml:"service"`
}

type MinKubeConfig struct {
	Contexts []struct {
		Context Context `yaml:"context"`
		Name    string  `yaml:"name"`
	} `yaml:"contexts"`
}

var zFlags = ZitiFlags{}

func main() {
	rand.Seed(time.Now().UnixNano())
	kubeConfigFlags := genericclioptions.NewConfigFlags(true).WithDeprecatedPasswordFlag()

	// set the wrapper function. This allows modification to the reset Config
	kubeConfigFlags.WrapConfigFn = wrapConfigFn

	// create the cobra command and set ConfigFlags
	command := cmd.NewDefaultKubectlCommandWithArgsAndConfigFlags(cmd.NewDefaultPluginHandler(plugin.ValidPluginFilenamePrefixes), os.Args, os.Stdin, os.Stdout, os.Stderr, kubeConfigFlags)

	//set and parse the ziti flags
	command = setZitiFlags(command)
	command.PersistentFlags().Parse(os.Args)

	// try to get the ziti options from the flags
	configFilePath = command.Flag("zConfig").Value.String()
	serviceName = command.Flag("service").Value.String()

	// get the loaded kubeconfig
	kubeconfig := getKubeconfig()

	// if both the config file and service name are not set, parse the kubeconfig file
	if configFilePath == "" || serviceName == "" {
		parseKubeConfig(command, kubeconfig)
	}

	// TODO: once we switch everything over to Cobra commands, we can go back to calling
	// cliflag.InitFlags() (by removing its pflag.Parse() call). For now, we have to set the
	// normalize func and add the go flag set by hand.

	logs.InitLogs()
	defer logs.FlushLogs()

	if err := command.Execute(); err != nil {
		os.Exit(1)
	}
}

// function for handling the dialing with ziti
func dialFunc(ctx context.Context, network, address string) (net.Conn, error) {
	service := serviceName
	configFile, err := config.NewFromFile(configFilePath)

	if err != nil {
		logrus.WithError(err).Error("Error loading ziti config file")
		os.Exit(1)
	}

	context := ziti.NewContextWithConfig(configFile)
	return context.Dial(service)
}

func wrapConfigFn(restConfig *rest.Config) *rest.Config {

	restConfig.Dial = dialFunc
	return restConfig
}

func setZitiFlags(command *cobra.Command) *cobra.Command {

	command.PersistentFlags().StringVarP(&zFlags.zConfig, "zConfig", "C", "", "Path to ziti config file")
	command.PersistentFlags().StringVarP(&zFlags.service, "service", "S", "", "Service name")

	return command
}

// function for getting the current kubeconfig
func getKubeconfig() clientcmd.ClientConfig {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules,
		configOverrides)

	return kubeConfig
}

func parseKubeConfig(command *cobra.Command, kubeconfig clientcmd.ClientConfig) {
	// attempt to get the kubeconfig path from the command flags
	kubeconfigPath := command.Flag("kubeconfig").Value.String()

	// if the path is not set, attempt to get it from the kubeconfig precedence
	if kubeconfigPath == "" {
		// obtain the list of kubeconfig files from the current kubeconfig
		kubeconfigPrcedence := kubeconfig.ConfigAccess().GetLoadingPrecedence()

		// get the raw API config
		apiConfig, err := kubeconfig.RawConfig()

		if err != nil {
			panic(err)
		}

		// set the ziti options from one of the config files
		getZitiOptionsFromConfigList(kubeconfigPrcedence, apiConfig.CurrentContext)

	} else {
		// get the ziti options form the specified path
		getZitiOptionsFromConfig(kubeconfigPath)
	}

}

func getZitiOptionsFromConfigList(kubeconfigPrcedence []string, currentContext string) {
	// for the kubeconfig files in the precedence
	for _, path := range kubeconfigPrcedence {

		// read the config file
		config := readKubeConfig(path)

		// loop through the context list
		for _, context := range config.Contexts {

			// if the context name matches the current context
			if currentContext == context.Name {

				// set the config file path if it's not already set
				if configFilePath == "" {
					configFilePath = context.Context.ZConfig
				}

				// set the service name if it's not already set
				if serviceName == "" {
					serviceName = context.Context.Service
				}

				break
			}
		}
	}
}

func readKubeConfig(kubeconfig string) MinKubeConfig {
	// get the file name from the path
	filename, _ := filepath.Abs(kubeconfig)

	// read the yaml file
	yamlFile, err := ioutil.ReadFile(filename)

	if err != nil {
		panic(err)
	}

	var minKubeConfig MinKubeConfig

	//parse the yaml file
	err = yaml.Unmarshal(yamlFile, &minKubeConfig)
	if err != nil {
		panic(err)
	}

	return minKubeConfig

}

func getZitiOptionsFromConfig(kubeconfig string) {

	// get the config from the path
	config := clientcmd.GetConfigFromFileOrDie(kubeconfig)

	// get the current context
	currentContext := config.CurrentContext

	// read the yaml file
	minKubeConfig := readKubeConfig(kubeconfig)

	var context Context
	// find the context that matches the current context
	for _, ctx := range minKubeConfig.Contexts {

		if ctx.Name == currentContext {
			context = ctx.Context
		}
	}

	// set the config file if not already set
	if configFilePath == "" {
		configFilePath = context.ZConfig
	}

	// set the service name if not already set
	if serviceName == "" {
		serviceName = context.Service
	}
}
