//go:build integration
// +build integration

package webhook

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	timeout = time.Minute * 2
	testNS  = "webhook-test"
)

// testCluster represents a test Kubernetes cluster
type testCluster struct {
	kubeconfig string
	clientset  *kubernetes.Clientset
}

func cleanup(t *testing.T) {
	t.Helper()
	cmd := exec.Command("kind", "delete", "cluster", "--name", "webhook-test")
	_ = cmd.Run()               // Ignore errors as cluster might not exist
	time.Sleep(5 * time.Second) // Give time for cleanup
}

func setupTestCluster(t *testing.T) (*testCluster, error) {
	t.Helper()

	// Clean up any existing cluster first
	cleanup(t)

	// Create temporary directory for kubeconfig
	tmpDir, err := os.MkdirTemp("", "webhook-integration-test")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %v", err)
	}

	kubeconfig := filepath.Join(tmpDir, "kubeconfig")

	// Create kind cluster
	cmd := exec.Command("kind", "create", "cluster",
		"--name", "webhook-test",
		"--kubeconfig", kubeconfig,
		"--wait", "60s")
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("failed to create kind cluster: %v: %s", err, out)
	}

	// Install cert-manager
	cmd = exec.Command("kubectl", "--kubeconfig", kubeconfig,
		"apply", "-f",
		"https://github.com/cert-manager/cert-manager/releases/download/v1.13.3/cert-manager.yaml")
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("failed to install cert-manager: %v: %s", err, out)
	}

	// Wait for cert-manager to be ready
	time.Sleep(30 * time.Second)

	// Create kubernetes client
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("failed to build config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("failed to create clientset: %v", err)
	}

	return &testCluster{
		kubeconfig: kubeconfig,
		clientset:  clientset,
	}, nil
}

func (tc *testCluster) cleanup(t *testing.T) {
	t.Helper()
	cmd := exec.Command("kind", "delete", "cluster", "--name", "webhook-test")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Logf("warning: failed to delete kind cluster: %v: %s", err, out)
	}
	if err := os.RemoveAll(filepath.Dir(tc.kubeconfig)); err != nil {
		t.Logf("warning: failed to remove temp dir: %v", err)
	}
}

func (tc *testCluster) deployWebhook(t *testing.T) error {
	t.Helper()

	// Build webhook image
	cmd := exec.Command("docker", "build", "-t", "pod-label-webhook:latest", "../..")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to build webhook image: %v: %s", err, out)
	}

	// Load image into kind
	cmd = exec.Command("kind", "load", "docker-image", "pod-label-webhook:latest", "--name", "webhook-test")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to load image into kind: %v: %s", err, out)
	}

	// Apply webhook configuration
	cmd = exec.Command("kubectl", "--kubeconfig", tc.kubeconfig,
		"apply", "-f", "../../manifests/webhook.yaml")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to apply webhook config: %v: %s", err, out)
	}

	// Wait for cert-manager to process the certificate
	time.Sleep(5 * time.Second)

	// Apply webhook deployment
	cmd = exec.Command("kubectl", "--kubeconfig", tc.kubeconfig,
		"apply", "-f", "../../manifests/deployment.yaml")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to deploy webhook: %v: %s", err, out)
	}

	// Wait for webhook pod to be ready
	return waitFor(func() error {
		pods, err := tc.clientset.CoreV1().Pods("pod-label-system").List(
			context.Background(),
			metav1.ListOptions{
				LabelSelector: "app=pod-label-webhook",
			},
		)
		if err != nil {
			return err
		}
		if len(pods.Items) == 0 {
			return fmt.Errorf("no webhook pods found")
		}
		for _, pod := range pods.Items {
			if pod.Status.Phase != corev1.PodRunning {
				return fmt.Errorf("webhook pod not running")
			}
		}
		return nil
	}, timeout)
}

// Update the TestWebhookIntegration function
func TestWebhookIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	// Setup test cluster
	cluster, err := setupTestCluster(t)
	if err != nil {
		t.Fatalf("Failed to setup test cluster: %v", err)
	}
	defer cluster.cleanup(t)

	// Deploy webhook
	if err := cluster.deployWebhook(t); err != nil {
		t.Fatalf("Failed to deploy webhook: %v", err)
	}

	// Add wait for webhook to be fully ready
	time.Sleep(10 * time.Second)

	// Create test namespace
	_, err = cluster.clientset.CoreV1().Namespaces().Create(
		context.Background(),
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNS,
			},
		},
		metav1.CreateOptions{},
	)
	if err != nil {
		t.Fatalf("Failed to create test namespace: %v", err)
	}

	// [rest of the test remains the same]
}

// Add helper function to verify webhook status
func (tc *testCluster) debugWebhookStatus(t *testing.T) {
	t.Helper()

	// Get webhook pods
	pods, err := tc.clientset.CoreV1().Pods("pod-label-system").List(
		context.Background(),
		metav1.ListOptions{
			LabelSelector: "app=pod-label-webhook",
		},
	)
	if err != nil {
		t.Logf("Failed to get webhook pods: %v", err)
		return
	}

	for _, pod := range pods.Items {
		t.Logf("Webhook pod %s status: %s", pod.Name, pod.Status.Phase)

		// Get pod logs
		logs, err := tc.clientset.CoreV1().Pods("pod-label-system").GetLogs(pod.Name, &corev1.PodLogOptions{}).Do(context.Background()).Raw()
		if err != nil {
			t.Logf("Failed to get logs for pod %s: %v", pod.Name, err)
		} else {
			t.Logf("Pod logs:\n%s", string(logs))
		}
	}
}

func waitForPod(client *kubernetes.Clientset, name, namespace string) error {
	return waitFor(func() error {
		pod, err := client.CoreV1().Pods(namespace).Get(
			context.Background(),
			name,
			metav1.GetOptions{},
		)
		if err != nil {
			return err
		}
		if pod.Status.Phase != corev1.PodRunning {
			return fmt.Errorf("pod not running")
		}
		return nil
	}, timeout)
}

func waitFor(condition func() error, timeout time.Duration) error {
	start := time.Now()
	for {
		err := condition()
		if err == nil {
			return nil
		}
		if time.Since(start) > timeout {
			return fmt.Errorf("timed out waiting for condition: %v", err)
		}
		time.Sleep(time.Second)
	}
}
