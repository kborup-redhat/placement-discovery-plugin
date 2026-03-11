import * as React from 'react';
import { PlacementTopology } from './PlacementTopology';

interface TabProps {
  obj?: {
    metadata?: {
      namespace?: string;
      name?: string;
    };
  };
}

// Console's loadComponent calls this as a loader (no args) during SPA navigation,
// expecting a Promise. The `any` return type accommodates both the Promise (loader)
// and ReactElement (normal render) return paths.
const VMPlacementTab = (props: TabProps | undefined | null): React.ReactElement | null | Promise<unknown> => {
  if (props === undefined || props === null) {
    return Promise.resolve(VMPlacementTab);
  }
  if (!props.obj) return null;
  const namespace = (props.obj.metadata && props.obj.metadata.namespace) || '';
  const name = (props.obj.metadata && props.obj.metadata.name) || '';
  if (!namespace || !name) return null;
  return <PlacementTopology resourceType="vm" namespace={namespace} name={name} />;
};

export default VMPlacementTab;
