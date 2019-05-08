/*
Copyright 2017 by the contributors.

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
	"flag"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/sample-controller/pkg/signals"
	"sigs.k8s.io/aws-iam-authenticator/pkg/config"
	"sigs.k8s.io/aws-iam-authenticator/pkg/controller"
	clientset "sigs.k8s.io/aws-iam-authenticator/pkg/generated/clientset/versioned"
	informers "sigs.k8s.io/aws-iam-authenticator/pkg/generated/informers/externalversions"
	"sigs.k8s.io/aws-iam-authenticator/pkg/server"

	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/aws-iam-authenticator/pkg/generated/clientset/versioned/fake"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// DefaultPort is the default localhost port (chosen randomly).
const DefaultPort = 21362

// serverCmd represents the server command
var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Run a webhook validation server suitable that validates tokens using AWS IAM",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {
		var err error
		var k8sconfig *rest.Config
		var kubeClient kubernetes.Interface
		var iamClient clientset.Interface
		var iamInformerFactory informers.SharedInformerFactory
		var noResyncPeriodFunc = func() time.Duration { return 0 }

		stopCh := signals.SetupSignalHandler()

		cfg, err := getConfig()
		if err != nil {
			logrus.Fatalf("%s", err)
		}

		logrus.Infof("Feature Gates %+v", cfg.FeatureGates)

		if cfg.FeatureGates.Enabled(config.IAMIdentityMappingCRD) {
			if cfg.Master != "" || cfg.Kubeconfig != "" {
				k8sconfig, err = clientcmd.BuildConfigFromFlags(cfg.Master, cfg.Kubeconfig)
			} else {
				k8sconfig, err = rest.InClusterConfig()
			}
			if err != nil {
				logrus.WithError(err).Fatal("can't create kubernetes config")
			}

			kubeClient, err = kubernetes.NewForConfig(k8sconfig)
			if err != nil {
				logrus.WithError(err).Fatal("can't create kubernetes client")
			}

			iamClient, err = clientset.NewForConfig(k8sconfig)
			if err != nil {
				logrus.WithError(err).Fatal("can't create iam authenticator client")
			}

			iamInformerFactory = informers.NewSharedInformerFactory(iamClient, time.Second*36000)

			ctrl := controller.New(kubeClient, iamClient, iamInformerFactory.Iamauthenticator().V1alpha1().IAMIdentityMappings())
			httpServer := server.New(cfg, iamInformerFactory.Iamauthenticator().V1alpha1().IAMIdentityMappings())
			iamInformerFactory.Start(stopCh)

			go func() {
				httpServer.Run(stopCh)
			}()

			if err := ctrl.Run(2, stopCh); err != nil {
				logrus.WithError(err).Fatal("controller exited")
			}
		} else {
			kubeClient = k8sfake.NewSimpleClientset([]runtime.Object{}...)
			iamClient = fake.NewSimpleClientset([]runtime.Object{}...)
			iamInformerFactory = informers.NewSharedInformerFactory(iamClient, noResyncPeriodFunc())

			iamInformerFactory.Start(stopCh)
			httpServer := server.New(cfg, iamInformerFactory.Iamauthenticator().V1alpha1().IAMIdentityMappings())
			go func() {
				httpServer.Run(stopCh)
			}()
			<-stopCh
		}
	},
}

func init() {
	viper.SetDefault("server.port", DefaultPort)

	serverCmd.Flags().String("generate-kubeconfig",
		"/etc/kubernetes/aws-iam-authenticator/kubeconfig.yaml",
		"Output `path` where a generated webhook kubeconfig (for `--authentication-token-webhook-config-file`) will be stored (should be a hostPath mount).")
	viper.BindPFlag("server.generateKubeconfig", serverCmd.Flags().Lookup("generate-kubeconfig"))

	serverCmd.Flags().Bool("kubeconfig-pregenerated",
		false,
		"set to `true` when a webhook kubeconfig is pre-generated by running the `init` command, and therefore the `server` shouldn't unnecessarily re-generate a new one.")
	viper.BindPFlag("server.kubeconfigPregenerated", serverCmd.Flags().Lookup("kubeconfig-pregenerated"))

	serverCmd.Flags().String("state-dir",
		"/var/aws-iam-authenticator",
		"State `directory` for generated certificate and private key (should be a hostPath mount).")
	viper.BindPFlag("server.stateDir", serverCmd.Flags().Lookup("state-dir"))

	serverCmd.Flags().String("kubeconfig",
		"",
		"kubeconfig file path for using a local kubeconfig to configure the client to talk to the API server for the IAMIdentityMappings.")
	viper.BindPFlag("server.kubeconfig", serverCmd.Flags().Lookup("kubeconfig"))
	serverCmd.Flags().String("master",
		"",
		"master is the URL to the api server")
	viper.BindPFlag("server.master", serverCmd.Flags().Lookup("master"))

	serverCmd.Flags().String(
		"address",
		"127.0.0.1",
		"IP Address to bind the server to listen to. (should be a 127.0.0.1 or 0.0.0.0)")
	viper.BindPFlag("server.address", serverCmd.Flags().Lookup("address"))

	fs := flag.NewFlagSet("", flag.ContinueOnError)
	_ = fs.Parse([]string{})
	flag.CommandLine = fs

	rootCmd.AddCommand(serverCmd)
}
