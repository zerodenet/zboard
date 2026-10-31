package observabilitystore

// A deployment attempt is historical evidence. It is actionable only while its
// endpoint and owning node still exist in the current management inventory.
// Do not rewrite failed attempts when resources are deleted or archived.
const currentDeploymentEndpointSQL = `EXISTS (
 SELECT 1 FROM protocol_endpoints current_endpoint JOIN nodes current_node ON current_node.id = current_endpoint.node_id
 WHERE current_endpoint.id = protocol_deployments.protocol_endpoint_id
 AND current_endpoint.deleted_at IS NULL AND current_node.archived_at IS NULL AND current_node.lifecycle_status <> 'archived'
)`

const currentNodeSQL = `(archived_at IS NULL AND lifecycle_status <> 'archived')`
