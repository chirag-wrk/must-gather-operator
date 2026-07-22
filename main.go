/*
Copyright 2022.

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
	"os"
	"runtime"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	apiruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	v1 "github.com/openshift/api/config/v1"
	imagev1 "github.com/openshift/api/image/v1"
	managedv1alpha1 "github.com/openshift/must-gather-operator/api/v1alpha1"
	"github.com/openshift/must-gather-operator/version"
)

const (
	// Environment variable to determine operator run mode
	ForceRunModeEnv = "OSDK_FORCE_RUN_MODE"
	// Flags that the operator is running locally
	LocalRunMode = "local"
)

var (
	scheme   = apiruntime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

// Change below variables to serve metrics on different host or port.
var (
	metricsPort = "8080"
	metricsPath = "/metrics"
)

var log = logf.Log.WithName("cmd")

func printVersion() {
	log.Info("Operator Version", "version", version.Version)
	log.Info("Go Version", "goVersion", runtime.Version())
	log.Info("Go OS/Arch", "goOS", runtime.GOOS, "goArch", runtime.GOARCH)
	log.Info("SDK Version", "sdkVersion", version.SDKVersion)
}

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1.AddToScheme(scheme))
	utilruntime.Must(imagev1.AddToScheme(scheme))
	utilruntime.Must(managedv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	rootCmd := newRootCmd()
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
