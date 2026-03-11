package placement

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	kubevirtclient "kubevirt.io/client-go/kubecli"

	"github.com/kborup-redhat/placement-discovery-plugin/pkg/models"
)

// VMCalculator extends Calculator with VM-specific placement logic
type VMCalculator struct {
	*Calculator
	kubevirtClient kubevirtclient.KubevirtClient
}

// NewVMCalculator creates a new VM placement calculator
func NewVMCalculator(calculator *Calculator, kubevirtClient kubevirtclient.KubevirtClient) *VMCalculator {
	return &VMCalculator{
		Calculator:     calculator,
		kubevirtClient: kubevirtClient,
	}
}

// CalculateVMPlacement determines which nodes a VM can be scheduled on
func (c *VMCalculator) CalculateVMPlacement(ctx context.Context, vm *kubevirtv1.VirtualMachine) (*models.PlacementResponse, error) {
	// Convert VM spec to Pod spec for placement calculation
	pod := c.vmToPod(vm)

	// Try to get the VMI to find current node
	var currentNode string
	var running bool
	opts := &metav1.GetOptions{}
	vmi, err := c.kubevirtClient.VirtualMachineInstance(vm.Namespace).Get(ctx, vm.Name, opts)
	if err == nil && vmi != nil {
		currentNode = vmi.Status.NodeName
		running = vmi.Status.Phase == kubevirtv1.Running
		pod.Spec.NodeName = currentNode
	}

	// Calculate placement using base calculator
	response, err := c.Calculator.CalculatePodPlacement(ctx, pod)
	if err != nil {
		return nil, err
	}

	// Add VM-specific information
	networks := make([]string, 0)
	for _, network := range vm.Spec.Template.Spec.Networks {
		if network.Multus != nil {
			networks = append(networks, network.Multus.NetworkName)
		}
	}

	response.VMInfo = &models.VMInfo{
		Running:      running,
		Networks:     networks,
		InstanceSpec: response.PodSpec,
	}

	return response, nil
}

// vmToPod converts a VirtualMachine spec to a Pod spec for placement calculation
func (c *VMCalculator) vmToPod(vm *kubevirtv1.VirtualMachine) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vm.Name + "-virt-launcher",
			Namespace: vm.Namespace,
			Labels:    vm.Spec.Template.ObjectMeta.Labels,
		},
		Spec: corev1.PodSpec{
			NodeSelector: vm.Spec.Template.Spec.NodeSelector,
			Affinity:     vm.Spec.Template.Spec.Affinity,
			Tolerations:  vm.Spec.Template.Spec.Tolerations,
			Containers:   []corev1.Container{},
		},
	}

	// Convert VM resource requirements to container resource requirements
	if vm.Spec.Template.Spec.Domain.Resources.Requests != nil || vm.Spec.Template.Spec.Domain.Resources.Limits != nil {
		container := corev1.Container{
			Name:  "compute",
			Image: "virt-launcher", // Placeholder
			Resources: corev1.ResourceRequirements{
				Requests: c.convertVMResources(vm.Spec.Template.Spec.Domain.Resources.Requests),
				Limits:   c.convertVMResources(vm.Spec.Template.Spec.Domain.Resources.Limits),
			},
		}
		pod.Spec.Containers = append(pod.Spec.Containers, container)
	} else {
		// If no explicit resources, add default container
		pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{
			Name:  "compute",
			Image: "virt-launcher",
		})
	}

	// Add resource overhead for virt-launcher sidecar
	// KubeVirt adds overhead for the launcher container
	c.addVirtLauncherOverhead(pod)

	return pod
}

// convertVMResources converts KubeVirt resource list to Kubernetes resource list
func (c *VMCalculator) convertVMResources(vmResources corev1.ResourceList) corev1.ResourceList {
	if vmResources == nil {
		return nil
	}

	k8sResources := corev1.ResourceList{}

	// Copy CPU and memory resources
	if cpu, exists := vmResources[corev1.ResourceCPU]; exists {
		k8sResources[corev1.ResourceCPU] = cpu
	}
	if memory, exists := vmResources[corev1.ResourceMemory]; exists {
		k8sResources[corev1.ResourceMemory] = memory
	}

	return k8sResources
}

// addVirtLauncherOverhead adds resource overhead for the virt-launcher container
// This is a simplified version - actual overhead calculation is more complex
func (c *VMCalculator) addVirtLauncherOverhead(pod *corev1.Pod) {
	// Add overhead container (simplified)
	// In reality, KubeVirt calculates this based on VM memory and other factors
	overheadCPU := resource.MustParse("100m")
	overheadMemory := resource.MustParse("200Mi")

	overheadContainer := corev1.Container{
		Name:  "virt-launcher-overhead",
		Image: "virt-launcher",
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    overheadCPU,
				corev1.ResourceMemory: overheadMemory,
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    overheadCPU,
				corev1.ResourceMemory: overheadMemory,
			},
		},
	}

	pod.Spec.Containers = append(pod.Spec.Containers, overheadContainer)
}

// extractVMSpecInfo extracts VM-specific spec information
func (c *VMCalculator) extractVMSpecInfo(vm *kubevirtv1.VirtualMachine) *models.PodSpecInfo {
	info := &models.PodSpecInfo{
		NodeSelector: vm.Spec.Template.Spec.NodeSelector,
	}

	// Extract tolerations
	if len(vm.Spec.Template.Spec.Tolerations) > 0 {
		info.Tolerations = make([]models.TolerationInfo, 0, len(vm.Spec.Template.Spec.Tolerations))
		for _, t := range vm.Spec.Template.Spec.Tolerations {
			info.Tolerations = append(info.Tolerations, models.TolerationInfo{
				Key:      t.Key,
				Operator: string(t.Operator),
				Value:    t.Value,
				Effect:   string(t.Effect),
			})
		}
	}

	// Extract affinity
	if vm.Spec.Template.Spec.Affinity != nil && vm.Spec.Template.Spec.Affinity.NodeAffinity != nil {
		info.Affinity = &models.AffinityInfo{
			NodeAffinity: c.Calculator.extractNodeAffinity(vm.Spec.Template.Spec.Affinity.NodeAffinity),
		}
	}

	// Extract resource requests/limits
	resources := vm.Spec.Template.Spec.Domain.Resources
	if resources.Requests != nil || resources.Limits != nil {
		info.Resources = models.PodResources{}
		if resources.Requests != nil {
			info.Resources.Requests = models.ResourceList{}
			if cpu, exists := resources.Requests[corev1.ResourceCPU]; exists {
				info.Resources.Requests.CPU = cpu.String()
			}
			if memory, exists := resources.Requests[corev1.ResourceMemory]; exists {
				info.Resources.Requests.Memory = memory.String()
			}
		}
		if resources.Limits != nil {
			info.Resources.Limits = models.ResourceList{}
			if cpu, exists := resources.Limits[corev1.ResourceCPU]; exists {
				info.Resources.Limits.CPU = cpu.String()
			}
			if memory, exists := resources.Limits[corev1.ResourceMemory]; exists {
				info.Resources.Limits.Memory = memory.String()
			}
		}
	}

	return info
}
