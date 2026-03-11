package models

// PlacementResponse represents the API response for placement analysis
type PlacementResponse struct {
	CurrentNode    string              `json:"currentNode"`
	CurrentNodes   []string            `json:"currentNodes"`
	EligibleNodes  []NodeEligibility   `json:"eligibleNodes"`
	PodSpec        *PodSpecInfo        `json:"podSpec,omitempty"`
	VMInfo         *VMInfo             `json:"vmInfo,omitempty"`
	Storage        []StorageInfo       `json:"storage,omitempty"`
	Networks       []NetworkInfo       `json:"networks,omitempty"`
}

// NetworkInfo represents network availability information
type NetworkInfo struct {
	Name      string   `json:"name"`
	Available bool     `json:"available"`
	AllNodes  bool     `json:"allNodes"`
	Nodes     []string `json:"nodes,omitempty"`
	Type      string   `json:"type,omitempty"`
}

// NodeEligibility represents whether a node can run the workload
type NodeEligibility struct {
	Name              string            `json:"name"`
	CanRun            bool              `json:"canRun"`
	Reasons           []string          `json:"reasons"`
	Resources         NodeResources     `json:"resources"`
	Labels            map[string]string `json:"labels"`
	Taints            []TaintInfo       `json:"taints"`
	CPUManagerEnabled bool              `json:"cpuManagerEnabled"`
}

// NodeResources represents node resource capacity and availability
type NodeResources struct {
	CPU    ResourceInfo `json:"cpu"`
	Memory ResourceInfo `json:"memory"`
	Pods   ResourceInfo `json:"pods"`
}

// ResourceInfo represents a specific resource's capacity and availability
type ResourceInfo struct {
	Allocatable string `json:"allocatable"`
	Available   string `json:"available"`
	Requests    string `json:"requests,omitempty"`
}

// TaintInfo represents a node taint
type TaintInfo struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Effect string `json:"effect"`
}

// PodSpecInfo contains relevant pod specification details
type PodSpecInfo struct {
	NodeSelector      map[string]string `json:"nodeSelector,omitempty"`
	Tolerations       []TolerationInfo  `json:"tolerations,omitempty"`
	Affinity          *AffinityInfo     `json:"affinity,omitempty"`
	Resources         PodResources      `json:"resources,omitempty"`
	CPUPinningEnabled bool              `json:"cpuPinningEnabled"`
	QoSClass          string            `json:"qosClass"`
	ServiceAccount    string            `json:"serviceAccount"`
	SCC               string            `json:"scc"`
}

// TolerationInfo represents a pod toleration
type TolerationInfo struct {
	Key      string `json:"key"`
	Operator string `json:"operator"`
	Value    string `json:"value,omitempty"`
	Effect   string `json:"effect,omitempty"`
}

// AffinityInfo contains node affinity rules
type AffinityInfo struct {
	NodeAffinity *NodeAffinityInfo `json:"nodeAffinity,omitempty"`
}

// NodeAffinityInfo represents node affinity requirements
type NodeAffinityInfo struct {
	RequiredDuringSchedulingIgnoredDuringExecution  []NodeSelectorTerm `json:"required,omitempty"`
	PreferredDuringSchedulingIgnoredDuringExecution []PreferredTerm    `json:"preferred,omitempty"`
}

// NodeSelectorTerm represents a node selector term
type NodeSelectorTerm struct {
	MatchExpressions []NodeSelectorRequirement `json:"matchExpressions,omitempty"`
}

// NodeSelectorRequirement represents a node selector requirement
type NodeSelectorRequirement struct {
	Key      string   `json:"key"`
	Operator string   `json:"operator"`
	Values   []string `json:"values,omitempty"`
}

// PreferredTerm represents a preferred scheduling term
type PreferredTerm struct {
	Weight     int32            `json:"weight"`
	Preference NodeSelectorTerm `json:"preference"`
}

// PodResources represents pod resource requests and limits
type PodResources struct {
	Requests ResourceList `json:"requests,omitempty"`
	Limits   ResourceList `json:"limits,omitempty"`
}

// ResourceList represents a list of resource quantities
type ResourceList struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
}

// VMInfo contains VM-specific information
type VMInfo struct {
	Running      bool     `json:"running"`
	Networks     []string `json:"networks,omitempty"`
	InstanceSpec *PodSpecInfo `json:"instanceSpec,omitempty"`
}

// StorageInfo represents information about attached storage volumes
type StorageInfo struct {
	Name         string `json:"name"`
	Size         string `json:"size"`
	StorageClass string `json:"storageClass"`
	VolumeMode   string `json:"volumeMode,omitempty"`
}
