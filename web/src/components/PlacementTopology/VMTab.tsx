import * as React from 'react';
import { PlacementTopology } from './PlacementTopology';

const VMPlacementTab: any = (props: any) => {
  // Console's loadComponent calls this as a loader (no args) during SPA navigation,
  // expecting a Promise. Return a Promise resolving to the component itself.
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
