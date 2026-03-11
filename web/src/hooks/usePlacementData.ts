import { useState, useEffect } from 'react';

export interface NodeEligibility {
  name: string;
  canRun: boolean;
  reasons: string[];
  resources: {
    cpu: { allocatable: string; available: string };
    memory: { allocatable: string; available: string };
    pods: { allocatable: string; available: string };
  };
  labels: Record<string, string>;
  taints: Array<{
    key: string;
    value: string;
    effect: string;
  }>;
  cpuManagerEnabled: boolean;
}

export interface PlacementData {
  currentNode: string;
  currentNodes?: string[];
  eligibleNodes: NodeEligibility[];
  podSpec?: {
    nodeSelector?: Record<string, string>;
    tolerations?: Array<{
      key: string;
      operator: string;
      value?: string;
      effect?: string;
    }>;
    affinity?: {
      nodeAffinity?: {
        required?: Array<{
          matchExpressions?: Array<{
            key: string;
            operator: string;
            values?: string[];
          }>;
        }>;
        preferred?: Array<{
          weight: number;
          preference: {
            matchExpressions?: Array<{
              key: string;
              operator: string;
              values?: string[];
            }>;
          };
        }>;
      };
    };
    resources?: {
      requests?: {
        cpu?: string;
        memory?: string;
      };
      limits?: {
        cpu?: string;
        memory?: string;
      };
    };
    cpuPinningEnabled?: boolean;
    qosClass?: string;
    serviceAccount?: string;
    scc?: string;
  };
  vmInfo?: {
    running: boolean;
    networks?: string[];
    instanceSpec?: any;
  };
  storage?: Array<{
    name: string;
    size: string;
    storageClass: string;
    volumeMode?: string;
  }>;
  networks?: Array<{
    name: string;
    available: boolean;
    allNodes: boolean;
    nodes?: string[];
    type?: string;
  }>;
}

interface UsePlacementDataResult {
  data: PlacementData | null;
  loading: boolean;
  error: Error | null;
  refetch: () => void;
}

export function usePlacementData(
  resourceType: string,
  namespace: string,
  name: string,
): UsePlacementDataResult {
  const [data, setData] = useState<PlacementData | null>(null);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<Error | null>(null);
  const [refetchTrigger, setRefetchTrigger] = useState(0);

  useEffect(() => {
    let cancelled = false;

    // Reset state immediately when dependencies change to prevent stale data rendering
    setData(null);
    setLoading(true);
    setError(null);

    const fetchData = async () => {
      if (!resourceType || !namespace || !name) {
        setLoading(false);
        return;
      }

      try {
        const response = await fetch(
          `/api/plugins/placement-discovery-plugin/api/placement/${encodeURIComponent(resourceType)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
        );

        if (!response.ok) {
          throw new Error(
            `Failed to fetch placement data: ${response.statusText}`,
          );
        }

        const result: PlacementData = await response.json();

        if (!cancelled) {
          setData(result);
          setLoading(false);
        }
      } catch (err) {
        if (!cancelled) {
          setError(
            err instanceof Error ? err : new Error('Unknown error occurred'),
          );
          setLoading(false);
        }
      }
    };

    fetchData();

    return () => {
      cancelled = true;
    };
  }, [resourceType, namespace, name, refetchTrigger]);

  const refetch = () => {
    setRefetchTrigger((prev) => prev + 1);
  };

  return { data, loading, error, refetch };
}
