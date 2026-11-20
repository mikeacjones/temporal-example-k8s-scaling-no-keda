package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/contrib/envconfig"
	"go.temporal.io/sdk/contrib/opentelemetry"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// ProcessMedia stays Pinned so existing executions finish on their original
// worker version while the controller routes new work to the next version.
func ProcessMedia(ctx workflow.Context, duration time.Duration) (string, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: duration + time.Minute,
		HeartbeatTimeout:    15 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
	})
	var result string
	err := workflow.ExecuteActivity(ctx, AnalyzeMedia, duration).Get(ctx, &result)
	return result, err
}

// The wait represents media processing. Heartbeats and cancellation let the
// Activity recover through normal Temporal retries if a pod is removed.
func AnalyzeMedia(ctx context.Context, duration time.Duration) (string, error) {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	activity.RecordHeartbeat(ctx)
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-tick.C:
			activity.RecordHeartbeat(ctx)
		case <-timer.C:
			return "media analyzed", nil
		}
	}
}

func RolloutGate(ctx workflow.Context) error {
	_, err := ProcessMedia(ctx, time.Second)
	return err
}

func run(ctx context.Context) error {
	opts, err := envconfig.LoadClientOptions(envconfig.LoadClientOptionsRequest{DisableFile: true})
	if err != nil {
		return err
	}
	queue := os.Getenv("TEMPORAL_TASK_QUEUE")
	if queue == "" {
		return fmt.Errorf("TEMPORAL_TASK_QUEUE is required")
	}

	if len(os.Args) > 1 && os.Args[1] == "load" {
		c, err := client.Dial(opts)
		if err != nil {
			return err
		}
		defer c.Close()
		return sendLoad(ctx, c, queue, os.Args[2:])
	}

	deploymentName, buildID := os.Getenv("TEMPORAL_DEPLOYMENT_NAME"), os.Getenv("TEMPORAL_WORKER_BUILD_ID")
	if deploymentName == "" || buildID == "" {
		return fmt.Errorf("controller-injected TEMPORAL_DEPLOYMENT_NAME and TEMPORAL_WORKER_BUILD_ID are required")
	}
	exporter, err := otelprom.New()
	if err != nil {
		return err
	}
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	defer provider.Shutdown(context.Background())
	opts.MetricsHandler = opentelemetry.NewMetricsHandler(opentelemetry.MetricsHandlerOptions{
		Meter: provider.Meter("media-worker"),
	})
	c, err := client.Dial(opts)
	if err != nil {
		return err
	}
	defer c.Close()
	slots := 4
	if value := os.Getenv("ACTIVITY_SLOTS"); value != "" {
		slots, err = strconv.Atoi(value)
		if err != nil || slots < 1 {
			return fmt.Errorf("ACTIVITY_SLOTS must be a positive integer")
		}
	}
	w := worker.New(c, queue, worker.Options{
		DeploymentOptions: worker.DeploymentOptions{
			UseVersioning:             true,
			Version:                   worker.WorkerDeploymentVersion{DeploymentName: deploymentName, BuildID: buildID},
			DefaultVersioningBehavior: workflow.VersioningBehaviorPinned,
		},
		MaxConcurrentActivityExecutionSize: slots,
		MaxConcurrentActivityTaskPollers:   min(2, slots),
		WorkerStopTimeout:                  45 * time.Second,
	})
	w.RegisterWorkflow(ProcessMedia)
	w.RegisterWorkflow(RolloutGate)
	w.RegisterActivity(AnalyzeMedia)
	if err := w.Start(); err != nil {
		return err
	}
	defer w.Stop()
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	server := &http.Server{
		Addr: ":9090", Handler: mux,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 15 * time.Second, IdleTimeout: time.Minute,
	}
	errs := make(chan error, 1)
	go func() { errs <- server.ListenAndServe() }()
	slog.Info("worker started", "deployment", deploymentName, "build_id", buildID, "task_queue", queue)
	select {
	case <-ctx.Done():
	case err := <-errs:
		if err != http.ErrServerClosed {
			return err
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return server.Shutdown(shutdown)
}

func sendLoad(ctx context.Context, c client.Client, queue string, args []string) error {
	fs := flag.NewFlagSet("load", flag.ContinueOnError)
	count := fs.Int("count", 60, "finite number of workflows")
	interval := fs.Duration("interval", 500*time.Millisecond, "time between workflow starts")
	duration := fs.Duration("activity-duration", 30*time.Second, "simulated Activity duration")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *count < 1 || *count > 1000 || *interval < 0 || *duration <= 0 || *duration > 10*time.Minute {
		return fmt.Errorf("use count 1..1000, nonnegative interval, and activity-duration in (0,10m]")
	}
	batch := strconv.FormatInt(time.Now().UnixNano(), 10)
	for i := 0; i < *count; i++ {
		call, cancel := context.WithTimeout(ctx, 15*time.Second)
		_, err := c.ExecuteWorkflow(call, client.StartWorkflowOptions{
			ID: fmt.Sprintf("hpa-example-%s-%04d", batch, i), TaskQueue: queue,
			WorkflowExecutionTimeout: 30 * time.Minute,
		}, ProcessMedia, *duration)
		cancel()
		if err != nil {
			return err
		}
		if i+1 < *count {
			timer := time.NewTimer(*interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	slog.Info("load submitted", "workflow_count", *count, "batch", batch)
	return nil
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx); err != nil {
		slog.Error("worker stopped", "error", err)
		os.Exit(1)
	}
}
