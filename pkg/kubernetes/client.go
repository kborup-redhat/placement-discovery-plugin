package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	kubevirtv1 "kubevirt.io/api/core/v1"
	kubevirtclient "kubevirt.io/client-go/kubecli"
)

// Client wraps Kubernetes and KubeVirt clients
type Client struct {
	K8s      kubernetes.Interface
	Dynamic  dynamic.Interface
	KubeVirt kubevirtclient.KubevirtClient
	config   *rest.Config
}

// NewClient creates a new Kubernetes client using in-cluster configuration
// Falls back to kubeconfig if not running in-cluster (for local development)
func NewClient() (*Client, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		klog.Warningf("Failed to get in-cluster config: %v, trying kubeconfig", err)
		// Fallback to kubeconfig for local development
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		configOverrides := &clientcmd.ConfigOverrides{}
		kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
		config, err = kubeConfig.ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to get kubeconfig: %w", err)
		}
	}

	// Create standard Kubernetes clientset
	k8sClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Create dynamic client for CRD access
	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	// Create KubeVirt client
	kubevirtClient, err := kubevirtclient.GetKubevirtClientFromRESTConfig(config)
	if err != nil {
		klog.Warningf("Failed to create KubeVirt client: %v (KubeVirt may not be installed)", err)
		// Don't fail if KubeVirt is not installed - just log warning
		kubevirtClient = nil
	}

	return &Client{
		K8s:      k8sClient,
		Dynamic:  dynClient,
		KubeVirt: kubevirtClient,
		config:   config,
	}, nil
}

// GetPod retrieves a Pod by namespace and name
func (c *Client) GetPod(ctx context.Context, namespace, name string) (*corev1.Pod, error) {
	return c.K8s.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
}

// GetNodes retrieves all nodes in the cluster
func (c *Client) GetNodes(ctx context.Context) (*corev1.NodeList, error) {
	return c.K8s.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
}

// GetVirtualMachine retrieves a VirtualMachine by namespace and name
func (c *Client) GetVirtualMachine(ctx context.Context, namespace, name string) (*kubevirtv1.VirtualMachine, error) {
	if c.KubeVirt == nil {
		return nil, fmt.Errorf("KubeVirt client not available - is KubeVirt installed?")
	}
	opts := &metav1.GetOptions{}
	return c.KubeVirt.VirtualMachine(namespace).Get(ctx, name, opts)
}

// GetVirtualMachineInstance retrieves a VirtualMachineInstance by namespace and name
func (c *Client) GetVirtualMachineInstance(ctx context.Context, namespace, name string) (*kubevirtv1.VirtualMachineInstance, error) {
	if c.KubeVirt == nil {
		return nil, fmt.Errorf("KubeVirt client not available - is KubeVirt installed?")
	}
	opts := &metav1.GetOptions{}
	return c.KubeVirt.VirtualMachineInstance(namespace).Get(ctx, name, opts)
}

// IsKubeVirtAvailable returns true if KubeVirt client is available
func (c *Client) IsKubeVirtAvailable() bool {
	return c.KubeVirt != nil
}

// NetworkAttachmentDefinitionExists checks if a NetworkAttachmentDefinition exists
func (c *Client) NetworkAttachmentDefinitionExists(ctx context.Context, namespace, name string) bool {
	nadGVR := schema.GroupVersionResource{
		Group:    "k8s.cni.cncf.io",
		Version:  "v1",
		Resource: "network-attachment-definitions",
	}
	_, err := c.Dynamic.Resource(nadGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		klog.V(4).Infof("NetworkAttachmentDefinition %s/%s not found: %v", namespace, name, err)
		return false
	}
	return true
}

// nadCNIConfig represents the CNI config from a NetworkAttachmentDefinition
type nadCNIConfig struct {
	Type       string `json:"type"`
	DeviceType string `json:"deviceType,omitempty"`
}

// GetNetworkInfo retrieves detailed network information including per-node availability
func (c *Client) GetNetworkInfo(ctx context.Context, namespace, name string) (netType string, available bool, nodes []string) {
	nadGVR := schema.GroupVersionResource{
		Group:    "k8s.cni.cncf.io",
		Version:  "v1",
		Resource: "network-attachment-definitions",
	}

	nad, err := c.Dynamic.Resource(nadGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		klog.V(4).Infof("NetworkAttachmentDefinition %s/%s not found: %v", namespace, name, err)
		return "", false, nil
	}

	// Parse the CNI config to determine network type
	spec, ok := nad.Object["spec"].(map[string]interface{})
	if !ok {
		return "unknown", true, c.getSchedulableNodeNames(ctx)
	}

	configStr, _ := spec["config"].(string)
	var cniConfig nadCNIConfig
	if configStr != "" {
		if err := json.Unmarshal([]byte(configStr), &cniConfig); err != nil {
			klog.V(4).Infof("Failed to parse NAD config for %s/%s: %v", namespace, name, err)
		}
	}

	netType = cniConfig.Type
	if netType == "" {
		netType = "unknown"
	}

	// Check annotations for resource name (SR-IOV networks use this)
	annotations := nad.GetAnnotations()
	resourceName := ""
	if annotations != nil {
		resourceName = annotations["k8s.v1.cni.cncf.io/resourceName"]
	}

	// For SR-IOV networks, check SriovNetworkNodeState to find which nodes have the resource
	if resourceName != "" || netType == "sriov" {
		sriovNodes := c.getSriovNetworkNodes(ctx, resourceName)
		if len(sriovNodes) > 0 {
			return netType, true, sriovNodes
		}
	}

	// For non-SR-IOV networks (bridge, macvlan, ovn-k8s-cni-overlay, etc.),
	// assume available on all schedulable nodes if the NAD exists
	return netType, true, c.getSchedulableNodeNames(ctx)
}

// getSchedulableNodeNames returns names of all schedulable (non-cordoned) nodes
func (c *Client) getSchedulableNodeNames(ctx context.Context) []string {
	nodeList, err := c.K8s.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		klog.V(4).Infof("Failed to list nodes: %v", err)
		return nil
	}

	var names []string
	for _, node := range nodeList.Items {
		if node.Spec.Unschedulable {
			continue
		}
		// Skip control plane nodes
		if _, isMaster := node.Labels["node-role.kubernetes.io/master"]; isMaster {
			continue
		}
		if _, isCP := node.Labels["node-role.kubernetes.io/control-plane"]; isCP {
			continue
		}
		names = append(names, node.Name)
	}
	return names
}

// getSriovNetworkNodes checks SriovNetworkNodeState resources to find nodes with the given resource
func (c *Client) getSriovNetworkNodes(ctx context.Context, resourceName string) []string {
	sriovNodeStateGVR := schema.GroupVersionResource{
		Group:    "sriovnetwork.openshift.io",
		Version:  "v1",
		Resource: "sriovnetworknodestates",
	}

	// SriovNetworkNodeState resources are in openshift-sriov-network-operator namespace
	// but are named after the node
	stateList, err := c.Dynamic.Resource(sriovNodeStateGVR).Namespace("openshift-sriov-network-operator").List(ctx, metav1.ListOptions{})
	if err != nil {
		klog.V(4).Infof("Failed to list SriovNetworkNodeState: %v", err)
		return nil
	}

	var nodes []string
	for _, state := range stateList.Items {
		nodeName := state.GetName()

		// Check if this node has the resource configured via interfaces
		spec, ok := state.Object["spec"].(map[string]interface{})
		if !ok {
			continue
		}

		interfaces, ok := spec["interfaces"].([]interface{})
		if !ok {
			continue
		}

		for _, iface := range interfaces {
			ifaceMap, ok := iface.(map[string]interface{})
			if !ok {
				continue
			}

			// Check if the interface's resource name matches
			ifaceResource, _ := ifaceMap["resourceName"].(string)
			if resourceName != "" && ifaceResource == resourceName {
				nodes = append(nodes, nodeName)
				break
			}
		}
	}

	return nodes
}
