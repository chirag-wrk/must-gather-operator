package main

import (
	"context"
	goflag "flag"
	"fmt"
	"os"
	"strings"

	ctrl "sigs.k8s.io/controller-runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/openshift/must-gather-operator/config"
	"github.com/openshift/must-gather-operator/controllers/mustgather"
	"github.com/openshift/must-gather-operator/pkg/k8sutil"
	"github.com/openshift/must-gather-operator/pkg/localmetrics"
	osdmetrics "github.com/openshift/operator-custom-metrics/pkg/metrics"
	"github.com/operator-framework/operator-lib/leader"
	"github.com/redhat-cop/operator-utils/pkg/util"
	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	var enableLeaderElection bool
	var probeAddr string
	var trustedCAConfigMapName string
	opts := zap.Options{
		Development: true,
	}

	rootCmd := &cobra.Command{
		Use:   "must-gather-operator",
		Short: "Must Gather Operator",
		RunE: func(_ *cobra.Command, _ []string) error {
			ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
			printVersion()
			return runOperator(probeAddr, enableLeaderElection, trustedCAConfigMapName)
		},
	}

	rootCmd.Flags().StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	rootCmd.Flags().BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	rootCmd.Flags().StringVar(&trustedCAConfigMapName, "trusted-ca-configmap", "",
		"Name of the ConfigMap containing the trusted CA certificate bundle, default: disabled. "+
			"No CA config map will be explicitly mounted on Job pods if unset.")
	zapFlags := goflag.NewFlagSet("zap", goflag.ContinueOnError)
	opts.BindFlags(zapFlags)
	rootCmd.Flags().AddGoFlagSet(zapFlags)

	rootCmd.AddCommand(newObfuscateCmd())

	return rootCmd
}

func runOperator(probeAddr string, enableLeaderElection bool, trustedCAConfigMapName string) error {
	options := ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "b15e5fc1.openshift.io",
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), options)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return err
	}

	ctx := context.TODO()

	if strings.ToLower(os.Getenv(ForceRunModeEnv)) != LocalRunMode {
		err = leader.Become(ctx, "must-gather-operator-lock")
		if err != nil {
			log.Error(err, "")
			return err
		}
	} else {
		setupLog.Info("bypassing leader election due to local execution")
	}

	operatorNamespace, err := k8sutil.GetOperatorNamespace()
	if err != nil {
		if err != k8sutil.ErrRunLocal {
			log.Error(err, "Failed to get operator namespace")
			return err
		}
		operatorNamespace = mustgather.DefaultMustGatherNamespace
	}

	defaultMustGatherImage, varPresent := os.LookupEnv(mustgather.DefaultMustGatherImageEnv)
	if !varPresent {
		setupLog.Error(fmt.Errorf("environment variable %s not found", mustgather.DefaultMustGatherImageEnv), "unable to start manager")
		return fmt.Errorf("environment variable %s not found", mustgather.DefaultMustGatherImageEnv)
	}

	operatorSAName := os.Getenv("OPERATOR_SERVICE_ACCOUNT")
	if operatorSAName == "" {
		if strings.ToLower(os.Getenv(ForceRunModeEnv)) == LocalRunMode {
			operatorSAName = config.OperatorName
		} else {
			setupLog.Error(fmt.Errorf("OPERATOR_SERVICE_ACCOUNT environment variable not set"), "unable to discover operator service account")
			return fmt.Errorf("OPERATOR_SERVICE_ACCOUNT environment variable not set")
		}
	}
	setupLog.Info("operator service account", "name", operatorSAName)

	if err = (&mustgather.MustGatherReconciler{
		ReconcilerBase:             util.NewReconcilerBase(mgr.GetClient(), mgr.GetScheme(), mgr.GetConfig(), mgr.GetEventRecorderFor("must-gather-controller"), mgr.GetAPIReader()),
		TrustedCAConfigMap:         trustedCAConfigMapName,
		OperatorNamespace:          operatorNamespace,
		DefaultMustGatherImage:     defaultMustGatherImage,
		OperatorServiceAccountName: operatorSAName,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "MustGather")
		return err
	}

	metricsServer := osdmetrics.NewBuilder(operatorNamespace, config.OperatorName).
		WithPort(metricsPort).
		WithPath(metricsPath).
		WithCollectors(localmetrics.MetricsList).
		WithServiceMonitor().
		GetConfig()

	if err := osdmetrics.ConfigureMetrics(ctx, *metricsServer); err != nil {
		log.Error(err, "Failed to configure OSD metrics")
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		return err
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		return err
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		return err
	}

	return nil
}
