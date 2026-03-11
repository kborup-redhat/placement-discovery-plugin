package placement

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"github.com/kborup-redhat/placement-discovery-plugin/pkg/models"
)

// Calculator evaluates pod placement eligibility across cluster nodes
type Calculator struct {
	k8sClient kubernetes.Interface
}

// NewCalculator creates a new placement calculator
func NewCalculator(k8sClient kubernetes.Interface) *Calculator {
	return &Calculator{
		k8sClient: k8sClient,
	}
}

// CalculatePodPlacement determines which nodes a pod can be scheduled on
func (c *Calculator) CalculatePodPlacement(ctx context.Context, pod *corev1.Pod) (*models.PlacementResponse, error) {
	// Get all nodes
	nodeList, err := c.k8sClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	// Get all pods to calculate resource usage per node
	allPods, err := c.k8sClient.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: "status.phase!=Failed,status.phase!=Succeeded",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	// Build node resource usage map
	nodeResources := c.calculateNodeResources(nodeList.Items, allPods.Items)

	// Evaluate each node
	eligibleNodes := make([]models.NodeEligibility, 0, len(nodeList.Items))
	for _, node := range nodeList.Items {
		eligibility := c.evaluateNode(pod, &node, nodeResources[node.Name])
		eligibleNodes = append(eligibleNodes, eligibility)
	}

	// Extract pod spec info
	podSpecInfo := c.extractPodSpecInfo(&pod.Spec)

	// Extract SCC from pod annotations (only available for running pods)
	if scc, exists := pod.Annotations["openshift.io/scc"]; exists {
		podSpecInfo.SCC = scc
	}

	// Extract storage info
	storageInfo, err := c.extractStorageInfo(ctx, pod)
	if err != nil {
		klog.Warningf("Failed to extract storage info for pod %s/%s: %v", pod.Namespace, pod.Name, err)
	}

	response := &models.PlacementResponse{
		CurrentNode:   pod.Spec.NodeName,
		EligibleNodes: eligibleNodes,
		PodSpec:       podSpecInfo,
		Storage:       storageInfo,
	}

	return response, nil
}

// evaluateNode checks if a pod can be scheduled on a specific node
func (c *Calculator) evaluateNode(pod *corev1.Pod, node *corev1.Node, resources models.NodeResources) models.NodeEligibility {
	reasons := make([]string, 0)
	canRun := true

	// Check if node is schedulable
	if node.Spec.Unschedulable {
		canRun = false
		reasons = append(reasons, "Node is marked as unschedulable")
	}

	// Check node conditions
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady && condition.Status != corev1.ConditionTrue {
			canRun = false
			reasons = append(reasons, fmt.Sprintf("Node is not ready: %s", condition.Reason))
		}
		if condition.Type == corev1.NodeDiskPressure && condition.Status == corev1.ConditionTrue {
			canRun = false
			reasons = append(reasons, "Node has disk pressure")
		}
		if condition.Type == corev1.NodeMemoryPressure && condition.Status == corev1.ConditionTrue {
			canRun = false
			reasons = append(reasons, "Node has memory pressure")
		}
		if condition.Type == corev1.NodePIDPressure && condition.Status == corev1.ConditionTrue {
			canRun = false
			reasons = append(reasons, "Node has PID pressure")
		}
	}

	// Check node selector
	if len(pod.Spec.NodeSelector) > 0 {
		if !c.matchNodeSelector(pod.Spec.NodeSelector, node.Labels) {
			canRun = false
			for key, value := range pod.Spec.NodeSelector {
				if nodeValue, exists := node.Labels[key]; !exists {
					reasons = append(reasons, fmt.Sprintf("Node selector mismatch: %s (not present)", key))
				} else if nodeValue != value {
					reasons = append(reasons, fmt.Sprintf("Node selector mismatch: %s=%s (node has %s=%s)", key, value, key, nodeValue))
				}
			}
		}
	}

	// Check node affinity
	if pod.Spec.Affinity != nil && pod.Spec.Affinity.NodeAffinity != nil {
		if !c.matchNodeAffinity(pod.Spec.Affinity.NodeAffinity, node) {
			canRun = false
			reasons = append(reasons, "Node affinity requirements not satisfied")
		}
	}

	// Check taints and tolerations
	if !c.toleratesTaints(pod.Spec.Tolerations, node.Spec.Taints) {
		canRun = false
		for _, taint := range node.Spec.Taints {
			if !c.hasToleration(pod.Spec.Tolerations, taint) {
				reasons = append(reasons, fmt.Sprintf("Taint not tolerated: %s=%s:%s", taint.Key, taint.Value, taint.Effect))
			}
		}
	}

	// Check resource capacity
	podRequests := c.calculatePodRequests(&pod.Spec)
	if !c.hasEnoughResources(podRequests, resources) {
		canRun = false
		cpuReq := podRequests["cpu"]
		memReq := podRequests["memory"]

		availCPU := resource.MustParse(resources.CPU.Available)
		availMem := resource.MustParse(resources.Memory.Available)

		if cpuReq.Cmp(availCPU) > 0 {
			reasons = append(reasons, fmt.Sprintf("Insufficient CPU: requires %s, available %s", cpuReq.String(), availCPU.String()))
		}
		if memReq.Cmp(availMem) > 0 {
			reasons = append(reasons, fmt.Sprintf("Insufficient memory: requires %s, available %s", memReq.String(), availMem.String()))
		}
	}

	// Extract taints info
	taints := make([]models.TaintInfo, 0, len(node.Spec.Taints))
	for _, taint := range node.Spec.Taints {
		taints = append(taints, models.TaintInfo{
			Key:    taint.Key,
			Value:  taint.Value,
			Effect: string(taint.Effect),
		})
	}

	return models.NodeEligibility{
		Name:              node.Name,
		CanRun:            canRun,
		Reasons:           reasons,
		Resources:         resources,
		Labels:            node.Labels,
		Taints:            taints,
		CPUManagerEnabled: c.isCPUManagerEnabled(node),
	}
}

// matchNodeSelector checks if node labels match the pod's node selector
func (c *Calculator) matchNodeSelector(selector, labels map[string]string) bool {
	for key, value := range selector {
		if labelValue, exists := labels[key]; !exists || labelValue != value {
			return false
		}
	}
	return true
}

// matchNodeAffinity checks if node matches pod's node affinity rules
func (c *Calculator) matchNodeAffinity(affinity *corev1.NodeAffinity, node *corev1.Node) bool {
	// Check required node affinity
	if affinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
		terms := affinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
		if len(terms) == 0 {
			return true
		}

		// At least one term must match
		matched := false
		for _, term := range terms {
			if c.matchNodeSelectorTerm(term, node.Labels) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	return true
}

// matchNodeSelectorTerm checks if a node selector term matches node labels
func (c *Calculator) matchNodeSelectorTerm(term corev1.NodeSelectorTerm, labels map[string]string) bool {
	// All match expressions must be satisfied
	for _, expr := range term.MatchExpressions {
		if !c.matchNodeSelectorRequirement(expr, labels) {
			return false
		}
	}

	// All match fields must be satisfied
	for _, field := range term.MatchFields {
		// For simplicity, we're not implementing field selectors in this MVP
		klog.V(4).Infof("Skipping field selector evaluation: %v", field)
	}

	return true
}

// matchNodeSelectorRequirement checks if a node selector requirement matches
func (c *Calculator) matchNodeSelectorRequirement(req corev1.NodeSelectorRequirement, labels map[string]string) bool {
	labelValue, exists := labels[req.Key]

	switch req.Operator {
	case corev1.NodeSelectorOpIn:
		if !exists {
			return false
		}
		for _, value := range req.Values {
			if labelValue == value {
				return true
			}
		}
		return false
	case corev1.NodeSelectorOpNotIn:
		if !exists {
			return true
		}
		for _, value := range req.Values {
			if labelValue == value {
				return false
			}
		}
		return true
	case corev1.NodeSelectorOpExists:
		return exists
	case corev1.NodeSelectorOpDoesNotExist:
		return !exists
	case corev1.NodeSelectorOpGt:
		// Numeric comparison - simplified for MVP
		return exists && len(req.Values) > 0
	case corev1.NodeSelectorOpLt:
		// Numeric comparison - simplified for MVP
		return exists && len(req.Values) > 0
	default:
		return false
	}
}

// toleratesTaints checks if pod tolerates all node taints
func (c *Calculator) toleratesTaints(tolerations []corev1.Toleration, taints []corev1.Taint) bool {
	for _, taint := range taints {
		if taint.Effect == corev1.TaintEffectNoSchedule || taint.Effect == corev1.TaintEffectNoExecute {
			if !c.hasToleration(tolerations, taint) {
				return false
			}
		}
	}
	return true
}

// hasToleration checks if pod has a toleration for a specific taint
func (c *Calculator) hasToleration(tolerations []corev1.Toleration, taint corev1.Taint) bool {
	for _, toleration := range tolerations {
		if c.tolerationMatches(toleration, taint) {
			return true
		}
	}
	return false
}

// tolerationMatches checks if a toleration matches a taint
func (c *Calculator) tolerationMatches(toleration corev1.Toleration, taint corev1.Taint) bool {
	// Empty effect means tolerate all effects
	if toleration.Effect != "" && toleration.Effect != taint.Effect {
		return false
	}

	// Empty key with Exists operator means tolerate all taints
	if toleration.Key == "" && toleration.Operator == corev1.TolerationOpExists {
		return true
	}

	// Key must match
	if toleration.Key != taint.Key {
		return false
	}

	// Check operator
	if toleration.Operator == corev1.TolerationOpExists {
		return true
	}

	// Default operator is Equal
	return toleration.Value == taint.Value
}

// calculateNodeResources computes resource capacity and availability for each node
func (c *Calculator) calculateNodeResources(nodes []corev1.Node, pods []corev1.Pod) map[string]models.NodeResources {
	result := make(map[string]models.NodeResources)

	// Initialize with node allocatable resources
	for _, node := range nodes {
		allocatable := node.Status.Allocatable
		result[node.Name] = models.NodeResources{
			CPU: models.ResourceInfo{
				Allocatable: allocatable.Cpu().String(),
				Available:   allocatable.Cpu().String(),
			},
			Memory: models.ResourceInfo{
				Allocatable: allocatable.Memory().String(),
				Available:   allocatable.Memory().String(),
			},
			Pods: models.ResourceInfo{
				Allocatable: allocatable.Pods().String(),
				Available:   allocatable.Pods().String(),
			},
		}
	}

	// Subtract pod resource requests from available
	for _, pod := range pods {
		if pod.Spec.NodeName == "" {
			continue // Pod not scheduled yet
		}

		nodeRes, exists := result[pod.Spec.NodeName]
		if !exists {
			continue
		}

		podRequests := c.calculatePodRequests(&pod.Spec)

		cpuAvail := resource.MustParse(nodeRes.CPU.Available)
		memAvail := resource.MustParse(nodeRes.Memory.Available)
		podsAvail := resource.MustParse(nodeRes.Pods.Available)

		cpuAvail.Sub(podRequests["cpu"])
		memAvail.Sub(podRequests["memory"])
		podsAvail.Sub(*resource.NewQuantity(1, resource.DecimalSI))

		nodeRes.CPU.Available = cpuAvail.String()
		nodeRes.Memory.Available = memAvail.String()
		nodeRes.Pods.Available = podsAvail.String()

		result[pod.Spec.NodeName] = nodeRes
	}

	return result
}

// calculatePodRequests sums up resource requests from all containers in a pod
func (c *Calculator) calculatePodRequests(spec *corev1.PodSpec) corev1.ResourceList {
	requests := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("0"),
		corev1.ResourceMemory: resource.MustParse("0"),
	}

	for _, container := range spec.Containers {
		if container.Resources.Requests != nil {
			if cpu, ok := container.Resources.Requests[corev1.ResourceCPU]; ok {
				cpuReq := requests[corev1.ResourceCPU]
				cpuReq.Add(cpu)
				requests[corev1.ResourceCPU] = cpuReq
			}
			if mem, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
				memReq := requests[corev1.ResourceMemory]
				memReq.Add(mem)
				requests[corev1.ResourceMemory] = memReq
			}
		}
	}

	for _, container := range spec.InitContainers {
		if container.Resources.Requests != nil {
			if cpu, ok := container.Resources.Requests[corev1.ResourceCPU]; ok {
				if cpu.Cmp(requests[corev1.ResourceCPU]) > 0 {
					requests[corev1.ResourceCPU] = cpu.DeepCopy()
				}
			}
			if mem, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
				if mem.Cmp(requests[corev1.ResourceMemory]) > 0 {
					requests[corev1.ResourceMemory] = mem.DeepCopy()
				}
			}
		}
	}

	return requests
}

// hasEnoughResources checks if node has enough resources for pod requests
func (c *Calculator) hasEnoughResources(requests corev1.ResourceList, available models.NodeResources) bool {
	cpuReq := requests[corev1.ResourceCPU]
	memReq := requests[corev1.ResourceMemory]

	cpuAvail := resource.MustParse(available.CPU.Available)
	memAvail := resource.MustParse(available.Memory.Available)

	return cpuReq.Cmp(cpuAvail) <= 0 && memReq.Cmp(memAvail) <= 0
}

// extractPodSpecInfo extracts relevant pod spec information for the API response
func (c *Calculator) extractPodSpecInfo(spec *corev1.PodSpec) *models.PodSpecInfo {
	info := &models.PodSpecInfo{
		NodeSelector: spec.NodeSelector,
	}

	// Extract tolerations
	if len(spec.Tolerations) > 0 {
		info.Tolerations = make([]models.TolerationInfo, 0, len(spec.Tolerations))
		for _, t := range spec.Tolerations {
			info.Tolerations = append(info.Tolerations, models.TolerationInfo{
				Key:      t.Key,
				Operator: string(t.Operator),
				Value:    t.Value,
				Effect:   string(t.Effect),
			})
		}
	}

	// Extract affinity
	if spec.Affinity != nil && spec.Affinity.NodeAffinity != nil {
		info.Affinity = &models.AffinityInfo{
			NodeAffinity: c.extractNodeAffinity(spec.Affinity.NodeAffinity),
		}
	}

	// Extract resource requests/limits
	requests := c.calculatePodRequests(spec)
	info.Resources = models.PodResources{
		Requests: models.ResourceList{
			CPU:    requests.Cpu().String(),
			Memory: requests.Memory().String(),
		},
	}

	// Detect CPU pinning and QoS class
	info.CPUPinningEnabled, info.QoSClass = c.detectCPUPinning(spec)

	// Extract service account
	info.ServiceAccount = spec.ServiceAccountName
	if info.ServiceAccount == "" {
		info.ServiceAccount = "default"
	}

	klog.V(4).Infof("Extracted pod spec info: SA=%s, QoS=%s, CPUPinning=%v", info.ServiceAccount, info.QoSClass, info.CPUPinningEnabled)

	return info
}

// extractNodeAffinity converts NodeAffinity to model format
func (c *Calculator) extractNodeAffinity(affinity *corev1.NodeAffinity) *models.NodeAffinityInfo {
	info := &models.NodeAffinityInfo{}

	if affinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
		terms := affinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
		info.RequiredDuringSchedulingIgnoredDuringExecution = make([]models.NodeSelectorTerm, 0, len(terms))
		for _, term := range terms {
			modelTerm := models.NodeSelectorTerm{}
			if len(term.MatchExpressions) > 0 {
				modelTerm.MatchExpressions = make([]models.NodeSelectorRequirement, 0, len(term.MatchExpressions))
				for _, expr := range term.MatchExpressions {
					modelTerm.MatchExpressions = append(modelTerm.MatchExpressions, models.NodeSelectorRequirement{
						Key:      expr.Key,
						Operator: string(expr.Operator),
						Values:   expr.Values,
					})
				}
			}
			info.RequiredDuringSchedulingIgnoredDuringExecution = append(info.RequiredDuringSchedulingIgnoredDuringExecution, modelTerm)
		}
	}

	return info
}

// extractStorageInfo collects information about storage volumes attached to the pod
func (c *Calculator) extractStorageInfo(ctx context.Context, pod *corev1.Pod) ([]models.StorageInfo, error) {
	storageInfos := make([]models.StorageInfo, 0)

	// Iterate through pod volumes and find PVCs
	for _, volume := range pod.Spec.Volumes {
		if volume.PersistentVolumeClaim != nil {
			pvcName := volume.PersistentVolumeClaim.ClaimName

			// Get PVC details
			pvc, err := c.k8sClient.CoreV1().PersistentVolumeClaims(pod.Namespace).Get(ctx, pvcName, metav1.GetOptions{})
			if err != nil {
				klog.V(4).Infof("Failed to get PVC %s/%s: %v", pod.Namespace, pvcName, err)
				continue
			}

			// Extract size from PVC spec
			size := "unknown"
			if pvc.Spec.Resources.Requests != nil {
				if storage, ok := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
					size = storage.String()
				}
			}

			// Extract storage class
			storageClass := "default"
			if pvc.Spec.StorageClassName != nil && *pvc.Spec.StorageClassName != "" {
				storageClass = *pvc.Spec.StorageClassName
			}

			// Extract volume mode
			volumeMode := "Filesystem"
			if pvc.Spec.VolumeMode != nil {
				volumeMode = string(*pvc.Spec.VolumeMode)
			}

			storageInfos = append(storageInfos, models.StorageInfo{
				Name:         volume.Name,
				Size:         size,
				StorageClass: storageClass,
				VolumeMode:   volumeMode,
			})
		}
	}

	return storageInfos, nil
}

// isCPUManagerEnabled checks if CPU manager is enabled on a node
func (c *Calculator) isCPUManagerEnabled(node *corev1.Node) bool {
	// CPU manager is typically indicated by labels set by the node
	// Common labels: cpumanager=true, or feature.node.kubernetes.io/cpu-cpuid.AVX2=true
	// The most reliable indicator is checking for the presence of the cpu manager policy
	// which is reflected in node labels

	// Check for common CPU manager labels
	if val, exists := node.Labels["cpumanager"]; exists && val == "true" {
		return true
	}

	// Check for OpenShift PerformanceProfile label
	if val, exists := node.Labels["node-role.kubernetes.io/worker-rt"]; exists && val == "" {
		return true // Real-time worker nodes typically have CPU manager enabled
	}

	// Check for custom labels that operators might set
	if _, exists := node.Labels["feature.node.kubernetes.io/cpu-cpuid.AVX512VNNI"]; exists {
		// Nodes with advanced CPU features often have CPU manager
		return true
	}

	return false
}

// detectCPUPinning detects if a pod uses CPU pinning
// Returns (cpuPinningEnabled, qosClass)
func (c *Calculator) detectCPUPinning(spec *corev1.PodSpec) (bool, string) {
	// Determine QoS class
	qosClass := c.getQoSClass(spec)

	// CPU pinning requires:
	// 1. Guaranteed QoS (all containers have CPU/memory requests = limits)
	// 2. Integer CPU requests (whole cores)

	if qosClass != "Guaranteed" {
		return false, qosClass
	}

	// Check if all CPU requests are integers (whole cores)
	for _, container := range spec.Containers {
		if container.Resources.Requests != nil {
			if cpu, ok := container.Resources.Requests[corev1.ResourceCPU]; ok {
				// Check if CPU is a whole number (integer cores)
				cpuMillis := cpu.MilliValue()
				if cpuMillis%1000 != 0 {
					// Fractional CPU, no pinning
					return false, qosClass
				}
			}
		}
	}

	return true, qosClass
}

// getQoSClass determines the QoS class of a pod
func (c *Calculator) getQoSClass(spec *corev1.PodSpec) string {
	// Guaranteed: All containers have CPU and memory requests = limits
	// Burstable: At least one container has CPU or memory request
	// BestEffort: No containers have CPU or memory requests

	hasRequests := false
	allGuaranteed := true

	for _, container := range spec.Containers {
		if container.Resources.Requests == nil || container.Resources.Limits == nil {
			allGuaranteed = false
		} else {
			if len(container.Resources.Requests) > 0 {
				hasRequests = true
			}

			// Check if requests == limits
			cpuReq := container.Resources.Requests[corev1.ResourceCPU]
			cpuLim := container.Resources.Limits[corev1.ResourceCPU]
			memReq := container.Resources.Requests[corev1.ResourceMemory]
			memLim := container.Resources.Limits[corev1.ResourceMemory]

			if !cpuReq.Equal(cpuLim) || !memReq.Equal(memLim) {
				allGuaranteed = false
			}
		}
	}

	if allGuaranteed && hasRequests {
		return "Guaranteed"
	} else if hasRequests {
		return "Burstable"
	}

	return "BestEffort"
}
