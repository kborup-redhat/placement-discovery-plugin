package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/kborup-redhat/placement-discovery-plugin/pkg/kubernetes"
	"github.com/kborup-redhat/placement-discovery-plugin/pkg/models"
	"github.com/kborup-redhat/placement-discovery-plugin/pkg/placement"
)

// dns1123Regex validates Kubernetes resource names (RFC 1123 DNS labels)
var dns1123Regex = regexp.MustCompile(`^[a-z0-9]([a-z0-9\-\.]{0,251}[a-z0-9])?$`)

// PlacementHandler handles placement API requests
type PlacementHandler struct {
	client     *kubernetes.Client
	calculator *placement.Calculator
}

// NewPlacementHandler creates a new placement handler
func NewPlacementHandler(client *kubernetes.Client) *PlacementHandler {
	return &PlacementHandler{
		client:     client,
		calculator: placement.NewCalculator(client.K8s),
	}
}

// ServeHTTP handles placement API requests
// Path format: /api/placement/{type}/{namespace}/{name}
// Supported types: pod, deployment, vm
func (h *PlacementHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only allow GET requests
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse path: /api/placement/{type}/{namespace}/{name}
	path := strings.TrimPrefix(r.URL.Path, "/api/placement/")
	parts := strings.Split(path, "/")
	if len(parts) != 3 {
		http.Error(w, "Invalid path format. Expected: /api/placement/{type}/{namespace}/{name}", http.StatusBadRequest)
		return
	}

	resourceType := parts[0]
	namespace := parts[1]
	name := parts[2]

	// Validate resource type against whitelist
	switch resourceType {
	case "pod", "deployment", "vm":
		// valid
	default:
		http.Error(w, "Unsupported resource type", http.StatusBadRequest)
		return
	}

	// Validate namespace and name are valid Kubernetes names
	if !dns1123Regex.MatchString(namespace) || !dns1123Regex.MatchString(name) {
		http.Error(w, "Invalid namespace or resource name", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	var response *models.PlacementResponse
	var err error

	switch resourceType {
	case "pod":
		response, err = h.handlePodPlacement(ctx, namespace, name)
	case "deployment":
		response, err = h.handleDeploymentPlacement(ctx, namespace, name)
	case "vm":
		response, err = h.handleVMPlacement(ctx, namespace, name)
	}

	if err != nil {
		klog.Errorf("Failed to calculate placement for %s/%s/%s: %v", resourceType, namespace, name, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		klog.Errorf("Failed to encode response: %v", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// handlePodPlacement handles placement calculation for a pod
func (h *PlacementHandler) handlePodPlacement(ctx context.Context, namespace, name string) (*models.PlacementResponse, error) {
	pod, err := h.client.GetPod(ctx, namespace, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get pod: %w", err)
	}

	return h.calculator.CalculatePodPlacement(ctx, pod)
}

// handleDeploymentPlacement handles placement calculation for a deployment
// Uses the deployment's pod template to calculate placement
func (h *PlacementHandler) handleDeploymentPlacement(ctx context.Context, namespace, name string) (*models.PlacementResponse, error) {
	deployment, err := h.client.K8s.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment: %w", err)
	}

	// Create a virtual pod from the deployment template
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deployment.Name + "-template",
			Namespace: deployment.Namespace,
			Labels:    deployment.Spec.Template.Labels,
		},
		Spec: deployment.Spec.Template.Spec,
	}

	// Try to find all running pods from this deployment to get current nodes
	currentNodes := make([]string, 0)
	var runningPods *corev1.PodList
	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err == nil {
		runningPods, err = h.client.K8s.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector.String(),
			FieldSelector: "status.phase=Running",
		})
		if err == nil && runningPods != nil && len(runningPods.Items) > 0 {
			// Collect all unique nodes where pods are running
			nodeMap := make(map[string]bool)
			for _, p := range runningPods.Items {
				if p.Spec.NodeName != "" {
					nodeMap[p.Spec.NodeName] = true
				}
			}
			for nodeName := range nodeMap {
				currentNodes = append(currentNodes, nodeName)
			}
			// Use the first running pod's node for backward compatibility
			if len(currentNodes) > 0 {
				pod.Spec.NodeName = runningPods.Items[0].Spec.NodeName
			}
		}
	}

	response, err := h.calculator.CalculatePodPlacement(ctx, pod)
	if err != nil {
		return nil, err
	}

	// Add all current nodes to the response
	if len(currentNodes) > 0 {
		response.CurrentNodes = currentNodes
	}

	// Extract SCC from actual running pods (only available on real pods, not templates)
	if runningPods != nil && len(runningPods.Items) > 0 && response.PodSpec != nil {
		// Get SCC from the first running pod
		if scc, exists := runningPods.Items[0].Annotations["openshift.io/scc"]; exists {
			response.PodSpec.SCC = scc
			klog.V(2).Infof("Extracted SCC from pod: %s", scc)
		} else {
			klog.V(2).Infof("No SCC annotation found on pod %s", runningPods.Items[0].Name)
		}
	}

	klog.V(2).Infof("Deployment %s/%s - CurrentNodes: %v, SA: %s, SCC: %s, QoS: %s",
		namespace, name, response.CurrentNodes, response.PodSpec.ServiceAccount, response.PodSpec.SCC, response.PodSpec.QoSClass)

	return response, nil
}

// handleVMPlacement handles placement calculation for a VirtualMachine
func (h *PlacementHandler) handleVMPlacement(ctx context.Context, namespace, name string) (*models.PlacementResponse, error) {
	if !h.client.IsKubeVirtAvailable() {
		return nil, fmt.Errorf("KubeVirt is not available in this cluster")
	}

	vm, err := h.client.GetVirtualMachine(ctx, namespace, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get VirtualMachine: %w", err)
	}

	vmCalc := placement.NewVMCalculator(h.calculator, h.client.KubeVirt)
	response, err := vmCalc.CalculateVMPlacement(ctx, vm)
	if err != nil {
		return nil, err
	}

	// Check network availability for each Multus network
	networks := make([]models.NetworkInfo, 0)
	allSchedulableNodes := len(h.getSchedulableWorkerNodes(ctx))
	for _, network := range vm.Spec.Template.Spec.Networks {
		if network.Multus != nil {
			netName := network.Multus.NetworkName
			// Parse namespace/name format
			nadNamespace := namespace
			nadName := netName
			if parts := strings.SplitN(netName, "/", 2); len(parts) == 2 {
				nadNamespace = parts[0]
				nadName = parts[1]
			}

			netType, available, nodes := h.client.GetNetworkInfo(ctx, nadNamespace, nadName)
			allNodes := available && len(nodes) >= allSchedulableNodes
			networks = append(networks, models.NetworkInfo{
				Name:      netName,
				Available: available,
				AllNodes:  allNodes,
				Nodes:     nodes,
				Type:      netType,
			})
		}
	}
	// Always include pod network (default)
	if len(vm.Spec.Template.Spec.Networks) > 0 {
		for _, network := range vm.Spec.Template.Spec.Networks {
			if network.Pod != nil {
				networks = append(networks, models.NetworkInfo{
					Name:      "Pod Network (default)",
					Available: true,
					AllNodes:  true,
					Type:      "pod",
				})
				break
			}
		}
	}
	if len(networks) > 0 {
		response.Networks = networks
	}

	return response, nil
}

// getSchedulableWorkerNodes returns schedulable worker nodes
func (h *PlacementHandler) getSchedulableWorkerNodes(ctx context.Context) []corev1.Node {
	nodeList, err := h.client.K8s.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}
	var workers []corev1.Node
	for _, node := range nodeList.Items {
		if node.Spec.Unschedulable {
			continue
		}
		if _, isMaster := node.Labels["node-role.kubernetes.io/master"]; isMaster {
			continue
		}
		if _, isCP := node.Labels["node-role.kubernetes.io/control-plane"]; isCP {
			continue
		}
		workers = append(workers, node)
	}
	return workers
}
