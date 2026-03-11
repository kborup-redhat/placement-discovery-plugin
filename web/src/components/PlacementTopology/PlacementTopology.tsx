import * as React from 'react';
import {
  Bullseye,
  Spinner,
  Button,
  Title,
  Label,
  Card,
  CardBody,
  CardTitle,
  Stack,
  StackItem,
  Split,
  SplitItem,
  Text,
  TextVariants,
  EmptyState,
  EmptyStateIcon,
  EmptyStateBody,
  Flex,
  FlexItem,
} from '@patternfly/react-core';
import { Table, TableHeader, TableBody, TableVariant } from '@patternfly/react-table';
import {
  CheckCircleIcon,
  ExclamationCircleIcon,
  InfoCircleIcon,
} from '@patternfly/react-icons';
import { usePlacementData, NodeEligibility } from '../../hooks/usePlacementData';
import { formatCPU, formatMemory, formatStorage } from '../../utils/formatters';

interface PlacementTopologyProps {
  resourceType: string;
  namespace: string;
  name: string;
}

export const PlacementTopology: React.FC<PlacementTopologyProps> = ({
  resourceType,
  namespace,
  name,
}) => {
  const { data, loading, error, refetch } = usePlacementData(
    resourceType,
    namespace,
    name,
  );

  if (loading) {
    return (
      <Bullseye>
        <EmptyState>
          <EmptyStateIcon variant="container" component={Spinner} />
          <Title size="lg" headingLevel="h3">
            Loading placement data
          </Title>
        </EmptyState>
      </Bullseye>
    );
  }

  if (error) {
    return (
      <EmptyState>
        <EmptyStateIcon icon={ExclamationCircleIcon} color="var(--pf-global--danger-color--100)" />
        <Title size="lg" headingLevel="h3">
          Error loading placement data
        </Title>
        <EmptyStateBody>{error.message}</EmptyStateBody>
        <Button variant="primary" onClick={refetch}>
          Retry
        </Button>
      </EmptyState>
    );
  }

  if (!data) {
    return (
      <EmptyState>
        <EmptyStateIcon icon={InfoCircleIcon} />
        <Title size="lg" headingLevel="h3">
          No placement data available
        </Title>
        <EmptyStateBody>
          Unable to load placement information for this resource.
        </EmptyStateBody>
      </EmptyState>
    );
  }

  const eligibleCount = data.eligibleNodes.filter((n) => n.canRun).length;
  const ineligibleCount = data.eligibleNodes.length - eligibleCount;

  // Determine which nodes are currently running pods
  const currentNodesSet = new Set(data.currentNodes || (data.currentNode ? [data.currentNode] : []));

  // Helper function to shorten node names (remove domain suffix)
  const shortenNodeName = (nodeName: string): string => {
    // Remove common AWS/cloud suffixes
    return nodeName.replace(/\.(eu-west-1|us-east-1|us-west-2|compute)\.internal$/i, '')
                   .replace(/\.compute\.amazonaws\.com$/i, '');
  };

  // Node placement table
  const nodeColumns = [
    { title: 'Node Name', props: { style: { width: '15%', minWidth: '200px' } } },
    { title: 'Status', props: { style: { width: '10%', minWidth: '120px' } } },
    { title: 'Taints', props: { style: { width: '20%', minWidth: '250px' } } },
    { title: 'CPU Manager', props: { style: { width: '10%', minWidth: '120px' } } },
    { title: 'CPU Available (cores)', props: { style: { width: '8%', minWidth: '100px' } } },
    { title: 'CPU Allocatable (cores)', props: { style: { width: '8%', minWidth: '100px' } } },
    { title: 'Memory Available (GB)', props: { style: { width: '8%', minWidth: '100px' } } },
    { title: 'Memory Allocatable (GB)', props: { style: { width: '8%', minWidth: '100px' } } },
    { title: 'Ineligibility Reasons', props: { style: { width: '13%', minWidth: '150px' } } },
  ];

  const nodeRows = data.eligibleNodes.map((node: NodeEligibility) => {
    const isCurrentNode = currentNodesSet.has(node.name);

    return {
      cells: [
        {
          title: (
            <div style={{ padding: '8px' }}>
              <Split hasGutter>
                <SplitItem>
                  <Text component={TextVariants.small} style={{ fontWeight: isCurrentNode ? 'bold' : 'normal' }}>
                    {shortenNodeName(node.name)}
                  </Text>
                </SplitItem>
                {isCurrentNode && (
                  <SplitItem>
                    <Label color="green" icon={<CheckCircleIcon />}>
                      Current
                    </Label>
                  </SplitItem>
                )}
              </Split>
            </div>
          ),
        },
        {
          title: (
            <div style={{ padding: '8px' }}>
              {node.canRun ? (
                <span style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                  <CheckCircleIcon color="green" />
                  <Text component={TextVariants.small}>Eligible</Text>
                </span>
              ) : (
                <span style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                  <ExclamationCircleIcon color="red" />
                  <Text component={TextVariants.small}>Ineligible</Text>
                </span>
              )}
            </div>
          ),
        },
        {
          title: (
            <div style={{ padding: '8px' }}>
              {node.taints && node.taints.length > 0 ? (
                <Stack hasGutter style={{ minWidth: '200px' }}>
                  {node.taints.map((taint, idx) => {
                    const taintStr = `${taint.key}${taint.value ? '=' + taint.value : ''}`;
                    const isTolerated = data.podSpec?.tolerations?.some(
                      (t) =>
                        (t.key === taint.key || (t.key === '' && t.operator === 'Exists')) &&
                        (t.effect === '' || t.effect === taint.effect) &&
                        (t.operator === 'Exists' || t.value === taint.value)
                    );
                    return (
                      <StackItem key={idx}>
                        <Split hasGutter>
                          <SplitItem>
                            <Label color={isTolerated ? 'green' : 'red'} isCompact>
                              {taintStr}
                            </Label>
                          </SplitItem>
                          <SplitItem>
                            <Label color="grey" isCompact>
                              {taint.effect}
                            </Label>
                          </SplitItem>
                          {!isTolerated && (
                            <SplitItem>
                              <Label color="orange" icon={<ExclamationCircleIcon color="#f0ab00" />} isCompact>
                                Not tolerated
                              </Label>
                            </SplitItem>
                          )}
                        </Split>
                      </StackItem>
                    );
                  })}
                </Stack>
              ) : (
                <Text component={TextVariants.small}>—</Text>
              )}
            </div>
          ),
        },
        {
          title: (
            <div style={{ padding: '8px' }}>
              {node.cpuManagerEnabled ? (
                <span style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                  <CheckCircleIcon color="green" />
                  <Text component={TextVariants.small}>Enabled</Text>
                </span>
              ) : (
                <Text component={TextVariants.small}>—</Text>
              )}
            </div>
          ),
        },
        {
          title: <div style={{ padding: '8px' }}><Text component={TextVariants.small}>{formatCPU(node.resources.cpu.available)}</Text></div>,
        },
        {
          title: <div style={{ padding: '8px' }}><Text component={TextVariants.small}>{formatCPU(node.resources.cpu.allocatable)}</Text></div>,
        },
        {
          title: <div style={{ padding: '8px' }}><Text component={TextVariants.small}>{formatMemory(node.resources.memory.available)}</Text></div>,
        },
        {
          title: <div style={{ padding: '8px' }}><Text component={TextVariants.small}>{formatMemory(node.resources.memory.allocatable)}</Text></div>,
        },
        {
          title: (
            <div style={{ padding: '8px' }}>
              <Text component={TextVariants.small}>
                {node.reasons.length > 0 ? node.reasons.join('; ') : '—'}
              </Text>
            </div>
          ),
        },
      ],
    };
  });

  // Scheduling constraints table
  const constraintRows: Array<{ cells: Array<{ title: React.ReactNode }> }> = [];

  // Add Service Account information
  if (data.podSpec?.serviceAccount) {
    constraintRows.push({
      cells: [
        { title: <div style={{ padding: '8px' }}><Text component={TextVariants.small}>Service Account</Text></div> },
        {
          title: (
            <div style={{ padding: '8px' }}>
              <Label color="blue">
                {data.podSpec.serviceAccount}
              </Label>
            </div>
          ),
        },
      ],
    });
  }

  // Add Network information (for VMs)
  if (data.networks && data.networks.length > 0) {
    data.networks.forEach((network) => {
      constraintRows.push({
        cells: [
          {
            title: (
              <div style={{ padding: '8px' }}>
                <Text component={TextVariants.small}>
                  Network: {network.name}
                  {network.type && network.type !== 'pod' && (
                    <span style={{ color: '#8a8d90', marginLeft: '4px' }}>({network.type})</span>
                  )}
                </Text>
              </div>
            ),
          },
          {
            title: (
              <div style={{ padding: '8px' }}>
                <Stack hasGutter>
                  <StackItem>
                    <Split hasGutter>
                      <SplitItem>
                        <Label
                          color={network.available ? 'green' : 'red'}
                          icon={network.available ? <CheckCircleIcon /> : <ExclamationCircleIcon />}
                          isCompact
                        >
                          {network.available ? 'Available' : 'Unavailable'}
                        </Label>
                      </SplitItem>
                      {network.available && (
                        <SplitItem>
                          <Label color={network.allNodes ? 'blue' : 'orange'} isCompact>
                            {network.allNodes ? 'All Nodes' : `${network.nodes?.length || 0} of ${data.eligibleNodes.length} Nodes`}
                          </Label>
                        </SplitItem>
                      )}
                    </Split>
                  </StackItem>
                  {network.available && network.nodes && network.nodes.length > 0 && !network.allNodes && (
                    <StackItem>
                      <Flex spaceItems={{ default: 'spaceItemsXs' }} style={{ flexWrap: 'wrap' }}>
                        {network.nodes.map((nodeName) => (
                          <FlexItem key={nodeName}>
                            <Label color="grey" isCompact>
                              {shortenNodeName(nodeName)}
                            </Label>
                          </FlexItem>
                        ))}
                      </Flex>
                    </StackItem>
                  )}
                </Stack>
              </div>
            ),
          },
        ],
      });
    });
  }

  // Add SCC information
  if (data.podSpec?.scc) {
    constraintRows.push({
      cells: [
        { title: <div style={{ padding: '8px' }}><Text component={TextVariants.small}>SCC</Text></div> },
        {
          title: (
            <div style={{ padding: '8px' }}>
              <Label color="cyan">
                {data.podSpec.scc}
              </Label>
            </div>
          ),
        },
      ],
    });
  }

  // Add QoS Class and CPU Pinning information
  if (data.podSpec?.qosClass) {
    constraintRows.push({
      cells: [
        { title: <div style={{ padding: '8px' }}><Text component={TextVariants.small}>QoS Class</Text></div> },
        {
          title: (
            <div style={{ padding: '8px' }}>
              <Label color={data.podSpec.qosClass === 'Guaranteed' ? 'green' : data.podSpec.qosClass === 'Burstable' ? 'orange' : 'grey'} isCompact>
                {data.podSpec.qosClass}
              </Label>
            </div>
          ),
        },
      ],
    });
  }

  if (data.podSpec?.cpuPinningEnabled) {
    constraintRows.push({
      cells: [
        { title: <div style={{ padding: '8px' }}><Text component={TextVariants.small}>CPU Pinning</Text></div> },
        {
          title: (
            <div style={{ padding: '8px' }}>
              <Label color="purple" icon={<CheckCircleIcon />} isCompact>
                Enabled
              </Label>
            </div>
          ),
        },
      ],
    });
  }

  if (data.podSpec?.nodeSelector && Object.keys(data.podSpec.nodeSelector).length > 0) {
    Object.entries(data.podSpec.nodeSelector).forEach(([key, value]) => {
      constraintRows.push({
        cells: [
          { title: <div style={{ padding: '8px' }}><Text component={TextVariants.small}>Node Selector</Text></div> },
          {
            title: (
              <div style={{ padding: '8px' }}>
                <Label color="blue" isCompact>
                  {key}
                  {value ? `=${value}` : ''}
                </Label>
              </div>
            ),
          },
        ],
      });
    });
  }

  if (data.podSpec?.resources?.requests) {
    if (data.podSpec.resources.requests.cpu) {
      constraintRows.push({
        cells: [
          { title: <div style={{ padding: '8px' }}><Text component={TextVariants.small}>CPU Request</Text></div> },
          { title: <div style={{ padding: '8px' }}><Text component={TextVariants.small}>{formatCPU(data.podSpec.resources.requests.cpu)}</Text></div> },
        ],
      });
    }
    if (data.podSpec.resources.requests.memory) {
      constraintRows.push({
        cells: [
          { title: <div style={{ padding: '8px' }}><Text component={TextVariants.small}>Memory Request</Text></div> },
          {
            title: (
              <div style={{ padding: '8px' }}>
                <Text component={TextVariants.small}>{formatMemory(data.podSpec.resources.requests.memory)}</Text>
              </div>
            ),
          },
        ],
      });
    }
  }

  if (data.podSpec?.resources?.limits) {
    if (data.podSpec.resources.limits.cpu) {
      constraintRows.push({
        cells: [
          { title: <div style={{ padding: '8px' }}><Text component={TextVariants.small}>CPU Limit</Text></div> },
          { title: <div style={{ padding: '8px' }}><Text component={TextVariants.small}>{formatCPU(data.podSpec.resources.limits.cpu)}</Text></div> },
        ],
      });
    }
    if (data.podSpec.resources.limits.memory) {
      constraintRows.push({
        cells: [
          { title: <div style={{ padding: '8px' }}><Text component={TextVariants.small}>Memory Limit</Text></div> },
          { title: <div style={{ padding: '8px' }}><Text component={TextVariants.small}>{formatMemory(data.podSpec.resources.limits.memory)}</Text></div> },
        ],
      });
    }
  }

  if (data.podSpec?.tolerations && data.podSpec.tolerations.length > 0) {
    data.podSpec.tolerations.forEach((toleration) => {
      const tolerationStr = `${toleration.key || '*'}${
        toleration.operator === 'Exists'
          ? ''
          : toleration.value
            ? '=' + toleration.value
            : ''
      }:${toleration.effect || '*'}`;
      constraintRows.push({
        cells: [
          { title: <div style={{ padding: '8px' }}><Text component={TextVariants.small}>Toleration</Text></div> },
          {
            title: (
              <div style={{ padding: '8px' }}>
                <Label color="purple" isCompact>
                  {tolerationStr} ({toleration.operator || 'Equal'})
                </Label>
              </div>
            ),
          },
        ],
      });
    });
  }

  return (
    <Stack hasGutter style={{ gap: '24px' }}>
      {/* Summary and Scheduling Constraints - Side by Side */}
      <StackItem>
        <div style={{ display: 'flex', gap: '16px', width: '100%', alignItems: 'stretch' }}>
          {/* Summary Section */}
          <div style={{ flex: 1, minWidth: 0, display: 'flex' }}>
            <Card style={{ height: '100%', width: '100%' }}>
              <CardTitle>
                <Title headingLevel="h3" size="lg" style={{ fontWeight: 'bold' }}>
                  Placement Summary
                </Title>
              </CardTitle>
              <CardBody style={{ padding: '20px' }}>
                <div style={{
                  border: '1px solid #4f5255',
                  borderRadius: '4px',
                  padding: '16px',
                  backgroundColor: '#3c3f42',
                  color: '#ffffff',
                  display: 'inline-block',
                  maxWidth: '100%'
                }}>
                  <Flex spaceItems={{ default: 'spaceItemsLg' }}>
                    <FlexItem>
                      <Text component={TextVariants.p} style={{ color: '#ffffff' }}>
                        {data.currentNodes && data.currentNodes.length > 1 ? 'Current Nodes: ' : 'Current Node: '}
                        {data.currentNodes && data.currentNodes.length > 0 ? (
                          data.currentNodes.join(', ')
                        ) : data.currentNode ? (
                          data.currentNode
                        ) : (
                          <em>Not scheduled</em>
                        )}
                      </Text>
                    </FlexItem>
                    <FlexItem>
                      <Text component={TextVariants.p} style={{ color: '#ffffff' }}>
                        Cluster Nodes: {data.eligibleNodes.length} total
                      </Text>
                    </FlexItem>
                    <FlexItem>
                      <Text component={TextVariants.p} style={{ display: 'flex', alignItems: 'center', gap: '8px', color: '#ffffff' }}>
                        Eligible:
                        <CheckCircleIcon color="green" />
                        {eligibleCount}
                      </Text>
                    </FlexItem>
                    <FlexItem>
                      <Text component={TextVariants.p} style={{ display: 'flex', alignItems: 'center', gap: '8px', color: '#ffffff' }}>
                        Ineligible:
                        <ExclamationCircleIcon color="red" />
                        {ineligibleCount}
                      </Text>
                    </FlexItem>
                  </Flex>
                </div>
              </CardBody>
            </Card>
          </div>

          {/* Scheduling Constraints Section */}
          {constraintRows.length > 0 && (
            <div style={{ flex: 1, minWidth: 0, display: 'flex' }}>
              <Card style={{ height: '100%', width: '100%' }}>
                <CardTitle>
                  <Title headingLevel="h3" size="lg" style={{ fontWeight: 'bold' }}>
                    Scheduling Constraints
                  </Title>
                </CardTitle>
                <CardBody style={{ padding: '20px' }}>
                  <div style={{
                    border: '1px solid #4f5255',
                    borderRadius: '4px',
                    padding: '16px',
                    backgroundColor: '#3c3f42',
                    color: '#ffffff',
                    display: 'inline-block',
                    maxWidth: '100%'
                  }}>
                    <Table
                      aria-label="Scheduling constraints"
                      variant={TableVariant.compact}
                      cells={[{ title: 'Type' }, { title: 'Value' }]}
                      rows={constraintRows}
                      borders={false}
                    >
                      <TableHeader />
                      <TableBody />
                    </Table>
                  </div>
                </CardBody>
              </Card>
            </div>
          )}
        </div>
      </StackItem>

      {/* Storage Section */}
      {data.storage && data.storage.length > 0 && (
        <StackItem style={{ marginTop: '24px' }}>
          <Card>
            <CardTitle>
              <Title headingLevel="h3" size="lg" style={{ fontWeight: 'bold' }}>
                Attached Storage
              </Title>
            </CardTitle>
            <CardBody style={{ padding: '20px' }}>
              <div style={{
                border: '1px solid #4f5255',
                borderRadius: '4px',
                padding: '16px',
                backgroundColor: '#3c3f42',
                color: '#ffffff',
                width: '100%',
                boxSizing: 'border-box'
              }}>
                <Table
                  aria-label="Storage volumes"
                  variant={TableVariant.compact}
                  cells={[
                    { title: 'Volume Name' },
                    { title: 'Size' },
                    { title: 'Storage Class' },
                    { title: 'Volume Mode' },
                  ]}
                  rows={data.storage.map((vol) => ({
                    cells: [
                      { title: <Text component={TextVariants.small}>{vol.name}</Text> },
                      { title: <Text component={TextVariants.small}>{formatStorage(vol.size)}</Text> },
                      {
                        title: (
                          <Label color="cyan" isCompact>
                            {vol.storageClass || 'default'}
                          </Label>
                        ),
                      },
                      { title: <Text component={TextVariants.small}>{vol.volumeMode || 'Filesystem'}</Text> },
                    ],
                  }))}
                  borders={false}
                >
                  <TableHeader />
                  <TableBody />
                </Table>
              </div>
            </CardBody>
          </Card>
        </StackItem>
      )}

      {/* Node Eligibility Details Section */}
      <StackItem style={{ marginTop: '24px' }}>
        <Card>
          <CardTitle>
            <Title headingLevel="h3" size="lg" style={{ fontWeight: 'bold' }}>
              Node Eligibility Details
            </Title>
          </CardTitle>
          <CardBody style={{ padding: '20px' }}>
            <div style={{
              border: '1px solid #4f5255',
              borderRadius: '4px',
              padding: '16px',
              backgroundColor: '#3c3f42',
              color: '#ffffff',
              width: '100%',
              boxSizing: 'border-box'
            }}>
              <Table
                aria-label="Node placement details"
                variant={TableVariant.compact}
                cells={nodeColumns}
                rows={nodeRows}
                borders={false}
              >
                <TableHeader />
                <TableBody />
              </Table>
            </div>
          </CardBody>
        </Card>
      </StackItem>
    </Stack>
  );
};
